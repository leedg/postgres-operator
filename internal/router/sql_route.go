/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package router — sql_route.go 는 RouteKeyExtractor 의 *정규식(regex) 구현*이다
// (경량·제로 의존성 전략). full PostgreSQL 파서(pg_query_go = libpg_query CGO)는
// CGO_ENABLED=0 distroless 빌드와 충돌하므로, point-query 의 흔한 형태만 토큰
// 추출로 잡는다. 정확한 전략(prepared/복합 predicate/JOIN)은 parserExtractor
// (route_extractor_parser.go) 가 담당하고, auto 가 둘을 조합한다.
package router

import (
	"regexp"
	"strings"
)

// legacyRouteKeyPattern 은 shardKeyColumn 미지정(first-literal) 모드의 패턴이다:
// `VALUES ('key'` 또는 `WHERE <col> = 'key'` 의 첫 single-quoted 리터럴.
var legacyRouteKeyPattern = regexp.MustCompile(`(?i)(?:VALUES\s*\(\s*|WHERE\s+[a-z_][a-z0-9_]*\s*=\s*)'([^']*)'`)

// ExtractRoutingKey 는 query 에서 single-shard 라우팅 key 를 first-literal 모드로
// 추출하는 패키지 편의 함수다 (backward-compat: shardKeyColumn 미지정). 컬럼 지정
// 추출과 전략 선택은 RouteKeyExtractor / NewRouteKeyExtractor 를 사용한다.
func ExtractRoutingKey(query string) (string, bool) {
	return regexExtractor{}.ExtractRoutingKey(query, "")
}

// regexExtractor 는 RouteKeyExtractor 의 정규식 구현이다 (경량 전략).
type regexExtractor struct{}

func (regexExtractor) Name() string { return ExtractorRegex }

// ExtractRoutingKey 는 col 이 빈 문자열이면 first-literal 모드, 아니면 그 컬럼을
// 지정 추출한다 (WHERE/AND `<col> = 'x'` 또는 INSERT 컬럼-값 위치 매칭).
func (regexExtractor) ExtractRoutingKey(query, col string) (string, bool) {
	if col == "" {
		m := legacyRouteKeyPattern.FindStringSubmatch(query)
		if len(m) < 2 || m[1] == "" {
			return "", false
		}
		return m[1], true
	}
	if k, ok := matchColumnEquals(query, col); ok {
		return k, true
	}
	return matchInsertColumn(query, col)
}

// matchColumnEquals 는 query 어디에서든 `<col> = 'literal'` 또는 `<col> = 123`(따옴표 없는
// 숫자)을 잡는다(대소문자 무시). 복합 predicate (`WHERE a=1 AND tenant_id='t9'`) 에서도
// 지정 컬럼만 추출한다.
//
// B-13: 숫자 그룹이 없어서 `tenant_id int` 스키마가 통째로 라우팅되지 않았다.
func matchColumnEquals(query, col string) (string, bool) {
	pat := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(col) + `\s*=\s*(?:'([^']*)'|(-?[0-9]+(?:\.[0-9]+)?))`)
	m := pat.FindStringSubmatch(query)
	if len(m) < 3 {
		return "", false
	}
	if m[1] != "" {
		return m[1], true
	}
	if m[2] != "" {
		return m[2], true
	}
	return "", false
}

// insertPattern 은 `INSERT INTO t (c1, c2, ...) VALUES (v1, v2, ...)` 의 컬럼 목록과
// 첫 VALUES 튜플을 잡는다.
var insertPattern = regexp.MustCompile(`(?is)INSERT\s+INTO\s+[^\s(]+\s*\(([^)]*)\)\s*VALUES\s*\(([^)]*)\)`)

// matchInsertColumn 은 INSERT 의 컬럼 목록에서 col 의 위치를 찾아 같은 위치의 VALUES
// 리터럴을 반환한다. col 이 목록에 없거나 값이 따옴표 리터럴이 아니면 (",", false).
func matchInsertColumn(query, col string) (string, bool) {
	m := insertPattern.FindStringSubmatch(query)
	if len(m) < 3 {
		return "", false
	}
	cols := splitCSV(m[1])
	vals := splitCSV(m[2])
	if len(cols) != len(vals) {
		return "", false
	}
	for i, c := range cols {
		if strings.EqualFold(c, col) {
			v := strings.TrimSpace(vals[i])
			if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
				inner := v[1 : len(v)-1]
				if inner == "" {
					return "", false
				}
				return inner, true
			}
			// B-13: 따옴표 없는 숫자 리터럴(정수 샤딩 키)도 키다 — 해시는 키 문자열 기준.
			if numericLiteral.MatchString(v) {
				return v, true
			}
			return "", false
		}
	}
	return "", false
}

// numericLiteral 은 따옴표 없는 숫자 리터럴(정수/실수, 음수 포함)이다.
var numericLiteral = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

// insertMultiRowPattern 은 `INSERT INTO t (c1,...) VALUES (..),(..),...` 의 컬럼 목록과
// VALUES 절 전체(다중 튜플)를 잡는다. matchInsertColumnAll 이 각 튜플에서 키를 뽑는다.
var insertMultiRowPattern = regexp.MustCompile(`(?is)INSERT\s+INTO\s+[^\s(]+\s*\(([^)]*)\)\s*VALUES\s*(.+?)(?:\s+ON\s+CONFLICT|\s+RETURNING|;|\s*$)`)

// tuplePattern 은 VALUES 절에서 개별 `( ... )` 튜플을 하나씩 잡는다(중첩 괄호·함수
// 미지원 — regex 전략 best-effort).
var tuplePattern = regexp.MustCompile(`\(([^)]*)\)`)

// matchInsertColumnAll 은 다중행 INSERT 의 *모든 튜플*에서 col 위치의 키를 추출한다.
// #B-30: 라우터가 다중행 INSERT 를 첫 튜플 키로만 라우팅해 나머지 행을 오배치하던 것을,
// 모든 튜플 키를 뽑아 호출자가 "단일 샤드 수렴 여부"를 판정할 수 있게 한다.
// 반환: (키 목록, ok). 하나라도 키를 못 뽑으면(비-리터럴 등) ok=false.
func matchInsertColumnAll(query, col string) ([]string, bool) {
	m := insertMultiRowPattern.FindStringSubmatch(query)
	if len(m) < 3 {
		return nil, false
	}
	cols := splitCSV(m[1])
	idx := -1
	for i, c := range cols {
		if strings.EqualFold(c, col) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, false
	}
	tuples := tuplePattern.FindAllStringSubmatch(m[2], -1)
	if len(tuples) == 0 {
		return nil, false
	}
	keys := make([]string, 0, len(tuples))
	for _, t := range tuples {
		vals := splitCSV(t[1])
		if idx >= len(vals) {
			return nil, false
		}
		v := strings.TrimSpace(vals[idx])
		switch {
		case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
			inner := v[1 : len(v)-1]
			if inner == "" {
				return nil, false
			}
			keys = append(keys, inner)
		case numericLiteral.MatchString(v):
			keys = append(keys, v)
		default:
			return nil, false
		}
	}
	return keys, true
}

// splitCSV 는 comma 구분 목록을 trim 하여 분리한다 (단순 — 중첩 함수/콤마 미지원,
// regex 전략의 best-effort 한계).
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

var _ RouteKeyExtractor = regexExtractor{}
