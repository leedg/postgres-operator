/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package router

import "testing"

// TestRegexExtractor_ColumnMode 는 shardKeyColumn 지정 추출(WHERE/AND 등호 +
// INSERT 컬럼 위치 매칭)을 검증한다.
func TestRegexExtractor_ColumnMode(t *testing.T) {
	const col = "tenant_id"
	ex := regexExtractor{}
	cases := []struct {
		name    string
		query   string
		wantKey string
		wantOK  bool
	}{
		{"where eq", "SELECT v FROM t WHERE tenant_id = 'alice'", "alice", true},
		{"where multi-predicate", "SELECT v FROM t WHERE a = 1 AND tenant_id = 'carol'", "carol", true},
		{"where reordered", "SELECT v FROM t WHERE tenant_id='bob' AND x=2", "bob", true},
		{"insert position", "INSERT INTO t (id, tenant_id, v) VALUES (1, 'dave', 9)", "dave", true},
		{"insert first col", "INSERT INTO t (tenant_id, v) VALUES ('eve', 1)", "eve", true},
		{"wrong column only", "SELECT v FROM t WHERE other_id = 'zoe'", "", false},
		{"no predicate", "SELECT v FROM t", "", false},
		{"empty literal", "SELECT v FROM t WHERE tenant_id = ''", "", false},
		{"range predicate", "SELECT v FROM t WHERE tenant_id > 'a'", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := ex.ExtractRoutingKey(tc.query, col)
			if ok != tc.wantOK || key != tc.wantKey {
				t.Fatalf("ExtractRoutingKey(%q, %q) = (%q,%v), want (%q,%v)",
					tc.query, col, key, ok, tc.wantKey, tc.wantOK)
			}
		})
	}
}

// TestNewRouteKeyExtractor 는 전략 선택기와 빈/오류 이름 처리를 검증한다.
func TestNewRouteKeyExtractor(t *testing.T) {
	// regex 는 어느 빌드에서나 사용 가능.
	if ex, err := NewRouteKeyExtractor(ExtractorRegex); err != nil || ex.Name() != ExtractorRegex {
		t.Fatalf("regex: ex=%v err=%v", ex, err)
	}
	// 빈 이름 → 기본 전략(auto). 미컴파일 빌드에서도 auto 는 항상 생성 가능(regex 강등).
	if ex, err := NewRouteKeyExtractor(""); err != nil || ex.Name() != DefaultExtractorName {
		t.Fatalf("default: ex=%v err=%v", ex, err)
	}
	// 알 수 없는 이름 → error.
	if _, err := NewRouteKeyExtractor("nope"); err == nil {
		t.Fatal("unknown extractor: expected error, got nil")
	}
}

// TestAutoExtractor_FallsBackToRegex 는 parser 미컴파일(또는 미매치) 시 auto 가
// regex 로 라우팅함을 검증한다. (sqlparser 태그 빌드에서는 parser 가 primary 로
// 먼저 매치하며, 그 경우에도 동일 결과여야 한다.)
func TestAutoExtractor_FallsBackToRegex(t *testing.T) {
	ex, err := NewRouteKeyExtractor(ExtractorAuto)
	if err != nil {
		t.Fatalf("auto: err=%v", err)
	}
	key, ok := ex.ExtractRoutingKey("SELECT v FROM t WHERE tenant_id = 'alice'", "tenant_id")
	if !ok || key != "alice" {
		t.Fatalf("auto extract = (%q,%v), want (alice,true)", key, ok)
	}
}

// B-13: regex 추출기도 숫자 리터럴(정수 샤딩 키)을 키로 인정해야 한다.
// 배포된 라우터의 기본 추출기가 regex 였기 때문에, parser 만 고쳐선 라이브가 안 고쳐졌다.
func TestRegexExtractor_NumericKey(t *testing.T) {
	ex := regexExtractor{}
	cases := []struct{ name, query, want string }{
		{"where 정수", "SELECT * FROM orders WHERE tenant_id = 7", "7"},
		{"insert 정수", "INSERT INTO orders (tenant_id, amount) VALUES (7, 12.5)", "7"},
		{"where 문자열 회귀", "SELECT * FROM t WHERE tenant_id = 'alice'", "alice"},
		{"insert 문자열 회귀", "INSERT INTO t (tenant_id, v) VALUES ('alice','a')", "alice"},
	}
	for _, c := range cases {
		got, ok := ex.ExtractRoutingKey(c.query, "tenant_id")
		if !ok || got != c.want {
			t.Errorf("%s: = (%q,%v), want (%q,true)", c.name, got, ok, c.want)
		}
	}
}
