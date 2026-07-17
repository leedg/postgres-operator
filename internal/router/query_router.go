/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package router — query_router.go 는 *쿼리 라우팅 결정 엔진*("routing brain")이다.
// 한 쿼리에 대해 토폴로지(vindex) + 라우팅키 추출 + reference-table + 읽기/쓰기 분류 +
// 백엔드 해소를 합성해 단일 RouteDecision 을 낸다.
//
// 이것은 (E) 메시지 인지 프록시의 *핵심*이다 — full PostgreSQL 프로토콜 종단(자체 인증 +
// 백엔드 연결 풀 + 결과 재조립, vtgate급)은 별 대작업이지만, 프록시가 쿼리를 읽을 수
// 있게 되면 본 엔진을 호출해 어디로 보낼지 결정한다. 본 파일은 *순수·동기*(network 없음)
// 라 완전히 단위 검증 가능하다 — 종단 인프라와 독립.
package router

import (
	"errors"
	"fmt"
)

// ErrNoRoutingKey 는 단일 shard 키를 못 뽑은 경우(scatter-gather 필요)이다.
var ErrNoRoutingKey = errors.New("router: no single-shard routing key (scatter-gather required)")

// ErrWriteBlocked 는 resharding cutover 중 쓰기가 일시 차단된 경우이다(읽기는 허용).
var ErrWriteBlocked = errors.New("router: writes blocked (resharding cutover in progress)")

// ErrCrossShardInsert 는 다중행 INSERT 의 튜플들이 서로 다른 shard 로 라우팅되는 경우이다
// (#B-30). 라우터가 다중행 INSERT 를 첫 튜플 키로만 라우팅해 나머지 행을 오배치하던 조용한
// 데이터 손상 대신, 명시적 에러로 거부한다(호출자가 클라이언트에 알림). 클라이언트는 행을
// 나눠 보내거나 단일-샤드 배치로 재구성해야 한다.
var ErrCrossShardInsert = errors.New("router: multi-row INSERT spans multiple shards; split rows per shard")

// RouteDecision 은 한 쿼리의 라우팅 결정이다.
type RouteDecision struct {
	// Shard 는 대상 shard 이름. Scatter=true 면 비어 있다.
	Shard string
	// Backend 는 해소된 backend "host:port". Scatter=true 면 비어 있다.
	Backend string
	// Read 는 읽기 전용 쿼리 여부(가능하면 replica 로 라우팅됨).
	Read bool
	// Scatter 는 단일 shard 로 좁혀지지 않아 fan-out 이 필요함을 뜻한다(키 부재).
	Scatter bool
}

// QueryRouter 는 토폴로지 + extractor + 백엔드 resolver 를 쿼리 라우팅 결정으로 합성한다.
type QueryRouter struct {
	// Topology 는 key→shard(vindex) + reference table 정보.
	Topology Topology
	// Extractor 는 쿼리에서 샤딩 키를 뽑는다(regex/parser/auto).
	Extractor RouteKeyExtractor
	// Write 는 shard→primary(쓰기) 백엔드 resolver.
	Write BackendResolver
	// Read 는 shard→replica(읽기) 백엔드 resolver. nil 이면 Write 를 사용.
	Read BackendResolver
}

// Route 는 한 쿼리의 라우팅 결정을 낸다:
//
//  1. reference-only 읽기 쿼리(복제 테이블만 참조) → 키 없이 AnyShard.
//  2. 그 외 → 샤딩 키 추출 → 단일 shard. 키가 없으면 Scatter=true + ErrNoRoutingKey
//     (호출자가 scatter-gather 또는 거부 선택).
//
// 읽기 전용 쿼리(IsReadOnlyQuery)는 Read resolver(있으면)로 replica 에 분산한다.
func (qr QueryRouter) Route(query string) (RouteDecision, error) {
	read := IsReadOnlyQuery(query)
	// resharding cutover: 쓰기 일시 차단(읽기는 통과). 라우팅 전환 중 쓰기 유실 방지.
	if !read && qr.Topology.Spec.WriteBlocked {
		return RouteDecision{}, ErrWriteBlocked
	}
	pick := qr.Write
	if read && qr.Read != nil {
		pick = qr.Read
	}
	if pick == nil {
		return RouteDecision{}, fmt.Errorf("router: QueryRouter has no backend resolver")
	}

	// 1) reference-only 읽기 → 임의 shard. reference table 쓰기는 복제 불변식을 깨지
	// 않도록 여기서 단일 shard 로 보내지 않는다.
	if read && qr.Topology.ReferenceOnly(query) {
		return qr.decide(qr.Topology.AnyShard())(pick, read)
	}

	// 2) 샤딩 키 → 단일 shard.
	if qr.Extractor == nil {
		return RouteDecision{}, fmt.Errorf("router: QueryRouter has no route key extractor")
	}
	col := qr.Topology.Spec.Vindex.Column

	// #B-30: 다중행 INSERT 는 모든 튜플 키가 같은 shard 로 수렴할 때만 안전하다. 여러 shard 로
	// 갈리면 첫 키로만 라우팅해 나머지 행을 오배치하던 조용한 손상 대신 ErrCrossShardInsert 로
	// 거부한다. 단일 shard 수렴 시 그 shard 로 라우팅(정상). (단일행 INSERT 는 keys 1개라
	// 자연히 통과 — 아래 일반 추출 경로와 동치이나, 다중 튜플을 명시 검사하는 게 핵심.)
	if keys, ok := matchInsertColumnAll(query, col); ok && len(keys) > 1 {
		first, sErr := qr.Topology.Shard(keys[0])
		if sErr != nil {
			return RouteDecision{}, sErr
		}
		for _, k := range keys[1:] {
			sh, err := qr.Topology.Shard(k)
			if err != nil {
				return RouteDecision{}, err
			}
			if sh != first {
				return RouteDecision{}, ErrCrossShardInsert
			}
		}
		return qr.decide(first, nil)(pick, read)
	}

	key, ok := qr.Extractor.ExtractRoutingKey(query, col)
	if !ok {
		return RouteDecision{Read: read, Scatter: true}, ErrNoRoutingKey
	}
	return qr.decide(qr.Topology.Shard(key))(pick, read)
}

// decide 는 (shard, err) 로부터 backend 를 해소해 RouteDecision 을 만드는 클로저를
// 반환한다 (reference / key 경로의 공통 꼬리).
func (qr QueryRouter) decide(shard string, shardErr error) func(BackendResolver, bool) (RouteDecision, error) {
	return func(pick BackendResolver, read bool) (RouteDecision, error) {
		if shardErr != nil {
			return RouteDecision{}, shardErr
		}
		backend, err := pick(shard)
		if err != nil {
			return RouteDecision{}, err
		}
		return RouteDecision{Shard: shard, Backend: backend, Read: read}, nil
	}
}
