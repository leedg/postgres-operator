/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package router

import "testing"

// TestParserExtractor 는 토크나이저 기반 추출이 라우팅 key subset 을 정확히 처리함을
// 검증한다 (제로 의존성).
func TestParserExtractor(t *testing.T) {
	const col = "tenant_id"
	ex := parserExtractor{}
	cases := []struct {
		name    string
		query   string
		wantKey string
		wantOK  bool
	}{
		{"select eq", "SELECT v FROM t WHERE tenant_id = 'alice'", "alice", true},
		{"multi predicate", "SELECT v FROM t WHERE a = 1 AND tenant_id = 'carol'", "carol", true},
		{"insert position", "INSERT INTO t (id, tenant_id, v) VALUES (1, 'dave', 9)", "dave", true},
		{"update where", "UPDATE t SET v = 2 WHERE tenant_id = 'erin'", "erin", true},
		{"delete where", "DELETE FROM t WHERE tenant_id='frank'", "frank", true},
		{"table-qualified col", "SELECT v FROM t WHERE t.tenant_id = 'gwen'", "gwen", true},
		{"wrong column", "SELECT v FROM t WHERE other = 'zoe'", "", false},
		{"no predicate", "SELECT v FROM t", "", false},
		{"parameterized", "SELECT v FROM t WHERE tenant_id = $1", "", false},
		{"range predicate", "SELECT v FROM t WHERE tenant_id > 'a'", "", false},
		{"empty literal", "SELECT v FROM t WHERE tenant_id = ''", "", false},
		{"empty col delegates", "SELECT v FROM t WHERE tenant_id = 'x'", "", false}, // col="" → regex 위임
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useCol := col
			if tc.name == "empty col delegates" {
				useCol = ""
			}
			k, ok := ex.ExtractRoutingKey(tc.query, useCol)
			if k != tc.wantKey || ok != tc.wantOK {
				t.Fatalf("ExtractRoutingKey(%q,%q) = (%q,%v), want (%q,%v)",
					tc.query, useCol, k, ok, tc.wantKey, tc.wantOK)
			}
		})
	}
}

// TestParserBeatsRegex 는 토큰화가 정규식이 *틀리는* 케이스를 정확히 처리함을 보인다
// — 라우팅 키가 다른 컬럼의 문자열 리터럴 *안*에 등장하는 경우. 정규식 first-literal
// 모드는 오인할 수 있으나, 토크나이저는 문자열 내부를 식별자로 보지 않는다.
func TestParserBeatsRegex(t *testing.T) {
	// note 컬럼 값 안에 "tenant_id = 'evil'" 문자열이 들어 있다.
	q := "SELECT v FROM t WHERE note = 'tenant_id = evil' AND tenant_id = 'real'"
	k, ok := parserExtractor{}.ExtractRoutingKey(q, "tenant_id")
	if !ok || k != "real" {
		t.Fatalf("parser = (%q,%v), want (real,true)", k, ok)
	}
	// 주석 안의 가짜 predicate 도 무시.
	q2 := "SELECT v FROM t WHERE /* tenant_id = 'fake' */ tenant_id = 'genuine'"
	k2, ok2 := parserExtractor{}.ExtractRoutingKey(q2, "tenant_id")
	if !ok2 || k2 != "genuine" {
		t.Fatalf("parser(comment) = (%q,%v), want (genuine,true)", k2, ok2)
	}
}

// TestIsReadOnlyQuery 는 읽기/쓰기 분류가 보수적(확실한 읽기만 true)임을 검증한다.
func TestIsReadOnlyQuery(t *testing.T) {
	cases := []struct {
		query string
		read  bool
	}{
		{"SELECT v FROM t WHERE id = 'a'", true},
		{"  select 1", true},
		{"SHOW search_path", true},
		{"VALUES (1),(2)", true},
		{"TABLE t", true},
		{"SELECT * FROM t FOR UPDATE", false}, // 잠금 → 쓰기 취급
		{"select * from t for share", false},  // 잠금
		{"INSERT INTO t (id) VALUES ('a')", false},
		{"UPDATE t SET v=1", false},
		{"DELETE FROM t", false},
		{"WITH x AS (INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM x", false}, // WITH 보수적=쓰기
		{"", false},
	}
	for _, c := range cases {
		if got := IsReadOnlyQuery(c.query); got != c.read {
			t.Errorf("IsReadOnlyQuery(%q) = %v, want %v", c.query, got, c.read)
		}
	}
}

// TestParserSelectableViaFactory 는 "parser"/"auto" 선택이 토크나이저를 쓰는지 확인.
func TestParserSelectableViaFactory(t *testing.T) {
	ex, err := NewRouteKeyExtractor(ExtractorParser)
	if err != nil || ex.Name() != ExtractorParser {
		t.Fatalf("parser strategy: ex=%v err=%v", ex, err)
	}
	auto, _ := NewRouteKeyExtractor(ExtractorAuto)
	// 복합 predicate(정규식 컬럼 모드도 잡지만, auto 는 parser 를 primary 로 사용).
	if k, ok := auto.ExtractRoutingKey("SELECT v FROM t WHERE x = 1 AND tenant_id = 'zed'", "tenant_id"); !ok || k != "zed" {
		t.Fatalf("auto = (%q,%v), want (zed,true)", k, ok)
	}
}
