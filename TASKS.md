# TASKS — postgresql-operator

> 진척 추적: roadmap.md(14 Pillar × Maturity Level) 기반. 캘린더 미사용.
> 가중치: M0=0.25 / M1=0.5 / M2=0.75 / M3=1.0. 진척률 = Σ(가중치) ÷ 68.

## 작업 표

| ID  | 기능명/요약 | 단계 | 완성도 | 의존 | 영향 | 비고 |
|-----|-------------|------|--------|------|------|------|
| F01 | P1 Core Lifecycle (CRD + reconciler + webhook + envtest + e2e) | 테스트 | 90% | — | F03,F04,F08 | M1 도달, b13de93. M2 게이트: e2e 매트릭스 + 회귀 |
| F02 | P10 Extension Plugin SDK (depguard + 6 extensions) | 구현 | 60% | F08 | F11 | 3e948da. M1: 단위 ≥80% + e2e 1개 |
| F03 | P11 Citus Topology spike (DesiredNodes + ComputeActions + SQLExecutor) | 구현 | 25% | F02 | F12 | c7823bf, M0 도달 |
| F04 | P2 HA / Failover (election + fencing) | 테스트 | 60% | F01 | F03 | M1 도달 + P2-T2 fencing 추가. fencing 89.7%, RFC 0003 부록 A. M2 잔여: failover controller(P2-T3), pg_rewind(P2-T4), e2e 매트릭스 |
| F05 | P3 Storage / WAL | 설계 | 0% | F01 | F09 | RFC 미작성 |
| F06 | P4 Backup / PITR | 설계 | 0% | F04,F05 | F12 | RFC 0004 미작성 |
| F07 | P5 Connection / Pooling (PgBouncer) | 설계 | 0% | F01 | — | RFC 미작성 |
| F08 | P6 Observability (pgMonitor exporter) | 설계 | 0% | F01 | F04 | RFC 0007 미작성 |
| F09 | P7 Security / TLS (cert-manager + mTLS) | 설계 | 0% | F01 | F10 | RFC 0006 미작성 |
| F10 | P8 User / DB / RBAC | 설계 | 0% | F01,F09 | — | RFC 0006 통합 가능 |
| F11 | P9 Upgrade (in-place + blue/green) | 설계 | 0% | F04,F06 | — | RFC 0010 미작성 |
| F12 | P12 QueryRouter (stateless) | 설계 | 0% | F03,F07,F08 | — | RFC 0005,0009 미작성 |
| F13 | P13 Plugin SDK (gRPC out-of-process) | 구현 | 25% | F02,F06,F08 | F02 | T1만 동결. T2~T6 후속 |
| F14 | P14 Distribution (Helm/OLM/multi-arch) | 설계 | 0% | 모두 | — | 마지막 |
| I01 | TASKS.md / HANDOFF.md 운영 정착 | 완료 | 100% | — | 모두 | 본 세션 도입 |
| I02 | RFC 백로그 (0004~0012) 작성 | 설계 | 0% | 각 Pillar 진입 시 | — | 점진 작성 |

## 진행 중

(없음 — 다음 세션은 "다음 후보" 1순위 또는 사용자 지정으로 선택)

## 차단됨

(없음)

## 다음 후보 (의존 만족 순)

1. F04 P2-T3 — failover controller (election holder 변경 → PG primary promote/demote). 본격 PG supervise 시작.
2. F04 P2-T4 — pg_rewind 자동화 (fence 표시 후 인-flight write 회수).
3. F05 P3-M1 — PVC 관리 + WAL 아카이빙 + base restore. RFC 작성 선행.
4. F07 P5-M1 — PgBouncer 사이드카 + 독립 Deployment.
5. F08 P6-M1 — exporter + Grafana + PrometheusRule.
6. F02 P10-M1 — extension lifecycle e2e.
