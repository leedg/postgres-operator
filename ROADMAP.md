# ROADMAP — postgres-operator

본 ROADMAP 은 날짜 약속이 아니라 검증 가능한 Gate 로 진행을 추적한다. 현재 정체성은 **Apache-2.0 PostgreSQL Kubernetes Operator** 이며, PGO-class 운영 품질을 목표로 하지만 PGO, Citus, CNPG, Patroni 같은 외부 시스템을 fork, embed, wrapper 로 사용하지 않는다.

## 원칙

- **외부 설계 참고 허용**: PGO의 운영 UX, Citus의 분산 SQL 문제 분해, Vitess의 router idiom, CNPG의 Kubernetes 운영 패턴은 공개 문서와 논문 수준에서 참고할 수 있다.
- **외부 시스템 내장 금지**: Citus extension, CNPG `Cluster`, Patroni DCS, Cockroach/Yugabyte backend, PGO controller 코드를 제품 런타임에 포함하지 않는다.
- **신규 서비스로 구현**: operator manager, instance manager, sharding metadata, router, backup orchestration 은 본 repo 코드와 Apache-2.0 호환 의존성으로 구현한다.
- **PGO-class는 품질 기준**: HA, backup, restore, upgrade, observability, security UX의 목표 수준을 뜻하며 특정 제품 사용을 뜻하지 않는다.

## 현재 상태

| 항목 | 상태 | 검증 |
|---|---|---|
| 프로젝트/차트 이름 | `postgres-operator` | GitHub repo, Helm chart, argos GitOps path 정렬 |
| 라이선스 | Apache-2.0 | `LICENSE`, ADR-0003 |
| 최신 릴리스 | `0.3.0-alpha.3` | GHCR image + Helm chart publish |
| argos 배포 | Day-0 single-shard | `PostgresCluster/argos-postgres` Ready 검증 |
| GHCR runtime image | public pull 가능 | `ghcr.io/keiailab/pg:18` pull secret 없이 재기동 검증 |
| HA replica | 미완료 | `replicas=0`, production DB 전환 전 필수 |
| Backup/Restore | 미완료 | `BackupJob` 경로는 아직 production drill 미통과 |
| 1.0.0 GA | 미완료 | HA, backup/restore, upgrade, chaos, 장기 soak 필요 |

## Gate

| Gate | 목표 | 완료 조건 |
|---|---|---|
| G0 — Day-0 배포 | operator + single-shard PG가 GitOps로 기동 | ArgoCD Synced/Healthy, Pod 재시작 후 Ready, `psql select version()` 통과 |
| G1 — Single-shard production | 단일 PostgreSQL 운영 DB로 사용 가능 | HA replica, failover drill, backup/restore/PITR drill, upgrade rollback runbook |
| G2 — 운영 품질 | PGO-class 운영 표면 확보 | metrics, alerts, pooler, TLS, user/db/RBAC, security defaults, release smoke |
| G3 — 자체 sharding foundation | 외부 Citus 없이 샤딩 메타데이터 자체 구현 | `ShardRange`, `pg-router`, manual shard placement, GitOps drift guard |
| G4 — Online resharding | 데이터 손실 없는 split/rebalance | `ShardSplitJob` 7-step e2e, cutover rollback/forward-only 검증 |
| G5 — Distributed SQL | cross-shard query/transaction의 명확한 지원 범위 | scatter-gather, 2PC/saga, isolation matrix, benchmark |
| G6 — 1.0.0 GA | 상용 제품 수준 | 장기 soak, chaos, restore rehearsal, upgrade matrix, SBOM/signing, docs/runbooks 완비 |

## Non-Goals

- 외부 PostgreSQL operator를 내장해서 `postgres-operator` 이름으로 재포장하지 않는다.
- Citus를 1급 내장 기능으로 제공하지 않는다. Citus는 설계 비교 대상이며 런타임 의존성이 아니다.
- 범용 Plugin SDK를 제품 메시지의 중심으로 두지 않는다. 필요한 확장점은 안정화된 내부 인터페이스와 CRD로 좁게 공개한다.
- GitHub Actions를 필수 release gate로 쓰지 않는다. 로컬 자동화 게이트와 실클러스터 검증을 기준으로 한다.

## 변경 이력

| Date | Change |
|---|---|
| 2026-05-07 | `0.3.0-alpha.3` 배포, GHCR public pull 전환, legacy staging operator 제거, 외부 시스템 내장 금지 원칙 명시 |
