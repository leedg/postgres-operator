# 라우터/샤딩 테스트 카탈로그

> `internal/router` + `cmd/pg-router`(분산 SQL 라우터)의 테스트케이스를 영역별로 정리한
> *추후 참고용 색인*이다. 무엇을 검증하는지 + 어떻게 돌리는지 + 라이브 검증 절차를 담는다.
>
> 작성 기준: 2026-06-27. 관련: [ROUTER-GAP-ANALYSIS.ko.md](ROUTER-GAP-ANALYSIS.ko.md)
> (설계·능력 사다리·백로그) · [`docs/perf/baseline.md`](../perf/baseline.md)(성능 실측) ·
> [`docs/TEST_ANALYSIS.md`](../TEST_ANALYSIS.md)(오퍼레이터 전체 테스트 분석).

---

## 1. 실행 방법

호스트에 go/make 없음 → **컨테이너에서** 실행 (Dev Container 정식 절차는 dev-setup 문서).

```bash
# 라우터 + pg-router 단위 테스트 (라이브 클러스터 불필요, 전부 순수/in-memory)
docker run --rm -v <repo>:/src -w /src golang:1.26 \
  sh -c "go test ./internal/router/... ./cmd/pg-router/..."

# 커버리지
... go test -cover ./internal/router/...        # 최근 77.6%

# 전체 오퍼레이터 스위트(envtest 포함) — controller/webhook 등까지
... make test
```

- **외부 SQL 파서 의존성 없음**: 라우팅 키 추출 parser 전략은 *제로 의존성 토크나이저*라
  별도 build-tag 불필요(전부 평이하게 컴파일·실행). (auxten 등 외부 파서는 genproto 충돌로
  기각 — ROUTER-GAP-ANALYSIS §5.)
- 알려진 flaky: `internal/controller/failover` 의 `TestLeaseElection`(타이밍 의존)이 전체
  병렬 부하에서 간헐 실패 → 단독 재실행 시 통과(`go test -count=1 ./internal/controller/failover/...`).

---

## 2. 단위 테스트 카탈로그 (영역별)

### 2.1 Vindex (키 → 샤드)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `vindex_test.go` `TestResolveShard` | hash/range vindex, murmur3/fnv/crc32, 범위 매칭, no-match 에러 |
| `vindex_consistent_test.go` `TestConsistentHash_Deterministic` | 같은 키→같은 샤드, 모든 샤드 사용 |
| `…_MinimalMovement` | **핵심 속성**: 샤드 3→4 추가 시 키 ~29%만 이동(modulo 해시 ~75%) |
| `…_DefaultVirtualNodes` | VirtualNodes=0 시 기본값(128) 링 구성 |
| `…_NoShards` | 샤드 0개 → 에러 |

### 2.2 라우팅 키 추출 (regex / parser / auto)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `sql_route_test.go` `TestExtractRoutingKey` | regex first-literal 모드(VALUES/WHERE 등호), 빈 리터럴 거부 |
| `…_RoutesToShard` | 추출 키가 vindex로 단일 샤드에 결정적 매핑 |
| `route_extractor_test.go` `TestRegexExtractor_ColumnMode` | 지정 컬럼(WHERE/AND 등호 + INSERT 위치) 추출 |
| `…TestNewRouteKeyExtractor` | 전략 선택기(regex/parser/auto), 빈/오류 이름 |
| `…TestAutoExtractor_FallsBackToRegex` | auto가 parser 매치 실패 시 regex 폴백 |
| `route_extractor_parser_test.go` `TestParserExtractor` | 토크나이저 추출 — SELECT/INSERT/UPDATE/DELETE/복합 predicate/`t.col`/parameterized |
| `…TestParserBeatsRegex` | 따옴표 내부·주석 속 가짜 predicate를 오인하지 않음(정규식 대비 강점) |
| `…TestParserSelectableViaFactory` | "parser"/"auto" 선택이 실제 토크나이저 사용 |

### 2.3 읽기/쓰기 분류

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `route_extractor_parser_test.go` `TestIsReadOnlyQuery` | 보수적 분류 — SELECT/SHOW/VALUES/TABLE=읽기, `FOR UPDATE/SHARE`·DML·WITH=쓰기 |

### 2.4 토폴로지 (key→shard 공급)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `topology_test.go` `TestTopologyShard` | 키→샤드 vindex 평가 |
| `…TestCRDTopologyProvider` | ShardRange CRD에서 토폴로지 구성, cluster/keyspace 매칭, 캐시, 미매칭 에러 (fake lister) |

### 2.5 백엔드 해소 (failover-aware / 읽기 분산)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `topology_test.go` `TestStatusBackendResolver` | `status.primary.endpoint`(Ready만)에서 해소, not-ready/부재 에러, **failover 시 새 primary 추종** |
| `…TestStatusBackendResolver_ResolveRead` | Ready replica round-robin, replica 없으면 primary 폴백, 둘 다 없으면 에러 |

### 2.6 Reference table

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `reference_test.go` `TestExtractTables` | FROM/JOIN/INTO/UPDATE 테이블 추출, schema 한정 `s.t`→`t` |
| `…TestReferenceRouting` | reference-only 판정(전부 reference면 true, 샤딩 테이블 섞이면 false), AnyShard 결정성 |

### 2.7 쿼리 라우팅 결정 엔진 (E 핵심)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `query_router_test.go` `TestQueryRouter_WriteRoutesToPrimaryShard` | 쓰기 → primary 백엔드 |
| `…_ReadRoutesToReplica` | 읽기 → replica 백엔드 |
| `…_ReferenceOnlyUsesAnyShard` | reference 쿼리 → AnyShard |
| `…_NoKeySignalsScatter` | 키 부재 → Scatter=true + ErrNoRoutingKey |
| `…_BackendErrorPropagates` | 백엔드 해소 에러 전파(샤드 down) |

### 2.8 Scatter-gather

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `scatter_test.go` `TestScatterGather` | fan-out, FailFast/BestEffort 정책, ErrNoShards, 부분 실패 |
| `scatter_merge_test.go` `TestScatterGather_OrderByNumeric` | 타입 인지 정렬(숫자 `"10"<"9"` 버그 수정) |
| `…_Limit` | merge 후 LIMIT 적용 |

### 2.9 SQL Executor (연결 풀)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `sql_executor_test.go` `TestSQLShardExecutor_PoolReuse` | shard별 `*sql.DB` 재사용(per-call open 안 함), Close가 풀 비움 |
| `…_NoDSN` | DSN 없는 샤드 → ErrNoDSN |
| `…_SatisfiesInterface` | ScatterGather의 ShardExecutor로 주입 가능 |

### 2.10 Resharding · Placement · Metadata (기존)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `resharding_test.go` `TestValidateSplitPlan` / `TestHexSuccessor` | split 보존 불변식(gap/overlap/coverage), hex 인접성 |
| `reshard_copy_test.go` `TestBuildInsert` / `TestCopyTable_RejectsInjection` | copy SQL 생성, 테이블명 인젝션 거부 |
| `placement_test.go` `TestPlacementDrift` / `TestValidatePlacement` | drift 감지(Missing/Extra/Zone/Node/NotReady/RangeUncovered), placement 검증 |
| `metadata_store_test.go` `TestPostgresStore` | `pg_keiailab` 스키마 마이그레이션 + Upsert/List/Delete |

### 2.11 pg-router (PG wire 프록시)

| 파일 · 테스트 | 검증 내용 |
|---|---|
| `main_test.go` `TestReadStartupParsesParams` / `TestReadStartupHandlesSSLRequest` | v3 startup 파싱, SSLRequest('N' 거절 후 재파싱) |
| `…TestShardSpecRoutesByVindex` | 정적 2-shard spec 라우팅 |
| `…TestBackendForUsesEnvMapping` / `TestTemplateResolver` / `TestEnvBackendResolver` | env/DNS 템플릿 백엔드 해소 |
| `…TestWritePgError` | 샤드 down 시 우아한 PostgreSQL `ErrorResponse`('E') 인코딩(조용한 drop 금지) |
| `dialer_test.go` `TestDialer_RetryThenSuccess` | dial retry/backoff(주입 dial) |
| `…_CircuitOpensAndCooldown` | 연속 실패 시 circuit open→fast-fail, cooldown 후 재시도(주입 clock) |
| `…_SuccessResetsBreaker` | 성공이 실패 카운트 리셋 |

---

## 3. 라이브 검증 (실 클러스터)

단위 테스트로 못 잡는 부분은 호스트 kind(Docker Desktop/WSL2 — 컨테이너 안 중첩 아님)에서 검증.

### 3.1 완료된 라이브 검증
- **성능 baseline (2026-06-27)**: 오퍼레이터 배포 → 단일샤드 PostgresCluster Ready → pgbench.
  결과·환경·재현 명령은 [`docs/perf/baseline.md §3.0`](../perf/baseline.md). (배포 흐름:
  `docker build`→`kind load`→`kubectl apply -k config/default`→`set image`→dev 샘플 적용.)
- **referenceTables CRD**: 실 apiserver 수용 검증(server-side apply).

### 3.2 미완(라이브 failover 환경 필요 — 무검증 랜딩 금지)
- 자동 failover chaos drill (`make test-e2e-failover`): primary kill→fencing→승격→reseed.
- PITR restore drill (`make test-e2e`): WAL 아카이빙→offline restore→락 해제.
- 백로그 보류 3건(per-shard primary Service / watch hot-reload / failover lease P2-T3)의 라이브 검증.
- 분산 N-shard scatter 수치, percentile(pgbench `-l` 로그 후처리), sysbench, 전용 PV.

---

## 4. 용어집

> 정의는 [GLOSSARY.ko.md](../GLOSSARY.ko.md)에서 발췌해 동일하게 유지한다. 전체 용어는 해당 문서 참고.

| 용어 | 정의 |
|---|---|
| Vindex (가상 인덱스) | 샤딩 키 → 샤드를 결정하는 함수/정책(hash·range·consistent-hash 등). Vitess 용어 차용. |
| Scatter-gather | 한 쿼리를 여러 샤드에 fan-out하고 결과를 모아 merge하는 분산 읽기 패턴. |
| Reference table | 모든 샤드에 복제해 두는 작은 공통 테이블. 분산 조인을 우회하는 수단. |
| Failover (장애 조치) | Primary 장애 감지 후 Replica 하나를 새 Primary로 자동 승격해 서비스를 잇는 동작. |
| Topology (토폴로지) | 어떤 키 범위가 어떤 샤드에 있는지의 라우팅 메타데이터(ShardRange CRD). |
| envtest | 실제 클러스터 없이 API 서버/etcd만 띄워 컨트롤러를 통합 테스트하는 도구. |
| Circuit breaker | 반복 실패하는 대상으로의 호출을 일정 시간 빠르게 차단해 장애 전파를 막는 패턴. |
