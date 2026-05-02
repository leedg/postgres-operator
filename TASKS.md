# TASKS — postgresql-operator

> 작업 ID: F=기능 / I=개선 / B=버그 / T=그 외. 부여 후 재사용 금지.
> 단계: 설계(10%) / 구현(60%) / 테스트(90%) / 완료(100%).

## 현재 Phase: **P1** (0.4.0 — single-shard production-ready)

목표: RFC 0001 의 새 CRD spec 위에 single-shard reconcile + HA + pgBackRest 통합 + chaos-mesh E2E. 6+개월.

P0 (0.3.0-alpha redesign reset) 는 완료 (commit df1f2e1). 이전 phase 작업 표는 §"이전 Phase 기록" 참조.

## 작업 표 (P1)

| ID    | 기능명 / 요약 | 단계 | 완성도 | 의존 | 영향 | 비고 |
|-------|---------------|------|--------|------|------|------|
| F01a  | RFC 0001 PostgresCluster CRD v2 schema 실장 (types + CEL + webhook + deepcopy) | 완료 | 100% | - | F01b,F02~F05 | 2026-05-03. v1alpha1 in-place 본문 교체. 6-필드 spec, status.shards[], 3개 CEL XValidation. webhook 도메인 검증 (matrix/autoSplit/backup). |
| F01b  | F01a stub 해소 — 새 ShardsSpec/RouterSpec 기반 reconcile 본체 + builders 재배선 + envtest 재작성 | 설계 | 10% | F01a | F02~F05 | 진입점: `internal/controller/postgrescluster_controller.go` (현재 noop reconcile + `// TODO(F01b)` 주석). builders.go helper 5개 (`//nolint:unused`) 가 sentinel. |
| F02   | instance manager P2-T3+ — postgres 프로세스 supervise + promote/demote 실장 | 설계 | 10% | F01b | F03 | 진입점: `cmd/instance/main.go` |
| F03   | RFC 0003 election / fencing 인터페이스 위 실장 완성 | 설계 | 10% | F02 | F04 | `internal/instance/election`, `internal/instance/fencing` 상단 layer |
| F04   | pgBackRest 통합 — backup controller (`internal/controller/backup/`) | 설계 | 10% | F01b | F05 | BackupJob CRD 는 이미 정의됨. RFC 0001 의 `spec.backup` reconcile 연결. |
| F05   | single-shard E2E 시나리오 재설계 — chaos-mesh primary kill → failover < 30s | 설계 | 10% | F01b,F02,F03,F04 | - | RFC 0001 spec 기준 envtest 신규 작성 포함. |

## 이전 Phase 기록 (P0 — 0.3.0-alpha redesign reset, commit df1f2e1)

T01 ~ T14 완료. 자체 분산 SQL 정체성 정착, ADR/RFC 0001~0005 작성, Citus 의존 코드 폐기, lint/test/validate PASS.

## 차단됨

(없음 — F01a 가 P1 진입 게이트였고 완료 상태. F01b 부터 순차 진행 가능.)

## 영향도 추적

F01a (CRD schema) 변경 시 → F01b reconcile 본체 + 모든 envtest 재검토.
F01b (reconcile 본체) 변경 시 → F02~F04 의 spec 인터페이스 점검.
F04 (pgBackRest) 변경 시 → BackupJob CRD `spec.tool="pgbackrest"` 통합 검증.
