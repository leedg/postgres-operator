# ADR 0005 — Plugin SDK Interface Model (in-process + gRPC)

- **Status**: Accepted (interface frozen)
- **Date**: 2026-04-27
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0001 v2 (mission three axes, Plugin SDK as meta-differentiator), ADR 0004 (Build, not Fork/Layer)
- **Prior analysis**: `/Users/phil/.claude/plans/squishy-squishing-harp.md` §9.5

## Context

Among the three mission axes of ADR 0001 v2, the **Plugin SDK** is the meta-differentiator of this project. Its value lies in the following two promises.

1. **Adding a new backup tool, exporter, extension, router, or auth mechanism = implementing an interface in a week.** Zero core reconciler code changes.
2. **External contributors can distribute closed-license plugins separately.** The project itself is Apache-2.0, but the plugin separation model permits closed-module coupling.

To enforce these promises at the code level, the **interface must be frozen before entering any other Pillar (P1~P12, P14)**. Introducing it late forces a large-scale reconciler refactor right before v1.0.

## Decision

### Five interfaces frozen

The following 5 Go interfaces are frozen in `internal/plugin/api.go`.

| Interface | Responsibility | Pillar using it |
|---|---|---|
| `BackupPlugin` | Backup tool abstraction (pgBackRest, WAL-G, Barman, custom) | P4 |
| `ExporterPlugin` | Prometheus exporter + Grafana dashboard + alert rule | P6 |
| `ExtensionPlugin` | PG extension install/preload/post-init hooks + `SharedPreloadOrder()` | P10 |
| `RouterPlugin` | QueryRouter Pod spec builder + domain readiness | P12 |
| `AuthPlugin` | Authentication mechanism (SCRAM/mTLS/OIDC) + Secret schema | P7 |

Each interface commonly has `Name() string`, which is used as the lookup key in the `Registry`.

### Dependency minimization

This package (`internal/plugin/`) has no external dependencies outside these two groups:

- Go stdlib (`context`, `database/sql`, `time`, `sort`, `sync`, `fmt`)
- `k8s.io/api/core/v1` (already exists as indirect in go.mod)

In particular, the following are **intentionally excluded**:

- **prometheus-operator API** — `AlertRulesYAML()` is exposed as `[]byte`. This SDK is not tied to prometheus-operator versions.
- **apiextensions-apiserver's `JSONSchemaProps`** — `SecretSchemaJSON()` is also exposed as `[]byte`. Same reason.
- **HashiCorp `go-plugin`** — not yet imported. When added in P13-T4, it enters only as an adapter equivalent to the in-process API.

### Registration model — in-process first

In-process (compile-time) registration via the `Registry` struct. A single goroutine registers at `init()` or `main()` time, and reconcilers look up under `RLock` protection.

Duplicate registration causes `panic`. This is a choice to enforce determinism at init() time; the methods to swap dynamically (`Replace*`) will be added in a follow-up P13 task when needed.

### `SharedPreloadOrder()` — regression prevention for Crunchy PGO Issue #3194

`Registry.Extensions()` returns registered ExtensionPlugins sorted in the following order:

1. Ascending `SharedPreloadOrder()` (smaller comes first)
2. Lexicographic `Name()` on ties

Recommended priorities:

| Priority | extension | Reason |
|---|---|---|
| 0 | citus | "Citus must be first" convention (PostgreSQL hook registration order) |
| 100 | pgaudit | Standard audit tool |
| 100 | pgvector | AI workload differentiator |
| 200 | pg_cron | Scheduler (safe above Citus) |
| 300 | pg_partman, pgnodemx, set_user, postgis | General |

The P10 reconciler serializes the result of this method with `strings.Join` and injects it into `shared_preload_libraries`. Sorting policy violations are blocked from regressing by `internal/plugin/api_test.go:TestExtensions_PreloadOrder`.

### Enforcement mechanism

1. **golangci-lint custom rule (P13-T2)**: PR reject if core reconcilers (`internal/controller/`, `internal/webhook/`) directly import `internal/plugin/<concrete>/` sub-packages.
2. **Compile guards** (`var _ BackupPlugin = (*dummyBackup)(nil)` etc. in `api_test.go`): build fails if interface signatures change.
3. **Regression tests** (`TestExtensions_PreloadOrder`): tests fail if the SharedPreloadOrder policy changes.

### Change policy

- During alpha, **only additions of methods (non-breaking)** are permitted. Method removal or signature changes are handled together in RFC 0012 ("Plugin SDK stabilization").
- Changes to this ADR (adding/removing interfaces, introducing dependencies) require an RFC.

## Rationale

### Why five

All extension points abstracted by this SDK can be classified into the following 5 responsibilities:

1. **Data protection** (BackupPlugin)
2. **Observability** (ExporterPlugin)
3. **Extended functionality** (ExtensionPlugin)
4. **Request routing** (RouterPlugin)
5. **Identity verification** (AuthPlugin)

Analyzing the external integration points of PGO and CNPG (plan §7), no case was found that requires an additional classification beyond the above 5. If a 6th interface becomes necessary, add it after an ADR update.

### Why minimize dependencies

This SDK is a wire format imported by external plugins. Heavy dependencies cause:

- Increased external plugin build times
- Versioning conflicts in plugin isolation (gRPC mode)
- This SDK being dragged along with major changes of the monitoring stack (e.g., prometheus-operator v0.x → v1.0)

Exposing `[]byte` raw manifests may look like weak typing, but **in the trade-off with interface stability, the latter is overwhelmingly more important**.

### Why in-process first

P13-T4 (gRPC out-of-process) is a value for the v1.x timeframe when external closed plugins are needed. Until v1.0 GA, **this project's own plugins (pgbackrest, pgmonitor, citus, etc.) in-process are sufficient**, and we stabilize them first.

### Why panic on duplicate registration

Plugin registration is an init() time decision. Duplicates are defects that should be caught at build time, and runtime fallback creates the following two risks:

- A "last registrant wins" policy makes behavior depend on compile order (Go init order depends on the import graph)
- The user loses any clue to debug "why isn't my plugin working?"

Panic is harsh, but it enforces determinism.

## Tradeoffs

- **Cost of interface stability**: once frozen, signatures only allow additions even during alpha. Poorly designed methods are stuck until RFC 0012.
  - **Mitigation**: at P13-T1 we sufficiently survey external integration points of PGO/CNPG before freezing. This ADR is that result.
- **Verification burden of `[]byte` weak typing**: when the reconciler unmarshals `AlertRulesYAML()` results directly, schema verification responsibility lies on the reconciler side.
  - **Mitigation**: in the P6 reconciler, separately import a prometheus-operator schema validation library and apply it. The SDK stays stable; the verification logic can be updated.
- **Operational risk of panic policy**: an instance terminates immediately on init() panic in production.
  - **Mitigation**: registration is a single init() or main() call. External inputs (CR, ConfigMap) have no effect on registration timing.

## Enforcement mechanism summary

| Mechanism | Implementation location | Introduction timing |
|---|---|---|
| Compile guards (`var _ Iface = (*impl)(nil)`) | `internal/plugin/api_test.go` | P13-T1 (simultaneous with this ADR adoption) |
| Registry duplicate panic | `internal/plugin/registry.go` | Same |
| SharedPreloadOrder sorting regression test | `internal/plugin/api_test.go:TestExtensions_PreloadOrder` | Same |
| golangci-lint custom rule (block concrete imports) | `.custom-gcl.yml` | P13-T2 (separate task) |
| gRPC out-of-process adapter | `internal/plugin/grpc/` (new) | P13-T4 (v1.x) |

## Consequences

- `internal/plugin/api.go` + `internal/plugin/registry.go` + `internal/plugin/api_test.go` are frozen.
- All other Pillar reconcilers (P1~P12, P14) call only this package's interfaces.
- Changes to this ADR (adding/removing interfaces, introducing dependencies) require an RFC.
- Following work: P13-T2 (golangci-lint custom rule), P13-T3 (guarantee that P4/P6/P10 are actually written as interface implementations), RFC 0012 (SDK stabilization + external guide).
