# ADR-0005: Release channels (alpha/beta/stable) and CRD apiVersion evolution

- Date: 2026-05-02
- Status: Accepted
- Authors: @phil

## Context

This operator reaches v1.0 on a roughly 6-year timeline of P0~P7. During that period users must be able to gradually start *production adoption*, while at the same time the fast iteration of the alpha stage must be protected. A single-channel rolling model would force breaking changes on alpha users, and semver pre-1.0 (`0.x.y`) alone cannot clearly tell users *which 0.x is stable*. The CRD apiVersion must also follow the K8s standard evolution path (v1alpha1 → v1beta1 → v1), on top of the operator-managed CRD model from ADR-0004, and each stage must guarantee safe migration via a conversion webhook.

## Decision

Operate 3 release channels (alpha / beta / stable) and evolve the CRD apiVersion in lockstep with the phases.

Key parameters:

- **Channels**:
  - `alpha` — P0~P3 (chart `0.3.x`~`0.6.x`). Free to make breaking changes. No compatibility guarantees. High release cadence (1~2 times per month).
  - `beta` — P4~P5 (chart `0.7.x`~`0.8.x`). On field deprecation, at least 6 months + 1 minor of advance notice. Best-effort backward compatibility. Medium release cadence (once per quarter).
  - `stable` — P6~ (chart `0.9.x`, `1.x.y`). 24-month LTS. semver patch/minor compatibility. Major (1.x → 2.x) requires a mandatory 12-month advance RFC + migration guide.
- **CRD apiVersion evolution**:
  - `v1alpha1` — P0~P3 stage. All CRDs (PostgresCluster, ShardRange, ShardSplitJob, BackupJob).
  - `v1beta1` — upon entering P4. v1alpha1 is served concurrently (storage version is v1beta1). Conversion webhook is deployed.
  - `v1` — upon entering P6. v1beta1 is served (deprecated, removed after 24 months). Storage version is v1.
- **Image / chart tag mapping**:
  - alpha: `quay.io/postgres-operator/manager:v0.X.Y-alpha.N` + chart `version: 0.X.Y-alpha.N`.
  - beta: `:v0.X.Y-beta.N` + chart `0.X.Y-beta.N`.
  - stable: `:v1.Y.Z` + chart `1.Y.Z`. (0.9.x is the RC stage of stable, called out separately.)
- **Helm repo index**: each channel uses a separate repo URL, or is split within the same repo via `Chart.yaml` annotations (`artifacthub.io/channel: alpha|beta|stable`).
- **Breaking change policy**:
  - alpha → beta promotion: a migration guide (`docs/migrations/alpha-to-beta.md`) is mandatory for all alpha users.
  - beta → stable promotion: deprecated fields are removed at the stable entry point. After that, 24 months of LTS.
  - Within a channel (e.g., alpha → next alpha) breaking changes also require an explicit `BREAKING CHANGE:` entry in CHANGELOG + a migration note.
- **Conversion webhook**:
  - Hosted in the same pod as the manager (no separate Deployment, simplifying the P4 stage).
  - Uses certificates issued by cert-manager (no separate ADR — integrated into this one).
  - Conversion is mandatory between all pairs v1alpha1 ↔ v1beta1 ↔ v1 (bidirectional to the storage version).
- **Deprecation markers**:
  - CRD field: `// +kubebuilder:deprecatedversion:warning="..."` annotation.
  - values.yaml key: `deprecated: true` in `values.schema.json` + a warning in NOTES.txt.
- **User adoption guide (new `docs/channels.md`)**:
  - "Production usability starts at P1 (single-shard, alpha). Note that the alpha channel may break — migration is required when entering beta."
  - "Do not mix channels in a single cluster. One channel per cluster."

## Consequences

Positive:

- User trust — "which version is safe for production" is conveyed instantly by the channel name.
- Protects fast iteration for alpha users — experimentation is possible without impact on beta/stable users.
- CRD apiVersion evolution exactly follows the K8s standard pattern — compatible with kubectl and CRD-aware tools (Argo CD, Flux).
- The conversion webhook safely handles *mixed-version CRs* within the cluster.
- ArtifactHub's channel separation prevents accidents where users unintentionally pick up alpha.

Negative:

- Multi-channel maintenance cost — a single maintainer running alpha + beta + stable simultaneously has a backport burden. Until entering P6, the stable channel is empty so the burden is gradual.
- Conversion webhook operational burden — cert rotation, webhook availability; mitigated by adopting simple single-pod hosting at the P4 timepoint.
- LTS 24-month commitment — after entering stable, the RFC for the next major must start 12 months in advance.

Trade-offs:

- Operational complexity from channel separation ↔ user trust + production adoption potential. User trust wins (operator adoption rate is a function of trust).
- Additional components from the conversion webhook ↔ CRD evolution safety. Evolution safety wins (avoid data loss).
- Multi-channel backport burden ↔ alpha user iteration speed. Iteration speed wins (the essence of alpha).

## Alternatives Considered

| Alternative | Reason for rejection |
|---|---|
| (a) Single-channel rolling — semver only (`0.x.y` → `1.0.0` → `1.x.y`) | Ambiguous expression of pre-1.0 stability. No protection for alpha users — a `0.4.0` user is exposed to breaking changes in `0.5.0`. |
| (b) 2 channels (stable + edge) | edge would have to absorb both alpha and beta, but it cannot express the difference in compatibility commitments between alpha and beta. |
| (c) 4+ channels (nightly + alpha + beta + stable + LTS) | Excessive for a single-maintainer environment. nightly can be replaced by commit-id-based images (`:sha-abc1234`). |
| (d) Keep v1alpha1 forever with no CRD apiVersion evolution | Violates K8s conventions. CRD-aware tools (kubectl explain, Argo CD diff) do not present alpha as production. |
| (e) Immediate transition v1alpha1 → v1beta1 with no conversion webhook (only storage version change) | Reads of remaining v1alpha1 CRs in the cluster fail. Risk of data loss. |
| (f) Channel labels only (identical image tags) | The helm repo / ArtifactHub cannot recognize channels. Accident where users unintentionally pick up alpha. |
| (g) Date-based versioning (`2026.05.0`) | Loss of semver's breaking/feature/patch semantics. Violates K8s ecosystem conventions. |

## References

- ADR-0001 (self-built distributed SQL) — origin of the phase definitions
- ADR-0004 (operator-managed CRD) — premise of this ADR's CRD lifecycle
- standards/adr.md — formal standard
- standards/commits.md — uses the `BREAKING CHANGE:` footer of Conventional Commits
- Kubernetes API versioning conventions (https://kubernetes.io/docs/reference/using-api/#api-versioning)
- ArtifactHub channel annotations spec
- Operator SDK conversion webhook guide
