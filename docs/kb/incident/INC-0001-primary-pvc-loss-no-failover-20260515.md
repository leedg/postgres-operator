# INC-0001: Primary PVC + Pod 동시 삭제 시 operator promote 미수행, STS 가 빈 PGDATA primary 재생성

- Detected: 2026-05-15 15:31 (KST)
- Resolved: Open (root cause 식별, fix 별 PR)
- Severity: SEV-2 (kind smoke 환경) — production 동등 시나리오는 SEV-1
- Owners: @phil (라이브 검증), keiailab/postgres-operator
- Tags: [failover, sts, pvc-loss, split-brain, g1, chaos-drill]

## Impact

- **사용자 영향**: kind smoke 환경 — 사용자 데이터 없음. **production 동등 시나리오는 데이터 손실 위험**.
- **시스템 영향**:
  - Primary (`quickstart-shard-0-0`) 의 PVC 와 Pod 가 동시 삭제됐을 때, operator 가 standby (`quickstart-shard-0-1`) 를 promote *하지 않음*.
  - STS controller 가 PVC volumeClaimTemplate 으로 새 PVC + 새 Pod 를 생성 → 빈 PGDATA 로 fresh initdb → 새 primary 로 부트.
  - Standby 는 *옛 데이터* 를 보존했으나 replication 끊김 (`pg_stat_replication` 0 rows), 옛 timeline 에 freezing.
  - 결과: 두 timeline 분리 — 빈 primary + 데이터 있는 standby (write 불가).
- **재정/법적 영향**: 본 incident 자체는 없음. production 재현 시 RPO=∞ 의 데이터 손실.

## Timeline

- 06:31:09 UTC — marker row `INSERT INTO failover_test('pre-failover')` on shard-0-0.
- 06:31:11 UTC — standby shard-0-1 도 marker row 확인 (replication 정상).
- 06:31:38 UTC — `kubectl delete pvc/data-quickstart-shard-0-0 pod/quickstart-shard-0-0`.
- 06:31:39 UTC — operator 로그: `pvc-resize skip non-Bound PVC ... phase=Pending` 반복. *failover 결정 0건*.
- ~06:32:30 UTC — STS 가 새 PVC + 새 Pod 생성 → fresh initdb → Ready.
- 06:42:14 UTC (~10분 후) — 검증: shard-0-0 = empty primary (`failover_test` 없음), shard-0-1 = 옛 marker 보존, `pg_is_in_recovery()=t`, `pg_stat_replication` 0 rows.

## Root Cause

5 Whys:

1. **왜 operator 가 promote 하지 않았는가?** `DetectPrimaryFailure` (`internal/controller/failover/detection.go`) 가 *Pod 상태 기반* (Pod Ready=false N초 등) 으로 추정 — STS 가 새 Pod 를 빠르게 생성하여 *짧은 outage* 동안에만 Ready=false → detector threshold 미도달.
2. **왜 STS 가 빈 PGDATA 로 primary 를 시작했는가?** STS volumeClaimTemplate 표준 동작: PVC 삭제 시 동일 이름으로 재생성. 새 PV 는 빈 disk. PG 이미지 entrypoint 가 빈 PGDATA 발견 시 `initdb` 실행. 이는 operator 의 *Pod-level reconcile* 가 PGDATA epoch 검증을 *하지 않음* 을 의미.
3. **왜 operator 는 fresh initdb 를 막지 않는가?** Pod entrypoint (Dockerfile.pg 의 initdb 자동 실행 또는 `cmd/instance`) 가 빈 PGDATA 를 *정상 초기 상태* 로 해석. ImageCatalog / standby bootstrap 분기 없음.
4. **왜 standby 가 fresh primary 에 합류하지 않는가?** Streaming standby 는 *동일 timeline ID* 만 따라간다. fresh initdb 는 새 timeline 으로 시작 → 옛 timeline 과 호환 불가. `pg_rewind` 로 합류 가능하지만 *operator 가 호출하지 않음* (`Replica rejoin` 라이브 미검증 — ROADMAP G1 `[~]`).
5. **왜 ROADMAP G1 의 `[~] Replica rejoin ... Live chaos / rewind drill verification still pending` 가 *전체 시나리오 부재* 로 표현돼야 하는가?** 본 incident 가 정확히 *그 미검증 영역* — operator-level 의 "primary PGDATA 손실 → standby promote + 옛 primary 자리에 fresh basebackup" 흐름이 *없음*.

기여 요인:
- STS controller 의 *fast Pod recovery* 가 operator 의 *failure detection threshold* 보다 짧음.
- PG image entrypoint 가 빈 PGDATA 를 자동 initdb (defense 부재).
- operator 가 `instance-status` annotation 또는 timeline ID 같은 *durable identity* 를 primary Pod 의 PGDATA 와 cross-check 하지 않음.

## Resolution

현재 open. 미들 단계 fix 후보:

1. **Detection 강화**: `DetectPrimaryFailure` 가 *PGDATA loss* 도 신호로 인식 — 예: PVC UID 변경 감지, Pod restart 후 timeline regress 검출.
2. **Bootstrap 방어**: Pod entrypoint 가 빈 PGDATA + standby alive 상황 인지 시 `initdb` 대신 *standby 로부터 `pg_basebackup`* 또는 *대기 + operator 결정* 기다림.
3. **Promotion 트리거**: STS recreate event 를 operator 가 별도 watch → 짧은 grace period 후 standby promote → 새 Pod 는 fresh standby 로 합류.

근시: kind smoke 시나리오만으로는 production 영향 0 — fix 는 별 PR.

## Prevention

- **단기**:
  - INC 등록 (본 문서) + ROADMAP G1 `[~] Replica rejoin` row 에 *"PVC-loss scenario unsupported"* 명시.
  - `test/e2e/failover_e2e_test.go` 에 본 시나리오 (`PVC + Pod 동시 삭제 → expect promote within RTO`) 추가, 현 상태 `t.Skip` 또는 `expected failure`.
- **중기**:
  - `internal/controller/failover/detection.go` 에 *PGDATA timeline regression detection* 추가 + unit test.
  - `cmd/instance` 의 bootstrap 분기에 *standby-aware empty PGDATA* 처리.
- **장기**:
  - ROADMAP G6 "Chaos engineering — disk pressure" 확장 — PVC loss / node disk fail 등 시나리오 자동 drill.

## Action Items

- [ ] AI-0001: ROADMAP G1 `[~] Replica rejoin` row 에 본 INC 링크 + 시나리오 한계 명시 (Owner: @phil, Due: ~)
- [ ] AI-0002: `test/e2e/failover_e2e_test.go` 에 PVC-loss skipped test 추가 (Owner: TBD)
- [ ] AI-0003: detection.go 에 timeline regression 감지 설계 — design ADR 작성 (Owner: TBD)

## Refs

- ROADMAP G1 (`docs/roadmap.md:78` `[~] Replica rejoin`).
- standards/incident-kb.md (template).
- 라이브 evidence: kind cluster `postgres-operator-smoke`, operator image `ghcr.io/keiailab/postgres-operator:0.3.0-alpha.18`, PG image `ghcr.io/keiailab/pg:18`.

```
# Verify (재현 명령)
kubectl exec quickstart-shard-0-0 -c postgres -- psql -h /var/run/postgresql -U postgres -d postgres \
    -c "CREATE TABLE failover_test(id int); INSERT INTO failover_test VALUES (1);"
kubectl delete pvc/data-quickstart-shard-0-0 pod/quickstart-shard-0-0
# wait ~30s, then:
kubectl exec quickstart-shard-0-1 -c postgres -- psql -At -c "SELECT pg_is_in_recovery()"  # still t
kubectl exec quickstart-shard-0-0 -c postgres -- psql -At -c "SELECT count(*) FROM failover_test"  # ERROR: does not exist
kubectl exec quickstart-shard-0-1 -c postgres -- psql -At -c "SELECT count(*) FROM failover_test"  # 1 (frozen)
```
