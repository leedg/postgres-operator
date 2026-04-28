# HANDOFF — postgresql-operator

> 다음 세션이 *대화 컨텍스트 없이* 재개 가능하도록 유지. 작업 시작 시 가장 먼저 읽고, 종료 시 갱신.

## 현재 상태

- 마지막 커밋: 진행 중 (P2-M1 승격 + 거버넌스 정비 단일 커밋 예정)
- 직전 커밋: `44e9f6c fix(ci): 첫 push 후 발견된 4개 CI 실패 일괄 수정`
- 작업 상태: TASKS.md / HANDOFF.md 도입 + P2-M1 게이트 통과 + RFC 0003 Implemented 전이

## 도달 마일스톤

- **P1-M1** Core Lifecycle Alpha — CRD + reconciler + ValidatingWebhook + envtest + e2e Pillar 라벨
- **P2-M1** HA / Failover Alpha — Election 인터페이스 + Real/Null/Mock + envtest 통합 회귀 2종
  - 단위 커버리지 97.4% (게이트 80% +17.4%p)
  - 통합 회귀: TwoInstances_OneLeader, LeaderHandover (RFC 0003 §8 시나리오 A/B/C)
  - 운영 가이드 `docs/operator-guide/ha-election.md`
  - 알려진 한계 명시: PVC fencing 미구현, failover controller 미구현, Prometheus 미배선
- **P10-M0** Extension Plugin SDK spike — depguard + 6 ExtensionPlugin
- **P11-M0** Citus Topology spike — DesiredNodes / ComputeActions / SQLExecutor
- **P13-T1** Plugin SDK 인터페이스 동결

## 다음 단계 (단일 행동)

다음 세션 진입 시 1순위 후보:

```bash
# F05 P3-M1 — Storage / WAL 트랙 시작
git checkout -b feat/p3-storage-wal
# 1) RFC 작성: docs/rfcs/0011-storage-pvc-wal.md
# 2) internal/storage/ 신규 패키지: PVC 관리 + WAL 아카이빙 인터페이스
# 3) envtest 통합: PVC reconcile + base restore happy path
# 4) docs/operator-guide/storage-wal.md
```

대안:

- **F04 P2-M2 진행**: PVC fencing(P2-T2) + failover controller(P2-T3). RFC 0003 부록 A 작성.
- **F07 P5-M1 (PgBouncer)**: 사이드카 + 독립 Deployment. RFC 작성 선행.
- **F08 P6-M1 (Observability)**: pgMonitor exporter. P2의 `instance_election_status` 메트릭 활성 의존.

## 차단점

(없음)

## 본 세션 의사결정 기록

1. **TASKS.md / HANDOFF.md 도입** — 글로벌 standards/workflow.md §3 의무. ID 기반 작업표(F/I/B/T 접두), 단계·완성도(10/60/90/100%)·의존·영향 컬럼.
2. **P2-M1 e2e 정의 = envtest 통합** — Maturity "e2e 1개"는 "사용자 시점 통합 검증". 풀 K8s 클러스터 e2e(kill leader pod)는 P2-T4(pg_rewind 기반 PG 실프로세스 supervise) 선행 필요 → M2 이후로 연기.
3. **통합 회귀 매개변수** — LeaseDuration=2s/RenewDeadline=1s/RetryPeriod=200ms로 단축, 회귀 시간 ~10초 유지.
4. **RFC 0003 상태 전이**: Draft → Implemented (코드+테스트+가이드 완성으로 채택 후 구현 동시 도달).
5. **ADR 0006 / RFC 0004+ 작성 연기** — 해당 Pillar 진입 시점에 작성 (workflow §6 "시간 기반 추정 금지").

## 검증 명령 (재현)

```bash
make lint                                    # 0 issues
make test                                    # 모든 패키지 통과
go tool cover -func=cover.out | grep election  # 97.4% 확인
```

## 근거 링크

- `docs/roadmap.md` — 14 Pillar × DoD 모델
- `docs/adr/0001-stateless-query-router-on-citus.md` — 미션 재정의
- `docs/adr/0002-no-patroni-instance-manager.md` — 자체 instance manager + K8s API as DCS
- `docs/adr/0005-plugin-sdk-interface-model.md` — Plugin SDK 인터페이스
- `docs/rfcs/0003-ha-election.md` — Election + Fencing (Implemented)
- `docs/operator-guide/ha-election.md` — 본 세션 신규
