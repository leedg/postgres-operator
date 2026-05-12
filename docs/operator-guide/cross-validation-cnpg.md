# Cross-validation report: keiailab/postgres-operator vs CloudNativePG

> Date: 2026-05-03
> Environment: kind v0.31.0 / k8s v1.35.0 / single-node arm64 / Docker 29.4.1
> Both operators + one instance each deployed concurrently on the same node.

> **This report is not marketing material; it records the objective
> differences at the alpha stage.** Claiming our operator is superior on
> every axis is not true; the measurement itself exposed **three
> production bugs on our side.**

## TL;DR

| Dimension | CNPG 1.27.0 | keiailab 0.3.0-alpha | Δ |
|---|---|---|---|
| **Time-to-Ready** (CR apply → Pod Ready) | **24 s** | **62 s** | +158% (ours slower) |
| Pod RSS sum (single instance) | 188 MB | **144 MB** | −23% |
| Operator-manager RSS | 67 MB | **50 MB** | −25% |
| PID 1 RSS inside the Pod | 60 MB (manager) | **28 MB** (instance) | −53% |
| Manager image (host) | 169 MB | **106 MB** | −37% |
| Manager image (kind compressed) | 36.8 MB | **30.3 MB** | −18% |
| PG runtime image (host) | 1.04 GB | **675 MB** | −35% |
| PG runtime image (kind compressed) | 264 MB | **163 MB** | −38% |
| Go LoC (real code, tests excluded) | 94,130 | **5,220** | −94% |
| Package count | 171 | **20** | −88% |
| `go.mod` direct deps | 45 | **8** | −82% |
| CRD YAML lines | 18,955 | **1,778** | −91% |
| Minimum CR YAML lines | **8** | 13 | +63% (ours verbose) |

**Reading**:

- **Our resource footprint is smaller** — a natural consequence of the
  alpha stage (fewer features).
- **Time-to-Ready is roughly 2.6× slower on our side** — conservative
  `readinessProbe initialDelaySeconds=30` plus a `waitSupReady` polling
  loop. Reducible (cycle 7 follow-up).
- **User cognitive load on the CR**: CNPG 8 lines vs ours 13 lines.
  CNPG's minimum CR is shorter because it lacks sharding/router fields;
  ours is explicit (`shardingMode=none`).
- **Code/CRD size delta is a proxy for GA distance** — we are smaller
  because backup, monitoring, replication automation, etc. are missing.

## Measurement method

### Environment

```
kind create cluster --name pg-bench
# 4 images loaded into the kind node:
# - ghcr.io/cloudnative-pg/cloudnative-pg:1.27.0
# - ghcr.io/cloudnative-pg/postgresql:18-bookworm
# - local/postgres-operator:bench  (cmd/main.go bench build)
# - local/pg:18 → ghcr.io/keiailab/pg:18 retag
```

### CNPG scenario

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata: {name: cnpg-bench}
spec:
  instances: 1
  imageName: ghcr.io/cloudnative-pg/postgresql:18-bookworm
  storage: {size: 1Gi}
```

Wall clock from `kubectl apply -f cnpg-cr.yaml` to
`Pod conditions[Ready]=True`.

### Ours scenario

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata: {name: ours-bench, namespace: default}
spec:
  postgresVersion: "18"
  shardingMode: none
  shards:
    initialCount: 1
    replicas: 0
    storage: {size: 1Gi}
```

Measured at the same wall-clock window on the same node.

### Memory measurement

Sum the `VmRSS` from `/proc/[pid]/status` of every process in the Pod:

```sh
kubectl exec <pod> -c postgres -- bash -c \
  'awk "/^VmRSS:/ {s+=\$2} END {print s\" KB\"}" /proc/[0-9]*/status'
```

(The distroless container has no `ps`, so we parse `/proc` directly.)

### Operator-manager memory

`crictl inspect` on the kind node host → extract PID → read VmRSS from
`/proc/<pid>/status`.

## Three production bugs surfaced by the measurement

The real value of this cross-validation was not the resource numbers but
exposing that **"alpha-deployable" was vaporware**. All unit tests,
envtests, and `make validate` runs prior to this session passed, yet on a
real Kubernetes cluster three issues blocked the deployment simultaneously.

### Bug 1 — RBAC privilege-escalation block

Kubernetes refuses to let an operator grant another SA permissions it
does not itself hold (privilege-escalation prevention). Our operator
ClusterRole lacked `persistentvolumeclaims/patch`, but `buildInstanceRole`
tried to grant it:

```
roles.rbac.authorization.k8s.io "ours-bench-instance" is forbidden:
  user "system:serviceaccount:postgres-operator-system:..." is attempting
  to grant RBAC permissions not currently held:
  {APIGroups:[""], Resources:["persistentvolumeclaims"], Verbs:["get" "list" "watch" "patch"]}
```

**Fix**: add a `+kubebuilder:rbac` marker and the
`persistentvolumeclaims [get,list,watch,patch]` rule to the Helm chart's
RBAC.

CNPG does not grant the same fencing pattern through a separate
ClusterRole — they prevent split-brain via webhooks + finalizers. We use
PVC-label fencing from RFC 0003, so we need this permission.

### Bug 2 — plugin auto-register blocks vanilla-PG boot

`cmd/main.go` was unconditionally registering 6 extensions (pgaudit,
pgvector, pgcron, pgnodemx, postgis, setuser) into the plugin Registry.
That forced `renderPostgresConf` to emit
`shared_preload_libraries=pgaudit,pgvector,...` into every cluster's
`postgresql.conf`. The official `postgres:18-bookworm` image lacks those
`.so`s:

```
2026-05-03 12:21:28.728 GMT [18] FATAL: could not access file "pgaudit": No such file or directory
2026-05-03 12:21:28.728 GMT [18] LOG:  database system is shut down
```

**Fix**: make every `Register` call dormant (`_ = X.Register`). A proper
per-cluster `spec.extensions` rewire is its own RFC. For now we only
guarantee a vanilla-PG boot.

CNPG pre-builds many extensions into the image and relies on the user
opting in through `spec.postgresql.shared_preload_libraries`.

### Bug 3 — promote race + no already-primary handling

Two sub-bugs:

3a) When `OnStartedLeading` grants leadership before the postgres unix
socket starts listening, the instance manager calls `sup.Promote()` and
gets:

```
dial unix /var/run/postgresql/.s.PGSQL.5432: connect: no such file or directory
```

3b) A freshly `initdb`-ed database is already the primary (no
`recovery.signal`). `pg_promote(true, 30)` returns `false`; we treated
that as an error → `fencingErrCh` → `main` exited with code 2 →
CrashLoopBackOff.

**Fix**:

- New `waitSupReady` helper — poll `IsReady` for 30 s before calling
  `Promote`.
- `Promote` first checks `pg_is_in_recovery()`; if `!inRecovery`, returns
  `nil` immediately (idempotent).

CNPG's manager owns PG bootstrap end-to-end (initdb + pg_basebackup +
standby/primary selection), so this race is structurally absent.

## Feature matrix (alpha limits made explicit)

> 2026-05-12: the Pooler / Monitoring rows were re-checked against
> CNPG 1.29's official documentation for the Pooler CRD, the PgBouncer
> parameter map, the Service template, and the exporter surface.

| Feature | CNPG 1.29 | Ours 0.3.0-alpha | Notes |
|---|---|---|---|
| Single-shard PG (primary only) | ✅ | ✅ | Both deployable. |
| Image catalog | ✅ `ImageCatalog` + `ClusterImageCatalog` | ⚠️ CRD + rollout path | `ImageCatalog` / `ClusterImageCatalog` CRDs, CNPG-compatible `spec.imageCatalogRef.{apiGroup,kind,name,major}`, namespaced / cluster-scoped lookup, catalog image → shard StatefulSet init/main container image, image-hash annotation rollout-drift tracking, catalog watch + envtest verified. Extension-image volume mount, official digest-catalog supply, and live rollout measurement still pending. |
| Streaming replication (replicas ≥ 1) | ✅ | ⚠️ partial | First-boot standby `pg_basebackup`, `primary_conninfo application_name=<pod>` wiring, and status-based replica observation implemented. Live long-run / partition verification still pending. |
| Synchronous replication | ✅ `.spec.postgresql.synchronous` | ⚠️ partial | `spec.postgresql.synchronous.{method,number,dataDurability}` CRD + CEL `number<=shards.replicas`, PostgreSQL `ANY/FIRST N (...)` rendering, `required` uses every desired/observed Pod name, `preferred` uses only ready replicas and lowers the quorum. ConfigMap hash → StatefulSet rolling reconcile applied. Live commit / RPO=0 drill still pending. |
| Declarative hibernation | ✅ `cnpg.io/hibernation=on/off` | ⚠️ envtest + smoke path | CNPG-compatible annotation supported. `on` preserves shard StatefulSet / PVC-template ownership, sets the database Pod `replicas=0`, the native router `replicas=0`, `status.phase=Hibernated`, `condition cnpg.io/hibernation=True`. `off` or annotation removal rehydrates to the desired replicas. `SMOKE_HIBERNATION=1` was extended to create a PVC marker row → hibernate → PVC preservation → rehydrate → marker `SELECT`. Live kind verification still pending. |
| Automated failover | ✅ (Patroni-less, native) | ⚠️ partial | Instance-manager election / promote smoke previously passed + controller-side pure failover decision/helper + post-Ready primary failure surfaced as `status.phase=Degraded`, `FailoverReady=False`. Controller-layer promotion goes through replica-Pod `postgres` container exec (remove `standby.signal` → `pg_ctl promote` → `pg_is_in_recovery()` polling → `instance-status` primary annotation patch). Old-primary `standby.signal` generalized + current `PRIMARY_ENDPOINT` injected into the main container + `pg_rewind --target-pgdata ... --source-server ...` command-runner + fresh `pg_basebackup` fallback on rewind failure. Rejoin-prep failure surfaces in `status.shards[].replicas[].reason/message`. Network-partition / STONITH + live pg_rewind verification still pending. |
| `pg_basebackup` → standby join / `pg_rewind` rejoin | ✅ | ⚠️ partial | First-boot standby `pg_basebackup` + existing-PGDATA old-primary marker + `pg_rewind`-based former-primary rejoin path implemented. New clusters carry `wal_log_hints=on` for rewind feasibility. To allow the `pg_rewind --source-server` normal connection, the alpha HBA places a `postgres` normal-connection trust line before the SCRAM host rules. On rewind failure, the data dir is replaced via fresh `pg_basebackup`, with original-data-dir restore-on-failure. Live e2e re-verification still pending. |
| Backup (Barman / pgBackRest) | ✅ Barman built-in + scheduled / on-demand | ⚠️ CRD/controller partial | `BackupJob` phase transitions + `ScheduledBackup` cron → BackupJob + `RestorePIT` call path + `executionMode=job` runner Job observation + pgBackRest command-runner / sidecar exec path. Barman / restore drill still pending. |
| PITR | ✅ | ⚠️ controller/plugin path only | The pgBackRest `restore --type=time --target=...` call path and the sidecar exec path are present; actual restore + checksum drill is still pending. |
| Connection pooler (PgBouncer) | ✅ Pooler CRD + PgBouncer Deployment | ⚠️ Pooler CRD/controller partial | `instances`, `type=rw/ro`, `pgbouncer.poolMode`, `parameters`, `pg_hba`, Pod template, Service template, `deploymentStrategy`, `serviceAccountName`, ConfigMap/Deployment/Service generation, auth-Secret fail-closed validation, PgBouncer HBA-file rendering + operator-owned `auth_type=hba` / `auth_hba_file` validation, user-supplied server/client TLS Secret rendering + required-key fail-closed validation, PgBouncer TCP readiness/liveness/startup probes, CNPG-compatible parameter allowlist + operator-owned-key fail-closed validation, read-only-root-filesystem-compatible `unix_socket_dir = ` default, automatic topology spread / PDB at `instances > 1`, `maxUnavailable=0` rolling-update default, `type=ro` ready-replica host list + `server_round_robin=1` + `server_login_retry=2`, status `instances/readyReplicas/backendTargets/configHash`, explicit PgBouncer-exporter sidecar + `metrics` ServicePort + `/metrics` probe + PodMonitor selector-label / sample contract, render verification of PrometheusRule alerts on CNPG `cnpg_pgbouncer_*` metric prefixes, `spec.paused` PAUSE/RESUME reconcile + `status.paused`, in-place reload on `pgbouncer.parameters` patches (ConfigMap `config.sha256` projection wait + ready-Pod `SIGHUP` + Pod hash annotation audit + Pooler-Service reconnect measurement) all complete. **Built-in auth** — when `spec.pgbouncer.authSecretRef` is empty, the operator auto-creates the `keiailab_pooler_pgbouncer` LOGIN role + the userlist.txt Secret with idempotent ensure (T27 ⑤, CNPG `cnpg_pooler_pgbouncer` compatible). **Password rotation** — `postgres.keiailab.io/rotate-pooler-password=true` annotation triggers an in-place `ALTER ROLE` + Secret update + `status.builtinAuthLastRotation` (T27 ⑥). Remaining: built-in TLS auto-issuance (T29), live Prometheus scrape / Grafana verification. |
| Monitoring (ServiceMonitor / Rules) | ✅ | ⚠️ partial chart support | Helm metrics Service / ServiceMonitor / PrometheusRule + BackupJob phase metric + Pooler phase metric/alert + PostgresCluster status-based replication-lag-bytes metric/alert + Pooler PodMonitor sample + render verification of the PgBouncer-exporter collection / client-waiting / max-wait alerts + Grafana dashboard ConfigMap render verification all exist. Live Prometheus scrape / Grafana import verification still pending. |
| Declarative database management | ✅ `Database` CRD | ⚠️ CRD/controller partial + smoke automation done | `PostgresDatabase` CRD + `spec.cluster/name/owner/ensure/tablespace/extensions/schemas/fdws/servers/privileges` + ready-primary `psql` reconcile + `status.applied/observedGeneration/conditions` + `databaseReclaimPolicy=delete` finalizer implemented. Includes database/schema privilege grant/revoke verification beyond CNPG. T27 ① `SMOKE_DATABASE=1` automation (status.applied + pg_database + reclaim=delete DROP). Remaining: live kind measurement. |
| Declarative role management | ✅ `.spec.managed.roles` + `status.managedRolesStatus` | ⚠️ CRD/controller partial + smoke automation done | `PostgresUser` CRD + `spec.cluster/name/ensure/login/superuser/createdb/createrole/replication/bypassrls/inherit/connectionLimit/inRoles/passwordSecretRef/disablePassword/validUntil` + ready-primary `psql` reconcile + `status.applied/observedGeneration/conditions/passwordSecretResourceVersion` implemented. Off-spec membership `REVOKE`, password-Secret username match, `passwordSecretRef`+`disablePassword` fail-closed, referenced-Secret update watch, and `PostgresCluster.status.managedRolesStatus.byStatus/cannotReconcile/passwordStatus` aggregation are all verified. T27 ② `SMOKE_USER=1` automation (status.applied + pg_roles + DROP ROLE). Remaining: live kind measurement, actual password-rotation SQL round-trip. |
| Replica clusters / externalClusters | ✅ standalone + distributed topology | ⚠️ streaming standalone path | `externalClusters[].connectionParameters`, `password`, `sslKey`, `sslCert`, `sslRootCert`, `bootstrap.pg_basebackup.source`, `replica.enabled/source` surface added. A standalone replica runs `pg_basebackup` from the external source even on ordinal 0 and writes `standby.signal` / `primary_conninfo`; the instance manager uses a persistent-follower election to block local promotion. The password Secret is wired in as `passfile`; the TLS Secret as `sslkey/sslcert/sslrootcert`. Source mismatch / missing host / incomplete `SecretKeySelector` fail-closed as `ReplicaClusterRejected`. WAL-archive / object-store hybrid, distributed-topology demotion/promotion-token, and live cross-cluster drill still pending. |
| Multi-region | ✅ replica clusters | ⚠️ partial via replica-cluster API | The streaming standalone replica-cluster path covers multi-region read-only / DR, but distributed-topology controlled switchover and symmetric backup are not complete. |
| Multi-shard sharding | ❌ | ⚠️ schema + plugin SDK | RFC 0005 in progress. |
| In-place major upgrade | ✅ | ❌ | Undecided. |
| Webhook validation | ✅ | ✅ CEL XValidation | Both production-grade. |
| Native fencing (PVC label) | ❌ (CR finalizer) | ✅ RFC 0003 | Different models. |
| OLM bundle (operatorhub.io) | ✅ registered on the community-operators channel (Artifact Hub OLM) | ⚠️ bundle 0.3.0-alpha.18 prepared; PR pending | `bundle/manifests/` aligned with 8 owned CRDs + alm-examples + CSV descriptions (T26); `operator-sdk bundle validate --select-optional suite=operatorframework` clean. T28 will open the k8s-operatorhub/community-operators PR. |
| Helm chart (Artifact Hub helm) | ✅ artifacthub.io/packages/helm/cloudnative-pg | ⚠️ artifacthub.io/packages/helm/keiailab-postgres-operator registered, alternativeName aligned (Chart annotation) | T26 aligned the Artifact Hub URL and alternativeName. Live Artifact Hub Verified-Publisher processing (repositoryID match) is still pending. |
| Local supply-chain gates | ✅ pre-commit / test / lint | ✅ ADR-0009 4-layer gate (L1 pre-commit, L2 pre-push test/audit/govulncheck, L3 Makefile validate/audit/gate, L4 PR evidence) | Both run under the "no GitHub Actions" policy (RFC-0002 §2). |
| Security: vulnerability scan | ✅ trivy + govulncheck (CI) | ✅ `make audit` (govulncheck + trivy fs HIGH/CRITICAL + gosec) | T26: after `moby/spdystream` v0.5.0 → v0.5.1 (CVE-2026-35469), `trivy fs` reports 0 vulnerabilities. |
| DCO sign-off enforcement | ✅ GitHub bot | ✅ lefthook commit-msg + `DCO_STRICT=1` | T26: `make hooks-install` wrapper + CONTRIBUTING alignment. Cannot retroactively fix without force-push; from this cycle on every commit is DCO-compliant. |

## Suitable use cases

**Recommend CNPG**:

- Need immediate production deployment.
- Backup / PITR / HA must work on day 1.
- No in-house operator team or unwilling to own operator internals.
- No multi-shard requirement (single PG primary + replicas).

**Suits ours (current alpha)**:

- Single-shard only + HA not required (dev / staging).
- Smaller footprint (50 MB operator + 144 MB PG Pod).
- A small team that wants to *read and patch the codebase directly* —
  CNPG's 94k LoC vs ours' 5k LoC.
- A long-term roadmap toward self-built distributed SQL (multi-shard) —
  RFC 0005's native sharding plugin starts to matter at P2+.

## Reproducibility

Both operators are exercised with the same `hack/smoke.sh` pattern. Re-run:

```fish
# 1. Cleanup
kind delete cluster --name pg-bench

# 2. Setup
kind create cluster --name pg-bench
docker pull --platform linux/arm64 ghcr.io/cloudnative-pg/cloudnative-pg:1.27.0
docker pull --platform linux/arm64 ghcr.io/cloudnative-pg/postgresql:18-bookworm
docker buildx build --provenance=false --sbom=false --load -t local/postgres-operator:bench .
docker buildx build --provenance=false --sbom=false --load \
    -f Dockerfile.pg --build-arg PG_MAJOR=18 -t local/pg:18 .
docker tag local/pg:18 ghcr.io/keiailab/pg:18

# 3. Load images into kind (`ctr import` rejects multi-arch manifest lists, so we route through `docker save`)
for img in local/postgres-operator:bench local/pg:18 \
           ghcr.io/cloudnative-pg/cloudnative-pg:1.27.0 \
           ghcr.io/cloudnative-pg/postgresql:18-bookworm \
           ghcr.io/keiailab/pg:18; do
    docker save "$img" -o /tmp/img.tar
    docker exec -i pg-bench-control-plane ctr -n k8s.io images import /dev/stdin < /tmp/img.tar
end

# 4. CNPG
kubectl apply -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.27/releases/cnpg-1.27.0.yaml

# 5. Ours
make build-installer IMG=local/postgres-operator:bench
sed -i.bak 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' dist/install.yaml
kubectl apply --server-side -f dist/install.yaml

# 6. Apply the CR and measure (repeat)
```

At the time of this measurement, kind import only succeeded when Docker
BuildKit produced a single-arch single-platform image
(`--provenance=false --sbom=false`); a multi-arch manifest list forces
`--all-platforms`, which fails because digests for the other arch are
missing. This effectively limits our `Dockerfile.pg` to a non-multi-arch
posture (kind / dev only).

## Conclusion

- **The cross-validation verified whether "alpha-deployable" was an
  honest claim** — partially **No**. Three bugs had to be fixed-forward
  before deployment was possible.
- **The resource-footprint advantage (~25% smaller) is a proxy for GA
  distance** — we are smaller because we ship fewer features.
- **The Time-to-Ready gap (2.6×) is a fixable conservative-probe
  setting** — cycle 7 follow-up.
- **Production adoption is recommended only after the user has digested
  the ❌ / ⚠️ rows in the feature matrix.**

*Every number in this report is a single measurement — no statistical
confidence interval. Formal SLA measurement is GA-stage work.*

---

## 2026-05-04 additional measurement — RFC 0006 R3 regression + smoke re-verify

> Environment: identical (kind v0.31 / Docker 29.4.1 / arm64), after this
> cycle's commit chain (R1+R2+R3 task-a/b/c) merged.

### A. Smoke regression (`hack/smoke.sh`) — F02 90→100% gate

| Metric | Measured | Gate (RFC 0006 §7 alpha) |
|---|---|---|
| Operator manager Available | ~12 s | < 60 s ✅ |
| **CR apply → cluster Ready** | **18 s** (22:32:52 → 22:33:10) | **< 60 s ✅** |
| psql round-trip (`SELECT 1`) | PASS | PASS ✅ |
| status.shards[0].primary.ready | true | true ✅ |
| status.conditions[Ready] | True ("all subsystems ready") | True ✅ |

A **3.4× improvement** versus the earlier cross-validation's 62 s
(single-shard Time-to-Ready) — readinessProbe 30 → 5 s (`78c93db`)
combined with the R1/R2 wiring landing.

### B. RFC 0006 R3 regression (`make test-e2e-failover`) — beta gate

| It | spec | Result | Measured |
|---|---|---|---|
| #1 | Elects ord-0 as initial primary | ✅ PASS | — |
| #2 | Spawns ord-1 as standby with role=replica annotation | ✅ PASS | — |
| **#3** | **Promotes new primary within RTO 30 s after primary kill** | **✅ PASS** | **RTO = 7.45 s** |
| #4 | Old primary rejoins as standby after pod restart | ⚠️ code path implemented | Generic existing-PGDATA marker implemented; live e2e re-verification still pending. |

**Headline**: passes the RFC 0006 §7 beta criterion (`primary kill on a
replicas=2 cluster → new primary within RTO < 30 s`) with **4× margin**
(7.45 s vs the 30 s target). Beta-phase measurement gate satisfied.

**Status of It #4**:

- To work around the case where `OnStoppedLeading` is not called when
  the old primary is killed outright, the bootstrap container writes a
  `.keiailab-restart-primary-as-standby` marker whenever it sees an
  existing PGDATA + HA + a current `PRIMARY_ENDPOINT` that is not itself.
- The instance manager, on finding the marker, runs `pg_rewind
  --target-pgdata <PGDATA> --source-server "host=<current-primary> ..."`
  with the main container's current `PRIMARY_ENDPOINT`, then writes
  `standby.signal` and `primary_conninfo`, and delays joining election.
- On `pg_rewind` failure, a fresh `pg_basebackup` fallback is attempted.
  If that fallback also fails, the original data dir is restored, the
  marker is left in place for the next restart, and `standby.signal` is
  not created.
- Rejoin-prep failure writes `reason=RejoinPreparationFailed` plus a
  detailed message to the own Pod's
  `postgres.keiailab.io/instance-status` annotation before exit; the
  controller aggregates it into
  `PostgresCluster.status.shards[].replicas[].reason/message`.
- Remaining: kind/chaos-based live e2e re-verification, arbitrary
  network-partition / STONITH, real divergent-WAL rewind drill.

### C. Five additional *test-infra* regressions surfaced (all fixed forward)

This cycle was the *first real kind execution of the RFC 0006 R3 commit
chain* — task-c had only done compile-only validation, so the following
environment-alignment regressions all surfaced at once:

| # | Location | Symptom | Fix |
|---|---|---|---|
| 1 | `hack/smoke.sh:72` | Operator-namespace mismatch → `kubectl wait` NotFound | `postgres-operator-system` |
| 2 | `hack/smoke.sh:36` | `OPERATOR_IMG :smoke` ↔ `install.yaml :0.3.0-alpha` drift → ImagePullBackOff | Derive `OPERATOR_TAG` from `Chart.yaml` `appVersion` |
| 3 | `hack/smoke.sh:32` | `NS` env override conflicts with the sample CR's hardcoded `metadata.namespace=default` | Hardcode `NS=default` |
| 4 | `test/e2e/e2e_suite_test.go:36,~64` | `managerImage example.com/...` ↔ `install.yaml :0.3.0-alpha` drift + missing operator-install step | Align `managerImage` + add `make build-installer` + `kubectl apply -f dist/install.yaml` + `wait Available` |
| 5 | `test/e2e/{failover,postgrescluster}_e2e_test.go` | Label selector `postgres.keiailab.io/cluster=` did not match the controller's actual label (`app.kubernetes.io/instance=`) → Pod selector matched zero forever → 5-minute timeout | 6 occurrences fixed in bulk |

**Class analysis**: every one of the five was an *environment-alignment*
regression that unit + envtest cannot catch. This is the RFC 0006 §1
"unverified features are vaporware" principle applied to the test code
itself — tests that don't run are also vaporware.

### D. Phase gate update (RFC 0006 §4)

| Phase | Code gate | Measurement gate | Status |
|---|---|---|---|
| **alpha** (R1+R2) | ✅ implemented | ✅ smoke Pod Ready 18 s < 60 s | **passes** |
| **beta** (R3) | ✅ implemented (R3 task-a/b/c) | ✅ RTO 7.45 s < 30 s (It #3) / ⚠️ It #4 follow-up fix needed | **partial pass** — R3 rejoin gap follow-up |
| GA-single (R4) | ❌ pending | — | not entered |
| GA-distributed (R5) | schema only | — | not entered |

### E. Reproduction steps

```fish
# 1. Smoke (F02 single-cluster verification)
./hack/smoke.sh

# 2. R3 regression (replicas=1 + primary kill)
make test-e2e-failover

# 3. Cleanup
kind delete cluster --name postgres-operator-test-e2e
kind delete cluster --name postgres-operator-smoke
```

---

*This measurement was done on a single environment (M1 arm64 / Docker
29.4.1). Differences on other architectures / kernels / runtimes require
separate measurement.*
