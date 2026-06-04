/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package pgvector는 pgvector extension의 ExtensionPlugin 구현이다.
//
// SharedPreloadOrder=100. AI 워크로드 차별화를 위해 1급 동봉 결정(ADR 0001 v2).
// 본 extension은 shared_preload_libraries에 등록할 필요가 없으나, 일관성을
// 위해 plugin SDK에 동일 형태로 등록한다(PreloadOrder는 무해함).
package pgvector

import (
	"context"
	"database/sql"

	"github.com/keiailab/postgres-operator/internal/plugin"
)

const (
	Name         = "pgvector"
	PreloadOrder = 100
)

type Plugin struct{}

var _ plugin.ExtensionPlugin = (*Plugin)(nil)

func (Plugin) Name() string                                   { return Name }
func (Plugin) SharedPreloadOrder() int                        { return PreloadOrder }
func (Plugin) PreInstall(_ context.Context, _ *sql.DB) error  { return nil }
func (Plugin) PostInstall(_ context.Context, _ *sql.DB) error { return nil }
func (Plugin) Validate(_ string) error                        { return nil }

func Register(r *plugin.Registry) { r.RegisterExtension(Plugin{}) }
