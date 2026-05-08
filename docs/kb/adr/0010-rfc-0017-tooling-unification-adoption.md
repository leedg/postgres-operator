# ADR-0010: RFC-0017 operator tooling unification 채택

- Date: 2026-05-09
- Status: Proposed
- Authors: @eightynine01
- Tags: tooling, ci, hook, lint, event-recorder

## Context

ai-dev RFC-0017 (`~/Documents/ai-dev/rfcs/0017-operator-tooling-unification.md`) 가 4 keiailab operator repo 의 도구 통합을 제안한다. 본 ADR 은 postgres-operator 측 채택 결정과 본 repo 한정 변경을 기록한다.

본 repo 의 현 상태 (2026-05-09 audit):
- Hook: `.pre-commit-config.yaml` (lefthook 아님) — RFC-0017 §3.1 위반
- `.golangci.yml`: 보유 (18 linter) ★ **RFC-0017 §3.2 표준 원본**
- `.custom-gcl.yml`: 보유 (logcheck plugin) ★ **표준 원본**
- Makefile: lint/test/validate/audit 모두 존재 — ✓
- Dockerfile: distroless static base — RFC-0017 §3.5 (HEALTHCHECK) 철회 후 N/A. helm chart probe 정합 확인 필요.
- **EventRecorder: ✗ 완전 미구현** — Recorder 필드 / GetEventRecorderFor / Eventf 호출 *전무* (2026-05-09 grep 검증). 단순 미구현이며 의도적 테스트 격리 아님. RFC-0017 §3.4 직접 대상.

## Decision

RFC-0017 을 **Accepted** 로 채택하고 본 repo 에서:

1. `.lefthook.yml` 신규 (valkey 패턴 + 본 repo postgres 고유 hook 통합)
2. `.pre-commit-config.yaml` 제거 (DAY 2)
3. `.golangci.yml` / `.custom-gcl.yml` 변경 없음 (이미 표준)
4. **EventRecorder 도입** — `mgr.GetEventRecorderFor("postgres-cluster-controller")` + Reconciler 내 Eventf 호출로 FakeRecorder TODO 제거
5. ~~Dockerfile HEALTHCHECK 추가~~ — 철회 (RFC-0017 §3.5 distroless 부적합). 대신 helm chart probe 보강 검증.

## Consequences

### 긍정
- 본 repo 의 linter 가 4-repo 표준 원본으로 승격됨 → 기존 ADR-0005 (Plugin SDK 우회 차단 depguard 규칙) 가 *cross-repo 표준의 일부* 로 격상
- EventRecorder 도입으로 K8s 표준 운영 신호 (kubectl describe 의 Events) 가 노출됨 — 운영자 디버깅 용이
- govulncheck / gitleaks / go-mod-tidy drift 가 pre-push 로 진입

### 부정 / 트레이드오프
- EventRecorder 도입 시 *FakeRecorder 의도가 의도적 (테스트 격리) 였다면* 테스트 영향 — 검증 필요 (RFC §7 open question)
- 기존 contributor 환경에서 `lefthook install` 1회 실행

### 후속 작업
- [x] ~~AI-PG10-1: FakeRecorder 의 원래 의도 확인~~ → 2026-05-09 완료: Recorder 코드 자체가 부재 (단순 미구현). 정상 도입 가능.
- [ ] AI-PG10-2: EventRecorder 도입 PR — Reconciler 2종 (PostgresClusterReconciler, BackupJobReconciler) 의 struct field `Recorder record.EventRecorder` 추가 + SetupWithManager 의 `r.Recorder = mgr.GetEventRecorderFor("<name>-controller")` + Reconcile 내 핵심 분기 (생성/실패/삭제) 에 Eventf 호출 추가 + suite_test.go fake recorder 주입 + envtest 회귀 0 검증 (Owner: @eightynine01, Due: 2026-05-15)
- [ ] AI-PG10-3: Event reason 카탈로그 patch — RFC-0017 §3.4 follow-up commons 추출 RFC 의존 (Owner: @eightynine01, Due: 2026-05-26)
- [ ] AI-PG10-4: helm chart probe 정합성 검증 (Owner: @eightynine01, Due: 2026-05-12)

## Alternatives Considered

| 대안 | 거절 사유 |
|------|----------|
| FakeRecorder 유지 | K8s 표준 운영 신호 부재, kubectl describe 출력 빈약 |
| EventRecorder 직접 구현 (record.New 등) | controller-runtime 표준 manager.GetEventRecorderFor 가 metric / leader election 통합 — 자체 구현 부적절 |

## References

- 글로벌 RFC: `~/Documents/ai-dev/rfcs/0017-operator-tooling-unification.md`
- 관련 audit: `~/.claude/plans/mongodb-operator-operator-commons-postgr-tranquil-horizon.md`
- 관련 ADR: ADR-0005 (Plugin SDK 우회 차단 — depguard 규칙 보존)
