# ADR 0004 — Build from Scratch (Both PGO Fork and Soft Layer Rejected)

- **Status**: Accepted
- **Date**: 2026-04-27
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0001 (mission redefinition), ADR 0002 (no Patroni), ADR 0003 (QueryRouter Stateless)
- **Prior analysis**: `/Users/phil/.claude/plans/squishy-squishing-harp.md` §7 (Crunchy PGO comparison), §8 (strategy options A/B/C evaluation), §9 (redefined mission)

## Context

As the mission of this project expanded from "Citus + QueryRouter single differentiator" to **"PGO-class full stack + Citus first-class + Plugin SDK-based extensibility"** (ADR 0001 update), we re-evaluated the following three integration strategies.

| Option | Essence |
|---|---|
| **A. Hard fork** | Fork `crunchydata/postgres-operator` → remove Patroni → add Citus |
| **B. Soft layer (out-of-tree)** | Keep PGO as an upstream dependency, we only compose via a `CitusCluster` CRD |
| **C. Build from scratch** | Don't use PGO code; write from scratch ourselves, only borrow operational idioms proven by PGO |

All three options are license-compatible (Apache-2.0 ↔ Apache-2.0). The decision basis lies in **long-term control**, **maintenance debt**, **differentiator alignment**, and **responsibility for the commercial quality promise**.

## Decision

**We adopt option C (Build from scratch).** Both options A and B are rejected.

We do not import PGO code. The following idioms and patterns proven by PGO are **borrowed only conceptually** (safe because Apache-2.0 licenses are compatible):
- pgBackRest integration pattern (P4)
- pgMonitor dashboard structure (P6)
- Pod anti-affinity / fencing operational idioms (P2)
- `PostgresCluster` CRD naming (already adopted by ADR 0001)

## Rationale

### Reasons for rejecting option A (Hard fork)

1. **The cost of removing Patroni > the cost of writing from scratch**: the core of PGO HA is Patroni, and per ADR 0002 we adopt "K8s API as DCS + own instance manager". Removing Patroni would require rewriting 30% of PGO's reconcilers, and at that point the fork is effectively a separate project.
2. **Upstream catch-up debt**: PGO ships minors per branch + monthly patch releases. A cherry-pick or rebase strategy is essential, and after 6 months divergence makes it hard even to absorb security patches.
3. **Brand dilution**: the "PGO fork" label muddles our differentiator message of **"PGO-class + Citus first-class + Plugin SDK"**.
4. **Governance trust**: we have to build maintainer trust capital from scratch, but starting from a fork lets doubts about origin shape first impressions.

### Reasons for rejecting option B (Soft layer)

1. **Incompatible with commercial quality promise**: the ADR 0001 update specifies that "we take responsibility for PGO-level single PG HA operational quality". But in a soft layer, the surface we are responsible for is just one layer, `CitusCluster`, and HA/backup/pooling/monitoring quality is PGO's responsibility. **A contradiction where we made a quality promise but the quality control is external**.
2. **PGO API stability as hostage**: when major changes like the PGO v5→v6 transition happen, we have to absorb the compatibility cost of our adapter. A dependency without control will, long-term, bind our release schedule and our users' experience to PGO's schedule.
3. **Citus priority bug**(crunchydata/postgres-operator#3194)**pre-blocked**: PGO automatically prepends `pgaudit` to `shared_preload_libraries`, breaking the "Citus must be first" convention. Tying our release to an upstream PR merge further weakens schedule control.
4. **Extensibility impossible**: Plugin SDK (P13) is only meaningful when we enforce an interface-calling convention across the entire controller code. We cannot modify PGO's controllers, so in a soft layer the Plugin SDK applies only to our area (the `CitusCluster` composer), losing 70% of the meta-differentiator value.

### Reasons for adopting option C (Build)

1. **Long-term control**: **we own every code path** for HA/backup/pooling/monitoring/security/upgrade. The responsibility for the quality promise (ADR 0001 §non-negotiable bar) is clear.
2. **The meaning of the Plugin SDK survives**: the 5 P13 interfaces (Backup/Exporter/Extension/Router/Auth) apply to every controller area, enabling the promise that "adding a new backup tool = implementing an interface in a week".
3. **Differentiator alignment**: "concentrate resources on the four things PGO does not (Citus first-class, Stateless Router, distributed PITR, Plugin SDK)" is enforced through code structure.
4. **Legal simplicity**: since we do not import PGO code, there is no accumulation of Apache-2.0 NOTICEs or obligation to declare a fork. Simple idiom borrowing does not create license obligations.

## Tradeoffs

- **Large short-term time cost**: we must directly write the single PG HA operational quality PGO built up over 6+ years. The workload to v1.0 GA is roughly 3~4× compared to option B.
  - **Mitigation**: 6-track parallel progress per the 14 Pillar dependency graph (plan §10.3), and we make Pillar-owning contributor recruitment explicit in governance.
- **Directly compared with PGO in the single PG HA area**: users are bound to ask "why this and not PGO?".
  - **Mitigation**: state at the top of the README comparison table that "if you only need single PG HA, prefer PGO/CNPG. If you need Citus distributed + plugin extension, choose this project". Honest positioning.
- **Gray area of operational idiom borrowing**: how far can we reference pgBackRest integration code structure or pgMonitor dashboard JSON.
  - **Mitigation**: under Apache-2.0, borrowing patterns without code imports is safe. However, when direct copying is necessary (e.g., parts of dashboard JSON), record the source in the NOTICE file.

## Alternatives (additional options considered and rejected)

- **CNPG fork**: same problem as option A, plus CNPG's non-Citus assumptions are even more deeply embedded, raising costs further.
- **Place a PGO/CNPG adapter (`PostgresBackend` interface) at the core and add our own backend option in v1.x**: ends up with the same control problem as option B + the burden of tracking two codebases simultaneously.

## Enforcement mechanism

1. **Prohibit `crunchydata/postgres-operator` or `cloudnative-pg/cloudnative-pg` imports in `go.mod`** — custom golangci-lint rule.
2. **Track sources of borrowed idioms in the NOTICE file**: limited to areas of direct copying such as dashboard JSON, alert rules, and operations guides.
3. **Add "direct reference to PGO code" item to the PR review checklist** — if PGO code patterns are identified anywhere in `internal/`, `api/`, or `cmd/`, require a source comment or rewrite from scratch.
4. **Changes to this ADR (reverting to options B/A) require an RFC** — GOVERNANCE.md "architecture change" procedure.

## Consequences

- All Pillars (P1~P14) of this project are written in our own code.
- The README "Why another PG Operator" table is updated together with ADR 0001's update (queue A2 task).
- `docs/roadmap.md` is rewritten with the 14 Pillar × 4 Maturity Level + 68 task structure (queue A3 task).
- This decision guarantees that every work unit defined in §10 of the plan file `/Users/phil/.claude/plans/squishy-squishing-harp.md` proceeds without importing PGO code.
