# ADR-0010: License + Sharding Strategy (Citus AGPL Isolation, vanilla PG default)

- Date: 2026-05-01
- Status: Accepted
- Authors: @keiailab
- Supersedes: in part the "Citus first-class" premise of ADR-0001 (stateless QueryRouter on Citus)

## Context

Since the initial ADR-0001 v2 ("Citus first-class"), this operator has assumed the Citus extension as the *default* for distributed SQL — reflected in many files such as chart samples, version matrix, and plugin model. However, a license review right before 0.2.0-alpha confirmed the following:

1. **Citus license = AGPL-3.0** (GNU Affero General Public License v3, copyleft + network use clause).
   - Directly verified from the LICENSE file header.
   - Microsoft retained AGPL even after acquiring it in 2019 — an intentional policy to block SaaS competitors.
2. **This operator license = Apache-2.0** (chart annotation, repo LICENSE).
3. AGPL-3.0 §13 (network use) creates an obligation to provide source code (including modifications) to users when *modified AGPL code is provided to users in SaaS form*.
4. This operator *does not include Citus source*, and only *manipulates* a PostgreSQL extension running in a separate process via SQL/control messages (mere aggregation likely). The operator code itself can be kept Apache-2.0 clean.
5. However, *users who activate Citus through this operator* bear AGPL §13 obligations — following the default without awareness can lead to license violations.

Also, PostgreSQL 18.3 was released as stable on 2026-02-26, but **Citus had not released a PG18-compatible minor as of 2026-05** (latest Citus 14.0/13.2 supports up to PG17). That is, the "Citus first-class + latest PG" combination is impossible in the short term.

## Decision

This ADR decides the following four:

1. **Switch the default stack to vanilla PostgreSQL 18**. The Stable channel of the version matrix includes only vanilla PG 16/17/18.
2. **Isolate Citus integration as Beta channel opt-in**. It is activated only when the user explicitly sets `spec.version.citus` + `spec.extensions: [{name: citus}]` + `citusLibPQ.dsn`. Users who activate it are deemed to have explicitly accepted the AGPL-3.0 §13 obligations.
3. **The operator code stays Apache-2.0 clean**. We do not include or link Citus source, and only manipulate a PostgreSQL extension running in a separate process via SQL/control messages.
4. **Reserve a follow-up RFC for native sharding plugin** (RFC-0005) to secure a path to gradually remove Citus dependency in the long term. RFC-0005 decomposes the 7 core Citus mechanisms (distributed query planner, executor, placement, rebalancer, 2PC, reference tables, columnar storage) and derives a ShardingPlugin interface that extends this operator's 5-interface Plugin SDK.

## Consequences

### Positive

- **License safety**: the operator itself is Apache-2.0 clean. Prevents accidents where users unknowingly bear AGPL obligations.
- **Immediate PG 18 activation**: vanilla PG18 can be provided immediately on the Stable channel without waiting for a Citus-compatible minor release. PostgreSQL 18.3's new features (asynchronous I/O, partitioning improvements, virtual generated columns, etc.) are immediately usable.
- **Simple default**: new users can use it for production without separate license review.
- **Long-term autonomy**: once RFC-0005 Native sharding plugin is implemented, an Apache-2.0 compatible path is also secured for distributed SQL scenarios.

### Negative

- **Short-term distributed SQL regression**: at 0.2.0-alpha time, there is *no* Apache-2.0 compatible distributed SQL option. Users for whom distributed SQL is mandatory must choose among (a) Citus opt-in with AGPL burden, (b) waiting for RFC-0005, or (c) considering external distributed solutions (YugabyteDB, CockroachDB).
- **Weakened ADR-0001 differentiator**: half of "Citus first-class + stateless QueryRouter" differentiator (the Citus part) moves out of the default. Stateless QueryRouter is still valuable on vanilla PG, but market messaging needs realignment.
- **Cost of building a native sharding plugin**: Citus has ~500K LOC accumulated over 10 years. Self-built sharding is a multi-year effort. See RFC-0005 for details.
- **breaking change**: `VersionSpec.Citus` Required → Optional, Stable channel entries changed. Signaled by SemVer 0.2.0 bump.

### Neutral

- The existing PG 16/17 + Citus 12.1/13.0 combinations are *retained* in the matrix, demoted to Beta. Existing users may continue to use them under explicit opt-in.
- License disclosure is added to chart NOTES.txt + plugin docs + sample yaml — license surfaced at all user entry points.

## Alternatives Considered

### A. Keep Citus as default + only strengthen license warnings

- Reason for rejection: risk of users unknowingly bearing AGPL §13 obligations. "Strengthened warnings" are easily ignored under pressure to accept defaults.
- Tradeoff: short-term feature richness vs. long-term license incident risk.

### B. Remove Citus entirely + implement native sharding immediately

- Reason for rejection: native sharding implementation is multi-year. Immediate removal leaves distributed SQL users with nowhere to go.
- Tradeoff: clean license boundary vs. feature gap (12-24 months).

### C. Use only Citus FDW (postgres_fdw + implement sharding directly)

- Reason for rejection: postgres_fdw is under PostgreSQL License (BSD-like), so no AGPL impact. However, no distributed query planning → push-down limitations. Not a full sharding solution.
- Tradeoff: license safety vs. feature limitations. Worth considering as a short-term placeholder (RFC-0005 Phase 2A candidate).

### D. Dual license negotiation (purchase commercial Citus usage rights)

- Reason for rejection: this operator is open-source alpha. Cost + lack of operational feasibility.

### E. Fork Citus and change the license (e.g., redistribute under PostgreSQL License)

- Reason for rejection: under AGPL's viral clauses, forks must also retain AGPL. License-change forks are invalid + legal liability arises.

## Rationale for choice (B vs. this decision)

- Short-term: preserve Citus opt-in path → prevent existing distributed SQL users from leaving.
- Long-term: once RFC-0005 native sharding is complete, Citus can be removed → gradual transition.
- Operator's own license protection achieves 95% just by *changing the default* (the user's default behavior stays in the license-safe region).

## Action Items

- [x] AI-001: Reconfigure the Stable channel of `internal/version/matrix.go` to vanilla PG.
- [x] AI-002: Make `VersionSpec.Citus` Optional + remove webhook PG18 feature gate verification.
- [x] AI-003: Remove citus from chart `config/samples/*` default extensions.
- [x] AI-004: Update Chart.yaml description + keywords (Citus first-class → vanilla default).
- [x] AI-005: Add license disclosure to NOTES.txt.
- [x] AI-006: Add AGPL §13 warning to the `internal/plugin/extension/citus/` package doc.
- [ ] AI-007: ShardingPlugin interface definition PR when entering RFC-0005 Phase 2A.
- [ ] AI-008: Update README.md — realign "Citus first-class" messaging to "vanilla PG default + plugin extensibility".
