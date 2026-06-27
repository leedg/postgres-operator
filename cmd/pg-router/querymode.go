/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// querymode.go 는 *쿼리 인지 라우팅*(E)을 결선한다 — 연결 단위(startup param) 라우팅과
// 달리, 클라이언트의 *첫 쿼리*에서 샤딩 키를 뽑아 QueryRouter 로 샤드를 정한다.
//
// 동작: startup 수신 → trust 핸드셰이크(라우터가 클라이언트를 인증된 것으로 수용) →
// 첫 'Q'(simple Query) 수신 → QueryRouter.Route(sql) → 해당 샤드 backend 에 연결 →
// startup + 첫 Query 전달 후 양방향 proxy.
//
// *제약(PoC, trust 백엔드 한정)*: 라우터가 클라이언트 비밀번호를 검증하지 않고(trust),
// 백엔드에는 클라이언트의 startup 을 그대로 전달한다 — 백엔드가 trust auth 일 때만
// 동작한다. 일반(비밀번호) 백엔드 인증을 라우터가 대행하는 완전한 종단은 별 트랙(라이브
// PG 검증 필요). 또 simple-query 첫 문장만 라우팅하며 extended protocol(Parse/Bind)·
// 멀티샤드 scatter 는 후속.
package main

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/keiailab/postgres-operator/internal/router"
)

// routeDecisionFunc 는 SQL → 라우팅 결정. QueryRouter + 현재 토폴로지를 묶는다.
type routeDecisionFunc func(sql string) (router.RouteDecision, error)

// buildQueryRouterFunc 는 provider(현재 토폴로지) + 백엔드 resolver 로 라우팅 함수를
// 만든다. write=primary, read=replica(nil 이면 write 사용). extractor 는 기본 전략.
func buildQueryRouterFunc(provider router.TopologyProvider, write, read router.BackendResolver) routeDecisionFunc {
	extractor, _ := router.NewRouteKeyExtractor("")
	return func(sql string) (router.RouteDecision, error) {
		topo, err := provider.Current(context.Background())
		if err != nil {
			return router.RouteDecision{}, err
		}
		qr := router.QueryRouter{Topology: topo, Extractor: extractor, Write: write, Read: read}
		return qr.Route(sql)
	}
}

// handleQueryMode 는 쿼리 인지 라우팅으로 한 연결을 처리한다.
func handleQueryMode(client net.Conn, route routeDecisionFunc, dialer *backendDialer, serverVersion string) {
	defer func() { _ = client.Close() }()

	raw, _, err := readStartup(client)
	if err != nil {
		return
	}
	if err := sendTrustHandshake(client, serverVersion); err != nil {
		return
	}

	for {
		m, err := readMessage(client)
		if err != nil {
			return
		}
		switch m.Type {
		case 'X': // Terminate
			return
		case 'Q': // simple Query → 라우팅
			sql, _ := querySQL(m)
			d, err := route(sql)
			if err != nil {
				writePgError(client, "08006", "routing failed: "+err.Error())
				return
			}
			if d.Scatter {
				writePgError(client, "0A000", "multi-shard query not supported yet (single-shard fast-path only)")
				return
			}
			proxyToShard(client, raw, m, d, dialer)
			return
		default:
			writePgError(client, "0A000", fmt.Sprintf("message type %q not supported in query-routing PoC", m.Type))
			return
		}
	}
}

// proxyToShard 는 결정된 샤드 backend 에 연결해 startup + 첫 Query 를 전달하고 양방향
// proxy 한다.
func proxyToShard(client net.Conn, raw []byte, firstQuery pgMessage, d router.RouteDecision, dialer *backendDialer) {
	server, err := dialer.Dial(d.Backend)
	if err != nil {
		writePgError(client, "08006", fmt.Sprintf("cannot reach shard %s (%s): %v", d.Shard, d.Backend, err))
		return
	}
	defer func() { _ = server.Close() }()

	if _, err := server.Write(raw); err != nil { // backend startup (trust 백엔드 전제)
		return
	}
	if err := writeMessage(server, 'Q', firstQuery.Payload); err != nil { // 첫 Query 재생
		return
	}
	proxyBidi(client, server)
}

// proxyBidi 는 두 연결을 양방향으로 복사하고 한쪽이 끝나면 반환한다.
func proxyBidi(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	<-done
}
