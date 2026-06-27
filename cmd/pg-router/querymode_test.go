/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package main

import (
	"testing"

	"github.com/keiailab/postgres-operator/internal/router"
)

// TestBuildQueryRouterFunc 는 query-mode 라우팅 결정(토폴로지 + extractor + 백엔드
// resolver 합성)을 검증한다 — (E) 쿼리 인지 라우팅의 핵심.
func TestBuildQueryRouterFunc(t *testing.T) {
	provider := router.StaticTopologyProvider{T: router.Topology{Spec: shardSpec()}} // vindex column "id"
	write := func(s string) (string, error) { return s + ":5432", nil }
	route := buildQueryRouterFunc(provider, write, nil)

	// 샤딩 키가 있는 쿼리 → 단일 샤드 + 그 샤드 backend.
	for _, q := range []string{
		"INSERT INTO t (id, v) VALUES ('alice', 1)",
		"SELECT v FROM t WHERE id = 'bob'",
		"UPDATE t SET v = 2 WHERE id = 'carol'",
	} {
		d, err := route(q)
		if err != nil {
			t.Fatalf("route(%q): %v", q, err)
		}
		if d.Shard == "" || d.Backend != d.Shard+":5432" {
			t.Fatalf("route(%q) = %+v, want shard+backend", q, d)
		}
		if d.Scatter {
			t.Fatalf("route(%q) unexpectedly Scatter", q)
		}
	}

	// 샤딩 키 없음 → Scatter 신호.
	d, err := route("SELECT * FROM t")
	if err == nil {
		t.Fatal("no-key query should error (ErrNoRoutingKey)")
	}
	if !d.Scatter {
		t.Fatalf("no-key decision should set Scatter, got %+v", d)
	}
}
