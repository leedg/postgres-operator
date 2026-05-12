# RFC 0002 — Metadata Sync Algorithm (`pg_dist_node` ↔ K8s)

- **Status**: Draft (proposed simultaneously with P11-T1 spike)
- **Submitted**: 2026-04-27
- **Authors**: @keiailab/maintainers
- **Comment window**: 14 days (closes 2026-05-11)
- **Approval criteria**: 2/3 of maintainers (GOVERNANCE.md "architecture change")
- **Related**: ADR 0001 v2 §differentiator 1 (Citus first-class), ADR 0002 (K8s as DCS), ADR 0003 (QueryRouter Stateless), ADR 0005 (Plugin SDK), RFC 0001 §Appendix D §Pillar mapping (P11)
- **Prior artifact**: P10-T2 (register 7 ExtensionPlugins + ordering regression)

## Context

The first differentiator of this operator (ADR 0001 v2) is **first-class support for the Citus distributed topology**. One of the core features of that differentiator is defined by this RFC: **automatic sync between `pg_dist_node` (Citus metadata catalog) and K8s topology (`PostgresCluster.spec.workers[]` + Service Endpoints)**.

PGO/CNPG do not provide this feature — users must call `citus_add_node`/`citus_update_node`/`citus_remove_node` themselves. In this operator, once the user declares `PostgresCluster.spec.workers`, the reconciler authoritatively syncs.

## Decision

### 1. Node model

This operator represents a single Citus node with the following 7-tuple.

```go
type Node struct {
    Group            int32  // Citus pg_dist_node.groupid
    Name             string // hostname (Pod DNS)
    Port             int32  // PG port (5432)
    Role             string // "coordinator" | "worker"
    Pool             string // worker pool name (empty string for coordinator)
    Index            int32  // ordinal within the same pool (StatefulSet order)
    ShouldHaveShards bool   // pg_dist_node.shouldhaveshards
}
```

`Name` is the stable Pod DNS guaranteed by the K8s headless Service:

```
<sts-name>-<index>.<svc-name>.<namespace>.svc.cluster.local
```

Example: `orders-worker-pool-a-0.orders-worker-pool-a.default.svc.cluster.local`

### 2. groupid assignment rule

| Role | groupid |
|---|---|
| Coordinator | **0** (Citus standard) |
| Worker pool i (`spec.workers[i]`) | **i + 1** (1, 2, 3, ...) |

All members (StatefulSet replicas) within the same worker pool **share the same groupid**. Citus recognizes nodes with the same groupid as a streaming replication HA pair.

**Invariant**: if the user changes the order of `spec.workers[]`, groupids are reassigned and distributed table shard locations break. To prevent this:
- The webhook rejects changes in the order of `spec.workers[].name` on update (at P9-T5 time). This RFC only freezes signatures; enforcement is delegated to the follow-up RFC 0010 (Upgrade).
- During alpha (the current time), users are only guided to "fix the order" via documentation.

### 3. ShouldHaveShards default

| Role | Default | Reason |
|---|---|---|
| Coordinator | **false** | ADR 0003 §Coordinator: the coordinator focuses on metadata + DDL gateway. Not holding distributed table shards is recommended. The user can override with `spec.coordinator.shouldHaveShards=true`. |
| Worker | **true** | Holding distributed table shards is the essential responsibility of a worker |

### 4. Conversion function — `DesiredNodes`

```go
func DesiredNodes(cluster *postgresv1alpha1.PostgresCluster) []Node
```

**Input**: a `PostgresCluster` CR
**Output**: a flattened list of expected `pg_dist_node` entries

**Algorithm** (M0 spike, simplified):

1. coordinator: create `members` count of Nodes, all with group=0, role="coordinator". Name in the form `<cluster>-coordinator-<idx>.<svc>.<ns>.svc.cluster.local`.
2. For each worker pool i: create `members` count of Nodes, all with group=i+1, role="worker", pool=name.

**Determinism**: identical output for identical input. Sort keys are (Group, Index).

**M1 reinforcement (follow-up task)**:
- handle the case where a non-primary standby may respond at failover time
- take Service Endpoints (Pod ready state) as an additional input to exclude unready Pods

### 5. diff algorithm — `ComputeActions`

```go
type Action struct {
    Op   string // "add" | "update" | "remove"
    Node Node   // target
}

func ComputeActions(current, desired []Node) []Action
```

**Algorithm**:

1. Map both slices by (group, name, port) key
2. Present in desired but absent in current → `add`
3. Present in desired and current → compare fields → different → `update`
4. Present in current but absent in desired → `remove`
5. Stable-sort the result: remove → update → add (place `add` last to preserve distributed table availability)

**Determinism**: identical result regardless of the order of the input slices.

### 6. SQL execution — `SQLExecutor` interface

```go
type SQLExecutor interface {
    Apply(ctx context.Context, actions []Action) error
}
```

**Implementations**:
- `LibPQExecutor` (production, P11-M1): connect to the coordinator primary via `database/sql` + `github.com/lib/pq`, and translate each Action to the following SQL:
  - `add` → `SELECT citus_add_node('<name>', <port>, groupid => <group>, ...)`
  - `update` → `SELECT citus_update_node(<old_id>, '<new_name>', <new_port>)` (after looking up nodeid in pg_dist_node)
  - `remove` → `SELECT citus_remove_node('<name>', <port>)`
- `NullExecutor` (spike default, M0): no-op. Only reflects the desired state in Status; does not call SQL. Both envtest and cmd/main.go use this implementation.
- `MockExecutor` (unit tests): only records called Actions.

**Selection**: cmd/main.go picks either LibPQExecutor or NullExecutor at compile time and injects it into the reconciler. We may later review whether the SQLExecutor itself is integrated into the RouterPlugin interface in RFC 0009 (QueryRouter CRD separation).

### 7. Sync timing

v1 of this RFC adopts **authoritative sync on every reconcile**.

- Triggers: changes to the PostgresCluster CR + changes to all subordinate resources registered via Owns()
- Procedure: refreshStatus → confirm all resources ready → DesiredNodes → (query current pg_dist_node) → ComputeActions → SQLExecutor.Apply
- If not all resources are ready, skip sync + ConditionMetadataInSync=False(Reason=Progressing)

**Alternative (rejected)**: delegate to Citus metadata sync itself (`pg_dist_node` → workers) and we only update the coordinator. Not chosen — drift recovery from users directly modifying `pg_dist_node` would not work.

### 8. Concurrency and ordering

- All mutating SQL executes only on one coordinator primary
- Reconciles of the same PostgresCluster are serialized by controller-runtime (single worker per CR in the work queue)
- Different PostgresClusters can proceed independently in parallel

### 9. Recovery mechanism

- Drift detection (e.g., a user directly called `citus_remove_node`): ComputeActions on every following reconcile detects the difference between desired vs. current and auto-restores
- Coordinator failover: at P2 (election) integration, reconnect SQLExecutor to the new primary
- Partial failure (only K of N Actions applied): the remaining Actions are auto-applied on the next reconcile (idempotency)

### 10. Status reflection

`PostgresClusterStatus.Topology` is filled with the following fields (matching the RFC 0001 signatures):

- `Coordinator.Primary`/`Replicas`/`LeaseHolder`: coordinator Pod state (meaning given after P2 integration)
- `Workers[].Name`: pool name
- `Workers[].DistNode.GroupID`: groupid assignment result
- `Workers[].DistNode.NodeName`/`NodePort`/`ShouldHaveShards`: expected values

`ConditionMetadataInSync`:
- True: the last call to SQLExecutor.Apply was either 0 actions or successful
- False/Progressing: resources not ready or the last Apply failed
- Unknown/NotApplicable: NullExecutor in use (M0 spike default)

## Enforcement mechanism

1. **DesiredNodes in `internal/citus/topology.go` is a pure function** — ≥3 unit tests (coordinator only / 1 pool / N pools)
2. **ComputeActions determinism** — input-order shuffle regression test
3. **SQLExecutor interface freeze** — compile guards `var _ SQLExecutor = ...`
4. **Changes to this RFC (Node model, groupid rule) require an RFC update**

## Tradeoffs

- **Burden of fixed groupid ordering**: if a user changes worker pool order, distributed tables break. Scheduled to enforce via webhook rejection in P9 (Upgrade) RFC 0010. During alpha, only guided via documentation.
- **Intent of NullExecutor default**: at the spike stage, actual SQL calls cannot be verified by envtest. Surfacing the desired state alone allows verification of reconciler integration and Status consistency. Reinforced in P11-M1 with LibPQExecutor + real PG integration e2e.
- **Single coordinator primary assumption**: before P2 (election) integration, "who is primary" is simplified (assume coordinator-0). After P2, follow the K8s lease holder.

## Consequences

- Pillar P11 can reach M0 (spike)
- Differentiator 1 (Citus first-class) of ADR 0001 v2 begins at the code level
- This RFC is committed in the same PR as the P11-T1 code (consistency breaks if separated)

## Verification (How to verify)

```bash
cd /Users/phil/WorkSpace/public/postgresql-operator

# 1) topology + sync unit tests
go test ./internal/citus/... -v

# 2) Verify Status.Topology.Workers[].DistNode is filled in reconciler integration (envtest)
go test ./internal/controller/... -v -run "P11"

# 3) RFC-frozen signature regression (DesiredNodes·ComputeActions·SQLExecutor)
go vet ./...
```
