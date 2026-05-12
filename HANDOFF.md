# HANDOFF — postgres-operator

> The next session must be able to resume *without conversation
> context*. Reading order on startup: this file → `TASKS.md` → the
> latest commit log.

## Current state (2026-05-12, 0.3.0-alpha.18)

- Release: `0.3.0-alpha.18` — GHCR image + Helm chart published.
  Chart `version` and `appVersion` plus `config/manager/kustomization.yaml`
  `newTag` and the `dist/install.yaml` image tag are all aligned (a
  `make validate` assertion blocks future drift).
- OLM bundle: `bundle/manifests/` is aligned to **8 owned CRDs**
  (PostgresCluster, BackupJob, ScheduledBackup, Pooler,
  PostgresDatabase, PostgresUser, ImageCatalog, ClusterImageCatalog),
  with alm-examples for 7 of them and CSV descriptions for all 8.
  `operator-sdk bundle validate --select-optional suite=operatorframework`
  is clean. Bundle package name: `keiailab-postgres-operator`.
- community-operators draft PR:
  [k8s-operatorhub/community-operators#8109](https://github.com/k8s-operatorhub/community-operators/pull/8109).
  Remaining: maintainer-owned bundle image push to
  `ghcr.io/keiailab/postgres-operator-bundle:0.3.0-alpha.18` (requires a
  PAT with `write:packages` scope) → CI green → flip to Ready → merge.
- argos production cluster: ArgoCD `platform-data-postgres-operator`
  Synced/Healthy, `PostgresCluster/argos-postgres` Ready=True (Day-0
  single-shard, no HA replicas yet).

## Active work

| Task | Stage / % | Notes |
|---|---|---|
| T26 OSS/OLM standards alignment | Complete 100% | spdystream CVE fix, OLM bundle, CHANGELOG/SUPPORT, README, 4 new `make validate` gates, lefthook DCO enforced. |
| T27 Live kind smoke + Pooler built-in auth + password rotation | Implementation 98% | `SMOKE_DATABASE` / `SMOKE_USER` / `SMOKE_SCHEDULEDBACKUP` / `SMOKE_IMAGECATALOG` scenarios + built-in auth (`keiailab_pooler_pgbouncer`) + rotation annotation. PostgresDatabase / PostgresUser `status.applied` non-convergence root-caused (finalizer Requeue race + statusUpdate conflict swallow) and fixed in single-pass apply + retry. Live PG18 re-run in progress to confirm. |
| T28 community-operators PR | Implementation 60% | Draft PR opened (#8109). Awaiting bundle image push + CI. |
| T29 Pooler TLS auto-issuance | Implementation 70% | cert-manager `Certificate` CR auto-issuance via `spec.pgbouncer.autoTLS` (stage 1 spec + stage 2 controller). Live cert-manager kind drill still pending. |

## Local 4-layer gate

The release gate is enforced locally (GitHub Actions is permanently
forbidden per ADR-0009 / RFC-0002).

```bash
make hooks-install       # lefthook pre-commit / commit-msg / pre-push
make gate                # lint + test + audit + validate (one-shot)
make validate            # CRD count + 18 monitoring grep + version drift + sdk validate + kube-linter
make audit               # govulncheck + trivy fs HIGH/CRITICAL + gosec
```

`make validate` enforces (among others):

- `bundle/manifests/postgres.keiailab.io_*.yaml` count ≥ 8.
- `operator-sdk bundle validate ./bundle` (default + operatorframework suite).
- Chart `appVersion` ↔ kustomize `newTag` ↔ dist image-tag drift.
- `.github/workflows/` absence (RFC-0002 / ADR-0009).
- kube-linter on `dist/install.yaml` and the helm-template output.

## Next-session entry points

### To finish T28 (community-operators)

1. Maintainer obtains a PAT carrying `write:packages` scope and authenticates:
   `gh auth refresh -s write:packages` or
   `echo $PAT | docker login ghcr.io -u eightynine01 --password-stdin`.
2. `make bundle-build VERSION=0.3.0-alpha.18` (already passes; just rebuild if needed).
3. `docker push ghcr.io/keiailab/postgres-operator-bundle:0.3.0-alpha.18`.
4. On PR [#8109](https://github.com/k8s-operatorhub/community-operators/pull/8109)
   wait for the community-operators CI; flip the PR to Ready when green.

### To finish T29 (Pooler TLS)

- Stage 3 — live cert-manager kind drill: install cert-manager, create an
  `Issuer` (or self-signed root), apply
  `config/samples/postgres_v1alpha1_pooler_autotls.yaml`, and confirm the
  Pooler Deployment mounts the cert-manager-issued Secret.
- Stage 4 — self-signed fallback for cert-manager-less environments.
- Stage 5 — automatic rotation observability (status condition that tracks
  cert-manager `Certificate.status.notAfter`).

### To finish T27 (live kind smoke)

- Run the 5 scenarios × PG17 / PG18 matrix:
  ```bash
  SMOKE_DATABASE=1 SMOKE_USER=1 SMOKE_POOLER=1 \
  SMOKE_SCHEDULEDBACKUP=1 SMOKE_IMAGECATALOG=1 \
  CR_NAME=quickPG18 SHARD_REPLICAS=0 \
  ./hack/smoke.sh
  ```
  Repeat with `PG_MAJOR=17 POSTGRES_VERSION=17 CR_NAME=quickPG17`.
- Record the result in `docs/operator-guide/cross-validation-cnpg.md`
  (move the matching rows from ⚠️ → ✅).

### To rotate the Pooler built-in auth password (T27 ⑥)

```bash
kubectl annotate pooler <name> postgres.keiailab.io/rotate-pooler-password=true --overwrite
# The operator runs ALTER ROLE, updates the userlist.txt Secret in place,
# strips the annotation, and records status.builtinAuthLastRotation.
```

## Reference

- `README.md` — quickstart and the 8 CRD surface table.
- `ROADMAP.md` and `docs/roadmap.md` — Gates G0–G6 with sub-task checklists.
- `TASKS.md` — full P1 task table (F01a–T29) and the next-phase preview.
- `CHANGELOG.md` — Keep-a-Changelog history through 0.1.1-alpha → 0.3.0-alpha.18.
- `docs/operator-guide/cross-validation-cnpg.md` — feature matrix vs CloudNativePG.
- `docs/operator-guide/community-operators-onboarding.md` — community-operators PR procedure.
- `SUPPORT.md` / `SECURITY.md` / `CONTRIBUTING.md` / `GOVERNANCE.md` / `MAINTAINERS.md` — community policy surface.
