# ADR 0006 — Data Plane PodSecurityContext Defaults (Security Defaults Rationale)

- **Status**: Accepted
- **Date**: 2026-04-30
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0001 v2 (PGO-class parity), Bitnami PostgreSQL Helm Chart comparison (`/Users/phil/.claude/plans/1-https-artifacthub-io-packages-helm-bit-sunny-wozniak.md` §4 P0-2)

## Context

The *manager Pod* (the operator itself) of this project applies a strong SecurityContext at `config/manager/manager.yaml:53-74` — `runAsNonRoot=true`, `readOnlyRootFilesystem=true`, `seccompProfile=RuntimeDefault`, `capabilities.drop=[ALL]`. However, the *data plane Pods* (`buildPGStatefulSet:184-198`, `buildRouterDeployment:243-256`) have zero SecurityContext.

This is an **asymmetric security debt**:
- Admission may be denied in clusters where the PSS (Pod Security Standards) `restricted` policy is applied
- With `runAsNonRoot=false` (default), the PG container may start as root — host escape risk
- Falls short of the security settings the Bitnami PostgreSQL Helm Chart provides as *defaults* → contradicts the "PGO-class parity" promise

## Decision

Always apply the following SecurityContext defaults to all data plane Pods produced by `buildPGStatefulSet` and `buildRouterDeployment`:

```yaml
podSecurityContext:
  runAsNonRoot: true
  runAsUser: 70           # standard PG postgres user UID
  runAsGroup: 70
  fsGroup: 70
  seccompProfile:
    type: RuntimeDefault
container.securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: [ALL]
```

Companion changes for `readOnlyRootFilesystem=true`:
- Add emptyDir mounts — `/tmp`, `/run`, `/var/run/postgresql` (PG needs to write locks/sockets)
- `/var/lib/postgresql/data` is a PVC mount, so no separate emptyDir needed

## Rationale

### Why UID 70
The official PostgreSQL container image (postgres:*) defines the `postgres` user with UID/GID 70. The forthcoming cmd/instance image of this project will follow the same convention.

### Why `readOnlyRootFilesystem=true`
- Blocks writing arbitrary binaries inside the container → mitigates supply chain attacks
- Bitnami chart default
- emptyDir mount cost is negligible (memory backed) → trade-off ↑

### Why enforce as a *default*
If we put a `Spec.SecurityContext` override field on the PostgresCluster CR, it becomes *opt-in security* — root remains possible whenever the operator forgets. This ADR enforces *opt-out*: defaults are always the settings above, and the webhook validates overrides.

## Tradeoffs

- **`readOnlyRootFilesystem` compatibility**: some PG extensions (e.g., pg_cron, pg_stat_statements) create temporary files on disk. Resolution: `/tmp` emptyDir + per-extension PVC subpath. This ADR is sufficient with the 3 default emptyDirs; per-extension additions are handled in P10.
- **Custom UID requirements**: some K8s environments (parts of OpenShift SCC) enforce random UIDs. Resolution: the webhook allows leaving `runAsUser` as nil so K8s SCC can fill it (add webhook at P0-2 implementation time).
- **Ownership of existing PVC data**: to read data written by an existing PG as root with UID 70 requires changing fsGroup. K8s `fsGroup` handles this automatically, but the first transition may take time.

## Consequences

- Inject SecurityContext into `buildPGStatefulSet` and `buildRouterDeployment` in `internal/controller/builders.go` (when P0-2 recommendation is applied).
- envtest assertion: verify generated Pods have `runAsNonRoot=true`, `runAsUser=70`.
- e2e regression: verify admission passes in a namespace with restricted PSA applied.
- Changes to this ADR (changing UID, adding capabilities) are handled as part of RFC 0006 "Security/TLS".

## Enforcement mechanism

| Mechanism | Location | Introduction timing |
|---|---|---|
| Default injection | `internal/controller/builders.go` | P0-2 implementation |
| webhook validation | `internal/webhook/v1alpha1/postgrescluster_webhook.go` | P0-2 follow-up (enforce minima on override) |
| envtest regression | `internal/controller/builders_test.go` | P0-2 implementation |
| e2e regression | `test/e2e/security_test.go` (new) | P0-2 implementation |
