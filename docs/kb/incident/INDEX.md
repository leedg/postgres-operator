# Incident KB — postgres-operator

> standards/incident-kb.md §1 INDEX. 본 repo *자체* incident.

## Accepted

| ID | Title | Severity | Detected | Status |
|---|---|---|---|---|
| [INC-0001](INC-0001-primary-pvc-loss-no-failover-20260515.md) | Primary PVC + Pod 동시 삭제 시 operator 가 promote 미수행, STS 가 빈 PGDATA primary 재생성 | SEV-2 (prod: SEV-1) | 2026-05-15 | Open |

## 작성 정책

- 형식: standards/incident-kb.md §3 Postmortem-lite
- 명명: INC-NNNN-<영어 kebab-case>-YYYYMMDD.md
- ID 재사용 금지.

## cross-repo INC navigation

| INDEX | 위치 | scope |
|---|---|---|
| 본 INDEX | `docs/kb/incident/` | postgres-operator OSS *자체* |
| wrapper INDEX | `keiailab/.../docs/kb/incident/` | argos cluster production |
