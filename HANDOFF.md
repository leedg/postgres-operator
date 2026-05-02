# HANDOFF — postgresql-operator

> 다음 세션이 *컨버세이션 컨텍스트 없이* 재개 가능해야 한다. 시작 의식: 본 파일 → `TASKS.md` → 마지막 commit log 순서로 읽는다.

## 현재 상태 (2026-05-03)

- **마지막 commit (HEAD 직전)**: df1f2e1 `feat!: 0.3.0-alpha redesign reset — 자체 분산 SQL, 의존 제로`
- **본 세션 신규 commit (예정)**: `feat!(api): RFC 0001 PostgresCluster CRD v2 schema 실장 (F01a — types/webhook only)`
- **브랜치**: main (RFC 0002 archive 적용 — GH Actions 0)
- **현재 phase**: **P1 진행 중**. F01a 완료. F01b ~ F05 대기.
- **검증 결과 (F01a)**: `make lint` 0 issues / `make test` 모든 패키지 PASS / `make validate` helm lint --strict PASS / `make manifests` idempotent.

## 본 세션 (F01a) 의사결정 기록

1. **2026-05-03 사용자 결정**: API 버전 점프 (v1alpha1 → v1beta1) 가 아닌 **v1alpha1 in-place schema 교체**. 이유: RFC 0001 §2.2 시나리오 YAML + §7 P1 작업 항목이 `api/v1alpha1/` 디렉토리 유지 명시. 0.3.0-alpha 의 alpha 채널 정책이 schema 자체의 breaking change 를 정당화하므로 ADR 추가 불필요.
2. **2026-05-03 사용자 결정**: F01 영향 범위 (~16 파일, ~1500 라인) 측정 후 T2 → T3 재판정. **F01a (types + webhook + deepcopy + minimal stub)** 와 **F01b (reconciler/builders/envtest 새 spec 기반 재작성)** 으로 분할. F01a 가 본 세션 산출물.
3. webhook 의 cron / duration parse 정밀 검증은 F01b 또는 F02 로 연기 — F01a 범위에서 외부 의존 (`robfig/cron`) 추가 회피.
4. envtest 2 종 (`postgrescluster_controller_test.go`, `cascade_delete_test.go`) 삭제 결정 — 옛 spec 기반이라 의미 상실. F01b 에서 RFC 0001 spec 기준 새 envtest 로 재작성.
5. `internal/controller/builders.go` 의 helper 5 개 (`buildConfigMap`/`buildHeadlessService`/`buildClientService`/`renderSharedPreloadLibraries`/`renderPostgresConf`) 는 `//nolint:unused` directive 로 보존 — F01b 가 reconcile 본체에서 호출. sentinel 패턴은 staticcheck unused linter 가 dead-store 로 잡으므로 함수 단위 nolint 가 정답.

## 다음 단계 (F01b 진입)

**F01b — RFC 0001 spec 기반 reconcile 본체 + builders 재배선 + envtest 재작성**

진입점:
1. `internal/controller/postgrescluster_controller.go` 의 noop Reconcile 본체 — 새 ShardsSpec / RouterSpec → StatefulSet (shard 별) + headless Service + ConfigMap + Router Deployment + ClusterIP Service 생성.
2. `internal/controller/builders.go` helper 들의 호출 패턴 재정립 — pool 식별자가 `worker-<pool>` 에서 `shard-<ordinal>` 로 변경됨에 따라 `WorkerStatefulSetName` 등 `internal/controller/names.go` 의 명명 함수도 `ShardStatefulSetName(cluster, ordinal)` 로 추가 (worker* 함수는 deprecated 또는 제거).
3. `internal/controller/status.go` 의 Condition 카탈로그 — `ConditionCoordinatorReady` / `ConditionWorkersReady` 폐기, 새 `ConditionShardsReady` / `ConditionRouterReady` 도입 (RFC 0001 §3.4 권장 condition 카탈로그 그대로).
4. envtest 재작성: `internal/controller/postgrescluster_controller_test.go` (새 spec 기반 single-shard 시나리오) + `internal/controller/cascade_delete_test.go` (ADR 0008 회귀 — 새 shard 자원 OwnerReference 검증).
5. `internal/plugin/sharding/api.go` 의 doc comment 갱신 — `PostgresClusterSpec.Sharding.Backend 와 일치` 문구는 더 이상 정확하지 않음 (새 spec 에 `sharding.backend` 부재). F01b 또는 별도 PR 에서 정정.

## 후속 정리 작업 (F01b 와 분리)

- `docs/roadmap.md` 새 8-Phase (P0~P7) 본문 재작성 — 현재 deprecated stub.
- `docs/concepts/` / `docs/how-to/` / `docs/reference/` 의 v0.x spec 표현 (`coordinator/workers/routers`) 정리.
- F02 진입점: `cmd/instance/main.go` 의 todo 주석 ("supervise postgres process + 분산 SQL metadata 갱신").

## 차단점

없음. F01b 는 F01a 의 type 정의 위에서 mechanical 진행 가능.

## 근거 링크

- 본 세션 plan: `/Users/phil/.claude/plans/nifty-tumbling-marshmallow.md` (F01a/F01b 분할 결정 포함)
- RFC 0001: `docs/rfcs/0001-postgrescluster-crd-v2.md`
- ADR 0001 (keystone): `docs/adr/0001-self-built-distributed-sql.md`
- ADR 0004 (CRD managed by operator): `docs/adr/0004-crd-managed-by-operator.md`
- standards 적용: `~/Documents/ai-dev/standards/principles.md` §3 Surgical Changes (분할 결정의 근거)
- 이전 phase HANDOFF: 본 파일 git history (commit df1f2e1 전).
