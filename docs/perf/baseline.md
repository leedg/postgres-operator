# Performance Baseline — postgres-operator G5

> ROADMAP G5 §183 의 *측정 schema + 결과 placeholder*. 실 측정값은 cluster + 분산 인스턴스 도달 후 별 turn 에서 채워짐. 본 문서는 *재현 가능한 측정 protocol* 표준화 + 결과 형식 sealing 목적.
>
> **상태**: 1차 로컬 실측 완료 (2026-06-27, §3.0 — single-shard). 분산/sysbench/2PC/전용 PV 는 pending.

## 1. 측정 환경 명시 표준

각 측정 결과는 *환경 metadata* 를 동반:

| 필드 | 예 | 비고 |
|---|---|---|
| date | 2026-MM-DD | UTC ISO 8601 |
| cluster | keiailab-prod / dev-kind / 등 | `kubectl config current-context` |
| postgres version | 16.4 | `SELECT version()` |
| operator version | v0.4.0-beta.1 | helm chart appVersion |
| shard count | 1 / 4 / 16 | ShardPlane spec.shards |
| client host | 동일 cluster / 외부 / 동일 node | network locality 영향 |
| client cores / RAM | 8C / 16G | bench 클라이언트 자원 |
| backend cores / RAM | per-pod 4C / 8G | postgres pod 자원 |
| storage class | ceph-rbd / local-path | IOPS 영향 |

## 2. Workload matrix

3 축 × N value 측정:

### 2.1 Topology (router fan-out 단계)

| ID | 설명 | 2PC | 비고 |
|---|---|---|---|
| T-1S | single-shard baseline | N/A | router 우회 가능 |
| T-NS-RO | N-shard read-only | N/A | scatter-gather 측정 |
| T-NS-2PC | N-shard cross-shard tx | YES | ADR-0015 핵심 측정 |

### 2.2 Workload type

| ID | 도구 | mode |
|---|---|---|
| W-pgb-tpcb | pgbench | tpcb-like |
| W-pgb-ro | pgbench | select-only |
| W-pgb-upd | pgbench | simple-update |
| W-sb-rw | sysbench | oltp_read_write |
| W-sb-ro | sysbench | oltp_read_only |
| W-sb-ps | sysbench | oltp_point_select |

### 2.3 Isolation level (D.10.3 cross-ref)

| ID | level | anomaly |
|---|---|---|
| I-RC | READ COMMITTED | dirty read X, non-repeatable read 허용 |
| I-RR | REPEATABLE READ (SI) | write skew 일부 |
| I-SER | SERIALIZABLE (SSI) | anomaly 0 |

## 3. 결과 표

> **1차 로컬 실측 추가됨 (2026-06-27, §3.0).** §3.1~ 는 여전히 schema placeholder
> (분산 N-shard / sysbench / 2PC / 전용 PV 미측정).

### 3.0 실측 1차 — 로컬 kind 단일샤드 (2026-06-27)

오퍼레이터를 호스트 kind(Docker Desktop/WSL2)에 배포 → 단일샤드 PostgresCluster Ready
→ pgbench. *제품 첫 baseline 수치* (single-shard HA-PG-on-K8s).

**환경**:

| 필드 | 값 |
|---|---|
| date | 2026-06-27 |
| cluster | 로컬 kind v0.32 (`pgop-dev`, 단일 노드) on Docker Desktop / WSL2 |
| operator | local build `pgop:dev` (브랜치 `chore/ha-pitr-e2e-consolidation`) |
| postgres | 18.3 (`ghcr.io/keiailab/pg:18`) |
| topology | shardingMode=none · 단일 shard · replicas=0 (HA 없음) |
| node 자원 | 16 vCPU / ~7.6 GiB (WSL2 VM 할당) |
| PG 설정 | shared_buffers=160MB · effective_cache_size=5GB · max_connections=100 |
| storage | local-path (kind 기본, WSL2 overlay) |
| client 위치 | **PG pod 내 co-located** (pgbench가 같은 16코어 공유 → TPS 보수적) |
| scale | `pgbench -s 50` (5M rows ≈ 750MB, OS 캐시 적재) · 각 30s · `-j 8` |

**결과**:

| workload | clients | TPS | latency avg |
|---|---|---|---|
| W-pgb-tpcb (read-write) | 8 | 496 | 16.1 ms |
| W-pgb-tpcb (read-write) | 16 | 646 | 24.8 ms |
| W-pgb-tpcb (read-write) | 32 | 889 | 36.0 ms |
| W-pgb-ro (select-only) | 32 | 9,035 | 3.5 ms |
| W-pgb-ro (select-only) | 64 | 10,493 | 6.1 ms |

**해석**:
- 읽기(select-only)가 쓰기(tpcb)의 **~10–18배** — 쓰기는 매 커밋 WAL fsync 가 로컬
  overlay 디스크에 바운드. 프로덕션 SSD/전용 PV 에선 쓰기 TPS 가 크게 오를 여지.
- 클라이언트 증가에 TPS 단조 증가(RW 8→32 ≈ 1.8×, RO 32→64 ≈ 1.16× — 64에서 포화 접근).
- **caveats**: ① pgbench client co-located(동일 pod/코어) → 분리 클라이언트 대비 보수적
  ② percentile(P50/95/99)은 pgbench `-l` 로그 후처리 필요(미측정, 후속) ③ WSL2 overlay
  스토리지라 쓰기 IO 가 실 PV 보다 느림.

**재현**:
```bash
kubectl -n default exec quickstart-shard-0-0 -c postgres -- pgbench -i -s 50 -U postgres postgres
kubectl -n default exec quickstart-shard-0-0 -c postgres -- pgbench -c 32 -j 8 -T 30 -U postgres postgres      # tpcb
kubectl -n default exec quickstart-shard-0-0 -c postgres -- pgbench -S -c 64 -j 8 -T 30 -U postgres postgres   # select-only
```

### 3.0b 실측 2차 — pg-router 분산 라우팅 처리량 (2026-06-28)

`cmd/router-bench`(in-repo, `internal/router.ResolveShard` 로 데이터 배치 → 라우터 동일
vindex) 로 **per-query 라우팅**의 워커 수 × TPS 와 라우터 오버헤드를 실측. *제품 첫 분산
라우터 수치*.

**환경**:

| 필드 | 값 |
|---|---|
| date | 2026-06-28 |
| 구성 | Docker 컨테이너 2 PG 샤드(`postgres:18`, scram) + `pgrouter:dev`(query-mode, env backend) 동일 네트워크 |
| 호스트 | 16 vCPU / ~7.6 GiB (WSL2), **샤드 2개 + 라우터 + 클라이언트가 같은 코어 공유** |
| storage | Docker overlay (local) |
| topology | static murmur3 hash, 2-shard (shard-0/shard-1), 키 10,000 (shard-0=4,914 / shard-1=5,086) |
| 워크로드 | 점(point) 쿼리 `SELECT val FROM kv WHERE id=$1` (lib/pq, autocommit, 키당 unnamed Parse+Bind+Execute), 각 5s |
| 클라이언트 | `db.SetMaxOpenConns(W)` = W 연결, W 워커 goroutine |

**결과 (read point-query, TPS)**:

| 시나리오 | w=1 | w=2 | w=4 | w=8 | w=16 | w=32 |
|---|---|---|---|---|---|---|
| direct-shard0 (라우터 없음, 기준선) | 3,881 | 9,843 | 13,878 | 17,561 | 24,540 | 38,069 |
| router-1shard (라우터 경유, shard-0 키만) | 1,761 | 3,185 | 3,560 | 5,185 | 7,083 | 9,437 |
| router-2shard (라우터 경유, 전 키스페이스 분산) | 1,570 | 2,938 | 3,488 | 5,044 | 6,872 | 8,955 |

(avg latency: direct 0.26→0.84 ms, router 0.57→3.4 ms.)

**해석**:
- **워커 수 × TPS (라우터 경유)**: 점 읽기 처리량이 동시성에 따라 단조 증가 — 1,761(w=1) →
  9,437(w=32) TPS. per-query 라우팅이 다중 연결을 병렬 처리함을 실증.
- **라우터 오버헤드**: direct 대비 w=1 ~2.2×, w=32 ~4×. 원인 = 프록시 1-hop + **키당
  Parse 비용**(lib/pq `db.QueryRow` 는 unnamed Parse+Bind+Describe+Execute 를 매번 전송 →
  라우터가 describe 대행·라우팅을 매 쿼리 수행). prepared statement 재사용(샤드별 한 번만
  Parse, 이후 Bind/Execute)이면 이 비용이 크게 줄 것 — 후속 측정 항목.
- **2샤드 ≈ 1샤드 (point read)**: 분산이 처리량을 *늘리지 않음*. 이유 = 점 읽기에선 **라우터가
  병목**(~9K TPS 천장)이고 단일 샤드(38K)는 한참 여유 → 일을 두 샤드로 나눠도 라우터 천장이
  그대로. **수평 스케일이 드러나려면 샤드가 병목인 조건**(멀티호스트로 라우터·샤드 코어 분리,
  또는 라우터 다중 인스턴스, 또는 샤드 CPU 를 포화시키는 무거운 쿼리)이 필요 — 단일 호스트
  단일 라우터로는 분산 이득 미관측. 이것이 다음 측정의 핵심 과제.
- **쓰기(update)**: Docker overlay fsync + 단일 호스트 CPU 경합으로 변동 극심(direct w=32→64
  가 3,152→17,424 로 튐)해 분산 결론 부적합 — 전용 PV / 멀티호스트 재측정 필요(보류).

**caveats**: ① 샤드·라우터·클라이언트가 같은 16코어 공유 → 분산 스케일 관측 불가(설계상
한계) ② overlay 스토리지 ③ percentile 미측정(avg 만) ④ lib/pq unnamed-Parse 경로 →
prepared 재사용 시 라우터 처리량 상향 여지.

**재현**:
```bash
# 2 scram 샤드 + pgrouter:dev 동일 네트워크 기동 후:
BENCH_KEYS=10000 BENCH_DURATION=5s BENCH_WORKERS=1,2,4,8,16,32 BENCH_MODE=select \
  go run ./cmd/router-bench
```

### 3.1 Single-shard baseline (T-1S)

| date | workload | iso | clients | TPS | P50 ms | P95 ms | P99 ms | comment |
|---|---|---|---|---|---|---|---|---|
| _(pending)_ | W-pgb-tpcb | I-RC | 10 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | W-pgb-ro | I-RC | 50 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | W-sb-rw | I-RC | 8 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | W-sb-ps | I-RC | 32 | — | — | — | — | _(pending live measurement)_ |

### 3.2 N-shard read-only (T-NS-RO)

scatter-gather 의 fan-out overhead 측정.

| date | shards | workload | clients | TPS | P50 ms | P95 ms | P99 ms | comment |
|---|---|---|---|---|---|---|---|---|
| _(pending)_ | 4 | W-pgb-ro | 50 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 16 | W-pgb-ro | 50 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 4 | W-sb-ps | 32 | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 16 | W-sb-ps | 32 | — | — | — | — | _(pending live measurement)_ |

### 3.3 N-shard cross-shard 2PC (T-NS-2PC) — ADR-0015 핵심

cross-shard tx 의 2PC overhead 측정.

| date | shards | workload | iso | clients | TPS | P50 ms | P95 ms | P99 ms | 2PC abort% | comment |
|---|---|---|---|---|---|---|---|---|---|---|
| _(pending)_ | 4 | W-pgb-tpcb | I-RC | 10 | — | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 4 | W-pgb-tpcb | I-SER | 10 | — | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 16 | W-pgb-tpcb | I-RC | 10 | — | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 4 | W-sb-rw | I-RC | 8 | — | — | — | — | — | _(pending live measurement)_ |
| _(pending)_ | 4 | W-sb-rw | I-SER | 8 | — | — | — | — | — | _(pending live measurement)_ |

### 3.4 Isolation × throughput trade-off (D.10.3 cross-ref)

per-isolation level TPS 비교 (4-shard W-sb-rw 기준):

| iso | TPS | anomaly count (D.10.3) | comment |
|---|---|---|---|
| I-RC | — | per D.10.3 matrix | _(pending live measurement)_ |
| I-RR | — | per D.10.3 matrix | _(pending live measurement)_ |
| I-SER | — | 0 (expected) | _(pending live measurement)_ |

## 4. Target metric (G5 졸업 조건 후보)

ROADMAP G5 가 합의될 때 본 target 이 SLO 로 격상:

- W-pgb-ro on T-NS-RO 4-shard: TPS ≥ 4× single-shard (linear scale-out)
- W-pgb-tpcb on T-NS-2PC 4-shard: P99 < 2× single-shard (2PC overhead < 100%)
- 2PC abort rate < 5% under normal load (no chaos)
- vindex lookup P99 < 10μs (per RFC-0002 §297)
- `_vindex_hash` write overhead < 5% (per RFC-0003 §286)

## 5. 측정 protocol

1. cluster + N-shard ShardPlane provisioning (별 turn, 사용자 영역)
2. `test/bench/pgbench.sh` 또는 `sysbench.sh` env 변수 지정 후 실행
3. `bench-results/` log → 본 표 채움 (PR 또는 별 commit)
4. 환경 metadata §1 동반 명시
5. 동일 시나리오 3회 측정 → median 적재 (variance 별 column 후보)

## 6. Refs

- ROADMAP.md L183 G5 Benchmarks
- test/bench/README.md — 측정 wrapper 사용법
- docs/rfcs/0002-shardrange-crd.md §297 — vindex P99 target
- docs/rfcs/0003-shardsplitjob-7step.md §286 / §303 — overhead target
- docs/sharding/SHARDING.md §118 — sysbench-tpcc + pgbench --select-only
- ADR-0015 — cross-shard transaction semantics
- D.10.3 isolation-matrix — anomaly × throughput trade-off
