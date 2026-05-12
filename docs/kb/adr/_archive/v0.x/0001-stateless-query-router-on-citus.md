# ADR 0001 — Mission Redefinition: PGO-class full stack + Citus first-class + Plugin SDK

- **Status**: Accepted (redefined, updated 2026-04-27)
- **Date**: 2026-04-26 (original) → 2026-04-27 (redefined)
- **Decision makers**: @keiailab/maintainers
- **Supersession history**:
  - v1 (2026-04-26): "MongoDB sharded topology on Citus" abandoned → "Citus standard + Stateless QueryRouter single differentiator" adopted
  - **v2 (2026-04-27, this document)**: "Single differentiator" narrow positioning abandoned → "PGO-class full stack + Citus first-class + Plugin SDK" three-axis adopted
- **Related**: ADR 0002 (no Patroni), ADR 0003 (QueryRouter Stateless), **ADR 0004 (Build, not Fork/Layer)**
- **Prior analysis**: `/Users/phil/.claude/plans/squishy-squishing-harp.md` §7~§10

## Context

The v1 ADR adopted the narrow positioning of "Citus + Stateless QueryRouter single differentiator". However, as the user vision expanded to **"a highly extensible, flexible, commercial-grade open-source PG Kubernetes operator"**, the narrow positioning creates the following contradictions.

1. **Commercial quality promise vs. narrow differentiator conflict**: The v1 position of "we don't compete on single PG HA" only holds while PGO/CNPG cover that area. Once the user has promised "PGO-level quality", we must take direct responsibility for single PG HA as well.
2. **Absence of meaning for extensibility**: The essence of "flexible" is not the diversity of CRD fields the user sees, but **an SDK structure that allows adding a new backup tool, exporter, extension, or router within a week**. v1 lacked this meta-differentiator.
3. **Treating Citus as the "sole differentiator" puts the entire project at risk** if Citus licensing trends change. We must keep Citus as a first-class feature, but distribute risk through additional axes of single PG HA operation and Plugin SDK.

## Decision

We redefine the mission of this operator along the following three axes.

### Mission (one sentence)

> A single Apache-2.0 Go operator that delivers **PGO-level single PG HA operational quality** + **first-class support for Citus distributed topology** + **plugin SDK-based extensibility** all at once.

### Meaning of the three axes

1. **PGO-class full stack (Pillar P1~P10, P14)**
   - Provide single PG HA operational quality on par with Crunchy PGO 6.0.1, in our own code
   - HA, backup/PITR, pooler, monitoring, security, upgrade, extension management, multi-K8s standby
   - Direct code responsibility — no external operator dependency (ADR 0004)
2. **Citus first-class (Pillar P11~P12, the first differentiator of this project)**
   - Single CR representation of `coordinator + workers[]`
   - Automatic `pg_dist_node` metadata sync
   - Declarative distributed tables via `DistributedTable`/`ReferenceTable`/`RebalanceJob`/`ShardPlacementPolicy`
   - **Stateless QueryRouter layer** (ADR 0003 retained)
   - **Distributed PITR** (`citus_create_restore_point` 2PC coordinator)
3. **Plugin SDK (Pillar P13, meta-differentiator)**
   - Five interfaces: `BackupPlugin`/`ExporterPlugin`/`ExtensionPlugin`/`RouterPlugin`/`AuthPlugin`
   - Core reconcilers call only interfaces (concrete implementation imports prohibited — linter-enforced)
   - Two models: in-process (compile-time) + out-of-process (gRPC over UDS)

### Non-negotiable quality bar (PGO 6.0.1 baseline vs. this project's v1.0 GA commitment)

| Dimension | PGO 6.0.1 baseline | This project's v1.0 GA commitment |
|---|---|---|
| Supported PG majors | 14, 15, 16, 17, 18 | **16, 17, 18** (14/15 near EOL, P10 revisit) |
| HA mechanism | Patroni distributed consensus + Pod anti-affinity | **K8s API as DCS + own instance manager** (ADR 0002) |
| Backup | pgBackRest 2.58, multi-repo (local/S3/GCS/Azure) | **pgBackRest primary GA, WAL-G/Barman plugins** (P4) |
| Pooler | PgBouncer 1.25 | **PgBouncer 1.25+** (sidecar + standalone modes) |
| Monitoring | pgMonitor (Prometheus + Grafana + Alertmanager) | **pgMonitor-compatible dashboards + Citus-specific metrics** |
| Security | Full TLS, custom CA | **TLS + mTLS + cert-manager integration** |
| Upgrade | major in-place | **major in-place + bidirectional blue/green** |
| Multi-K8s standby | ✅ | **P14, async + sync options** |
| Base image | UBI 9 (Red Hat) | **UBI 9 + Debian bookworm dual** |
| Architectures | amd64 + arm64 | amd64 + arm64 (matrix build) |
| Bundled extensions | pgaudit, pg_cron, pg_partman, pgnodemx, set_user, pgvector, postgis, timescaledb, wal2json | **First 7 + Citus + pgvector** (timescaledb·orafce in v1.x) |

### Four things PGO does not do (the reason this project exists)

1. **First-class Citus topology** — single CR for `coordinator + workers[]` + automatic metadata sync
2. **Stateless QueryRouter** — Citus 11+ metadata-synced PG + PgBouncer sidecar + HPA
3. **Distributed PITR** — `citus_create_restore_point` 2PC coordinator
4. **Plugin SDK** — backup/exporter/extension/router-plugin abstracted as interfaces so external modules can be added

### Topology (unchanged — v1 ADR retained)

- **Coordinator** (HA replica set): standard Citus coordinator. Authority of `pg_dist_*` metadata, distributed DDL gateway.
- **Worker** (HA replica set per pool): standard Citus worker. Holds shards of distributed tables.
- **QueryRouter** (new, stateless): Citus 11+ `metadata_synced=true` PG + PgBouncer sidecar. Stateless, HPA. See ADR 0003.

### CRD naming — Citus standard retained (v1 ADR retained)

- API Group: `postgres.keiailab.io`
- Root CR: `PostgresCluster` (naming collides with PGO but the groups differ, so it is safe)
- Auxiliary CRDs: `DistributedTable`, `ReferenceTable`, `RebalanceJob`, `ShardPlacementPolicy`, `BackupJob`, `PgUser`, `PgDatabase`, `ClusterUpgrade`
- Separating `QueryRouter` CRD vs. `PostgresCluster.routers` subfield: **delegated to RFC 0009**

## Rationale

### Why we expand from narrow differentiator to full stack + SDK

- **A commercial quality promise requires control** (ADR 0004 §Reasons option B was rejected): once we have promised "PGO-level", we must own the entire code path of that quality.
- **Plugin SDK is the real meaning of "flexibility"**: not increasing CRD fields, but having a structure that lets you add a new backup tool within a week. This structure is only meaningful when we enforce an interface-calling convention across the entire controller codebase.
- **Distributing the risk of single Citus dependency**: even if Citus changes license (e.g., AGPL tightening), the PGO-class area of this project preserves value.

### Parts of v1's "single differentiator" thesis that are still valid

- **Resource concentration priority**: we aim to put more than 60% of maintainer time into the three differentiator areas P11/P12/P13. P1~P10/P14 target PGO parity, so we can borrow proven patterns.
- **Positioning message**: external marketing does not expose the three axes "PGO-class quality + Citus first-class + Plugin SDK" equally. **The four differentiators (axes 2 and 3 in §three axes) are fixed at 70% of the message**, with PGO parity mentioned in a single line as "baseline quality".

## Tradeoffs

- **3~4× increase in short-term workload**: compared to option B (soft layer). Time to v1.0 GA may go from 18~21 months → 24~30 months (depending on maintainer availability).
  - **Mitigation**: 6-track parallel progress based on the 14 Pillar dependency graph, recruiting Pillar-owning contributors (specified in governance).
- **Direct comparison with PGO in the single PG HA area**: users will ask "why this and not PGO?".
  - **Mitigation**: the README comparison table honestly states "if you only need single PG HA, prefer PGO/CNPG". Our audience is teams that need Citus distributed + plugin extension.
- **Increased marketing message complexity**: "single differentiator" can be described in one sentence, but three axes cannot.
  - **Mitigation**: fix the first sentence of the README as a two-sentence pattern: "Citus + Plugin SDK is the differentiator. Single PG HA is provided at PGO-level quality in our own code".

## Enforcement mechanism

1. **Update the README "Why another PG Operator" table** — applied simultaneously with this ADR adoption (queue A2 task).
2. **Reflect 14 Pillar structure in `docs/roadmap.md`** — queue A3 task.
3. **Quality gates** (plan §9.8): unit ≥80%, e2e matrix, chaos test, SBOM, cosign signing, CVE SLA — v1.0 GA conditions.
4. **Plugin SDK interface freeze**(P13-T1) is specified as a prerequisite before entering any other Pillar (plan §10.3).
5. **Changes to this ADR (adding/removing mission axes) require an RFC** — GOVERNANCE.md "architecture change" procedure.

## Consequences

- PGO parity matrix promise at v1.0 GA (plan §9.12).
- Pillars P1~P14 are all written in our own code (ADR 0004).
- The 5 Plugin SDK interfaces (P13-T1) are de facto prerequisites for other Pillars.
- External marketing: "Citus first-class + Plugin SDK" message at 70% / "PGO-class quality" at 30%.

---

## Appendix A — v1 ADR body (preserved for history)

> v1 adopted "Citus standard + Stateless QueryRouter single differentiator". v2 expanded it, but the topology and CRD naming decisions of v1 remain valid, so they are preserved in this appendix.

### v1 decision summary (unchanged)

- **Coordinator + Worker topology retains Citus standard naming**
- **CRD root representation**: `apiVersion: postgres.keiailab.io/v1alpha1`, `kind: PostgresCluster`, `spec.{coordinator, workers, routers}`
- **Auxiliary CRDs**: `RebalanceJob`, `ShardPlacementPolicy`, `DistributedTable`, `ReferenceTable`
- **Reason for abandoning the MongoDB topology model**: "shard" naming collision, CSS/SS is merely a renaming of the Citus standard, and the only real value is the separation of Router. This part still applies in v2, and "Router separation" is explicitly listed as #2 of the four differentiators.

### Explicit differences between v1 and v2

| Item | v1 (2026-04-26) | v2 (2026-04-27) |
|---|---|---|
| Differentiator definition | Single (Stateless QueryRouter) | Four (Citus first-class + Router + Distributed PITR + Plugin SDK) |
| Single PG HA responsibility | "Will not compete" | **"Direct responsibility at PGO level"** |
| External operator dependency | Undecided | **Prohibited (ADR 0004)** |
| Plugin SDK | Not mentioned | **Introduced as first-class via P13** |
| Marketing message | "Citus distributed PG K8s operator" | "Citus + Plugin SDK differentiator + PGO-class quality" |
| Roadmap | 14 Phase × 10-month timeline | **14 Pillar × DoD-based (dates removed)** |
