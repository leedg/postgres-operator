---
title: "Roadmap"
description: "postgres-operator Gate 기반 로드맵 (sub-task 체크리스트)"
---

# Roadmap

본 ROADMAP 은 *날짜 약속이 아니라* 검증 가능한 Gate + sub-task 체크리스트로 진행을 추적한다. `postgres-operator` 는 외부 PostgreSQL operator 나 distributed SQL backend 를 내장하지 않는 *독립 신규 구현* 이다. PGO-class / Citus-class 같은 표현은 *품질·문제 영역을 설명하기 위한 비교 기준*이며, 특정 제품을 fork 하거나 runtime dependency 로 포함한다는 뜻이 아니다.

## 체크박스 의미

| 마커 | 의미 |
|---|---|
| `[x]` | 코드 + 테스트 양쪽 존재. e2e 또는 unit test 로 회귀 가드 확보 |
| `[~]` | 부분 구현 (CRD 필드만, helper 미통합, 또는 e2e 미완) |
| `[ ]` | 미시작 (설계 또는 PoC 단계) |

각 sub-task 우측 *Verify* 는 검증 명령 또는 e2e 파일을 인용한다.

## 설계 원칙

| 원칙 | 의미 |
|---|---|
| PGO-class quality | HA / backup / restore / upgrade / observability / security UX 를 상용 운영 기준으로 맞춘다. PGO 코드는 사용하지 않는다. |
| Citus-class problem coverage | shard placement / routing / rebalance / distributed transaction 문제를 분석한다. Citus extension 은 포함하지 않는다. |
| Plugin SDK message 폐기 | v0.x archive 의 broad Plugin SDK 포지셔닝은 폐기됐다. 필요한 확장점만 좁게 설계한다. |
| Apache-2.0 clean room | 허용 라이선스 의존만 사용하고 금지 라이선스 코드는 복사·번역·포팅하지 않는다. |
| GitOps first | argos production 배포는 GitOps path 와 Helm chart dependency 로 재현 가능해야 한다. |

## 현재 상태 스냅샷

| 영역 | 상태 | 검증 근거 |
|---|---|---|
| Naming | `postgres-operator` 로 repo/chart/GitOps path 정렬 | archive 문서는 history 보존 |
| Release | `0.3.0-alpha.16` image/chart/SBOM publish | 1.0.0 GA 전환 불가 |
| Runtime image | `ghcr.io/keiailab/pg:18` public pull 검증 | multi-arch / runtime SBOM 보강 잔여 |
| Production cluster | `platform-data-postgres-operator` Synced/Healthy, `argos-postgres` Ready | HA replica / backup/restore / long soak 잔여 |
| Fencing | PVC fence 골격 | operator-driven recovery / runbook 자동화 잔여 |
| Backup | 부분 구현 | `BackupJob` phase 전이 + `ScheduledBackup` CRD/controller + `RestorePIT` 호출 경로 + pgBackRest command-runner plugin + K8s sidecar exec 경로 존재. 실제 restore drill 잔여 |

## Gate Plan

### Gate G0 — Day-0 배포 (완충율 ~100%)

**목표**: 사용자가 GitOps 로 operator + single-shard Postgres 를 배포한다.

- [x] CRD `PostgresCluster` 정의 — `api/v1alpha1/postgrescluster_types.go` (RFC-0001 v2 schema)
- [x] CRD `BackupJob` 정의 (Phase 1 스펙) — `api/v1alpha1/backupjob_types.go`
- [x] PostgresClusterReconciler — desired state 생성 (ConfigMap / Headless Service / StatefulSet) — `internal/controller/postgrescluster_controller.go`
- [x] Status phase 전이 (Provisioning → Ready) — `internal/controller/status.go`, `aggregate_status.go`
- [x] Pod readiness 추적 — Reconciler 내 endpoint watch
- [x] ArgoCD Synced/Healthy — argos production 검증
- [x] GHCR public pull
- [x] Day-0 e2e — `test/e2e/e2e_test.go`, `postgrescluster_e2e_test.go`
- Verify: ArgoCD Synced/Healthy + Pod 1/1 Running + `psql -c 'select version()'`

### Gate G1 — Single-shard production HA (완충율 ~30%)

**목표**: 단일 PostgreSQL 운영 DB 로 사용 가능 (HA 포함).

- [x] `Replicas` 필드 정의 (0-15 비동기 복제본) — `postgrescluster_types.go`
- [x] STS scale 매핑 — Reconciler
- [x] Primary delete drill 기초 e2e — `test/e2e/failover_e2e_test.go`
- [x] PDB 자동 생성 — `internal/controller/pdb.go`
- [~] PVC fence (split-brain fail-fast) — fencing 골격만 존재, runbook 자동화 잔여
- [ ] **자동 failover 로직** — `internal/controller/failover/` 신규
  - [x] Primary 장애 감지 — `DetectPrimaryFailure` + `SelectPromotionCandidate`
  - [x] Standby promotion (`pg_ctl promote` 또는 logical replication 승격) — plan/helper + controller-layer replica Pod exec + promoted `instance-status` annotation patch 완료
  - [x] Ready 이후 primary failure 상태 표면 — `status.phase=Degraded` + `FailoverReady=False` + promotion candidate 메시지
  - [~] Replica 재합류 (`pg_basebackup` or `pg_rewind`) — first boot `pg_basebackup` + existing PGDATA old-primary marker 일반화 + current primary endpoint main env + `pg_rewind` command-runner + HBA normal connection auth + fresh `pg_basebackup` fallback 완료, live chaos/rewind drill 검증 잔여
  - [~] Synchronous replication — `spec.postgresql.synchronous.{method,number,dataDurability}` + CEL `number<=shards.replicas` + `ANY/FIRST N (...)` 렌더링 + `required/preferred` quorum policy + standby `application_name` wiring + ConfigMap hash rolling reconcile 완료, live commit/RPO drill 잔여
  - [ ] HA election 분산락 (K8s Lease)
- [ ] **Backup/Restore controller 실구현** — `internal/controller/backupjob_controller.go` 보강
  - [x] `BackupJob.Phase` 전이 (Pending → Running → Succeeded/Failed)
  - [x] `ScheduledBackup` CRD/controller — 6-field cron schedule → atomic `BackupJob` 생성, suspend/immediate/ownerReference/concurrency guard
  - [x] `BackupJob.spec.type=restore` → `BackupPlugin.RestorePIT(targetTime)` 호출 경로 + targetTime 필수 검증
  - [x] `BackupJob.spec.executionMode=job` → owned `batch/v1.Job` 생성/관찰 + `jobTemplate` 표준 env 주입
  - [~] Plugin invocation — pgBackRest command-runner + sidecar command planning 완료. WAL-G/Barman 후속
  - [x] Sidecar 모드 분기 — ready primary Pod 의 `postgres` 컨테이너에 K8s `pods/exec` 로 pgBackRest argv 전달
- [~] **PITR 복원** — `BackupRestoreSpec.TargetTime` 기반 pgBackRest `restore --type=time --target=...` 호출 경로 + sidecar exec 경로 존재. 실제 restore + checksum drill 잔여
- [ ] **Upgrade rollback runbook** — `docs/runbooks/upgrade.md` 신규
- [ ] **RTO/RPO 측정 + 기록** — `docs/runbooks/ha.md` 신규
- Verify: primary delete 후 N초 내 replica 승격 + `pg_is_in_recovery()=false` + 데이터 손실 0 + backup 후 새 클러스터 복원 + 데이터 checksum 일치

### Gate G2 — 운영 품질 (완충율 ~25%)

**목표**: PGO-class 운영 표면 확보.

- [x] /metrics 기초 노출 (port 8443) — `internal/controller/metrics.go`, `cmd/main.go`
- [x] TLS 경로 설정 (인증서 마운트 + ssl=on) — `internal/controller/builders.go:renderPostgresConf()`, `tls.go`
- [x] Topology spread 통합 — `internal/controller/topology_spread.go`
- [x] PVC online resize — `internal/controller/pvc_resize.go`
- [x] Cascade delete 가드 — `internal/controller/cascade_delete_test.go`
- [~] cert-manager 통합 — 마운트 경로만, 발급 메커니즘 미정
- [~] **PrometheusRule 자동 생성** — Helm Metrics Service/ServiceMonitor/PrometheusRule 렌더링 + 실제 `postgres_operator_backupjob_phase` metric 기반 BackupJob 실패 알림
  - [x] Replication lag 경고 — instance status `LagBytes` → `postgres_operator_postgrescluster_replication_lag_bytes` + Helm `PostgresReplicationLagHigh`
  - [x] Pooler 실패/포화 경고 — `postgres_operator_pooler_phase{phase="Failed"}` + CNPG `cnpg_pgbouncer_*` exporter metric 기반 collection 실패, client waiting, maxwait alert 렌더 검증
  - [x] Disk pressure — `kubelet_volume_stats_*` data PVC alert
  - [x] Backup failure — `postgres_operator_backupjob_phase{phase="Failed"}`
- [~] **Grafana 대시보드** — Helm dashboard ConfigMap 렌더링 완료 (`postgres-operator-cluster-overview.json`, `postgres-operator-pooler.json`), live Grafana import/패널 검증 잔여
- [~] **Connection pooler (pgBouncer)** — `Pooler` CRD + ConfigMap/Deployment/Service reconcile 첫 조각
  - [x] CRD `Pooler.spec.{cluster,instances,type,pgbouncer.poolMode,pgbouncer.parameters}` 추가
  - [x] 별도 PgBouncer Deployment/Service/ConfigMap 생성 + `userlist.txt` Secret fail-closed 검증
  - [x] PgBouncer readiness/liveness/startup probe + exporter `/metrics` readiness/liveness probe 기본값
  - [x] CNPG-compatible PgBouncer parameter allowlist + operator-owned key fail-closed 검증
  - [x] `instances>1` 기본 topology spread + PodDisruptionBudget 자동 생성
  - [x] rolling update 기본값 강화 — `maxUnavailable=0`, `maxSurge=1`, `minReadySeconds=5`
  - [x] CNPG Pooler parity — `deploymentStrategy`, `serviceAccountName`, status `backendTargets/configHash`
  - [x] `pg_hba` → PgBouncer `pg_hba.conf` 렌더링 + `auth_type=hba`/`auth_hba_file` operator-owned 검증
  - [x] 사용자 제공 server/client TLS Secret 렌더링 + Secret/키 fail-closed 검증
  - [x] `type=ro` ready replica 전체 host list 렌더 + `server_round_robin=1` + `server_login_retry=2` 기본값
  - [~] PgBouncer exporter — explicit sidecar + `metrics` ServicePort + PodMonitor selector label/sample + CNPG metric prefix 기반 PrometheusRule alert 렌더 검증 완료, live Prometheus scrape/Grafana 검증 잔여
  - [ ] built-in auth user/TLS 자동 생성 reconciliation
  - [x] paused PAUSE/RESUME reconciliation — `spec.paused` → PgBouncer `SIGUSR1/SIGUSR2`, `status.paused`, Pod annotation audit
  - [x] Pooler Service 경유 `psql` smoke — 2026-05-12 `SMOKE_POOLER=1 ./hack/smoke.sh --keep` kind 실측 PASS (`quickstart` + Pooler Service `SELECT 1 = 1`, PAUSE 신규 client timeout, RESUME 후 `SELECT 1 = 1`, Deployment `2/2`)
  - [x] in-place PgBouncer config reload — `pgbouncer.parameters` 패치 후 ConfigMap `config.sha256` projection 대기 + ready Pod `SIGHUP` + Pod hash annotation audit, Deployment generation/Pod 이름 유지
- [ ] **User/DB/RBAC declarative**
  - [~] CRD `PostgresDatabase` — `spec.cluster/name/owner/ensure/tablespace/extensions/schemas/fdws/servers/privileges` + ready primary `psql` reconcile + status `applied` + `databaseReclaimPolicy=delete` finalizer + database/schema privilege grant/revoke 구현, live smoke/retain 검증 잔여
  - [~] CRD `PostgresUser` — `spec.cluster/name/ensure/login/superuser/createdb/createrole/replication/bypassrls/inherit/connectionLimit/inRoles/passwordSecretRef/disablePassword/validUntil` + ready primary `psql` reconcile + status `applied/passwordSecretResourceVersion` 구현, membership REVOKE + password Secret username match + `disablePassword` conflict fail-closed + referenced Secret update watch + `PostgresCluster.status.managedRolesStatus` 집계 완료, live smoke/password rotation SQL round-trip 검증 잔여
  - [~] Role/permission reconcile — PostgresUser role flags + membership GRANT/REVOKE + cluster-level managed role status 1차 완료, database object privilege 모델 잔여
- [ ] **Upgrade smoke** — `test/e2e/version_upgrade_e2e_test.go` 보강 (이미 골격 존재)
- [ ] **Security defaults 강화** — restricted PSA, NetworkPolicy 기본 활성
- [~] **ImageCatalog / ClusterImageCatalog** — CRD + `spec.imageCatalogRef.{apiGroup,kind,name,major}` + catalog image → StatefulSet init/main container image 반영 + image hash annotation rollout drift 추적 + catalog 변경 watch/envtest 완료. extension image volume mount, official digest catalog 공급, live rollout 실측 잔여
- [~] **Replica clusters / externalClusters** — `externalClusters[].connectionParameters` + `password` + `sslKey/sslCert/sslRootCert` + `bootstrap.pg_basebackup.source` + `replica.enabled/source` 표면, streaming standalone replica bootstrap, ordinal 0 external `pg_basebackup`, `standby.signal`/`primary_conninfo`, password passfile + TLS client/root cert conninfo, 영구 follower election 으로 local promotion 차단, fail-closed status 검증 완료. WAL archive/object-store hybrid, distributed topology demotion/promotion token, live cross-cluster drill 잔여
- [~] **Declarative hibernation** — CNPG-compatible `cnpg.io/hibernation=on/off` annotation, shard StatefulSet/PVC template 보존 + `replicas=0`, native router `replicas=0`, `status.phase=Hibernated`, condition `cnpg.io/hibernation` envtest 검증 완료. `SMOKE_HIBERNATION=1` PVC marker row 보존 + 재수화 SQL round-trip drill 경로 추가, live kind 실측 잔여
- [ ] **Release smoke test** — `hack/release-smoke-test.sh` 12/12 (mongodb 패턴)
- Verify: PrometheusRule/Grafana dashboard 렌더링 + Pooler Service 경유 `psql` 접속 + PgBouncer exporter live scrape + upgrade rolling restart 정상

### Gate G3 — 자체 sharding foundation (완충율 ~0%)

**목표**: 외부 Citus 없이 샤딩 메타데이터 자체 구현.

- [~] `ShardingMode` 필드 (none/native) 정의 — `postgrescluster_types.go`
- [~] `ShardsSpec` 정의 (초기 샤드 수/복제본/스토리지) — `postgrescluster_types.go`
- [~] Sharding plugin 인터페이스 — `internal/plugin/sharding/api.go`
- [ ] **`ShardRange` CRD** — `api/v1alpha1/shardrange_types.go` 신규
  - [ ] hash range / list / range 정책 분기
  - [ ] 메타데이터 저장소 (Postgres system catalog 또는 별도)
- [ ] **`pg-router` 서비스 PoC** — `cmd/pg-router/` 신규
  - [ ] SQL parser (libpg_query 또는 자체)
  - [ ] Shard placement lookup
  - [ ] Connection routing (libpq passthrough)
- [ ] **Manual shard placement** — `ShardRange.Spec.PlacementHints`
- [ ] **GitOps drift guard** — sharding metadata vs 실제 placement 불일치 감지
- Verify: 2-shard 클러스터에서 `pg-router` 경유 query 가 올바른 샤드로 라우팅

### Gate G4 — Online resharding (완충율 ~0%)

**목표**: 데이터 손실 없는 split/rebalance.

- [ ] **`ShardSplitJob` CRD** — `api/v1alpha1/shardsplitjob_types.go` 신규
- [ ] **7-step e2e** 시나리오
  - [ ] 1. 스냅샷 + WAL 캡처
  - [ ] 2. 대상 샤드 부트스트랩
  - [ ] 3. 초기 복사
  - [ ] 4. CDC 따라잡기
  - [ ] 5. Cutover (쓰기 차단 최소 윈도우)
  - [ ] 6. 라우팅 갱신
  - [ ] 7. 원본 정리
- [ ] **Cutover rollback / forward-only** 검증
- Verify: split 중 데이터 무결성 (checksum) + cutover 윈도우 측정 + 롤백 가능

### Gate G5 — Distributed SQL (완충율 ~0%)

**목표**: cross-shard query/transaction 의 *명확한 지원 범위* 정의.

- [ ] **Scatter-gather** 쿼리 경로
- [ ] **2PC / saga** 분산 트랜잭션 선택
- [ ] **Isolation matrix** 문서화 — 어느 격리 수준이 어디까지 보장되는가
- [ ] **벤치마크** — sysbench / pgbench 변형
- Verify: 격리 수준별 anomaly 발생/회피 표 + 벤치마크 수치

### Gate G6 — 1.0.0 GA (완충율 ~15%)

**목표**: 상용 제품 수준.

- [x] E2E 기초 — `test/e2e/`
- [ ] **장기 soak 테스트** — 7일 이상 무중단 운영
- [ ] **Chaos engineering** — pod kill / network partition / disk pressure
- [ ] **Restore rehearsal** — 주기적 백업 복원 자동화 + 검증
- [ ] **Upgrade matrix** — N → N+1 / N → N+2 / minor patch 모두 검증
- [ ] **SBOM + signing** — SPDX SBOM, cosign signature
- [ ] **Docs / runbooks 완비**
  - [ ] HA / backup / restore / upgrade / security / migration runbook
- Verify: 7일 soak PASS + chaos 시나리오 N건 PASS + SBOM 첨부 + 모든 runbook 존재

## 명시적 비대상 (Non-Goals)

- ❌ PGO fork / CNPG wrapper / Patroni bundle / Citus extension bundle
- ❌ 외부 시스템 CRD 를 그대로 재노출하는 compatibility shell
- ❌ 금지 라이선스 프로젝트의 코드 복사·번역·포팅
- ❌ 아직 검증되지 않은 HA/backup 기능을 `1.0.0` 또는 `production-ready` 로 표기
- ❌ **GitHub Actions 필수 release gate** — RFC 0002 글로벌. 로컬 4 계층 위임.
- ❌ **시간 기반 로드맵 deadline** — 글로벌 §workflow.md.

## Archive Policy

`docs/**/_archive/v0.x/` 문서는 *과거 판단의 증거* 로 보존한다. archive 안의 "PGO-class + Citus 1급 + Plugin SDK" 표현은 *현행 메시지 아님*. 새 구현·문서는 ADR-0001, ADR-0003, 본 Roadmap 을 기준으로 한다.

## 변경 이력

| Date | Change |
|---|---|
| 2026-05-11 | 전면 재작성 — Gate 별 sub-task 체크리스트 입자도 도입, 완충율 표기, 날짜 기반 표현 0건 정렬, 루트 ROADMAP.md 와 본문 1:1 동기 |
| 2026-05-07 | `0.3.0-alpha.3` 배포, GHCR public pull 전환, legacy staging operator 제거, 외부 시스템 내장 금지 원칙 명시 |
