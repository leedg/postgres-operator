# ADR 0002 — No Patroni, Instance Manager + K8s API as DCS

- **Status**: Accepted
- **Date**: 2026-04-26
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0001, ADR 0003

## Context

The de facto standard for PostgreSQL HA is Patroni (Python), which uses an external DCS (Distributed Consensus Store) such as etcd/Consul/ZooKeeper as the consensus storage. However:

- **External DCS dependency**: operational overhead of an etcd cluster, and a full PG HA failure if it goes down
- **Python runtime**: running Patroni as a sidecar in a Go-based operator increases image size and security surface area
- **CloudNativePG validation**: the model of using the K8s API itself as the DCS and supervising PG via an instance manager (Go agent running as PID 1 inside the Pod) has been proven to work sufficiently in production

## Decision

This operator does not use Patroni. Instead:

1. **Instance Manager**: run a Go binary (`cmd/instance`) as PID 1 in each PG Pod to supervise the postgres child process.
2. **K8s API as DCS**: the Pod's role (primary/standby) is decided by leader election based on K8s lease objects (`coordination.k8s.io/v1`).
3. **CRD Status as topology authority**: `PostgresCluster.status.topology` holds the current RS primary roster.

## Rationale

1. **Operational simplification**: removes etcd dependency. The K8s control plane already guarantees consensus.
2. **Image/security**: single Go static binary, distroless base, zero external runtimes.
3. **CRD authority**: reduces the possibility of divergence between PG state and K8s state (removes the duplication between the state Patroni writes to etcd and the state the operator writes to K8s).
4. **CNPG precedent**: production-grade operational track record with the same model.
5. **Natural Citus integration**: the instance manager directly calls `citus_update_node` → a single responsible party propagates the new primary IP.

## Tradeoffs

- **K8s API server availability dependency**: an API server outage may block election.
  - **Mitigation**: the K8s control plane is already a premise of cluster operation. We assume the same availability class as PG. Additionally, PVC fencing prevents split-brain.
- **Patroni ecosystem tools (patronictl, etc.) not applicable**: operators cannot use their familiar CLI.
  - **Mitigation**: `kubectl pgo` or our own CLI will be provided in Phase 13. For general operations, `kubectl` + CR is enough.
- **Long-term license/maintenance risk**: perpetually maintaining our own instance manager code.
  - **Mitigation**: reference the Apache 2.0 code patterns published by CNPG (license-compatible). The core logic is on the order of hundreds of lines.

## Alternatives (considered and rejected)

- **Patroni sidecar**: operational burden of external DCS, dual authority (K8s vs etcd) problem
- **pg_auto_failover**: requires running a separate monitor node, K8s integration immature
- **Stolon**: lack of active maintenance

## Consequences

- `cmd/instance/main.go` is a separate binary, packaged as a distroless image
- All RS (CSS, SS) use the same instance manager
- K8s lease naming convention: `<cluster>-<rs-name>-primary` (e.g., `orders-css-primary`, `orders-shard-a-primary`)
- Changes to this ADR require an RFC
