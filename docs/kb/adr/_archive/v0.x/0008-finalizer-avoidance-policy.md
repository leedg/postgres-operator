# ADR 0008 — Finalizer Avoidance Policy (Cascade Delete via OwnerReference)

- **Status**: Accepted
- **Date**: 2026-04-30
- **Decision makers**: @keiailab/maintainers
- **Related**: ADR 0002 (no Patroni — K8s API as DCS), Crunchy PGO comparison (`/Users/phil/.claude/plans/1-https-artifacthub-io-packages-helm-bit-sunny-wozniak.md` §4 P0-4)

## Context

In this project, the PostgresCluster reconciler calls `controllerutil.SetControllerReference` on all subordinate resources (StatefulSet, Service, ConfigMap, PVC) (`internal/controller/postgrescluster_controller.go:262`). This is the standard pattern that lets K8s GC *automatically cascade-delete* when the PostgresCluster is deleted.

However:
- The *temptation to introduce a finalizer* may arise in future PRs — "shouldn't we also clean up external resources (backup repo, S3 prefix, certificate)?"
- A finalizer costs an *additional reconcile loop* + *increased K8s API dependence* + *delayed deletion*
- The ADR 0002 "K8s API as DCS" principle preserves *distributed consensus simplicity* — a finalizer can conflict with that principle
- Crunchy PGO also pins the same pattern (controllerutil + GC TTL) in an ADR — this ADR *explicitly records* that decision

## Decision

This project **will not introduce new finalizers.** Only the following two mechanisms are used:

1. **OwnerReference + K8s GC**: set OwnerReferences on subordinate resources via `controllerutil.SetControllerReference`. K8s GC cascade-deletes them on PostgresCluster deletion.
2. **External resource cleanup goes in a separate Job CRD**: when external resources have a *separate lifecycle* from PostgresCluster deletion — such as backup repo cleanup or S3 prefix cleanup — handle them in a separate CRD like `BackupCleanupJob`. PostgresCluster can be deleted in a state where it *does not know* about external resources.

### Enforced regression tests

Add the following scenarios to `test/e2e/cascade_delete_test.go`:
1. Create PostgresCluster → verify StatefulSet/Service/ConfigMap are created
2. Delete PostgresCluster
3. Verify all subordinate resources are GC'd **within 60 seconds**

envtest assertions in `internal/controller/postgrescluster_controller_test.go`:
- All subordinate resources have `controllerutil.HasControllerReference == true`

## Rationale

### Why finalizers are excluded *by default*
Finalizers create the following 4 costs:

1. **Delayed deletion**: after `kubectl delete`, the PostgresCluster resource remains in the K8s API *until the finalizer is processed*. Operator confusion.
2. **Stuck risk**: reconciler dies while processing a finalizer → resource stuck permanently. Force-removing it leaks external resources.
3. **Reconcile complexity**: adds an `if obj.DeletionTimestamp != nil { ... } else { ... }` branch to every reconcile cycle.
4. **Distributed consensus conflict**: ADR 0002 "K8s API as DCS" makes etcd the single source of truth. A finalizer *delays* that truth — it adds timing assumptions in the distributed model.

### Why external resources go in a separate Job CRD
PostgresCluster deletion ≠ backup repo deletion. A user might delete the PostgresCluster but want to *preserve backups*. Or, *preserve backups only* for cluster recreation. Forcing this separation:

- Simplifies PostgresCluster lifecycle (zero external dependencies)
- Users must intentionally create a cleanup CRD to delete external resources → *explicit expression of intent*
- Without a finalizer, deletion completes immediately

### Why write the ADR *now*
At the moment of adding P0-4 regression tests, the rationale for *why these tests exist* is pinned in an ADR. If a future PR attempts to "add a finalizer for external resource cleanup", this ADR is the rejection basis.

## Tradeoffs

- **Possibility of external resource leak**: without a finalizer → an external backup repo can leak after PostgresCluster deletion. Mitigation: recommend using a *separate cleanup CRD* in the operations guide. Also, utilize cloud provider lifecycle policies (S3 expiration, etc.).
- **Absence of *graceful drain* tools like kubectl-cnpg**: PGO uses finalizers to ensure PG shuts down gracefully before deletion. This project follows a fail-fast model (ADR 0002 + RFC 0003 Appendix A), so graceful drain is *handled by the operator as a restart signal* — same distributed consensus principle.

## Consequences

- This ADR is the basis for rejecting finalizer introduction in *all future PRs*.
- Add regression tests at P0-4 recommended implementation time.
- When external resource cleanup is needed (P4 Backup, P7 Security), design *separate Job CRDs*.
- Changes to this ADR (introducing a finalizer exception) require an RFC — RFC is mandatory due to *broad impact*.

## Enforcement mechanism

| Mechanism | Location | Introduction timing |
|---|---|---|
| Regression test (e2e cascade delete) | `test/e2e/cascade_delete_test.go` | P0-4 implementation |
| envtest assertion (OwnerReference) | `internal/controller/postgrescluster_controller_test.go` | P0-4 |
| golangci-lint policy (where possible — block finalizer imports) | `.custom-gcl.yml` | P13-T2 follow-up |
| PR review checklist | `standards/checklist.md §3` | Same time as this ADR adoption |
