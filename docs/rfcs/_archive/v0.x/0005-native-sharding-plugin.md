# RFC-0005: Native Sharding Plugin (Citus mechanism decomposition + Apache-2.0 compatible path)

- Date: 2026-05-01
- Status: Draft
- Authors: @keiailab
- Related: ADR-0010 (license + sharding strategy), ADR-0005 (plugin SDK interface model)

## 0. Summary (TL;DR)

The *long-term path* for the "Citus isolation + vanilla PG default" policy decided by ADR-0010. To self-implement an Apache-2.0 compatible plugin for a subset of distributed SQL capabilities provided by Citus, this RFC decomposes the 7 core Citus mechanisms and presents a `ShardingPlugin` interface extending this operator's 5-interface Plugin SDK. It defines milestones by stage (Phase 2A~Phase 4) and evaluates the trade-offs, risks, and comparison with existing solutions for each phase.

**Reality check**: Citus is ~500K LOC of C code accumulated by a dedicated Microsoft team over more than 10 years. A 1:1 replacement is a multi-year effort, and what this RFC promises is *capability subset* implementation, not parity.

## 1. Context

### 1.1 Decision background (ADR-0010 summary)

- Citus = AGPL-3.0 → operator users bear §13 obligations when operating SaaS
- This operator = Apache-2.0 → keeps default clean
- After 0.2.0-alpha, default = vanilla PG18, Citus is Beta opt-in

### 1.2 Gap from removal

Since distributed SQL is excluded from the default, the following user scenarios fall into a *functional gap*:
- OLTP scale that a single PG cannot handle (>1TB working set, >10K QPS)
- Multi-node sharded write workloads
- Analytical queries needing distributed joins

This RFC presents a path to fill that gap in stages.

### 1.3 Comparison: existing distributed PostgreSQL solutions

| Solution | License | Approach | This RFC's view |
|---|---|---|---|
| Citus | AGPL-3.0 | PG extension, distributed planner C extension | Avoided (license) |
| Vitess | Apache-2.0 | MySQL/PG sharding proxy | PG support is alpha. Reference for external proxy pattern |
| YugabyteDB | Apache-2.0 (formerly PG fork) | PG-compatible distributed DB | Separate stack, not a drop-in |
| CockroachDB | BUSL (Business Source) | PG wire compatible distributed | Outside this operator, comparison group |
| pgcat / pgbouncer + sharding rules | MIT/PostgreSQL | Connection-level sharding proxy | Phase 2A candidate |
| postgres_fdw + pg_partman | PostgreSQL License | FDW + partition routing | Phase 2A starting point |

This RFC aims for a self-implementation in *plugin form*, and treats external solutions only as alternatives users can choose.

## 2. Decomposition of Citus core mechanisms (7 components)

This section is based on analysis of Citus's public documentation, code structure, and academic papers (VLDB 2021 "Citus: Distributed PostgreSQL for Data-Intensive Applications").

### C1. Distributed Query Planner

**Role**: takes SQL and decides which shard to route/parallelize to. Converts the result of PG's native planner (query tree) into a distributed query plan.

**Citus implementation**: `multi_planner` (~150K LOC). Injected into PG planner_hook via Hook. Logical planner → physical planner → produces a job tree.

**Difficulty**: ★★★★★ (hardest). Citus's core value and 5+ years of accumulated work.

### C2. Distributed Executor

**Role**: takes the plan tree, sends fragment queries to worker nodes, and aggregates results. Adaptive executor handles connection pool reuse and transaction recovery.

**Citus implementation**: `multi_executor` + `adaptive_executor`. Two modes: Real-time + Task-tracker.

**Difficulty**: ★★★★ — connection pool, transaction state, and error recovery are complex.

### C3. Shard Placement & Metadata Catalog

**Role**: tracks which shard is located on which worker node (`pg_dist_placement`, `pg_dist_shard`, `pg_dist_node`). Reference table replication, distribution column hash ranges.

**Citus implementation**: PG catalog tables. Metadata is synced to coordinator + all workers (Citus 11+).

**Difficulty**: ★★★ — the data model is intuitive, but synchronization consistency guarantees are tricky.

### C4. Shard Rebalancer

**Role**: automatically relocates shards on node add/remove. Considers CPU/disk balance and locality.

**Citus implementation**: `rebalance_table_shards()` SQL function. A background worker performs non-blocking shard moves.

**Difficulty**: ★★★★ — non-blocking shard move + transaction safety + abort recovery.

### C5. Distributed Transactions (2PC + heartbeat)

**Role**: guarantees ACID transactions spanning multiple shards. Two-phase commit + cohort heartbeat + recovery.

**Citus implementation**: `pg_dist_transaction` + custom 2PC coordinator. Automatic cleanup of long-running prepared transactions (timer-based).

**Difficulty**: ★★★★★ — distributed system consistency + infinite corner cases for recovery under node failure.

### C6. Reference Tables

**Role**: small tables synchronously replicated to all workers (for lookup/dim tables). Can collocate join with distributed tables.

**Citus implementation**: single placement → replicated to all nodes. INSERT/UPDATE applied synchronously across all nodes.

**Difficulty**: ★★★ — synchronous replication consistency + write throughput limits.

### C7. Columnar Storage

**Role**: append-only columnar table access method. Large-scale compression for analytical workloads.

**Citus implementation**: `cstore_fdw` → `citus_columnar` (PG access method). zstd compression, predicate push-down.

**Difficulty**: ★★★★ — PG access method API + compression + chunk metadata.

### Summary

C1 + C2 + C5 sit at the *real difficulty of distributed systems* and account for 80% of Citus's value. C3 + C4 are operational automation. C6 + C7 are *additional* capabilities — no obligation for drop-in replacement.

## 3. Mapping to this operator's plugin model

Current 5-interface plugin SDK (`internal/plugin/api.go`):

1. `BackupPlugin` — pgBackRest, WAL-G, etc.
2. `ExporterPlugin` — postgres_exporter, custom exporters
3. `ExtensionPlugin` — pgaudit, pgcron, pgvector, **citus** (currently Beta)
4. `RouterPlugin` — pgbouncer, pgcat
5. `AuthPlugin` — Vault-issued credentials, IAM

These 5 act on **a single PG instance or outside the cluster**. Distributed SQL requires *coordination across multiple nodes inside the cluster*, so a new interface is needed.

### New interface: `ShardingPlugin`

```go
// ShardingPlugin abstracts the distributed sharding backend.
// Implementations: Citus (AGPL, opt-in), Native (Apache-2.0, RFC 0005 Phase 2+), Vitess gateway, etc.
//
// This interface is alpha-frozen at RFC 0005 Phase 2A time.
// During alpha, only adding methods is allowed (non-breaking).
type ShardingPlugin interface {
    // Name is this plugin's unique identifier.
    // Must match PostgresClusterSpec.Sharding.Backend.
    Name() string

    // Capabilities reports the set of features this backend supports.
    // The webhook rejects ShardingSpec specifying unsupported features.
    Capabilities() ShardingCapabilities

    // PreparePlacement updates shard placement when PostgresCluster topology changes
    // (node add/remove).
    // Idempotent, called every controller reconcile loop.
    PreparePlacement(ctx context.Context, target ClusterTarget, topo Topology) error

    // CreateDistributedTable interprets user SQL DDL ("DISTRIBUTED TABLE ... BY (col)")
    // to create shards + register metadata.
    CreateDistributedTable(ctx context.Context, conn *sql.DB, spec DistributedTableSpec) error

    // CreateReferenceTable creates a small table synchronously replicated to all nodes.
    // If the backend does not support reference tables, return false from Capabilities().
    CreateReferenceTable(ctx context.Context, conn *sql.DB, table string) error

    // RebalanceShards triggers shard rebalance (background asynchronous).
    // Progress is checked via the backend's status table queries.
    RebalanceShards(ctx context.Context, conn *sql.DB) (RebalanceJob, error)

    // RouteQuery takes a SQL statement and decides which shard/worker to send it to.
    // This method may cooperate with RouterPlugin to perform connection-level routing,
    // or delegate to the backend's own distributed planner (signaled via Capabilities).
    RouteQuery(ctx context.Context, query string, params []any) ([]ShardTarget, error)

    // Validate inspects ShardingSpec user input from this backend's perspective.
    // Called at the webhook stage.
    Validate(spec *ShardingSpec) error
}

// ShardingCapabilities advertises backend features.
type ShardingCapabilities struct {
    DistributedTables    bool   // C3 hash/range distribution
    ReferenceTables      bool   // C6 broadcast tables
    DistributedJoin      bool   // C1+C2 multi-shard join
    Distributed2PC       bool   // C5 cross-shard ACID
    OnlineRebalance      bool   // C4 non-blocking shard move
    ColumnarStorage      bool   // C7 columnar tables
    NativeQueryPlanner   bool   // backend's own planner (Citus). If false, routing-only.
}

// DistributedTableSpec is a distributed table definition.
type DistributedTableSpec struct {
    Name             string  // including schema (e.g. "public.events")
    DistributionCol  string  // shard key column
    ShardCount       int32   // default 32. range or hash distribution
    ColocateWith     string  // collocate with another table that has the same distribution
    Strategy         string  // "hash" | "range"
}

// ShardTarget is a single shard location to send the query to.
type ShardTarget struct {
    Worker      string  // hostname (Pod DNS)
    Port        int32
    ShardID     int64
}

// Topology is a snapshot of the PostgresCluster's current node topology.
// A generalized form independent of the Node struct in internal/citus/topology.go.
type Topology struct {
    Coordinator *NodeInfo
    Workers     []NodeInfo
}

type NodeInfo struct {
    Pool     string
    Host     string
    Port     int32
    GroupID  int32
}

// RebalanceJob is for tracking an in-progress rebalance.
type RebalanceJob struct {
    ID       string
    Started  time.Time
    Status   string  // "running" | "complete" | "failed"
}

// ShardingSpec is the spec.sharding subfield of the PostgresCluster CRD (introduced in RFC 0005 Phase 2A).
type ShardingSpec struct {
    Backend         string  // "citus" | "native-fdw" | ...
    DistributedTables []DistributedTableSpec
    ReferenceTables []string
    DefaultShardCount int32
    // Backend-specific additional options are in a separate BackendOptions struct (omitempty)
}
```

### Mapping table

| Citus mechanism | ShardingPlugin method | Priority |
|---|---|---|
| C3 Placement | `PreparePlacement`, `CreateDistributedTable` | Phase 2A |
| C2 Executor | `RouteQuery` (simple case), backend's own planner (complex case) | Phase 2C |
| C6 Reference | `CreateReferenceTable` | Phase 2D |
| C4 Rebalance | `RebalanceShards` | Phase 3 |
| C5 2PC | (consider splitting into a separate `DistributedTxnPlugin` interface) | Phase 3 |
| C1 Planner | `RouteQuery` or delegated to backend | Phase 4 |
| C7 Columnar | (isolated as a separate ExtensionPlugin) | Phase 4+ |

## 4. Phased Roadmap

### Phase 2A — Sharding Plugin Interface Freeze + FDW Skeleton

**Goal**: freeze the interface and implement the simplest single backend to prove *operation*.

**Deliverables**:
- `internal/plugin/sharding/api.go` — the ShardingPlugin interface in §3 above + auxiliary types.
- Add `PostgresClusterSpec.Sharding` CRD field (optional, omitempty).
- `internal/plugin/sharding/fdw/` — hash sharding plugin based on postgres_fdw (PostgreSQL License — license-clean).
  - DistributedTable: parent table + per-worker partition foreign table.
  - ReferenceTable: postgres_fdw broadcast (UPDATE applied across all nodes).
  - RouteQuery: hash(key) % shardCount.
- Hash-based INSERT/SELECT works on a single distributed table.
- e2e test: 3-worker cluster + distributed `events` table + distributed INSERT/SELECT.

**Not included**: distributed JOIN, 2PC, online rebalance, columnar.

**Estimated duration**: 2~3 months. However, distributed JOIN is not supported due to postgres_fdw push-down limitations.

### Phase 2B — Reference Tables + Collocate Join

**Deliverables**: synchronous replication of ReferenceTable (trigger-based), collocated join (handled inside a worker).

**Estimated duration**: 1~2 months.

### Phase 2C — Smart Routing (read-only)

**Deliverables**: extraction of distribution column from SELECT queries + routing to the appropriate worker. SQL parser introduction required (use pg_query_go).

**Estimated duration**: 2~3 months.

### Phase 2D — Online Add/Remove Worker

**Deliverables**: automatic shard rebalance on PostgresClusterSpec.Workers changes. However, blocking move (read-only window).

**Estimated duration**: 2~3 months.

### Phase 3 — Distributed 2PC + Online Rebalance

**Deliverables**: cross-shard ACID + non-blocking shard move. Entering the *real difficulty* of distributed systems.

**Estimated duration**: 6~12 months. The riskiest single phase of this RFC.

### Phase 4 — Distributed Query Planner (optional)

**Deliverables**: distributed execution of general JOIN/aggregation. Introduce PG planner_hook or consider an external proxy (pgcat fork).

**Estimated duration**: 12~24 months. Or decide to *postpone permanently* and recommend Citus opt-in for users who need distributed JOIN.

## 5. Risk analysis

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Gap between user expectations and implementation after Phase 2A | High | Medium | State *capability subset* in RFC + chart NOTES + README |
| Accumulation of Phase 3 (2PC) corner cases | Very high | Very large | Jepsen-style testing required, alpha labeling for 6+ months |
| Cases where native sharding operations are harder than Citus opt-in | High | Medium | Permanently preserve the Citus opt-in path (license only is clarified) |
| Upstream PG 18+ logical replication changes break reference tables | Medium | Medium | upstream-watch (note: RFC 0002 GH Actions abandoned — manual monitoring) |
| postgres_fdw push-down limits (Phase 2A) | Confirmed | Medium | Narrow Phase 2A itself to "distributed INSERT only as the primary goal" |

## 6. Decision criteria (Phase entry gates)

Before entering each phase, confirm:
1. **Market signal**: number of users of the immediately previous phase deliverables (alpha → beta transition metric).
2. **Operational fitness**: frequency of corner case reports in the previous phase (Jepsen + customer reports).
3. **Personnel availability**: phases of this RFC are all multi-month. A single contributor is insufficient.
4. **Alternatives comparison**: when entering, re-evaluate this path if new solutions (Vitess for PG, pgcat-shard, pg_dirtread, etc.) emerge.

## 7. Alternatives Considered

### A. Permanently abandon Phase 4 (Native query planner) + recommend Citus opt-in

Funnel users for whom distributed JOIN/aggregation is mandatory toward Citus opt-in. We self-implement only through Phase 2A~3 (placement, routing, 2PC).

- Pros: reduces this RFC's scope by 70%, realistic.
- Cons: weakens the "plug-and-play" distributed SQL messaging. SaaS users still bear AGPL.

### B. External proxy integration (Vitess for PG)

Implement ShardingPlugin as a Vitess gateway. PG 18 compatibility depends on Vitess upstream.

- Pros: we implement only the routing layer. Distributed planner depends on Vitess.
- Cons: external stack dependency. Weakens the operator's own value.

### C. *Delegate* sharding to users

The operator only guarantees single PG HA. Sharding is implemented by users at the application level (e.g., pgbouncer + routing rules + middleware).

- Pros: simplest. Clear operator responsibility.
- Cons: this differentiator (distributed SQL operator) disappears.

## 8. Follow-up work if this RFC is adopted

1. **Enter ADR-0010 AI-007**: ShardingPlugin interface PR (new `internal/plugin/sharding/api.go`).
2. **CRD extension**: add `PostgresClusterSpec.Sharding` optional field. webhook validation.
3. **Update README + roadmap.md**: state Phase 2A~Phase 4. Permanently preserve Citus opt-in path.
4. **examples/sharding/** new: postgres_fdw-using distributed table sample.
5. **e2e test extension**: 3-worker cluster + distributed events scenario.

## 9. Open questions

1. What is the responsibility boundary between ShardingPlugin and RouterPlugin (pgbouncer/pgcat)? The RouteQuery method could be in both — to be decided in RFC 0005 v2.
2. For 2PC, should the *cohort heartbeat* be a separate sidecar container or a goroutine inside the manager? To be decided at Phase 3 entry.
3. Dependencies when introducing the SQL parser: pg_query_go (PostgreSQL License) vs. own lexer? To be decided at Phase 2C entry.
4. *Synchronous* vs. *semi-synchronous* replication for reference tables? Trigger-based synchronous limits write throughput. Phase 2B decision.

## 10. Timeline

This RFC is a **path**, not a **roadmap**. Time estimates are minimums and may grow 2~3× when proceeding with a single contributor. We do not make time commitments at 0.2.0-alpha.

Progress is tracked in the Pillar P11 (distributed SQL) section of `docs/roadmap.md`.
