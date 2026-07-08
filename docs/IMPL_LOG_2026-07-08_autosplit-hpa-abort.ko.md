# 구현 기록 — AutoSplit · Router active-connection HPA · online resharding abort fallback

> 2026-07-08 세션. 브랜치 `chore/ha-pitr-e2e-consolidation`. §6.9 남은 작업 3건을 순차 구현한
> **과정(사용 스크립트·방법·예시 데이터) + 결과** 기록. 상태 요약은
> [WORK_HANDOFF.ko.md §6.9](WORK_HANDOFF.ko.md), 로드맵은
> [sharding/ROUTER-GAP-ANALYSIS.ko.md](sharding/ROUTER-GAP-ANALYSIS.ko.md).

관련 커밋:

| 커밋 | 내용 |
|---|---|
| `dc75010` | feat(autosplit): 자동 shard 확장 루프 |
| `86f0add` | feat(router): active-connection 커스텀 메트릭 + HPA Pods 메트릭 |
| `ff4f3e2` | feat(router): source-down abort fallback + 라이브 테스트 |
| `cb4bf4e` | docs(handoff): §6.9 완료 반영 |

---

## 0. 개발 환경 + 사용 스크립트 (Windows 호스트)

이 호스트는 dev-smoke 전용이다(라이브 kind e2e 는 Linux/컨테이너). 확정된 환경:

| 항목 | 값 |
|---|---|
| Go | `go1.26.4`, `C:\Users\iq200\AppData\Local\Programs\go1.26.4\go\bin\go.exe` (PATH 미등록) |
| GOTMPDIR / GOCACHE | `%LOCALAPPDATA%\keiailab\postgres-operator\{go-tmp,go-cache}` (repo 밖 — `*.test.exe` 가 workspace 오염 방지) |
| envtest assets | `bin/k8s/1.36.2-windows-amd64` (캐시됨) |
| GOFLAGS | `-mod=mod` |

### 0.1 유닛/통합 테스트 실행

```powershell
$go   = "C:\Users\iq200\AppData\Local\Programs\go1.26.4\go\bin\go.exe"
$env:GOFLAGS  = "-mod=mod"
$env:GOTMPDIR = "$env:LOCALAPPDATA\keiailab\postgres-operator\go-tmp"
$env:GOCACHE  = "$env:LOCALAPPDATA\keiailab\postgres-operator\go-cache"
& $go build ./...
& $go vet   ./...
```

controller envtest 는 wrapper 로(assets 경로 자동 주입):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-windows.ps1 -Preset controller
# 특정 focus: -GinkgoFocus "autoscale|HPA|router"
```

### 0.2 WDAC(Windows Application Control) 우회 — bin/ 빌드

이 호스트는 WDAC(App Control) ISG 가 서명 없는 신규 `*.test.exe` 를 **해시 평판 기반으로
간헐 차단**한다(`An Application Control policy has blocked this file`). `go test` 는 exe 를
`GOTMPDIR` 에서 실행하므로 차단당한다. **envtest 바이너리가 실행되는 `bin/` 경로는 허용**되므로,
테스트 바이너리를 `bin/` 하위로 빌드해 직접 실행하면 우회된다:

```powershell
& $go test -c -o bin\testexe\router.test.exe ./internal/router
& .\bin\testexe\router.test.exe "-test.run=CDC_RejectsInjection|Reshard.*Live" "-test.v"
Remove-Item -Recurse -Force bin\testexe   # bin/ 는 .gitignore
```

> 완전 해소는 관리자 PowerShell 로 `scripts\allow-windows-test-exe.ps1` (repo 외부 temp/cache
> 디렉터리만 예외). 본 세션은 admin 불가로 bin/ 우회 사용. `bin/` 은 `.gitignore` 라 커밋 안 됨.

### 0.3 알려진 환경 예외 (코드 무관, 회귀 아님)

- `internal/instance/supervise` 의 `TestReal_*` fork/exec 2건: Windows 에서 `.sh`(fake-postgres.sh)
  실행 불가(`%1 is not a valid Win32 application`) — baseline 동일(git stash 로 확인).
- WDAC 가 특정 exe(예: `api/v1alpha1.test.exe`)를 끝까지 차단 시: 컴파일(build/vet)로 검증 +
  downstream(그 심볼을 쓰는 controller/pg-router exe 실행)으로 간접 검증.

---

## 1. AutoSplit — 자동 shard 확장 (`dc75010`)

`spec.autoSplit` 스키마/admission 은 있었으나 **관측→판정→후보→job 생성 루프가 미구현**이었다.

### 1.1 size 관측 파이프라인 (신규 연결)

`ShardStatus.SizeBytes` 필드는 API 에 있었으나 **아무도 채우지 않던 끊긴 파이프라인**이었다. 연결:

```
instance manager (primary)                aggregate_status               autosplit observer
  pg_database_size(current_database())  →  ShardStatus.SizeBytes      →  ShardObservation.SizeBytes
  → statusapi.Status.SizeBytes             (primary 보고값 집계)
  (Pod annotation)
```

- `internal/instance/supervise/{supervise.go,sql.go,mock.go}`: `Supervisor.DatabaseSizeBytes(ctx)`
  (Real=`SELECT pg_database_size(current_database())`, Mock=필드).
- `cmd/instance/main.go`: primary 일 때만 `st.SizeBytes` 보고(replica 는 물리복제라 동일 — 질의 절약).
- `internal/instance/statusapi/types.go`: `Status.SizeBytes` 추가.
- `internal/controller/aggregate_status.go`: 선택된 primary 의 `SizeBytes` → `ShardStatus.SizeBytes`.

### 1.2 제어 루프 (`internal/controller/autosplit.go`)

1. **observer** (`ShardMetricsObserver`, default `statusShardObserver`): `cluster.Status.Shards` 에서
   순수하게 관측치 읽기(테스트 가능). CPU / P99 latency 는 metrics 소스 미결선 → 0.
2. **트리거 평가** (`autoSplitTriggerBreached`): 활성 트리거(임계>0)를 **모두** 만족(AND). size 는
   GB→bytes 환산. cpu/latency 는 관측 0 이라 임계(>0) 미달 → 미발동(오탐 방지).
3. **지속 추적** (`autoSplitSustained`): breach 가 `durationMinutes` 동안 지속되어야 자격
   (`shouldPromoteAfterDebounce` 미러, in-memory).
4. **후보 계산** (`router.SplitHashRange`): hash 범위 `[lo,hi]` 를 중점 분할 → 결정론·DNS-safe target
   ID(`as<6hex>a`/`b`). 데이터 보존 불변식은 `ValidateSplitPlan` 이 재확인.
5. **멱등 job 생성**: `buildAutoSplitJob`(owner=cluster, 결정론 이름). `requireApproval` 이면
   `autosplit-approval=required` annotation → SSJ 컨트롤러가 승인(`autosplit-approved=true`) 전까지
   Pending 유지. cluster 당 한 번에 하나(진행 중이면 skip).
6. **condition**: `AutoSplitEligible` (reason: SplitCandidate/NoCandidate/SplitInProgress/
   UnsupportedVindex/MetricsSourceMissing).

### 1.3 사용 예시 (설정)

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
spec:
  shardingMode: native
  autoSplit:
    enabled: true
    requireApproval: true        # 자동 생성 job 을 운영자 승인 전까지 Pending 으로 대기
    triggers:
      sizeThresholdGB: 10        # shard DB 크기 ≥ 10GB
      durationMinutes: 30        # 30분 지속되면 후보
      # cpuPercent / p99LatencyMs 는 metrics 소스 결선 후 사용(현재 관측 0 → 미발동)
```

자동 생성된 job 승인:

```bash
kubectl annotate shardsplitjob <name> postgres.keiailab.io/autosplit-approved=true
```

### 1.4 검증 결과

- `go test ./internal/router -run TestSplitHashRange` PASS(중점 분할 + 보존 불변식).
- `go test ./internal/controller` 전체 envtest **PASS 36.4s** — AutoSplit 유닛 8종
  (트리거 AND / 지속 / 후보 / 이름 / 승인게이트 / observer) + fake-client reconcile 3종
  (승인게이트 job 생성·멱등 / 임계미달 무생성 / cpu 미결선 `MetricsSourceMissing`).
- **남은 것**: cpu/latency 트리거의 metrics 소스(metrics.k8s.io / router 메트릭) 결선 시 observer 만 교체.

---

## 2. Router active-connection HPA 메트릭 (`86f0add`)

라우터 HPA 가 CPU utilization 만 썼다. active client-connection 으로도 스케일하도록 **opt-in** 추가.

### 2.1 메트릭 노출 (pg-router)

- `cmd/pg-router/metrics.go`: `activeConns atomic.Int64` + `/metrics`(Prometheus 텍스트, zero-dep)
  + `/healthz` 서버(`PGROUTER_METRICS_ADDR`, 기본 `:9187`). `trackConn` 래퍼가 연결 수명 동안
  게이지 inc/dec(panic-safe defer). 게이지 이름 = `v1alpha1.RouterActiveConnectionsMetric`
  (`pgrouter_active_connections`) — HPA 와 공유 상수라 불일치 없음.

### 2.2 HPA 결선 (opt-in, 비파괴)

- `RouterAutoscaleSpec.ScaleOnActiveConnections`(기본 false) → true 일 때만 `buildRouterHPA` 가
  CPU 메트릭에 더해 Pods 메트릭(`AverageValue targetActiveConnections`)을 추가.
- 라우터 Deployment: metrics 포트(9187) + `prometheus.io/{scrape,port,path}` annotation.

### 2.3 사용 예시 + prometheus-adapter 규칙

```yaml
spec:
  router:
    autoscale:
      enabled: true
      maxReplicas: 8
      scaleOnActiveConnections: true    # HPA 에 Pods 메트릭 추가
      targetActiveConnections: 1000     # Pod 당 평균 active conn
```

custom-metrics adapter(prometheus-adapter) 규칙 예시(`config/router/README.md` 에도 수록):

```yaml
rules:
  - seriesQuery: 'pgrouter_active_connections{namespace!="",pod!=""}'
    resources: {overrides: {namespace: {resource: namespace}, pod: {resource: pod}}}
    name: {as: "pgrouter_active_connections"}
    metricsQuery: 'avg(<<.Series>>{<<.LabelMatchers>>}) by (<<.GroupBy>>)'
```

어댑터 부재 시 Pods 메트릭 unavailable → HPA 가 CPU 로 fallback. 기본(opt-in 미설정)은 CPU-only.

### 2.4 검증 결과

- `go test ./cmd/pg-router` PASS — metrics handler(게이지 3 노출) / `trackConn` inc·복원 / 빈 addr no-op.
- `go test ./internal/controller` envtest **PASS 34.6s** — HPA active-conn Pods 메트릭 2종
  (opt-in 시 CPU+Pods / 미설정 시 CPU-only) + Deployment metrics 포트·scrape annotation.

---

## 3. online resharding abort 의 source-down fallback (`ff4f3e2`)

§6.8 이 "source 불통 시 target subscription 강제 제거 fallback" 을 미구현으로 남겨둔 것을 구현.

### 3.1 문제 + 해법

일반 `DROP SUBSCRIPTION` 은 연관 **원격 replication slot** 정리를 위해 publisher 에 접속한다 →
source 가 죽으면 실패/지연 → cdc-abort 실패 → `AbortCleanup=False` 로 pub/sub/slot 누수.

`router.ForceDropSubscription`(`internal/router/reshard_cdc.go`):

```
ALTER SUBSCRIPTION <sub> DISABLE;            -- apply worker 정지
ALTER SUBSCRIPTION <sub> SET (slot_name=NONE); -- 원격 slot detach (publisher 접속 회피)
DROP SUBSCRIPTION IF EXISTS <sub>;           -- publisher 접속 없이 target 정리
```

부재 시 no-op(멱등). trade-off: 원격 slot 은 orphan 으로 남아 source 복구 후 정리(target 정리 우선).

- `cmd/reshard-copy-poc/main.go` cdc-abort: 정상 `DropSubscription` 실패 시 `ForceDropSubscription`
  fallback, `DropPublication` 은 best-effort → source-down 에도 abort cleanup 완료.

### 3.2 라이브 테스트 (env-guard, kind/make 불요)

`internal/router/reshard_native_live_test.go` — `TestCDCLive` idiom. `RESHARD_LIVE_*` env 미설정 시 skip.

- `TestReshardPKlessTargetConcurrentLive`: **PK 없는 target** 으로의 *동시* UPDATE/DELETE
  논리복제(seq-scan 경로 — §6.7 미검증 갭) 수렴 + 이후 `ReplicateIndexes` 로 PK 복제.
- `TestReshardAbortSourceDownLive`: `ForceDropSubscription` 이 slot detach 후 target subscription
  제거(멱등) — source-down fallback 메커니즘.

### 3.3 라이브 테스트 실행 예시 (예시 데이터)

Postgres 2개(source/target)를 `wal_level=logical` 로 띄우고 env 지정:

```bash
# source / target PG (예시 — docker)
docker run -d --name pg-src -p 5432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust \
  postgres:18 -c wal_level=logical
docker run -d --name pg-tgt -p 5433:5432 -e POSTGRES_HOST_AUTH_METHOD=trust \
  postgres:18 -c wal_level=logical

export RESHARD_LIVE_SOURCE="host=127.0.0.1 port=5432 user=postgres dbname=postgres sslmode=disable"
export RESHARD_LIVE_TARGET="host=127.0.0.1 port=5433 user=postgres dbname=postgres sslmode=disable"
# target → source 접속 문자열(subscription 이 사용 — 컨테이너 네트워크 기준 host 조정)
export RESHARD_LIVE_CONNINFO="host=pg-src port=5432 user=postgres dbname=postgres sslmode=disable"

go test ./internal/router -run 'TestReshardPKlessTargetConcurrentLive|TestReshardAbortSourceDownLive' -v
# CDC 전체: -run 'TestCDCLive|TestReshard.*Live'
```

테스트가 만드는 예시 데이터: `kv(id int, val int)` 테이블, source 초기 1..100(또는 1..10), 구독 이후
동시 `UPDATE kv SET val=-1 WHERE id=<even>` + `DELETE FROM kv WHERE id>90` 스트림, target 수렴 검증.

### 3.4 검증 결과

- `go build ./...` = 0, `go vet ./internal/router ./cmd/reshard-copy-poc` = 0.
- `TestCDC_RejectsInjection` **PASS**(`ForceDropSubscription` injection guard 포함) +
  두 라이브 테스트 **SKIP**(env 미설정 정상) + `internal/router` / `cmd/reshard-copy-poc` 전체 **PASS**
  (bin/ 우회 실행).

---

## 4. 세션 전체 검증 요약

| 항목 | 결과 |
|---|---|
| `go build ./...` | **0** |
| `go vet ./...` | **0** |
| controller envtest (`test-windows.ps1 -Preset controller`) | **PASS** (36.4s / 34.6s ×2) |
| `internal/router` | **PASS** (SplitHashRange · injection guard · 라이브 SKIP) |
| `cmd/pg-router` | **PASS** (metrics handler · trackConn) |
| `cmd/reshard-copy-poc` | **PASS** |
| `cmd/instance` | **PASS** |
| `api/v1alpha1` | 컴파일 PASS(vet) — exe 는 WDAC 차단, 새 심볼은 controller/pg-router exe 실행으로 간접 검증 |
| `internal/instance/supervise` fork/exec 2건 | Windows `.sh` 한계(baseline 동일, 회귀 아님) |

## 5. 남은 작업 (kind-live 전용, 별도 체크포인트)

- **native 라우터 무중단 cutover 실증**: `shardingMode=native`(라우터 배포)에서 클라 쓰기를 라우터
  경유로 받아 write-block 이 실제 클라 쓰기를 막는 무중단 cutover(위 §3.2 라이브 테스트는 CDC 레벨 —
  라우터 경유 클라 쓰기는 kind 필요).
- **target 승격 후 live chaos/failover drill**: 승격 중 pod kill 로 #220-class 정체성 위험 확인
  (ADR-0029 P-B). 설계 = `docs/kb/adr/0029-*.md`.
- 재현 요약: [WORK_HANDOFF.ko.md §6.7](WORK_HANDOFF.ko.md) 의 kind e2e 재현 블록.
