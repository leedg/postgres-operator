# Deployment Guide — keiailab/postgres-operator (alpha)

> 0.3.0-alpha 시점의 K8s 배포 가이드. RFC 0001 v2 schema 기준.
> production 사용은 alpha 단계 한계 (e2e 미검증, secret rotation 부재) 인지 후 진행.

## 사전 요구

- K8s 1.28+ (CEL XValidation 의존)
- StorageClass — PVC 동적 프로비저닝 가능해야 함
- Container registry 접근 — `ghcr.io/keiailab/*` (또는 사용자 자체 mirror)

## 1. 이미지

### 운영자 (manager) 이미지

```
ghcr.io/keiailab/postgres-operator:<version>
```

빌드:

```fish
make docker-build IMG=ghcr.io/keiailab/postgres-operator:dev
make docker-push IMG=ghcr.io/keiailab/postgres-operator:dev
```

### PG runtime 이미지 (instance manager + postgres)

```
ghcr.io/keiailab/pg:18  (PG_MAJOR=18)
ghcr.io/keiailab/pg:17  (옵션)
```

빌드:

```fish
make docker-build-pg PG_MAJOR=18 PG_IMG=ghcr.io/keiailab/pg:18
make docker-push-pg PG_IMG=ghcr.io/keiailab/pg:18
```

본 이미지는 `Dockerfile.pg` 정의 — 2-stage build (golang:1.25-bookworm 으로
`cmd/instance` 빌드 + postgres:18-bookworm base + pgBackRest + UID/GID 70 user).
ENTRYPOINT 는 `/usr/local/bin/instance` (instance manager 가 PID 1 으로 동작,
postgres child 를 fork). `BackupJob.spec.executionMode=job` runner 는 command 를
override 해서 같은 이미지의 `pgbackrest` 바이너리를 직접 실행할 수 있다.

## 2. CRD + operator 설치

### Helm

```fish
helm install postgres-operator charts/postgres-operator \
    --namespace postgres-operator-system --create-namespace \
    --set image.repository=ghcr.io/keiailab/postgres-operator \
    --set image.tag=dev
```

### kustomize (개발/테스트)

```fish
make build-installer
kubectl apply -f dist/install.yaml
```

## 3. PostgresCluster 인스턴스 배포

### Quickstart (single shard, dev)

```fish
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_dev.yaml
```

기대 결과:

- ConfigMap (postgresql.conf + pg_hba.conf), Headless Service, StatefulSet 1 개
- ServiceAccount + Role + RoleBinding (instance Pod 가 leases + PVC fence patch 권한)
- Pod 부팅 흐름:
  1. initdb init container — fresh PVC 면 PGDATA 초기화
  2. instance manager 가 PID 1 으로 시작
  3. postgres child fork
  4. K8s lease 기반 election 수렴 → primary 선출 → `pg_promote()`
  5. `/readyz` 200 → Pod Ready

```fish
# 상태 확인
kubectl get postgrescluster quickstart -o yaml | yq '.status'
kubectl get sts,svc,pod -l app.kubernetes.io/instance=quickstart
```

### 접속

Pod 안 Unix socket 으로 (peer auth, dev):

```fish
kubectl exec quickstart-shard-0-0 -c postgres -- \
    psql -h /var/run/postgresql -U postgres -c 'SELECT version()'
```

Pod 외부 (cluster 내부 다른 Pod 에서, scram-sha-256):

```
psql "host=quickstart-shard-0-headless.default.svc.cluster.local user=postgres dbname=postgres"
```

(password 는 alpha 단계에서 secret 으로 별도 주입 — 후속).

## 4. Production 토폴로지 (예시)

`config/samples/postgres_v1alpha1_postgrescluster_prod.yaml` — multi-shard +
router, replicas=2 (3-way HA), monitoring 활성, custom storageClass.

production 적용 전 확인:

- StorageClass 가 fast SSD-backed (alpha 권장 1GB+/min IOPS)
- replicas≥1 (HA — RFC 0001 §3 권장)
- RPO=0 요구 시 `spec.postgresql.synchronous` 설정. `number` 는 `shards.replicas`
  이하만 허용된다.
- monitoring.serviceMonitor + Prometheus operator 사전 설치
- backup.enabled — F04 후속 PR 후에만 의미

### 동기 복제 예시

CloudNativePG 와 같은 구조화 표면을 사용한다. 사용자는 PostgreSQL GUC
`synchronous_standby_names` 를 직접 쓰지 않고, operator 가 shard Pod 이름과
`primary_conninfo application_name` 을 맞춰 생성한다.

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata:
  name: quicksync
spec:
  postgresVersion: "18"
  shardingMode: none
  shards:
    initialCount: 1
    replicas: 2
    storage: {size: 10Gi}
  postgresql:
    synchronous:
      method: any
      number: 1
      dataDurability: required
```

- `method=any` 는 PostgreSQL `ANY N (...)` quorum 방식이다.
- `method=first` 는 `FIRST N (...)` priority 방식이다.
- `dataDurability=required` 는 요청한 standby 수가 부족하면 commit 을 대기시킨다.
- `dataDurability=preferred` 는 현재 Ready replica 수에 맞춰 quorum 을 낮추고,
  Ready replica 가 0 이면 동기 복제를 일시 비활성화해 write availability 를 유지한다.
- 설정 변경은 ConfigMap hash 를 StatefulSet Pod template annotation 에 반영해
  shard Pod rolling reconcile 로 적용한다.

### ImageCatalog 기반 runtime image 선택

CloudNativePG 와 같은 `spec.imageCatalogRef` 필드 형태를 지원한다. `ImageCatalog` 는
namespace 범위, `ClusterImageCatalog` 는 cluster 범위 catalog 이며, catalog entry 가
바뀌면 해당 `PostgresCluster` 의 StatefulSet Pod template image 와
`postgres.keiailab.io/postgres-image-catalog-sha256` annotation 이 함께 바뀌어
Kubernetes rollout 으로 이어진다.

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: ImageCatalog
metadata:
  name: postgresql
  namespace: default
spec:
  images:
    - major: 18
      image: ghcr.io/keiailab/pg:18
---
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata:
  name: quickcatalog
  namespace: default
spec:
  imageCatalogRef:
    apiGroup: postgresql.cnpg.io
    kind: ImageCatalog
    name: postgresql
    major: 18
  shardingMode: none
  shards:
    initialCount: 1
    replicas: 1
    storage: {size: 10Gi}
```

호환성/안전 규칙:

- `apiGroup` 은 빈 값, `postgres.keiailab.io`, `postgresql.cnpg.io` 를 허용한다.
- `imageCatalogRef.major` 는 `postgresVersion` 을 대체하는 image/bin-dir 선택의
  단일 진실원이다. 둘을 같이 쓰면 값이 같아야 한다.
- catalog 나 major entry 를 찾지 못하면 fallback image 로 진행하지 않고
  `status.phase=Degraded`, `Ready=False`, `Reason=ImageCatalogRejected` 로 실패한다.

### Standalone replica cluster

CloudNativePG 의 standalone replica cluster 기본 흐름과 같은 `externalClusters` +
`bootstrap.pg_basebackup.source` + `replica.enabled/source` 표면을 지원한다. 이 모드에서는
ordinal 0 Pod 도 `initdb` 를 수행하지 않고 외부 source 에서 `pg_basebackup` 을 실행한 뒤
`standby.signal` 과 `primary_conninfo` 를 기록한다. instance manager 는
`POSTGRES_REPLICA_CLUSTER=standalone` 환경 변수로 영구 follower election 을 사용하므로
local promotion 을 수행하지 않는다.

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata:
  name: quickreplica
  namespace: default
spec:
  postgresVersion: "18"
  externalClusters:
    - name: primary-eu
      connectionParameters:
        host: primary-eu-rw.data.svc
        port: "5432"
        user: streaming_replica
        dbname: postgres
        sslmode: verify-full
      password:
        name: primary-eu-replication-password
        key: password
      sslKey:
        name: primary-eu-replication
        key: tls.key
      sslCert:
        name: primary-eu-replication
        key: tls.crt
      sslRootCert:
        name: primary-eu-ca
        key: ca.crt
  bootstrap:
    pg_basebackup:
      source: primary-eu
  replica:
    enabled: true
    source: primary-eu
  shardingMode: none
  shards:
    initialCount: 1
    replicas: 0
    storage: {size: 10Gi}
```

Fail-closed 규칙:

- `replica.enabled=true` 이면 `replica.source` 와 `bootstrap.pg_basebackup.source` 가
  모두 필요하며 값이 같아야 한다.
- source 이름은 `externalClusters[].name` 에 존재해야 한다.
- `connectionParameters.host` 가 없으면 Pod 를 만들지 않고
  `status.phase=Degraded`, `Ready=False`, `Reason=ReplicaClusterRejected` 로 실패한다.
- `password`, `sslKey`, `sslCert`, `sslRootCert` 를 지정하면 `name` 과 `key` 를 모두
  채워야 한다. 누락 시 동일하게 `ReplicaClusterRejected` 로 실패한다.

현재 범위:

- streaming `pg_basebackup` + continuous recovery path 와 local promotion 차단은
  envtest/unit test 로 검증했다.
- password Secret 은 `PRIMARY_PASSWORD` Secret env 로 주입한 뒤 `/tmp/primary.pgpass` 로
  변환하고, TLS client key/cert/root cert 는 projected Secret 을 init container 에
  mount 한 뒤 `/tmp/primary-client.*` 파일로 복사해 `primary_conninfo` 의
  `passfile`, `sslkey`, `sslcert`, `sslrootcert` 에 연결한다.
- WAL archive/object-store hybrid, distributed topology demotion/promotion token,
  live cross-cluster drill 은 후속이다.

### 선언형 하이버네이션

CloudNativePG 와 같은 annotation 을 지원한다. 하이버네이션은 shard StatefulSet 과
PVC template 소유권은 유지하면서 database Pod 수를 0 으로 낮춘다. PVC 는 삭제하지
않으므로 나중에 같은 클러스터를 재수화할 수 있다.

```fish
kubectl annotate postgrescluster quickstart --overwrite cnpg.io/hibernation=on
kubectl get postgrescluster quickstart -o \
  'jsonpath={.status.conditions[?(@.type=="cnpg.io/hibernation")]}'

# 재수화
kubectl annotate postgrescluster quickstart --overwrite cnpg.io/hibernation=off
# 또는 annotation 제거
kubectl annotate postgrescluster quickstart cnpg.io/hibernation-
```

하이버네이션 중 기대 상태:

- shard StatefulSet `spec.replicas=0`
- `status.phase=Hibernated`
- `status.conditions[type=cnpg.io/hibernation].status=True`
- `Ready=False`, `Progressing=False`
- native router 가 켜져 있으면 router Deployment 도 `replicas=0`

## 5. 스모크 검증 (kind)

```fish
./hack/smoke.sh           # cleanup 후 종료
./hack/smoke.sh --keep    # cluster 유지 (디버깅)
PG_MAJOR=17 POSTGRES_VERSION=17 CR_NAME=quickstart17 ./hack/smoke.sh
PG_MAJOR=18 POSTGRES_VERSION=18 CR_NAME=quickstart18 ./hack/smoke.sh
PG_MAJOR=17 POSTGRES_VERSION=17 CR_NAME=quickstart17ha SHARD_REPLICAS=1 ./hack/smoke.sh
PG_MAJOR=18 POSTGRES_VERSION=18 CR_NAME=quickstart18ha SHARD_REPLICAS=1 ./hack/smoke.sh
SMOKE_POOLER=1 CR_NAME=quickstartpooler ./hack/smoke.sh
SMOKE_HIBERNATION=1 CR_NAME=quickstarthibernate ./hack/smoke.sh
PG_MAJOR=18 POSTGRES_VERSION=18 CR_NAME=quickstart18fo SHARD_REPLICAS=1 SMOKE_FAILOVER=1 ./hack/smoke.sh
```

본 스크립트는:

1. kind cluster `postgres-operator-smoke` 생성
2. operator + PG 이미지 로컬 빌드 + kind load (`SMOKE_POOLER=1` 은 PgBouncer image 도 load)
3. dist/install.yaml server-side apply
4. quickstart sample apply
5. StatefulSet ReadyReplicas≥1 대기 (5분 timeout)
6. `psql -c 'SELECT 1'` round-trip 검증
7. `SMOKE_HIBERNATION=1` 이면 `cnpg.io/hibernation=on/off` annotation, StatefulSet `replicas=0`, PVC 보존, 재수화 후 marker row `SELECT` 를 검증
8. `SMOKE_POOLER=1` 이면 Pooler CR + PgBouncer auth Secret 생성 후 Pooler Service 경유 `psql SELECT 1`, `spec.paused=true` 신규 client 차단, `spec.paused=false` 재접속, `pgbouncer.parameters` 패치 후 configHash 변경 + Pod 교체 없는 `SIGHUP` in-place reload + 재접속을 검증
9. `SHARD_REPLICAS>=1` 이면 `pg_stat_replication` 에서 streaming standby 관측
10. `SMOKE_FAILOVER=1` 이면 primary Pod 삭제 후 standby promote RTO 측정

## 6. Pooler 모니터링

Pooler PgBouncer exporter sidecar 를 활성화하면 Pooler Pod/Service 에 안정적인
selector 라벨이 붙는다. Prometheus Operator 환경에서는 자동 생성 대신
`PodMonitor` 를 직접 관리한다.

```fish
kubectl apply -f config/samples/postgres_v1alpha1_pooler_podmonitor.yaml
```

상세 예시는 `docs/operator-guide/pooler-monitoring.md` 를 참조한다.

`PG_MAJOR` 는 빌드할 runtime image 의 base major 를, `POSTGRES_VERSION` 은
`PostgresCluster.spec.postgresVersion` 을 지정한다. 0.3.0-alpha 기준 smoke
matrix 는 PG17 + PG18 이다. `SHARD_REPLICAS` 는 `spec.shards.replicas` 에
그대로 반영된다.

## 7. alpha 단계 한계 (production 도입 전 인지 사항)

- **secret 미통합** — postgres user password 가 alpha 에서는 trust/peer auth 의존.
  scram-sha-256 host auth 는 ConfigMap 으로만 활성. K8s Secret + dynamic
  password rotation 은 후속 cycle (F04 backup 과 같이).
- **HA 검증 범위 제한** — 2026-05-07 PG18 `SHARD_REPLICAS=1 SMOKE_FAILOVER=1`
  smoke 에서 primary Pod 삭제 → standby promote RTO 21s(<30s), CR status primary 수렴,
  restarted old primary standby 재진입을 확인했다. 다만 chaos-mesh kill/network
  partition, multi-node 장애, pgBackRest 결합까지 검증한 production HA 는 F05 후속이다.
- **standby 재구성 범위 제한** — restarted old primary 의 marker 생성은
  existing PGDATA + current primary endpoint 비교 기준으로 일반화됐고, instance
  manager 는 same `PRIMARY_ENDPOINT` 로 `pg_rewind` 후 standby.signal /
  primary_conninfo 를 구성한다. first-boot standby 와 rejoin standby 의
  `primary_conninfo` 는 Pod 이름을 `application_name` 으로 설정하므로
  synchronous replication 의 standby name 과 일치한다. `pg_rewind` 실패 시
  fresh `pg_basebackup` fallback 을 시도하고, fallback 실패 시 원본 dataDir 을 복구한다. 실패 원인은
  `PostgresCluster.status.shards[].replicas[].reason/message` 로 표면화된다.
  live divergent WAL rewind drill 과 외부 fencing/STONITH 계열 검증은 F03/F05 후속이다.
- **동기 복제 live 검증 미완료** — `postgresql.synchronous` CRD/schema,
  required/preferred config 렌더링, ConfigMap hash rolling reconcile, standby
  `application_name` wiring 은 단위 테스트로 봉인했다. 실제 commit latency/RPO=0
  kind drill 은 F05 후속이다.
- **하이버네이션 live 실측 잔여** — `cnpg.io/hibernation=on/off` annotation,
  StatefulSet scale-to-zero/restore, PVC template 보존, condition/phase 표면은
  envtest 로 검증했고, `SMOKE_HIBERNATION=1` kind drill 경로도 추가했다. 실제 PVC data
  보존 후 재수화 `SELECT` round-trip 실측은 F05 후속이다.
- **단일 shard 만 GA** — shardingMode=native + multi-shard + router 는 P2 진입 후
  의미. 본 alpha 는 shardingMode=none (single shard) 만 보장.

## 8. 트러블슈팅

| 증상 | 원인 / 조치 |
|---|---|
| Pod 가 ImagePullBackOff | `ghcr.io/keiailab/pg:18` 가 cluster registry 에서 미발견. `make docker-build-pg` + `kind load docker-image` 또는 사설 mirror push. |
| PgBouncer image kind load 가 OCI index digest 오류로 실패 | `hack/smoke.sh` 는 `kind load docker-image` 실패 시 단일 플랫폼 `ctr images import` 로 fallback 한다. `ghcr.io/cloudnative-pg/pgbouncer:1.24.1` 의 attestation manifest 때문에 Docker Desktop arm64 에서 재현됨. |
| CRD apply 가 `metadata.annotations: Too long` 으로 실패 | `dist/install.yaml` 은 CRD 크기 때문에 client-side apply 대신 `kubectl apply --server-side -f dist/install.yaml` 을 사용한다. |
| PgBouncer Pod 가 `/tmp/.s.PGSQL.5432` read-only filesystem 으로 CrashLoop | operator 가 `unix_socket_dir = ` 를 렌더링해 Unix socket 을 비활성화한다. ConfigMap 에 해당 행이 없으면 최신 operator image 로 재빌드한다. |
| 단일 멤버 quickstart 가 PVC `postgres.keiailab.io/fenced=true` 후 CrashLoop | 구 alpha image 의 단일 멤버 election stop 처리 버그다. 최신 PG runtime image 는 `POSTGRES_MEMBER_COUNT=1` 에서 leadership stop 시 PVC fence/fast demote 를 건너뛰도록 수정됐다. |
| Pod CrashLoopBackOff (initdb) | PVC ownership 문제. SecurityContext FSGroup=70 이 적용되지 않은 StorageClass — PVC 동적 프로비저닝 시 fsGroup 전파 가능 검사. |
| /readyz 503 "starting election" | 정상 부트스트랩 phase. 30~60s 안 해소되지 않으면 leases RBAC 부재. `kubectl get role,rolebinding -l app.kubernetes.io/instance=<cluster>`. |
| /readyz 503 "postgres not ready" | postgres child 가 LocalDSN 으로 응답 안함. instance Pod 안 `kubectl exec ... -c postgres -- ls /var/run/postgresql` — Unix socket 부재 시 postgresql.conf 의 unix_socket_directories 확인. |
| Reconcile 무한 requeue | controller log 확인. webhook CEL XValidation 거부 가능 — `kubectl get postgrescluster <name> -o yaml` events. |

## 8. 참조

- ADR 0002 — instance manager PID 1 모델
- ADR 0006 — dataplane SecurityContext
- RFC 0001 — PostgresCluster CRD v2 schema
- RFC 0003 — election + fencing 인터페이스
- HANDOFF.md — 진행 중 작업 + 다음 cycle 진입점
