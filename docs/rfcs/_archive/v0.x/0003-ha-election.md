# RFC 0003 — HA Election + Fencing Protocol

- **Status**: Implemented (P2-M1, 2026-04-28 — election + envtest integration regression passing)
- **Submitted**: 2026-04-27
- **Authors**: @keiailab/maintainers
- **Comment window**: 14 days (closes 2026-05-11)
- **Approval criteria**: 2/3 of maintainers
- **Related**: ADR 0002 (no Patroni, K8s API as DCS), ADR 0003 (QueryRouter Stateless), RFC 0002 (Metadata Sync)

## Context

ADR 0002 adopts the **K8s API as DCS + own instance manager (Go PID1)** model instead of Patroni. This RFC freezes the interface, behavior, and operational parameters of **leader election**, the core component of that model.

Election is the answer to the following two scenarios.

1. **Primary decision within a single RS**: within the coordinator (1+ standby) or a worker pool (1+ standby), which Pod is the PG primary that accepts read/write.
2. **Source of metadata consistency**: when the P11 (RFC 0002) reconciler decides on a hostname to register in `pg_dist_node`, it follows the Pod DNS of the lease holder.

This RFC freezes the **election interface + Real (client-go leaderelection based) + Null/Mock**. Actual supervision of the PG process is a separate task from P2-T4 (`pg_rewind` integration). PVC fencing (split-brain prevention) is delegated to P2-T2 and a separate RFC.

## Decision

### 1. Lease naming convention

We freeze §Consequences of ADR 0002 in this RFC.

| Role | Lease name | namespace |
|---|---|---|
| Coordinator primary | `<cluster>-coordinator-primary` | same as PostgresCluster CR |
| Worker pool primary | `<cluster>-worker-<pool>-primary` | same |

Namespace not split — since PostgresCluster is Namespaced, leases in the same namespace provide natural isolation.

### 2. Lease parameters (operational constants)

| Parameter | Value | Rationale |
|---|---|---|
| LeaseDuration | **15s** | client-go default. Sufficiently short for PG primary transition, sufficiently long for transient network jitter |
| RenewDeadline | **10s** | Must be shorter than LeaseDuration. The holder attempts renewal within this time |
| RetryPeriod | **2s** | Interval at which a non-leader polls lease changes |

These values **can be overridden via CLI flags**: `--lease-duration`, `--renew-deadline`, `--retry-period`. Operators adjust based on network stability differences.

Follow-up task (P2-T2 fencing): use a PVC fence-out timeout longer than LeaseDuration to prevent split-brain.

### 3. Identity (Pod name)

Each instance manager's lease holder identity is **`<POD_NAME>`** (downward API).

```yaml
env:
  - name: POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
  - name: POD_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
  - name: POSTGRES_CLUSTER     # PostgresCluster CR name
  - name: POSTGRES_ROLE        # "coordinator" | "worker"
  - name: POSTGRES_POOL        # meaningful only when worker
```

The P11 (RFC 0002) reconciler reads the lease holder identity string and converts it to a hostname when registering in `pg_dist_node`.

### 4. Role transition model

| Current | Event | Next |
|---|---|---|
| follower | OnStartedLeading | leader (promote PG primary — outside the scope of this RFC) |
| leader | OnStoppedLeading | follower (demote PG to read-only — outside the scope of this RFC) |
| any | OnNewLeader (another Pod) | follower (no change if already follower) |

This RFC freezes only the callback signatures. PG process supervision (the actual behavior of promote/demote) is implemented inside LeaderCallbacks in follow-ups P2-T3 + P2-T4.

### 5. Election interface

```go
// internal/instance/election/election.go

type Status string

const (
    StatusLeader   Status = "Leader"
    StatusFollower Status = "Follower"
    StatusStarting Status = "Starting"
)

type Election interface {
    // Run executes the election loop blocking until ctx is done.
    // The caller (main) must run it in a separate goroutine.
    Run(ctx context.Context) error

    // Status returns the current state atomically.
    Status() Status

    // Identity returns this instance's lease identity (usually POD_NAME).
    Identity() string
}
```

### 6. Three implementations

- **Real** (`internal/instance/election/lease.go`): uses `client-go/tools/leaderelection` + `resourcelock.LeaseLock`. cmd/instance uses this in production.
- **Null** (`internal/instance/election/null.go`): always Leader. For single-node development mode or testing in election-disabled environments.
- **Mock** (`internal/instance/election/mock.go`): tests can set Status explicitly. For unit tests.

### 7. PVC Fencing — delegation note

The second line of defense against split-brain (PVC label-based fencing) is outside the scope of this RFC. **P2-T2 + RFC 0003 Appendix A** follow-up work.

Core idea (sketch only):
- Apply the `postgres.keiailab.io/fenced=true` label to the PVC of a Pod that lost its lease
- StorageClass ReclaimPolicy + PV controller rejects mount of fenced PVC
- The new primary can mount only PVCs without the fenced label

### 8. Operating scenarios

#### Scenario A — Normal boot
1. All instance manager Pods start election simultaneously
2. One Pod acquires the LeaseLock → OnStartedLeading callback → Status=Leader
3. Other Pods get OnNewLeader callback → Status=Follower
4. New followers attempt lease renewal (fails) every RetryPeriod → stay follower

#### Scenario B — Graceful leader termination
1. SIGTERM to the leader Pod
2. Instance manager ctx cancel → election.Run returns → LeaseLock released
3. One of the followers acquires a new lease within RetryPeriod
4. The new leader's OnStartedLeading callback → P11 reconcile detects lease holder change

#### Scenario C — Leader unresponsive (node failure)
1. Leader Pod becomes unresponsive (OOM, node down)
2. No renewal for LeaseDuration (15s)
3. A follower detects lease expiration and attempts to acquire a new lease
4. Same follow-up as scenario B

### 9. Signals and logging

- All transitions are structured logs (`slog.Info("Leadership transition", "from", ..., "to", ..., "identity", ...)`)
- Pod readyz: leader=200, follower=200, starting=503 (election bootstrap in progress)
- Prometheus metric (activated at P6 integration time): `instance_election_status{status=...}` gauge

## Enforcement mechanism

1. RFC update on Election interface change
2. Compile guard `var _ election.Election = (*real.Real)(nil)`
3. Lease parameter unit tests verify RenewDeadline < LeaseDuration
4. Verify the expected env vars of cmd/instance/main.go in build/images/instance/Dockerfile or in the PodSpec injected by the reconciler

## Tradeoffs

- **Dependence on client-go leaderelection**: external library (already a controller-runtime dependency, so zero additional cost)
- **Single lease holder identity = POD_NAME**: two PG instances on the same node cannot hold the same lease (K8s guarantees unique POD_NAME)
- **15s LeaseDuration**: shortening to 5s gives faster failover but increases K8s API server load. Operators can override via CLI, so the default is conservative

## Consequences

- Pillar P2 can reach M0 (spike)
- internal/instance/election/ new package + cmd/instance integration
- Follow-up: P2-T2 fencing, P2-T3 failover controller (detect RS primary down), P2-T4 pg_rewind

## Verification

```bash
# 1) Election interface + 3-implementation unit tests
go test ./internal/instance/election/... -v

# 2) cmd/instance build
go build ./cmd/instance/...

# 3) Lease parameter sanity regression (RenewDeadline < LeaseDuration)
go test ./internal/instance/election/... -run TestLeaseParameters
```

---

## Appendix A — PVC Fencing Protocol (P2-T2, Implemented 2026-04-28)

This appendix freezes the second line of defense against split-brain that was delegated in §7.

### A.1 Motivation — why lease alone is not enough

K8s lease only guarantees *logical* leader decision. The following scenarios cannot be prevented by lease.

1. The old leader Pod fails to renew its lease due to GC SLA or network jitter → a new leader is elected
2. The old Pod comes back (kubelet auto-restart)
3. The old Pod's PG process is still mounted on the same PVC and writes — the new leader also shares the same PVC (even with RWO policy, ReadWriteOnce is *per-Node* not *per-Pod*, so this can occur on the same node)
4. Two PGs write to the same data directory → data corruption

This appendix introduces a distributed defense using a PVC label as the single source of truth so that *the old leader itself marks its own PVC as fenced*.

### A.2 Label convention

| Key | Value | Meaning |
|---|---|---|
| `postgres.keiailab.io/fenced` | `"true"` | No instance manager should use this PVC for promote |
| (key absent) | — | Normal. Eligible for promote |

PVC naming convention: `data-<sts-pod-name>` (StatefulSet `VolumeClaimTemplates[].metadata.name="data"`).

### A.3 Operational protocol

```
                Election event           Fencing action
   ─────────────────────────────────────────────────────────
   OnStartedLeading (this Pod becomes leader)
                   │
                   ▼
        VerifyNotFenced(self.PVC)
                   │
        ┌──────────┴──────────┐
        │                     │
       OK                  ErrFenced
        │                     │
        ▼                     ▼
  Proceed PG promote   exit(2) — refuse
                       leadership until operator intervenes
   ─────────────────────────────────────────────────────────
   OnStoppedLeading (this Pod lost the lease)
                   │
                   ▼
         MarkFenced(self.PVC)
                   │
                   ▼
         Proceed PG demote (or terminate)
   ─────────────────────────────────────────────────────────
   OnNewLeader(other) (another Pod becomes leader)
                   │
                   ▼
              (no-op)
```

Core rules:

1. **Each Pod fences only its own PVC** (distributed decision). If the controller batch-fences, the ADR 0002 "K8s API as DCS" principle (§Consequences) breaks.
2. **Fence marking is idempotent**. After restart, the fenced state is maintained until the same Pod again takes the lease and unfences its own PVC.
3. **Unfence is a manual operator action**. Automatic unfence is outside the scope of this RFC — automation risks bypassing the wrong data verification stage and is intentionally excluded.

### A.4 Fail-Fast policy

If `VerifyNotFenced` returns `ErrFenced`, the instance manager terminates with **exit code 2** (`cmd/instance/main.go`). This termination means:

- K8s auto-restarts the Pod (assuming Pod restartPolicy=Always)
- After restart, fence still unresolved → exit(2) again → CrashLoopBackOff
- Refuse to take leadership until the operator verifies/recovers PVC and clears via the `Unfence` API

This fail-fast is an explicit tradeoff that **sacrifices some availability to guarantee consistency** (ADR 0001 v2 §principles).

### A.5 Unfence operation procedure

```bash
# 1) Check PVC state — verify data integrity (pg_controldata, replication slot state, etc.)
kubectl exec -it <new-leader-pod> -- /usr/lib/postgresql/16/bin/pg_controldata /var/lib/postgresql/data

# 2) Verify that the old leader Pod has fully terminated
kubectl get pod <old-leader-pod> -o yaml | grep phase

# 3) Clear the fence after verification
kubectl label pvc data-<old-leader-pod> postgres.keiailab.io/fenced-

# Or restart the instance manager so it auto-verifies its own PVC and attempts promote
```

### A.6 Interface freeze

```go
type Fencer interface {
    MarkFenced(ctx context.Context) error
    Unfence(ctx context.Context) error
    IsFenced(ctx context.Context) (bool, error)
    VerifyNotFenced(ctx context.Context) error // ErrFenced if fenced=true
}
```

Implementations — `internal/instance/fencing/`:
- **Real** (`fencing.go`): PVC label patch via `kubernetes.Interface`
- **Mock** (`mock.go`): in-memory flag + call counter, for unit tests

### A.7 RBAC requirements

The instance manager's ServiceAccount needs the following permissions on PVCs in its own namespace:

```yaml
- apiGroups: [""]
  resources: ["persistentvolumeclaims"]
  verbs: ["get", "patch"]
```

`get` is used in IsFenced/VerifyNotFenced, `patch` in MarkFenced/Unfence. List/Watch is unnecessary (handles only one's own PVC).

### A.8 Limitations — M3 follow-up

- **No mount protection beyond PVC**: when PV is NFS, S3FS, etc. with weak exclusive-mount guarantees, protection at the StorageClass layer is needed besides this mechanism. ADR follow-up (ADR-007 candidate).
- **In-flight write reclamation**: even *after* fence marking, if the old leader's PG process does not terminate immediately, writes can occur for a short time. P2-T4 (`pg_rewind` automation) recovers this residual write into consistency between new and old leaders.
- **No fence violation alerting**: at P6 integration time, introduce `instance_fencing_violations_total` metric + PrometheusRule.

### A.9 Verification

```bash
# Unit regression (fake clientset)
go test ./internal/instance/fencing/... -v

# Build regression — instance manager integrates fencer
go build ./cmd/instance/...
```
