# HANDOFF — postgresql-operator

> 다음 세션이 *대화 컨텍스트 없이* 재개 가능하도록 유지. 작업 시작 시 가장 먼저 읽고, 종료 시 갱신.

## 현재 상태

- 마지막 커밋: P2-T2 fencing 추가 (커밋 진행 중)
- 직전 커밋: `45e95b8 feat(p2): P2-M1 승격 — election envtest 통합 회귀 + 운영 가이드 + 거버넌스 정착`
- 작업 누적: 본 세션 두 번째 사이클로 P2-T2 (PVC fencing) 완성

## 도달 마일스톤

- **P1-M1** Core Lifecycle Alpha
- **P2-M1** + **P2-T2** HA / Failover Alpha + PVC Fencing
  - election: 단위 97.4%, envtest 통합 2종
  - fencing: 단위 89.7%, RFC 0003 부록 A 동결, cmd/instance 통합 (`--fencing-disabled` 노브)
  - fail-fast: ErrFenced 시 exit(2) → CrashLoopBackOff → 운영자 개입 신호
- **P10-M0** Extension Plugin SDK spike
- **P11-M0** Citus Topology spike
- **P13-T1** Plugin SDK 인터페이스 동결

## 다음 단계 (단일 행동)

```bash
cd /Users/phil/WorkSpace/public/postgresql-operator
make lint && make test          # 회귀 확인 — fencing 89.7%, election 97.4% 유지

# 다음 작업 후보 (의존 만족 순):
git checkout -b feat/p2-t3-failover-controller   # P2-T3 promote/demote 실 PG supervise
# 또는
git checkout -b feat/p3-storage-wal              # F05 P3-M1 트랙
```

## 차단점

(없음)

## 본 세션 의사결정 기록 (누적)

1. **TASKS.md / HANDOFF.md 도입** — workflow.md §3 의무.
2. **P2-M1 e2e = envtest 통합** — 풀 K8s 클러스터 e2e는 P2-T4 + 실 PG 이미지 필요로 M2+로 연기.
3. **P2-T2 분산 fence 모델** — *각 Pod이 자기 PVC만* fence (controller 일괄 fence 거부). 사유: ADR 0002 "K8s API as DCS" 원칙. 컨트롤러가 PVC를 일괄 patch하면 단일 진실 출처가 controller로 이동, 분산 합의 모델 위배.
4. **Fail-fast 정책 채택** — ErrFenced 시 `os.Exit(2)`. CrashLoopBackOff가 운영자 개입 신호 역할. 자동 unfence 거부 (잘못된 데이터 검증 우회 위험).
5. **fence 인터페이스 단일 PVC 스코프** — 멀티 PVC 트랜잭션 인터페이스는 분산 모델에서 불필요.
6. **Mock fencer를 fencing-disabled 모드에 사용** — Null 구현 별도 추가하지 않고 Mock(unfenced 디폴트)으로 대체. 단순성 우선(§2 Simplicity).

## 검증 명령 (재현)

```bash
make lint                                      # 0 issues
make test                                      # 모든 패키지 통과
go tool cover -func=cover.out | grep fencing   # 89.7% 확인
go tool cover -func=cover.out | grep election  # 97.4% 확인
go build ./cmd/instance/...                    # instance manager 빌드 통과
```

## 근거 링크

- `docs/roadmap.md` — 14 Pillar × DoD
- `docs/adr/0002-no-patroni-instance-manager.md` — K8s API as DCS
- `docs/rfcs/0003-ha-election.md` — Election (§1~9) + 부록 A (PVC Fencing)
- `docs/operator-guide/ha-election.md` — 운영 가이드 (Fencing 섹션 §10)
- 코드: `internal/instance/election/`, `internal/instance/fencing/`, `cmd/instance/main.go`
