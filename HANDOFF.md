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
| T27 Live kind smoke + Pooler built-in auth + password rotation | Complete 100% | PG18 iter#8 → 5/5 PASS + PG17 → 5/5 PASS across all five new-CRD smoke scenarios (Pooler 4-test, PostgresDatabase 2-test, PostgresUser 2-test, ScheduledBackup 2-test, ImageCatalog 2-test). Six latent issues were root-caused and fixed end-to-end across iter#3..#8 (PostgresDatabase/User finalizer race + statusUpdate IsConflict swallow, Pooler missing Watches(PostgresCluster), eval SQL re-tokenisation, psql -U postgres OS-user mismatch, smoke.sh missing rollout-restart after image load, PostgresUser missing DROP ROLE finalizer). All fixed with unit-test regression guards. |
| T28 community-operators PR | Implementation 60% | Draft PR opened (#8109). Awaiting bundle image push + CI. |
| T29 Pooler TLS auto-issuance | Implementation 90% | cert-manager `Certificate` CR auto-issuance via `spec.pgbouncer.autoTLS` (stage 1 spec + stage 2 controller). **Stage 3 cert-manager kind drill 2026-05-13 → 6/6 PASS** (`hack/smoke-cert-manager.sh`). **Stage 5 rotation observability** completed 2026-05-13: `Pooler.Status.AutoTLSClientCertNotAfter` / `AutoTLSServerCertNotAfter` mirror cert-manager `Certificate.status.notAfter`; additionalPrinterColumns expose the fields under `kubectl get poolers -o wide`. Stage 4 (self-signed fallback when cert-manager is absent) remains as the only outstanding piece. |

| T30 HA bootstrap fence race | Implementation 50% | PG18/PG17 SHARD_REPLICAS=1 (HA) kind smoke surfaced a bootstrap fence race: the elector runs in every Pod, the lease can flip during initdb, and the previous "fence on every leader-stop in memberCount>1 cluster" rule fenced the bootstrap Pod's PVC even though it had never actually served as primary. Two staged fixes shipped: (i) skip MarkFenced when standby.signal is still on disk (covers replica failure modes); (ii) track `promotedAtLeastOnce atomic.Bool` and fence only when the pod has run a successful pg_promote (covers primary bootstrap). New regression test `TestHandleStoppedLeading_SkipsFenceWhenNeverPromoted`. SHARD_REPLICAS=0 5/5 PASS regression confirmed. **Remaining**: deeper election design — primary bootstrap election should prefer ordinal-0 OR a standby pod with standby.signal should refuse to acquire the lease until cluster.status confirms there is no live primary. Without that, the lease can race-flip during initdb and re-trigger the fence path via promotedAtLeastOnce. Tracked as T30 follow-up. |

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

- Stage 3 — live cert-manager kind drill: run
  `hack/smoke-cert-manager.sh` after a successful
  `./hack/smoke.sh --keep`. The helper installs cert-manager,
  creates a self-signed `Issuer`, applies a
  `Pooler` with `spec.pgbouncer.autoTLS.clientEnabled=true`, and
  verifies the operator emits a cert-manager `Certificate` CR
  whose issued Secret is mounted onto the Pooler Deployment.
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
