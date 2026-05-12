# ADR-0013: OperatorHub.io bundle scaffold (PR-B9 cross-cut)

- Date: 2026-05-10
- Status: Accepted
- Authors: @eightynine01

## Context

valkey-operator PR-B9 (ADR-0037 in valkey repo) established the technical
prerequisites for OperatorHub.io registration. Cross-cut unification — postgres-operator + mongodb-operator also gain external OperatorHub discoverability via
the same bundle scaffolding. Aligned with ADR-0016 (Cross-cut Audit Pattern,
from mongodb).

## Decision

Byte-identical port of the valkey pattern:
1. `config/manifests/bases/postgres-operator.clusterserviceversion.yaml` —
   2 CRDs owned (PostgresCluster, BackupJob), metadata (description / keywords
   / maintainers / provider / maturity=alpha / minKubeVersion=1.26.0).
2. `config/manifests/kustomization.yaml` — CSV + crd + rbac + manager + samples.
   webhook is excluded due to the absence of a kustomization.yaml
   (`config/webhook/manifests.yaml` is a single file) — OLM handles webhook
   deployment automatically.
3. Makefile `bundle` / `bundle-build` targets (operator-sdk 1.42 + kustomize).
4. alm-examples — 2 samples (dev + prod) inline JSON.

Updating the image tag in `config/manager/kustomization.yaml` is *omitted* in
this PR — the postgres release pipeline already handles `kustomize edit set image`
at image-push time.

## Consequences

Positive:
- Cross-cut unification across 3 operators (valkey + postgres + mongodb later).
- `make bundle VERSION=...` is reproducible — entry point for release-
  automation follow-ups.
- 2 CRDs are explicitly listed in `customresourcedefinitions.owned` —
  OLM catalog is accurate.

Negative:
- alm-examples absent for BackupJob (sample file missing) — operator-sdk
  warning. Add a BackupJob sample in the follow-up PR-B9.2.1.
- Compared to valkey, `containerImage` is `0.3.0-alpha.15` (alpha) — at the
  community-operators PR time, a decision to split into a stable channel
  is required.

## Alternatives Considered

1. **A different pattern from valkey**: rejected. Cross-cut unification ↑.
2. **Include webhook**: rejected. Adding a kustomization.yaml under
   config/webhook is a separate task (impacts kubebuilder regenerate). OLM
   can handle webhook deployment automatically.

## References

- valkey ADR-0037 (OperatorHub bundle scaffold).
- ADR-0016 (mongodb): Cross-cut Audit Pattern.
- operator-sdk 1.42: <https://sdk.operatorframework.io/docs/olm-integration/>.
- Follow-up: PR-B9.2.1 add BackupJob sample, PR-B9.3 submit community-operators PR.
