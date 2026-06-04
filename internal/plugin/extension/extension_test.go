/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package extension은 등록된 모든 ExtensionPlugin의 회귀 테스트만 보유한다.
//
// 본 패키지가 별도 존재하는 이유: depguard 규칙(.golangci.yml)이 internal/plugin/
// extension/ 하위 하위 패키지를 reconciler/webhook이 직접 import 하지 못하게
// 막지만, 본 패키지는 모든 구체 플러그인을 import 해 "정렬 정책 정확성"을
// 유일하게 검증할 수 있다. cmd/main.go도 동일 권한이 있으나, cmd/main.go에는
// 테스트가 없으므로 본 패키지가 회귀 차단의 단일 출처(SOT)다.
package extension

import (
	"testing"

	"github.com/keiailab/postgres-operator/internal/plugin"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/pgaudit"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/pgcron"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/pgnodemx"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/pgvector"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/postgis"
	"github.com/keiailab/postgres-operator/internal/plugin/extension/setuser"
)

// TestPreloadOrder_AllRegisteredExtensions는 본 오퍼레이터가 동봉하는 6개
// ExtensionPlugin이 모두 등록된 상태에서 Registry.Extensions()의 정렬 결과가
// 결정적임을 검증한다.
//
// SharedPreloadOrder 규약 (낮은 숫자가 앞쪽) 의 회귀 차단 통합 검증이며,
// 향후 새 ExtensionPlugin 추가 시 본 테스트의 wantNames에 위치를 명시해야
// 한다. 추가 위치는 ADR 0005 §SharedPreloadOrder 권장 표를 참조한다.
func TestPreloadOrder_AllRegisteredExtensions(t *testing.T) {
	r := plugin.NewRegistry()
	pgaudit.Register(r)
	pgcron.Register(r)
	pgnodemx.Register(r)
	pgvector.Register(r)
	postgis.Register(r)
	setuser.Register(r)

	// RFC 0006 R1 후 EnabledExtensions(names) 가 권장. 본 테스트는 *모든* 등록된
	// extension 의 정렬 규약 검증이므로 등록된 이름 전체를 명시 전달.
	got, missing := r.EnabledExtensions([]string{"pgaudit", "pg_cron", "pgnodemx", "pgvector", "postgis", "set_user"})
	if len(missing) > 0 {
		t.Fatalf("unexpected missing names: %v", missing)
	}
	// 정렬 규약: SharedPreloadOrder 오름차순, 동률 시 Name() 사전순.
	// pgaudit(100) → pgvector(100) → pg_cron(200) → pgnodemx(300) →
	// postgis(300) → set_user(300)
	wantOrder := []string{
		"pgaudit",  // 100
		"pgvector", // 100 — alpha 정렬에서 pgaudit < pgvector
		"pg_cron",  // 200
		"pgnodemx", // 300
		"postgis",  // 300 — alpha 정렬: pgnodemx < postgis
		"set_user", // 300 — alpha 정렬: postgis < set_user
	}

	if len(got) != len(wantOrder) {
		t.Fatalf("expected %d extensions, got %d", len(wantOrder), len(got))
	}
	for i, want := range wantOrder {
		if got[i].Name() != want {
			t.Errorf("position %d: want %q, got %q (full order: %v)",
				i, want, got[i].Name(), namesOf(got))
		}
	}
}

func namesOf(exts []plugin.ExtensionPlugin) []string {
	out := make([]string, len(exts))
	for i, e := range exts {
		out[i] = e.Name()
	}
	return out
}
