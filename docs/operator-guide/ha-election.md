---
title: "HA Leader Election"
---

# HA Leader Election — 운영 가이드 (P2-M1)

> 본 문서는 Pillar P2(HA / Failover)의 첫 번째 안정 산출물인 **K8s lease 기반 leader election**의 운영 인터페이스를 설명한다. 결정 근거·동결 매개변수는 [RFC 0003](../rfcs/0003-ha-election.md), [ADR 0002](../adr/0002-no-patroni-instance-manager.md) 참조.

## 1. 무엇이 동작하는가

각 `PostgresCluster`의 인스턴스 매니저(`cmd/instance`)는 부팅 시 다음 중 하나의 election 모드로 진입한다.

| 모드 | 용도 | CLI 플래그 |
|---|---|---|
| `real` (기본) | 멤버 ≥2 — K8s `coordination.k8s.io/v1` Lease를 사용한 leader election | `--election=real` |
| `disabled` (Null) | 단일 노드 development — 항상 leader | `--election=disabled` |

`real` 모드는 client-go `leaderelection.LeaderElector`를 위임한다. 본 오퍼레이터는 `Election` 인터페이스로 wrap하여 단위·통합 테스트에 동일 시그니처를 노출한다(`internal/instance/election`).

## 2. Lease 명명 규약 (RFC 0003 §1)

| 역할 | Lease 이름 |
|---|---|
| Coordinator primary | `<cluster>-coordinator-primary` |
| Worker pool primary | `<cluster>-worker-<pool>-primary` |

namespace는 PostgresCluster CR과 동일하다.

```bash
# 클러스터 orders의 lease 조회
kubectl get lease -n <ns> | grep '^orders-'
```

## 3. Lease 매개변수 — 운영 노브

| 파라미터 | 디폴트 | CLI 플래그 |
|---|---|---|
| LeaseDuration | 15초 | `--lease-duration` |
| RenewDeadline | 10초 | `--renew-deadline` |
| RetryPeriod | 2초 | `--retry-period` |

**제약**: `RetryPeriod < RenewDeadline < LeaseDuration` (기동 시 검증되며 위반 시 에러 — `internal/instance/election/lease.go:Validate`).

### 권장 튜닝

| 환경 | 권장 LeaseDuration | 사유 |
|---|---|---|
| 안정적 LAN, 단일 AZ | 10초 | 더 빠른 failover |
| 멀티 AZ / 멀티 리전 | 20~30초 | 네트워크 지터 흡수 |
| 카오스/장애 주입 테스트 | 5초 | 빠른 회귀 |

LeaseDuration을 줄이면 K8s API server의 lease 갱신 트래픽이 비례 증가한다. 100노드 이상 클러스터에서는 신중히.

## 4. Identity (POD_NAME 단일화)

instance manager의 lease holder identity는 **`$POD_NAME`** (downward API)이다. PodSpec 예:

```yaml
env:
  - name: POD_NAME
    valueFrom: { fieldRef: { fieldPath: metadata.name } }
  - name: POD_NAMESPACE
    valueFrom: { fieldRef: { fieldPath: metadata.namespace } }
  - name: POSTGRES_CLUSTER
    value: orders
```

K8s가 namespace 안에서 POD_NAME을 unique 보장하므로, 동일 노드에 같은 identity 두 인스턴스가 동시에 lease holder가 되는 시나리오는 발생하지 않는다.

## 5. 동작 시나리오

### A. 정상 부팅
1. 모든 instance manager Pod이 election 진입
2. 한 Pod가 LeaseLock 획득 → `OnStartedLeading` 콜백 → Status=`Leader`
3. 나머지 Pod은 `OnNewLeader` 콜백 + Status=`Follower` 전이
4. follower들은 RetryPeriod(2초) 마다 lease 변화 폴링

### B. Leader graceful 종료 (SIGTERM)
1. `kubectl delete pod <leader-pod>` 또는 deployment rollout
2. instance manager가 ctx cancel → election Run return → **`ReleaseOnCancel=true`이므로 lease 즉시 해제**
3. follower 중 하나가 RetryPeriod 안에 새 lease 획득
4. 새 leader가 `OnStartedLeading` 호출

### C. Leader 응답 불가 (노드 장애)
1. leader Pod 응답 불가 (OOM, kubelet 통신 단절)
2. **LeaseDuration(15초)** 동안 lease 갱신 없음
3. follower가 만료 감지 후 새 lease 획득 시도
4. 시나리오 B와 동일 후속

> ⚠️ **PVC fencing 미구현 (M2 후속)** — 시나리오 C에서 옛 leader Pod이 살아돌아오면 두 Pod이 같은 PVC에 동시 마운트할 수 있다(split-brain 위험). P2-T2(PVC label 기반 fencing)이 GA 전 의무. RFC 0003 §7 참조.

## 6. 관찰

### 로그
모든 전이는 structured log(`slog.Info`)로 기록된다.
```
{"msg":"Leadership transition", "from":"Starting", "to":"Leader", "identity":"orders-coordinator-0"}
```

### Pod readyz
- Leader: 200
- Follower: 200
- Starting (election 부트스트랩 중): 503

### Prometheus 메트릭 (P6 통합 시점에 활성)
- `instance_election_status{cluster, role, pool, status}` — gauge

## 7. 트러블슈팅

| 증상 | 원인 후보 | 진단 |
|---|---|---|
| 어떤 Pod도 leader가 안 됨 | RBAC: `coordination.k8s.io/leases` 권한 누락 | `kubectl auth can-i update lease.coordination.k8s.io -n <ns> --as system:serviceaccount:<ns>:<sa>` |
| 두 Pod이 동시에 Leader 주장 | identity 중복 (downward API 누락) | `kubectl exec <pod> -- env \| grep POD_NAME` |
| failover가 5분+ 걸림 | LeaseDuration 과다 또는 K8s API server 지연 | `kubectl get lease -n <ns> -o yaml` 의 `renewTime` 추적 |
| 부팅 즉시 panic("invalid lease parameters") | RenewDeadline ≥ LeaseDuration | CLI 플래그 재확인 |

## 8. 알려진 한계 (M1)

- **PVC fencing 미구현** — split-brain 시나리오 보호 없음. M2 의무 게이트.
- **Failover controller 미구현** — election holder 변경이 PG primary promote/demote로 이어지는 supervise 로직은 P2-T3 후속(현재는 election callback 시그니처만 동결).
- **Prometheus 메트릭 미배선** — `instance_election_status`는 P6 (Observability) 통합 시점에 활성.
- **이벤트 레코더 더미** — `record.FakeRecorder` 사용 중. 실 EventRecorder는 P6 통합 시 교체.

## 9. 검증 명령

```bash
# 단위 + 통합 회귀 (envtest 자동 부팅)
make test

# election 패키지만
go test ./internal/instance/election/... -v -count=1

# 통합 회귀 단독 (lease 전이)
go test ./internal/instance/election/... -run TestIntegration -v
```

## 10. 참조

- [RFC 0003 — HA Election + Fencing 프로토콜](../rfcs/0003-ha-election.md)
- [ADR 0002 — Patroni 미사용](../adr/0002-no-patroni-instance-manager.md)
- 코드: `internal/instance/election/`
- 후속 작업: P2-T2 (PVC fencing) / P2-T3 (failover controller) / P2-T4 (pg_rewind 통합)
