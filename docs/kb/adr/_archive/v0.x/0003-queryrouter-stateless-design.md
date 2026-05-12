# ADR 0003 — Stateless Design of the QueryRouter Layer

- **Status**: Accepted
- **Date**: 2026-04-26
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0001 (Citus standard + QueryRouter), ADR 0002 (no Patroni)

## Context

In ADR 0001 we adopted the **stateless QueryRouter layer** as the core differentiator of this operator. This ADR defines the responsibility boundary and implementation constraints of that layer.

If the responsibility boundary of the QueryRouter is ambiguous, then (1) race conditions between reconcilers, (2) stale metadata routing, and (3) operator confusion over "where should I do what" will occur.

## Decision

### The QueryRouter is stateless

**What it must do**:
- Perform distributed query planning on Citus 11+ `metadata_synced=true` PG instances
- Multiplex connections with PgBouncer sidecar (transaction pooling by default)
- HPA-based horizontal scaling (CPU/memory or custom metric)
- Terminate client authentication (SCRAM-SHA-256, mTLS)
- Monitor `pg_dist_*` cache staleness: expose the `router_metadata_lag_seconds` metric

**What it must not do**:
- Hold a PVC (guarantees Pod restart without data loss)
- Execute DDL directly → route to Coordinator
- Hold shard data (`shouldhaveshards=false` enforced)
- Participate in streaming replication (it is not an RS member)
- Hold a K8s lease (does not participate in election)

### Coordinator is metadata authority + DDL gateway

- The **sole write path** to `pg_dist_*` system catalogs
- Entry point for all distributed DDL such as `create_distributed_table`, `alter_distributed_table`
- HA: 1+ sync standby (CNPG-style instance manager + K8s lease)
- Whether or not it holds data shards follows the Citus standard as-is (defaulting to `shouldhaveshards=false` is recommended but not enforced)

### Worker is responsible for data

- Holds actual shards of distributed tables (`shouldhaveshards=true`)
- Performs its own streaming replication election per pool
- When a new primary is decided, the instance manager calls `citus_update_node` on the Coordinator
- Holds a metadata copy (Citus metadata sync, treated as read-only)

## Enforcement mechanism

1. **Validating Webhook**:
   - Reject if QueryRouter spec has a `storage` field (stateless)
   - Reject if `coordinator.members` is even (prevents split-brain)
2. **Reconciler auto-correction**:
   - If `shouldhaveshards=true` is detected on a QueryRouter Pod, auto-correct + warning event
   - Reject if a PVC is mounted on a QueryRouter Pod
3. **DDL routing**:
   - `DistributedTableReconciler` does not send DDL directly to Workers, but routes to the Coordinator primary
4. **Election isolation**:
   - K8s leases are per-RS (`<cluster>-coordinator-primary`, `<cluster>-worker-<pool>-primary`)
   - QueryRouter does not hold a lease

## Rationale

- **Single responsibility principle**: separate routing (QueryRouter) / metadata+DDL (Coordinator) / data (Worker).
- **Horizontal scaling freedom**: for workloads where only routing load grows (app server fan-out), only QueryRouter needs to be scaled by HPA. No impact on Coordinator/Worker.
- **Fault isolation**: QueryRouter Pod restarts/failures cause zero data loss and zero impact on Coordinator/Worker.
- **Operator intuition**: "routing problem? look at QueryRouter. metadata problem? look at Coordinator. data problem? look at Worker."

## Tradeoffs

- **Increased connection hops**: Client → QueryRouter → Worker (without going through Coordinator, so 1 hop. Connection cost is distributed by PgBouncer transaction pooling)
- **Router metadata stale risk**: Citus metadata sync lag may cause incorrect shard routing
  - **Mitigation**: readiness fails + alarm when `router_metadata_lag_seconds` exceeds the threshold
- **Increased operational surface area from an added layer**:
  - **Mitigation**: `spec.deployment: development` mode guarantees a 5-minute quickstart with routers.replicas=1 + single coordinator + 1 worker pool

## Consequences

- `cmd/router/main.go` is a separate binary, packaged as a distroless image
- Service exposure: `<cluster>-router` (ClusterIP/LoadBalancer)
- Changes to this ADR require an RFC
