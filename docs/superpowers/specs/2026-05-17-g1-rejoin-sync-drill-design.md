# G1 마감 — replica rejoin / synchronous replication 라이브 drill

- Date: 2026-05-17
- Status: Proposed
- Scope: postgres-operator ROADMAP Gate G1 `[~]` → `[x]` 전환을 위한 라이브 drill 자동화
- Refs: ROADMAP.md G1 `Verify` 행, HANDOFF.md `T29` 인접, `hack/smoke.sh` 기존 `SMOKE_FAILOVER` 패턴

## 1. 목적

ROADMAP G1 의 두 `[~]` 항목 — **replica rejoin** (pg_basebackup / pg_rewind) 과 **synchronous replication** (RPO=0) — 의 코드 path 는 이미 reconciler 에 통합되어 있다. 그러나 G1 `Verify` 행이 요구하는 **"0 data loss"** 와 **"rejoin 데이터 정합"** 은 라이브 환경에서 검증된 적이 없어 `[~]` 마커가 유지되고 있다.

본 spec 는 두 검증 시나리오를 기존 `hack/smoke.sh` 의 `SMOKE_*` 옵션 패턴으로 자동화해 단일 명령으로 PASS/FAIL 을 판정 가능하게 한다.

## 2. 비목표 (Non-Goals)

- ❌ HA election distributed lock (K8s Lease) — 별 task, G1 의 마지막 `[ ]` 항목
- ❌ chaos 다중 시나리오 (네트워크 partition, disk pressure) — G6 영역
- ❌ multi-AZ 시뮬레이션 — 단일 kind cluster scope 외
- ❌ Go e2e test 신규 작성 — bash 시나리오로 충분, ROADMAP `Verify` 라인 정합
- ❌ Plugin (pgBackRest / WAL-G) 검증 — G2 backup 영역

## 3. 아키텍처

### 3.1 통합 지점

기존 `hack/smoke.sh` (909+ 줄, bash) 의 step 시퀀스에 두 단계 추가:

```
[14/15] SMOKE_FAILOVER=1  (기존, 변경 없음)
[15a]   SMOKE_REJOIN=1    (신규)
[15b]   SMOKE_SYNC=1      (신규)
[15/15] cleanup           (기존)
```

각 신규 단계는:
- 환경변수로 opt-in (기본 skip)
- 독립 실행 가능 (`SMOKE_REJOIN=1` 단독 / `SMOKE_FAILOVER=1 SMOKE_REJOIN=1` 조합 모두 동작)
- 의존 단계 부재 시 명시적 skip 메시지 + exit 0 (시나리오 wiring 실수 차단)

### 3.2 환경변수 표면

| 변수 | 기본값 | 의미 |
|---|---|---|
| `SMOKE_REJOIN` | `0` | 1 = replica rejoin drill 실행. `SMOKE_FAILOVER=1` + `REPLICAS>=2` 전제. |
| `SMOKE_REJOIN_MODE` | `auto` | `auto` / `basebackup` / `rewind` — drill 분기 강제. `auto` = 두 분기 모두 순차 실행. |
| `SMOKE_SYNC` | `0` | 1 = synchronous replication RPO=0 drill 실행. `REPLICAS>=2` 전제. |
| `SMOKE_SYNC_KILL` | `0` | 1 = B.4 sync-standby kill 시나리오 포함 (침습적, default off). |

## 4. 시나리오 A — `SMOKE_REJOIN=1` replica rejoin drill

### 4.1 사전 조건 verify

- `REPLICAS >= 2`
- `SMOKE_FAILOVER=1` 직후 (old primary = `${STS_NAME}-0`, new primary = `${STS_NAME}-1`)
- new primary 에서 `pg_is_in_recovery()=f` 확인

### 4.2 A.1 — pg_basebackup fresh 분기 (`SMOKE_REJOIN_MODE=basebackup` 또는 `auto`)

1. drill 표 생성: new primary 에 `CREATE TABLE rejoin_drill_basebackup (id int); INSERT 1..100;`
2. old primary PVC 강제 삭제 (reconciler 가 PVC 재생성 → fresh basebackup 진입)
   ```bash
   kubectl -n "$NS" delete pvc "${PVC_PREFIX}-${STS_NAME}-0" --wait=true
   kubectl -n "$NS" delete pod "${STS_NAME}-0" --wait=false
   ```
3. old primary Pod Ready 대기 (max 180s)
4. verify:
   - `pg_stat_replication` 에 `${STS_NAME}-0` 등장 (`application_name` match)
   - `${STS_NAME}-0` 에서 `SELECT count(*) FROM rejoin_drill_basebackup` = 100
   - `${STS_NAME}-0` 의 `pg_is_in_recovery()=t`

### 4.3 A.2 — pg_rewind 분기 (`SMOKE_REJOIN_MODE=rewind` 또는 `auto`)

1. *전제 reset*: A.1 후라면 다시 failover 한 번 더 실행해 old/new 역할 swap
2. divergence 발생: failover 직전 old primary 에 의도적 local write (commit 후 sync 전 kill)
   - 구현: `INSERT INTO rejoin_drill_rewind ...` → `pg_switch_wal()` → 즉시 `kubectl delete pod` (graceful=0)
3. promotion 후 new primary 에서 *다른* row 추가: `INSERT INTO rejoin_drill_rewind ...`
4. old primary 재시작 → reconciler 가 `pg_rewind` path 선택 (existing-PGDATA + old-primary marker)
5. verify:
   - old primary log 에 `pg_rewind: source server ... timeline N` 발견 (`kubectl logs | grep pg_rewind`)
   - `${STS_NAME}-0` 의 `SELECT * FROM rejoin_drill_rewind` 가 new primary 와 동일 (divergent local row 사라짐)
   - `pg_stat_replication` streaming 재개

### 4.4 A.3 — 데이터 정합 종합

- failover 전후 `pg_stat_replication.flush_lsn` 가 new primary 의 `pg_current_wal_lsn()` 까지 catch-up
- 모든 rejoin_drill_* 테이블 row count 일치

## 5. 시나리오 B — `SMOKE_SYNC=1` synchronous replication RPO=0 drill

### 5.1 사전 조건 verify

- `REPLICAS >= 2`
- PostgresCluster `status.phase=Ready`

### 5.2 B.1 — sync replication patch + rolling 대기

```bash
kubectl -n "$NS" patch postgrescluster "$CR_NAME" --type=merge -p '{
  "spec": {
    "postgresql": {
      "synchronous": {
        "method": "ANY",
        "number": 1,
        "dataDurability": "required"
      }
    }
  }
}'
```

- ConfigMap `config.sha256` 변경 대기 + StatefulSet rolling restart 대기 (max 180s)
- verify: primary 의 `SHOW synchronous_commit;` = `on` (또는 `remote_apply`) + `SHOW synchronous_standby_names;` non-empty

### 5.3 B.2 — sync replica 등록 확인

- primary 에서 `SELECT application_name, sync_state FROM pg_stat_replication;`
- 최소 1개 row 의 `sync_state='sync'` 확인 (timeout 60s)

### 5.4 B.3 — RPO=0 증명

1. drill 표 생성: `CREATE TABLE sync_drill (id serial PRIMARY KEY, ts timestamptz DEFAULT now());`
2. 1000 row INSERT (single transaction commit)
3. commit LSN 캡처: `SELECT pg_current_wal_lsn();` → `$COMMIT_LSN`
4. sync standby 의 `flush_lsn` 또는 `apply_lsn` 가 `$COMMIT_LSN` 이상 도달 확인 (timeout 30s)
5. PASS 조건: `flush_lsn >= $COMMIT_LSN` (RPO=0 직접 증명)

### 5.5 B.4 — sync standby kill 차단 시나리오 (`SMOKE_SYNC_KILL=1`)

침습적이라 default off. 활성 시:

1. sync standby Pod kill (`kubectl delete pod ${STS_NAME}-1 --wait=false`)
2. primary 에서 write 시도: `INSERT INTO sync_drill ...` with `statement_timeout=10s`
3. verify: 10s timeout 내 commit 실패 (sync 가 차단 작동)
4. standby Pod Ready 복귀 대기
5. write 재시도 → 정상 commit verify

### 5.6 B.5 — cleanup

- sync 설정 revert (`spec.postgresql.synchronous=null` patch)
- drill 표 보존 (`--keep` flag 시 사용자가 직접 검토)

## 6. 출력 형식

각 단계는 기존 `log "  PASS: ..."` / `log "ERROR: ..."` + `exit 1` 패턴 유지. 마지막에 요약 블록:

```
[REJOIN drill summary]
  A.1 basebackup: PASS (rejoin_lsn=0/1A2B3C4D, catch_up=4s)
  A.2 rewind:     PASS (divergent_rows_removed=3, rewind_log_found=yes)
  A.3 integrity:  PASS

[SYNC drill summary]
  B.1 patch:      PASS (rolling=42s)
  B.2 sync_state: PASS (standby=quickstart-shard-0-1)
  B.3 RPO=0:      PASS (commit_lsn=0/1A2B3C4D, flush_lsn=0/1A2B3C4D, lag_ms=12)
  B.4 kill:       SKIP (SMOKE_SYNC_KILL=0)
```

## 7. 영향 파일

| 파일 | 변경 | 라인 추정 |
|---|---|---|
| `hack/smoke.sh` | 두 단계 함수 추가 + step 호출 + 환경변수 docstring | +180~250 |
| `ROADMAP.md` G1 | `[~]` → `[x]` 2개 (rejoin / sync) + Refs 갱신 | ~6 |
| `HANDOFF.md` | `Next-session entry points` 에 본 drill 명령 + `T29` 와 분리된 entry | ~20 |
| `TASKS.md` | 신규 task (예: `T30 G1 라이브 drill 자동화`) 행 추가 + 단계/완성도 | ~6 |
| `docs/runbooks/ha.md` | RTO≤60s / RPO=0 SLO 와 본 drill 측정값 연결 | ~30 |
| `docs/superpowers/specs/2026-05-17-g1-rejoin-sync-drill-design.md` | 본 spec | (현재 파일) |

## 8. 검증 명령 (Verify)

```bash
# 전체 시나리오 라이브 실행
SHARD_REPLICAS=2 SMOKE_FAILOVER=1 SMOKE_REJOIN=1 SMOKE_SYNC=1 ./hack/smoke.sh

# rejoin only (failover 후 단독)
SHARD_REPLICAS=2 SMOKE_FAILOVER=1 SMOKE_REJOIN=1 SMOKE_REJOIN_MODE=basebackup ./hack/smoke.sh

# sync only
SHARD_REPLICAS=2 SMOKE_SYNC=1 ./hack/smoke.sh

# 침습 kill 시나리오 포함
SHARD_REPLICAS=2 SMOKE_SYNC=1 SMOKE_SYNC_KILL=1 ./hack/smoke.sh

# bash 단위 (smoke_shell_test.sh 보강 — 환경변수 분기만)
bash hack/smoke_shell_test.sh
```

PASS 정의: 위 3 명령 모두 exit 0 + 출력에 모든 PASS 행 등장.

## 9. 실패 정의 (Failure Modes)

| 시나리오 | 실패 신호 | 대응 |
|---|---|---|
| A.1 PVC delete 후 Pod 가 Pending 영구 | StorageClass dynamic provisioning 부재 | kind config 에 local-path-provisioner 확인 |
| A.2 pg_rewind log 미발견 | reconciler 가 basebackup fallback 선택 | reconciler 로그 인용 + 분기 조건 RCA |
| B.3 flush_lsn lag 초과 | sync 등록 실패 또는 network noise | `pg_stat_replication` 전체 dump + 30s 재시도 → 영구 FAIL 시 exit 1 |
| B.4 timeout 미발생 | sync 가 실제 enforce 안 됨 = 코드 회귀 | 즉시 ERROR + reconciler 설정 + ConfigMap rendering audit |

## 10. 일정/단계 (Implementation Phases)

(구체적 일정은 writing-plans 단계에서 task 분해)

- Phase 1: `SMOKE_REJOIN=1` A.1 (basebackup 분기) 단독 + 라이브 PASS
- Phase 2: `SMOKE_REJOIN_MODE=rewind` A.2 + 라이브 PASS
- Phase 3: `SMOKE_SYNC=1` B.1~B.3 (RPO=0) + 라이브 PASS
- Phase 4: `SMOKE_SYNC_KILL=1` B.4 + 라이브 PASS
- Phase 5: ROADMAP/HANDOFF/TASKS/runbook 갱신 + commit
- Phase 6: `[~]→[x]` 마커 변경 PR (G1 closure 근거)

## 11. 변경 이력

| Date | Change | Refs |
|---|---|---|
| 2026-05-17 | 초안 작성 | brainstorming 세션 / ROADMAP G1 Verify |
