# ADR 0007 — Split Helm chart out of P14 and into the P1 track

- **Status**: Accepted
- **Date**: 2026-04-30
- **Decision makers**: @keiailab/maintainers
- **Related**: roadmap.md (14 Pillar), Bitnami PostgreSQL Helm Chart comparison (`/Users/phil/.claude/plans/1-https-artifacthub-io-packages-helm-bit-sunny-wozniak.md` §5 P1-4)

## Context

`docs/roadmap.md` places the Helm chart under **P14 Distribution** — bundled with install.yaml, OLM bundle, and multi-arch image. P14 is defined as the *final step before v1.0 GA*, entered after all other Pillars (P1~P13) pass M3.

The problem: how do users *install* the operator during alpha/beta? Currently only Kustomize manifests are provided (`config/default`). The actual user channel is *Helm chart, which is the de facto standard*, so:

- alpha-stage users cannot even try it → feedback loop blocked
- The "PGO-class" parity promise also includes *distribution channel parity*, but it is absent until P14
- Bitnami's chart is *the only channel* and very mature — to the same market users, "Kustomize and nothing else" is friction to adoption

## Decision

Separate the Helm chart *from P14* and reposition it as a follow-up task of the P1 Core Lifecycle track (P1-T5). P14 retains only the *remaining* distribution artifacts:

| Before (P14) | After |
|---|---|
| Helm chart | **Moved to P1 track** (alpha user channel) |
| install.yaml | Stays in P14 |
| OLM bundle | Stays in P14 |
| multi-arch image | Stays in P14 |

### Chart separation model

Two separately packaged charts under `charts/`:

- `charts/postgresql-operator/` — the operator itself (Deployment + RBAC + CRD + NetworkPolicy + ServiceAccount)
- `charts/postgrescluster/` — PostgresCluster CR instance (optional, P1-T5 sub-task)

This is *the reverse* of Bitnami's pattern of `postgresql` (CR instance) + a separate operator chart — the operator chart is *top*, and the CR chart is *optional*. Reason: users install the operator once, and there are N CRs per namespace.

## Rationale

### Why *separate* rather than move all of P14
P14's install.yaml, OLM, and multi-arch are *trailing artifacts* — it is safe to produce them after all CRDs are frozen. However, the Helm chart can be packaged with the *current CRD state*, so it is feasible *now*. Pulling OLM/multi-arch forward forces repackaging on every change while *all* CRDs are unstable in alpha.

### Why two charts
- The *operator* is cluster-scope and installed once
- A *PostgresCluster CR* is N per namespace — for a chart to support multiple instances, it must be separated per helm release
- Same reasoning Bitnami has for putting `postgresql` (CR instance chart) as the main one

### Why *now* — the cost of keeping the current P14 placement

| Cost | Impact |
|---|---|
| No alpha users | Zero real usage feedback, late discovery of regressions |
| User attrition to Bitnami | Loss of brand recognition for the "PGO-class" promise |
| Burden of postponing chart writing to right before v1.0 | Per-CRD template + values defaults all decided at once → regression risk |

## Tradeoffs

- **Maintaining two paths simultaneously**: Kustomize (`config/default`) + Helm (`charts/postgresql-operator`) operated in parallel → burden of updating two places on changes. Mitigation: have chart templates generate the manifests in `config/` (consider automation via `kustomize build config/default | helmify` in a Makefile target).
- **Chart stability promise**: alpha charts have `Chart.yaml: appVersion` at v0.x, so breaking changes are possible. Users must be aware. Mitigation: state "alpha stage, breaking changes possible" in the chart README.
- **Keep OLM bundle separate**: OperatorHub users still wait until P14 — acknowledge the user segment difference and keep P14 priority.

## Consequences

- Update P14 definition in `docs/roadmap.md` — move the Helm chart to the *P1 track*.
- New directory `charts/postgresql-operator/` (at P1-4 recommended implementation time).
- Add `chart-package`, `chart-lint` targets to the Makefile.
- Register P1-4 recommendation in TASKS.md.
- This ADR is *re-evaluated* at v1.0 GA time — review chart stability and OLM bundle integration timing.

## Enforcement mechanism

| Mechanism | Location | Introduction timing |
|---|---|---|
| roadmap.md update | `docs/roadmap.md` §14 Pillar | Same time as this ADR |
| Chart skeleton | `charts/postgresql-operator/` | P1-4 |
| chart lint CI step | Makefile + local hook | P1-4 |
| `helm install --dry-run` regression | e2e | P1-4 |
