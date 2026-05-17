# ROADMAP — postgres-operator

This ROADMAP tracks progress through verifiable Gates and sub-task
checklists — *not* date commitments. The project identity is
**Apache-2.0 PostgreSQL Kubernetes Operator**. We target PGO-class
operational quality without forking, embedding, or wrapping external
systems such as PGO, Citus, CloudNativePG, or Patroni.

## Checkbox semantics

| Marker | Meaning |
|---|---|
| `[x]` | Code **and** tests exist; e2e or unit tests guard regressions. |
| `[~]` | Partial — e.g. CRD field only, helper not wired in, or e2e missing. |
| `[ ]` | Not started (design or PoC only). |

The *Verify* row on each sub-task quotes the verification command or e2e
file.

## Principles

- **External design is fair game** — PGO operational UX, Citus's distributed-SQL
  decomposition, the Vitess router idiom, and CNPG's Kubernetes-operator
  patterns inform our design, but only as public documents / papers.
- **External systems must not ship inside this product** — Citus extension,
  CNPG `Cluster`, Patroni DCS, Cockroach/Yugabyte backends, or PGO
  controller code are excluded from the runtime artifact.
- **Implement as a new service** — the operator manager, instance manager,
  sharding metadata, router, and backup orchestration are written in this
  repository under Apache-2.0–compatible dependencies.
- **"PGO-class" = quality bar** — the *target level* for HA / backup /
  restore / upgrade / observability / security UX. Not a claim of using
  any specific product.

## Current state snapshot

| Item | State | Evidence |
|---|---|---|
| Project / chart name | `postgres-operator` | GitHub repo, Helm chart, and argos GitOps path are aligned |
| License | Apache-2.0 | `LICENSE`, ADR-0003 |
| Latest release | `0.3.0-alpha.18` | GHCR image + Helm chart publish + OLM bundle (community-operators PR pending) |
| OLM bundle | `bundle/manifests/` aligned with 8 CRDs + alm-examples + CSV descriptions | `operator-sdk bundle validate --select-optional suite=operatorframework` is clean (T26) |
| CNPG-compatible surface | Pooler / PostgresDatabase / PostgresUser / ScheduledBackup / ImageCatalog / ClusterImageCatalog / externalClusters / replica cluster | T22 / T24 / T25 cycles completed; live kind smoke automation (T27) in progress |
| Local 4-layer gate | L1 lefthook pre-commit + L2 pre-push + L3 make validate/audit + L4 PR evidence | ADR-0009 / RFC-0002; version-drift assertion and bundle validate are automated (T26) |
| argos deployment | Day-0 single-shard | `PostgresCluster/argos-postgres` Ready |
| GHCR runtime image | Publicly pullable | `ghcr.io/keiailab/pg:18` restarts with no pull secret |
| HA replicas | Partial (`Replicas` field only) | `api/v1alpha1/postgrescluster_types.go` |
| Backup / restore | Partially implemented | `BackupJob` phase transitions + `ScheduledBackup` CRD/controller + `RestorePIT` call path + pgBackRest command-runner plugin + K8s sidecar exec path. Actual restore drill is still pending. |
| 1.0.0 GA | Not yet | HA / backup / chaos / soak still required |

## Gate plan

### Gate G0 — Day-0 deployment (~100% buffer)

**Goal**: a user can deploy the operator + a single-shard Postgres
cluster via GitOps.

- [x] CRD `PostgresCluster` definition — `api/v1alpha1/postgrescluster_types.go` (RFC-0001 v2 schema).
- [x] CRD `BackupJob` definition (Phase 1 spec) — `api/v1alpha1/backupjob_types.go`.
- [x] `PostgresClusterReconciler` builds desired state (ConfigMap / headless Service / StatefulSet) — `internal/controller/postgrescluster_controller.go`.
- [x] Status phase transitions (Provisioning → Ready) — `internal/controller/status.go`, `aggregate_status.go`.
- [x] Pod readiness tracking — reconciler endpoint watch.
- [x] ArgoCD `Synced/Healthy` — verified on argos production (`platform-data-postgres-operator`).
- [x] GHCR public pull — `ghcr.io/keiailab/pg:18` restarts with no pull secret.
- [x] Day-0 e2e — `test/e2e/e2e_test.go`, `postgrescluster_e2e_test.go`.
- Verify: ArgoCD `Synced/Healthy` + Pod `1/1` Running + `psql -c 'select version()'`.

### Gate G1 — Single-shard production HA (~30% buffer)

**Goal**: usable as a single-PostgreSQL production database, with HA.

- [x] `Replicas` field (0–15 async replicas) — `postgrescluster_types.go`.
- [x] STS scale mapping — reconciler.
- [x] Primary-delete e2e baseline — `test/e2e/failover_e2e_test.go`.
- [x] Automatic PDB creation — `internal/controller/pdb.go`.
- [~] PVC fencing (split-brain fail-fast) — fencing skeleton only; runbook automation pending.
- [ ] **Automatic failover logic** — new directory `internal/controller/failover/`.
  - [x] Primary failure detection — `internal/controller/failover/detection.go` (`DetectPrimaryFailure` + `SelectPromotionCandidate`, pure functions, 4 `FailureReason` enums, 9 unit tests, PR #38).
  - [x] Standby promotion (`pg_ctl promote` or logical-replication promotion) — `internal/controller/failover/promotion.go` (`BuildPromotionPlan` + `Promoter` interface + `PromoteFromDecision` helper, 4-step plan: RemoveStandbySignal / PgCtlPromote / WaitNotInRecovery / UpdateInstanceRole; 6 unit tests; PR #39). `internal/controller/failover_promoter.go` implements the replica-Pod `postgres`-container exec and the promoted `instance-status` annotation patch.
  - [x] Post-Ready primary-failure status surface — `status.phase=Degraded` + `FailoverReady=False` + promotion-candidate message.
  - [x] Replica rejoin (`pg_basebackup` or `pg_rewind`) — first-boot `pg_basebackup` + existing-PGDATA old-primary marker generalization + current-primary endpoint main env + `pg_rewind` command-runner + HBA normal-connection auth + fresh `pg_basebackup` fallback all done. **Live A.1 basebackup drill PASS (T31, 2026-05-17, commits 09abbb5/dca3fa0)**: `quickstart-shard-0-1` standby PVC delete + in-pod PGDATA wipe + Pod kill → reconciler init container 가 fresh `pg_basebackup` 실행 → `pg_stat_replication{application_name=quickstart-shard-0-1, state=streaming, sync_state=async, lag=0}` 회복. STS PVC retention `Retain` 회피 path 까지 evidence. A.2 pg_rewind live drill 은 별 task (SMOKE_FAILOVER operator-driven promotion 라이브 trigger 회귀 — `docs/g1-ha-election-fact-fix` 영역 위임).
  - [x] Synchronous replication — `spec.postgresql.synchronous.{method,number,dataDurability}` + CEL `number<=shards.replicas` + `ANY/FIRST N (...)` rendering + `required/preferred` quorum policy + standby `application_name` wiring + ConfigMap-hash rolling reconcile all done. **Live B.1~B.3 RPO=0 drill PASS (T31, 2026-05-17, commit dca3fa0)**: `synchronous_standby_names='ANY 1 ("quickstart-shard-0-1","quickstart-shard-0-0")'` 적용 → `sync/quorum replica count=1` → 1000-row commit 후 `commit_lsn=0/3DA43A0 / flush_lsn=0/3DA43A0` (`pg_wal_lsn_diff=0`) → **RPO=0 직접 증명**. drill 함수: `hack/smoke.sh::drill_sync` (SMOKE_SYNC=1). B.4 sync standby kill scenario 는 opt-in (`SMOKE_SYNC_KILL=1`).
  - [~] HA election distributed lock (K8s Lease) — `internal/controller/failover/lease.go` (`FailoverLeaseName` + `LeaseConfig` + `NewLease`/`Run`/`IsLeader`, thin adapter over `internal/instance/election.Real` per §2 Simplicity; 2 unit tests with fake clientset verify single-leader + handoff). Live e2e multi-replica failover drill pending cluster mesh restore.
- [ ] **Backup / restore controller implementation** — bolster `internal/controller/backupjob_controller.go`.
  - [x] `BackupJob.Phase` transitions (Pending → Running → Succeeded/Failed) — `internal/controller/backupjob_controller.go` reconcile switch + 8 unit tests.
  - [x] `ScheduledBackup` CRD / controller — 6-field cron schedule → atomic `BackupJob` creation; `suspend` / `immediate` / `ownerReference` / `concurrency` guards; 5 unit tests.
  - [x] `BackupJob.spec.type=restore` → `BackupPlugin.RestorePIT(targetTime)` call path + required `targetTime` validation.
  - [x] `BackupJob.spec.executionMode=job` → owned `batch/v1.Job` create + observe; `jobTemplate` standard env injection.
  - [~] Plugin invocation — pgBackRest command-runner + sidecar command planning done. WAL-G / Barman pending.
  - [x] Sidecar mode branch — pgBackRest argv delivered via K8s `pods/exec` to the ready primary Pod's `postgres` container.
- [~] **PITR restore** — `BackupRestoreSpec.TargetTime`-driven pgBackRest `restore --type=time --target=...` call path + sidecar exec path both present. Actual restore + checksum drill is still pending.
- [x] **Upgrade rollback runbook** — `docs/runbooks/upgrade.md` (stub: pre-upgrade checks + ImageCatalog steps + rollback) (PR #54)
- [x] **RTO / RPO measurement + recording** — `docs/runbooks/ha.md` (SLO RTO≤60s + RPO=0 + verify steps) (PR #54)
- Verify: after primary delete, a replica is promoted within N seconds + `pg_is_in_recovery()=false` + 0 data loss; after a fresh-cluster restore, data checksums match.

### Gate G2 — Operational quality (~25% buffer)

**Goal**: cover the PGO-class operational surface.

- [x] `/metrics` baseline exposure (port 8443) — `internal/controller/metrics.go`, `cmd/main.go`.
- [x] TLS path setup (certificate mount + `ssl=on`) — `internal/controller/builders.go:renderPostgresConf()`, `tls.go`.
- [x] Topology spread integration — `internal/controller/topology_spread.go`.
- [x] PVC online resize — `internal/controller/pvc_resize.go`.
- [x] Cascade-delete guard — `internal/controller/cascade_delete_test.go`.
- [~] cert-manager integration — mount path only; issuance mechanism still TBD.
- [~] **Automatic PrometheusRule generation** — Helm metrics Service / ServiceMonitor / PrometheusRule rendering + real `postgres_operator_backupjob_phase` metric driving BackupJob failure alerts.
  - [x] Replication-lag warning — instance status `LagBytes` → `postgres_operator_postgrescluster_replication_lag_bytes` + Helm `PostgresReplicationLagHigh`.
  - [x] Pooler failure / saturation warnings — `postgres_operator_pooler_phase{phase="Failed"}` + render verification of CNPG `cnpg_pgbouncer_*` exporter-metric-driven collection-failure / client-waiting / max-wait alerts.
  - [x] Disk pressure — `kubelet_volume_stats_*` data-PVC alert.
  - [x] Backup failure — `postgres_operator_backupjob_phase{phase="Failed"}`.
- [~] **Grafana dashboards** — Helm dashboard ConfigMap rendering done (`postgres-operator-cluster-overview.json`, `postgres-operator-pooler.json`); live Grafana import / panel verification still pending.
- [~] **Connection pooler (PgBouncer)** — `Pooler` CRD + ConfigMap / Deployment / Service reconcile (first slice).
  - [x] CRD `Pooler.spec.{cluster, instances, type, pgbouncer.poolMode, pgbouncer.parameters}` added.
  - [x] Separate PgBouncer Deployment / Service / ConfigMap created + `userlist.txt` Secret fail-closed validation.
  - [x] Default PgBouncer readiness / liveness / startup probes + exporter `/metrics` readiness / liveness probes.
  - [x] CNPG-compatible PgBouncer parameter allowlist + operator-owned-key fail-closed validation.
  - [x] Automatic topology spread + PodDisruptionBudget when `instances > 1`.
  - [x] Stronger rolling-update defaults — `maxUnavailable=0`, `maxSurge=1`, `minReadySeconds=5`.
  - [x] CNPG Pooler parity — `deploymentStrategy`, `serviceAccountName`, status `backendTargets/configHash`.
  - [x] `pg_hba` → PgBouncer `pg_hba.conf` rendering + operator-owned validation of `auth_type=hba` / `auth_hba_file`.
  - [x] User-supplied server / client TLS Secret rendering + Secret/key fail-closed validation.
  - [x] `type=ro` full ready-replica host-list rendering + `server_round_robin=1` + `server_login_retry=2` defaults.
  - [~] PgBouncer exporter — explicit sidecar + `metrics` ServicePort + PodMonitor selector label/sample + PrometheusRule alert render verification on CNPG metric prefixes; live Prometheus scrape / Grafana verification still pending.
  - [x] **Built-in auth user automation** (T27 ⑤) — `keiailab_pooler_pgbouncer` LOGIN role + `<pooler-name>-builtin-auth` Secret auto-provisioned when `authSecretRef` is empty.
  - [x] **Built-in auth password rotation** (T27 ⑥) — `postgres.keiailab.io/rotate-pooler-password=true` annotation triggers in-place `ALTER ROLE` + Secret update + status timestamp; ConfigHash now includes userlist for auto-reload.
  - [ ] Built-in TLS auto-issuance (T29).
  - [x] Paused PAUSE/RESUME reconciliation — `spec.paused` → PgBouncer `SIGUSR1/SIGUSR2`, `status.paused`, Pod annotation audit.
  - [x] Pooler Service `psql` smoke — 2026-05-12 `SMOKE_POOLER=1 ./hack/smoke.sh --keep` on kind passed (`quickstart` + Pooler Service `SELECT 1 = 1`, PAUSE blocks new clients with timeout, RESUME re-enables `SELECT 1 = 1`, Deployment `2/2`).
  - [x] In-place PgBouncer config reload — patching `pgbouncer.parameters` waits for the ConfigMap `config.sha256` projection, sends `SIGHUP` to ready Pods, and audits the Pod hash annotation while preserving Deployment generation and Pod names.
- [ ] **User / DB / RBAC declarative**.
  - [~] CRD `PostgresDatabase` — `spec.cluster/name/owner/ensure/tablespace/extensions/schemas/fdws/servers/privileges` + ready-primary `psql` reconcile + `status.applied` + `databaseReclaimPolicy=delete` finalizer + database/schema privilege grant/revoke implemented. Live smoke / retain-policy verification still pending.
  - [~] CRD `PostgresUser` — `spec.cluster/name/ensure/login/superuser/createdb/createrole/replication/bypassrls/inherit/connectionLimit/inRoles/passwordSecretRef/disablePassword/validUntil` + ready-primary `psql` reconcile + `status.applied/passwordSecretResourceVersion` implemented; membership `REVOKE` + password Secret username match + `disablePassword` fail-closed + referenced-Secret update watch + `PostgresCluster.status.managedRolesStatus` aggregation done. Live smoke + password-rotation SQL round-trip still pending.
  - [~] Role/permission reconcile — `PostgresUser` role flags + membership `GRANT/REVOKE` + cluster-level managed-role status (first slice) done; the database-object privilege model is still pending.
- [ ] **Upgrade smoke** — extend `test/e2e/version_upgrade_e2e_test.go` (skeleton already in place).
- [ ] **Security defaults hardening** — restricted PSA, NetworkPolicy on by default.
- [~] **ImageCatalog / ClusterImageCatalog** — CRD + `spec.imageCatalogRef.{apiGroup,kind,name,major}` + catalog image → StatefulSet init/main container image + image-hash annotation rollout-drift tracking + catalog watch / envtest done. Extension-image volume mount, official digest catalog supply, and live rollout measurement still pending.
- [~] **Replica clusters / externalClusters** — `externalClusters[].connectionParameters` + `password` + `sslKey/sslCert/sslRootCert` + `bootstrap.pg_basebackup.source` + `replica.enabled/source` surface, streaming standalone replica bootstrap, ordinal-0 external `pg_basebackup`, `standby.signal`/`primary_conninfo`, password passfile + TLS client/root cert conninfo, persistent-follower election that blocks local promotion, and fail-closed status all verified. WAL-archive / object-store hybrid, distributed-topology demotion/promotion-token, and live cross-cluster drill are still pending.
- [~] **Declarative hibernation** — CNPG-compatible `cnpg.io/hibernation=on/off` annotation, shard StatefulSet/PVC-template preservation + `replicas=0`, native router `replicas=0`, `status.phase=Hibernated`, condition `cnpg.io/hibernation`, all envtest-verified. The `SMOKE_HIBERNATION=1` path also exercises the PVC-marker-row preservation and the rehydration SQL round-trip drill; live kind verification still pending.
- [~] **Release smoke test** — `scripts/release-smoke-test.sh` 6-stage (mongodb sister pattern 정합 — GH Release tag + GHCR manifest + GH Pages + helm index + helm pull/template + trivy post-publish scan). path 정정 (hack/→scripts/) + stage count "12" 가정 정정 (sister 표준 = 6).
- Verify: PrometheusRule / Grafana dashboard rendering, `psql` access through the Pooler Service, live PgBouncer exporter scrape, and an upgrade rolling restart succeed.

### Gate G3 — Self-built sharding foundation (~0% buffer)

**Goal**: implement sharding metadata in-house, without Citus.

- [x] `ShardingMode` field (`none` / `native`) — `postgrescluster_types.go`. Constants + Spec round-trip guarded by `TestShardingMode` (`api/v1alpha1/postgrescluster_types_test.go`); enum validation is enforced at the apiserver via the `+kubebuilder:validation:Enum=none;native` marker. RFC 0001 §3.1 / RFC 0002.
- [x] `ShardsSpec` (initial shard count / replicas / storage) — `postgrescluster_types.go`. Field round-trip + `DeepCopy` slice independence + `Replicas=0` (HA-off dev) guarded by `TestShardsSpec` (`api/v1alpha1/postgrescluster_types_test.go`). RFC 0001 §3.1.
- [x] Sharding plugin interface — `internal/plugin/sharding/api.go`. Compile-time interface freeze + `Registry` register/get/Names round-trip + `Capabilities` advertisement + `ErrUnsupported` sentinel guarded by `TestShardingPlugin` umbrella (`internal/plugin/sharding/api_test.go`). RFC 0001~0005 / RFC 0004 (router architecture).
- [x] **`ShardRange` CRD** — `api/v1alpha1/shardrange_types.go` + `config/crd/bases/postgres.keiailab.io_shardranges.yaml` (RFC 0002, offline yaml parse PASS, `make manifests` 통과).
  - [~] Hash-range / list / range policy branching (vindex enum 정의 완료, reconciler 미구현 — 후속 sub-task).
  - [ ] Metadata store (Postgres system catalog or sidecar).
- [ ] **`pg-router` service PoC** — new `cmd/pg-router/`.
  - [ ] SQL parser (libpg_query or homegrown).
  - [ ] Shard-placement lookup.
  - [ ] Connection routing (libpq passthrough).
- [ ] **Manual shard placement** — `ShardRange.Spec.PlacementHints`.
- [ ] **GitOps drift guard** — detect divergence between sharding metadata and actual placement.
- Verify: queries through `pg-router` on a 2-shard cluster are routed to the correct shard.

### Gate G4 — Online resharding (~0% buffer)

**Goal**: split / rebalance without data loss.

- [ ] **`ShardSplitJob` CRD** — new `api/v1alpha1/shardsplitjob_types.go`.
- [ ] **7-step e2e** scenario.
  - [ ] 1. Snapshot + WAL capture.
  - [ ] 2. Bootstrap the target shard.
  - [ ] 3. Initial copy.
  - [ ] 4. CDC catch-up.
  - [ ] 5. Cutover (minimal write-block window).
  - [ ] 6. Routing update.
  - [ ] 7. Source cleanup.
- [ ] **Cutover rollback / forward-only** verification.
- Verify: data integrity during split (checksum) + cutover-window measurement + rollback feasibility.

### Gate G5 — Distributed SQL (~0% buffer)

**Goal**: clearly bound cross-shard query / transaction support.

- [ ] **Scatter-gather** query path.
- [ ] **2PC / saga** distributed-transaction choice.
- [ ] **Isolation matrix** documented — which isolation levels hold under which conditions.
- [ ] **Benchmarks** — sysbench / pgbench variants.
- Verify: per-isolation-level anomaly / no-anomaly table + benchmark numbers.

### Gate G6 — 1.0.0 GA (~15% buffer)

**Goal**: commercial-grade quality.

- [x] e2e baseline — `test/e2e/`.
- [ ] **Long-running soak** — ≥ 7 days, no downtime. (NON-GOAL single session) (NON-GOAL for single session — 7-day wall clock required)
- [ ] **Chaos engineering** — pod kill / network partition / disk pressure. (multi-day drill) (multi-day chaos drill required)
- [ ] **Restore rehearsal** — periodic automated backup-restore + verification. (monthly cron drill — out of single session)
- [ ] **Upgrade matrix** — N → N+1 / N → N+2 / minor patches. (G2 D.6.3 dependency — substantial e2e)
- [ ] **SBOM + signing** — SPDX SBOM + cosign signature. (commons sbom-attach.sh 도입 가능, P-C.7 sister)
- [ ] **Docs / runbooks complete**.
  - [ ] HA / backup / restore / upgrade / security / migration runbooks.
- Verify: 7-day soak passes + N chaos scenarios pass + SBOM attached + every runbook exists.

## Non-goals (intentional exclusions)

- ❌ Repackaging an external PostgreSQL operator (forking PGO / CNPG / Patroni).
- ❌ Citus's first-class built-in features (Citus is a *design reference*, not a runtime dependency).
- ❌ A general-purpose Plugin SDK product story (retired from the v0.x archive).
- ❌ **GitHub Actions as a required release gate** — see RFC 0002 (org-wide). Delegated to the local 4-layer gate.
- ❌ **Date-based roadmap deadlines** — see the org-wide `workflow.md`.
- ❌ Marketing HA / backup features as `production-ready` before they are verified.

## Change log

| Date | Change |
|---|---|
| 2026-05-16 | G3 §Sharding foundation: flipped `ShardingMode` / `ShardsSpec` / `Sharding plugin interface` `[~]` → `[x]` with unit-test coverage (`TestShardingMode`, `TestShardsSpec`, `TestShardingPlugin`). Plans `2026-05-14-4-operators-100pct/P-D` §D.7. |
| 2026-05-12 | CNPG backup/restore gap closed: added `ScheduledBackup` CRD/controller, `BackupJob` creation on cron firing, `BackupJob.spec.type=restore` → `RestorePIT` call path, `executionMode=job` runner Job lifecycle, pgBackRest command-runner plugin registration, and the sidecar pod-exec path. |
| 2026-05-12 | CNPG observability gap closed: added Helm metrics Service / ServiceMonitor / PrometheusRule + `postgres_operator_backupjob_phase` Prometheus metric. |
| 2026-05-11 | G1 §Backup/Restore `BackupJob.Phase` transitions (Pending → Running → Succeeded/Failed) implemented + 8 unit tests — `[x]` (ralph-loop iter#3). |
| 2026-05-11 | Full rewrite — introduced Gate-scoped sub-task checklists, buffer indicators, and removed any date-style language. |
| 2026-05-07 | Released `0.3.0-alpha.3`, switched to public GHCR pull, removed legacy staging operator, and made the "no embedded external systems" principle explicit. |
