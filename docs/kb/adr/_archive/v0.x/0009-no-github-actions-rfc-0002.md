# ADR 0009 — Abandon GitHub Actions + Apply Local 4-Layer Gates (RFC 0002 application)

- **Status**: Accepted
- **Date**: 2026-04-30
- **Decision makers**: @keiailab/maintainers
- **Related**: global CLAUDE.md §2 (permanent ban on GitHub Actions), global RFC 0002 (2026-04-29), `standards/ci.md` v1.0
- **Trigger**: global §2 incident (2026-04-28) — one organization billing failure → all workflow runners on all PRs across all repos failed in 4 seconds → merges blocked for 24+ hours. A single external SaaS dependency is an SPOF.

## Context

This project currently has two active workflows: `.github/workflows/{ci,upstream-watch}.yml`. After the moment global §2 codified *permanent ban on GitHub Actions per RFC 0002* (2026-04-29), this project is *a leftover that has not been migrated*. This ADR records the decision to apply that migration.

Also, the *e2e fail* discovered right before merging PR #1 is caused by *incomplete cert-manager integration on existing main*, and exposes the risk of GH Actions itself acting as a *single SPOF for PR merge blocking*.

## Decision

### Targets to abandon

- `.github/workflows/ci.yml` — all 5 jobs (lint, test, matrix-build, e2e, scan) migrated to *local 4 layers* and then deleted.
- `.github/workflows/upstream-watch.yml` — cron to detect new Citus releases. Replaced by *RemoteTrigger or user schedule* (follow-up work to this ADR).

### Mapping to the 4 layers (global `ci.md` §1 standard)

| Existing ci.yml job | New location | Command |
|---|---|---|
| lint (golangci + .custom-gcl) | **L1 pre-commit** | `make lint-config && make lint` |
| test (Unit + envtest + go mod tidy drift) | **L2 pre-push** | `make test`, `go mod tidy && git diff --exit-code go.mod go.sum` |
| scan (trivy fs HIGH+CRITICAL) | **L2 pre-push** | `make audit` (new target, invokes trivy fs) |
| matrix-build (PG×Citus 3 combinations) | **L3 Makefile (manual)** | At release tag time or `make build-pg-images` manually |
| e2e (kind, PG 16/17, p1) | **L3 Makefile (manual)** | `make test-e2e` (kind 7-9 minutes, unsuitable for pre-push) |
| upstream-watch (cron) | **RemoteTrigger or user schedule** | This PR only abandons; replacement is follow-up |

### Three exceptions

This project *does not match any* of the three exceptions in global §2:
- ① GitHub Pages static deployment — not used
- ② Dependabot/Renovate tools themselves — `.github/dependabot.yml` absent (add separately if needed)
- ③ Release tag → automatic GitHub Release body generation — currently no release tag workflow

### Tool installation + enforcement

- Developer environment: require running `pre-commit install --hook-type pre-commit --hook-type pre-push` once (README instructions).
- `.pre-commit-config.yaml` is introduced together with this PR.
- Bypassing (`--no-verify`) requires *incident reporting* (`incident-kb.md`).

### PR merge evidence (global `ci.md` §2)

Require the following block in the PR body or first commit message (PR reviewer to verify):

```
Local gates PASS:
- pre-commit run --all-files: PASS
- pre-push hooks: PASS  (or N/A if no hook)
- make test: PASS  (or specific subset)
- make audit: PASS  (high+ vulnerabilities = 0)
```

If absent, the reviewer blocks merge.

## Rationale

### Why migrate *now*

1. **Explicit violation of global §2** — *non-compliant state* since 2026-04-29. To keep this project in a *consistent standard* with other Kei* repos (force-infra-modules, force-tenant-house, etc.), apply immediately.
2. **Remove the PR-blocking effect of the PR #1 e2e fail** — after abandoning GH Actions, an e2e fail only stays in the *local verification dimension* and *no longer blocks PR merge as SPOF*. The actual e2e fix is a separate PR.
3. **Avoid the incident trigger** — if the organization billing single SPOF recurs, this project will be similarly affected. Delayed migration is a manual acceptance of risk.

### Why e2e/matrix-build is at L3 and not L2

- e2e: kind cluster boot + 7-9 minutes. Running on every push *hurts developer experience*. It is reasonable to run explicitly right before PR entry or at release time.
- matrix-build: docker buildx builds 3 PG image combinations. Needed only at release tag time.

This matches the pattern where the global `ci.md` §3 tool catalog places e2e/build in separate tracks.

### Why pre-commit and not lefthook

Global `enforcement.md` §1.1 recommends lefthook, but this project is *a single Go language + Python (pre-commit) familiar to users*, so pre-commit is more natural. Per global §3 priority "Tier-3 project > Tier-2 standards", justified by this ADR. Future lefthook adoption is a separate ADR.

## Tradeoffs

- **Loss of upstream-watch automation**: the cron-based detection of new Citus releases is temporarily suspended. Mitigation: in follow-up work, replace with RemoteTrigger or user schedule tools.
- **Dependence on developer environment**: pre-commit + security tools (gitleaks, trivy) require local installation. Mitigation: indicate `brew install` or equivalent commands in the README. Assumes macOS/Linux environments.
- **Increased burden on PR reviewers**: the "Local gates PASS" evidence block is verified *manually*. Mitigation: since the global standard is the same, it is a *common burden across all Kei* repos* — a one-time learning curve.
- **e2e is excluded from automatic PR verification**: however, since *e2e is currently already failing* (cert-manager incomplete), there is an *effect of preventing existing PR blocking*. The real e2e fix (the P7 cert-manager integration PR) verifies via *explicit L3 execution*.

## Consequences

- *Delete* both files `.github/workflows/{ci,upstream-watch}.yml`.
- New `.pre-commit-config.yaml` — defines L1 + L2 hooks.
- Add a new `audit` target to `Makefile` (invokes `trivy fs`).
- Add "Local gates PASS evidence block" + developer environment setup instructions to `README.md`.
- *External step* after this PR merges: remove "Required status checks" from GitHub branch protection or replace with local hook result markers (admin to perform separately).

## Enforcement mechanism

| Mechanism | Location | Introduction timing |
|---|---|---|
| `.pre-commit-config.yaml` | repo root | Same time as this ADR |
| `Makefile` audit target | `Makefile` | Same time as this ADR |
| Developer environment instructions | `README.md` | Same time as this ADR |
| Enforced PR body evidence block | `commits.md §3` PR checklist | Global standard |
| upstream-watch replacement | RemoteTrigger / user schedule | Follow-up work |

## Follow-up work

1. **upstream-watch replacement** — reconfigure automation for new Citus release detection via RemoteTrigger or user schedule tools.
2. **Update branch protection rules** — admin removes Required status checks in the GitHub UI.
3. **Consider lefthook migration** (future) — when consistent with the recommendation in global `enforcement.md` §1.1, a separate ADR.
4. **Complete the e2e cert-manager integration PR** — config/certmanager/ + Certificate CR + e2e BeforeAll wait. Proceeds separately from this PR.
