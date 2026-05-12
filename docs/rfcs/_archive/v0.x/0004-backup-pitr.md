# RFC 0004 — Backup/PITR Model + pgBackRest First Integration

- **Status**: Draft
- **Date**: 2026-04-30
- **Author**: @keiailab/maintainers
- **Related**: ADR 0005 (Plugin SDK interface model, BackupPlugin freeze), Plan §5 P1-1, P1-6
- **Recommended triggers**: P1-1 (BackupJob CRD + reconciler), P1-6 (use of BackupOptions.ExecutionMode)

## §1 Context

This project's Pillar **P4 Backup / PITR** has not yet defined *how to call* the `BackupPlugin` interface frozen in ADR 0005. That is, the `BackupPlugin.PerformBackup(ctx, target, opts)` signature exists, but the *caller (reconciler)* is absent, leaving this interface *dead at the code level*.

PR #10 (P1-6) added the `BackupOptions.ExecutionMode` field, but without a caller it is also in a state of being *specified but not used*.

This RFC defines:

1. The `BackupJob` CRD spec — *when and how* the user commands a backup
2. The reconcile procedure of `BackupJobReconciler` — Spec → BackupPlugin call → Status surfacing
3. **Adopting pgBackRest as the first reference plugin** — reasoning + implementation location
4. **PITR (Point-in-Time Recovery)** semantics — `RestorePoint` CRD vs. a sub-spec inside BackupJob
5. **Separation boundary from distributed PITR (Citus environment)** — separately handled in RFC 0008 (DistributedTable semantics)

This RFC is limited to the *single PG HA backup* area. Citus distributed PITR (2PC `citus_create_restore_point` coordination) is the differentiator #1 area of this project and is split into a separate RFC (RFC 0008 §distributed PITR or a new RFC 0011) when P4+P11 converge.

## §2 Decision — `BackupJob` CRD spec

### §2.1 GVK + naming

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: BackupJob
metadata:
  name: nightly-fullbackup-2026-04-30
  namespace: prod
spec:
  cluster:
    name: prod-cluster      # reference to PostgresCluster CR in the same namespace
  tool: pgbackrest          # BackupPlugin.Name() — lookup in Registry
  repo: s3-primary          # Repo identifier (BackupSpec.Repo)
  type: full                # full | incremental | differential
  retention:
    keepFull: 7             # keep the latest 7 full backups
    keepIncremental: 30     # keep the latest 30 incremental backups
  executionMode: sidecar    # P1-6 — sidecar | job | "" (plugin default)
  labels:
    env: production
    schedule: nightly
status:
  phase: Succeeded          # Pending | Running | Succeeded | Failed
  backupID: "20260430-021500F"
  startedAt: "2026-04-30T02:15:00Z"
  endedAt: "2026-04-30T02:23:42Z"
  bytes: 12882956841        # 12.0 GiB
  conditions:
    - type: Ready
      status: "True"
      reason: BackupCompleted
      message: "pgBackRest full backup completed in 8m42s"
```

### §2.2 Immutable fields

The following fields are *immutable after BackupJob creation* (webhook rejects changes):
- `spec.cluster.name`
- `spec.tool`
- `spec.type`
- `spec.executionMode`

Reason: a BackupJob is the spec for *a single backup invocation*. To change it, create a new BackupJob. Only `retention` is mutable (policy update).

### §2.3 ScheduledBackup CRD (separate new CRD, follow-up RFC)

Cron-based regular backups have a separate `ScheduledBackup` CRD that creates BackupJob instances. This RFC is limited to a *single BackupJob*; ScheduledBackup is in a separate RFC at P4-T2 time.

## §3 `BackupJobReconciler` procedure

```
1. BackupJob CR Get
2. Spec validation (referenced PostgresCluster exists, BackupPlugin registered)
3. Status.Phase = Pending → Running transition
4. Look up BackupPlugin in the Plugin Registry (r.Plugins.Backup(spec.tool))
5. Compose ClusterTarget:
     {Namespace: cluster.namespace, Name: cluster.name, Role: "coordinator", PoolName: ""}
6. Compose BackupOptions (Type, Repo, ExecutionMode, Labels)
7. Call plugin.PerformBackup(ctx, target, opts)
8. Surface result (BackupResult) → Status
   - Phase = Succeeded | Failed
   - BackupID, StartedAt, EndedAt, Bytes
   - Condition Ready/True or False with reason
9. Apply retention policy (oldBackups cleanup on next reconcile)
```

### §3.1 ExecutionMode branching

- `sidecar`: call (via `kubectl exec` or directly via K8s API) into the pgBackRest sidecar container *already co-resident* in the PG Pod. The operator creates no new resources.
- `job`: create a K8s `batch/v1.Job` resource. The plugin binary runs standalone. The reconciler polls the Job's Status.
- `""` (empty string): use plugin default. pgBackRest is sidecar, WAL-G is job.

ExecutionMode is *branched directly by the BackupPlugin* — the reconciler delegates to the plugin.

## §4 First reference plugin: pgBackRest

### §4.1 Reasons for adoption

| Tool | Reason for adoption |
|------|-----------|
| **pgBackRest** | PostgreSQL ecosystem *de facto standard*, validated by Crunchy PGO usage, supports full+incremental+differential, native S3/GCS/Azure, explicit restore point commands |
| WAL-G | Distributed Citus is not well supported (note). A follow-up plugin |
| Barman | Small user pool |
| Velero (K8s native) | The unit of backup is K8s resources rather than PG — incompatible with the model of this project |

(Note) WAL-G will be added as a separate plugin at P4-T3 as a reference for the ExecutionMode=`job` pattern.

### §4.2 Location

`internal/plugin/backup/pgbackrest/` (new package). Separated as a package *outside* the Plugin SDK — depguard blocks direct imports by core reconcilers (ADR 0005 §enforcement mechanism).

```
internal/plugin/backup/pgbackrest/
├── plugin.go         # PgBackRestPlugin struct + BackupPlugin interface implementation
├── plugin_test.go    # unit tests (mock command runner)
├── sidecar.go        # ExecutionMode=sidecar branch — kubectl exec wrapper
├── job.go            # ExecutionMode=job branch — K8s Job creation
└── register.go       # register with Plugin Registry (called from cmd/main.go)
```

### §4.3 First implementation scope (P4-T1)

- `Name() = "pgbackrest"`
- `PerformBackup`: branch on ExecutionMode, run `pgbackrest backup --type=full|incr|diff` via sidecar, parse standard output
- `RestorePIT`: run `pgbackrest restore --target-time=...`
- `Validate`: `BackupSpec.Tool == "pgbackrest"` + format validation for repo specified in `Settings`

Follow-up (P4-T2~):
- ScheduledBackup CRD
- Multi-repo (S3 + local PVC) hybrid
- Distributed PITR (P4+P11 convergence, separate RFC)

## §5 PITR semantics

### §5.1 Single PG PITR — `BackupJob.Spec.Restore`

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: BackupJob
spec:
  cluster:
    name: prod-cluster
  tool: pgbackrest
  repo: s3-primary
  type: restore                     # new type value
  restore:
    targetTime: "2026-04-30T01:00:00Z"
    # or specify backupID directly
    backupID: "20260429-021500F"
```

The reconciler calls `BackupPlugin.RestorePIT(ctx, target, ts)`. Result → Status.

### §5.2 Distributed PITR (Citus, P4+P11 convergence)

*Out of scope* for this RFC. A follow-up RFC (0011 or RFC 0008 §distributed PITR) introduces `citus_create_restore_point` 2PC coordination + a new `RestorePoint` CRD.

## §6 Security

- **Secret integration**: `BackupJob.Spec.SecretRef` (when integrating with RFC 0006 §Auth Rotation Hook) — pgBackRest's `--repo1-cipher-pass`, S3 access key, etc., are looked up from K8s Secrets by the plugin.
- **Plugins must not reference Secrets directly**: the SDK *resolves the Secret in advance and passes byte slices*. The plugin does not know the secret name. (same principle as plan §11 risks §gRPC security).

## §7 RBAC

Additional permissions required by `BackupJobReconciler`:
- `postgres.keiailab.io/backupjobs`: get/list/watch/update/patch
- `postgres.keiailab.io/backupjobs/status`: update/patch
- `batch/jobs`: get/list/watch/create/update/patch/delete (when ExecutionMode=job)
- `""/secrets`: get/list/watch (Secret lookup — collaboration with RFC 0006)

## §8 Verification (DoD)

| Stage | Command | Pass criteria |
|------|------|-----------|
| Unit | `make test` | `internal/plugin/backup/pgbackrest/` ≥ 80%, mock runner covers all branches |
| envtest | `make test` | `internal/controller/backupjob_controller_test.go` PASS — BackupJob creation → Status.Phase transition verified |
| e2e (kind) | `make test-e2e PILLAR=p4` | With actual PG container + pgBackRest sidecar, `BackupJob → Succeeded`, backup artifact present in repo |
| Security regression | BackupJob works in a namespace with restricted PSA + NetworkPolicy | Admission passes + traffic blocking bypass avoided |

## §9 Tradeoffs

- **pgBackRest dependence**: it is the primary reference, but since we have a *plugin model*, adding another tool is a 1-week task (ADR 0005 §consequences). Avoids dependency lock-in.
- **CRD spec simplification**: fields like `Schedule`, `Retention` are kept simple as integers/cron strings in v1 of this RFC. In v2 we can add sub-specs like `Storage.PVC` (local backup repo).
- **ScheduledBackup separation**: instead of putting cron on a single BackupJob, use a separate CRD. Since the *atomic unit* is one BackupJob, traceability and debugging are clear.

## §10 Follow-up RFCs

- **RFC 0008 (DistributedTable semantics) §distributed PITR**: Citus `citus_create_restore_point` 2PC coordination + `RestorePoint` CRD
- **RFC 0006 §Auth Rotation Hook**: Secret integration for BackupJob
- **RFC 0011 (Extension priority algorithm)**: pgBackRest extension install priority (between citus<300, around 100~200)

## §11 Consequences

- When this RFC is **Accepted**, P1-1 (BackupJob CRD + reconciler) implementation enters.
- BackupOptions.ExecutionMode (PR #10, P1-6) has its *first usage site*.
- Among the 5 Plugin SDK interfaces, **BackupPlugin is activated at the code level** — the *first caller* of differentiator #2 (Plugin SDK).

## §12 Change policy

Changes to this RFC (adding/removing BackupJob CRD signatures, adding ExecutionMode values) are at the *Spec* level, so they may entail a **CRD v1alpha1 → v1alpha2** migration. The v1alpha1 freeze time is after P4-M1 is reached.
