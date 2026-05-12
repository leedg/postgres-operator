# Postgres Operator Helm Chart

`postgres-operator` chart는 `keiailab/postgres-operator`의 operator manager, RBAC, CRD, NetworkPolicy를 배포한다.

## 전제 조건

- Kubernetes 1.26+
- Helm 3.8+
- `kubectl`이 대상 클러스터를 바라보는 상태

## 설치

```bash
helm install postgres-operator ./charts/postgres-operator \
  --namespace postgres-operator-system \
  --create-namespace
```

CRD는 `crds/` 디렉터리에 포함되어 Helm 설치 시 기본 적용된다. CRD lifecycle을 별도로 관리하는 환경에서는 Helm 표준 옵션을 사용한다.

```bash
helm install postgres-operator ./charts/postgres-operator \
  --namespace postgres-operator-system \
  --create-namespace \
  --skip-crds
```

## 주요 값

| 값 | 설명 | 기본값 |
|---|---|---|
| `image.repository` | operator image repository | `ghcr.io/keiailab/postgres-operator` |
| `image.tag` | operator image tag. 비우면 chart `appVersion` 사용 | `""` |
| `replicas` | manager replica 수 | `1` |
| `rbac.create` | ClusterRole/ClusterRoleBinding 생성 여부 | `true` |
| `networkPolicies.enabled` | 데이터플레인 NetworkPolicy 생성 여부 | `true` |
| `metrics.enabled` | manager metrics port 노출 여부 | `true` |
| `metrics.service.enabled` | metrics ClusterIP Service 생성 여부 | `true` |
| `metrics.serviceMonitor.enabled` | Prometheus Operator ServiceMonitor 생성 여부 | `false` |
| `metrics.prometheusRule.enabled` | PrometheusRule 알림 규칙 생성 여부 | `false` |
| `metrics.grafanaDashboards.enabled` | Grafana dashboard ConfigMap 생성 여부 | `false` |
| `metrics.grafanaDashboards.labelName` | dashboard sidecar discovery label key | `grafana_dashboard` |
| `metrics.grafanaDashboards.labelValue` | dashboard sidecar discovery label value | `"1"` |
| `metrics.prometheusRule.thresholds.poolerClientWaiting` | PgBouncer 대기 client alert 임계값 | `0` |
| `metrics.prometheusRule.thresholds.poolerClientMaxWaitSeconds` | PgBouncer 최대 client 대기 시간 alert 임계값 | `30` |

## 사용자 시점 검증

[기능명] Helm chart 설치

사용자 시나리오:
1. 사용자는 chart를 대상 namespace에 설치한다.
2. 사용자는 CRD가 등록됐는지 확인한다.
3. 사용자는 operator Deployment와 RBAC가 생성됐는지 확인한다.
4. 사용자는 dev 샘플 `PostgresCluster`를 적용한다.

기대 결과:
- `postgresclusters.postgres.keiailab.io`, `imagecatalogs.postgres.keiailab.io`, `clusterimagecatalogs.postgres.keiailab.io`, `backupjobs.postgres.keiailab.io`, `scheduledbackups.postgres.keiailab.io`, `poolers.postgres.keiailab.io`, `postgresdatabases.postgres.keiailab.io`, `postgresusers.postgres.keiailab.io` CRD가 표시된다.
- manager Deployment가 생성된다.
- RBAC와 NetworkPolicy가 렌더링된다.
- metrics Service가 기본 렌더링되고, ServiceMonitor/PrometheusRule은 값으로 활성화할 수 있다.
- PrometheusRule은 BackupJob 실패, Pooler 실패, replica WAL lag 증가, PgBouncer exporter collection 실패, pool client 대기/최대 대기시간 증가를 감지한다.
- Grafana dashboard ConfigMap은 cluster overview와 Pooler dashboard JSON을 포함하며, kube-prometheus-stack sidecar label로 자동 import 할 수 있다.
- 샘플 CR 적용 시 schema 에러가 발생하지 않는다.
- `ImageCatalog`/`ClusterImageCatalog`는 `PostgresCluster.spec.imageCatalogRef`에서 runtime image를 선택하는 데 사용한다. catalog entry가 바뀌면 StatefulSet init/main container image와 image catalog hash annotation이 함께 바뀌어 rollout drift가 표면화된다.
- `BackupJob.spec.executionMode=job` 사용 시에는 runner 이미지/명령을 담은
  `spec.jobTemplate`을 함께 지정한다. operator는 해당 `batch/v1.Job`의
  ownerReference, label, 표준 env만 주입한다.
- `Pooler`는 PgBouncer 이미지와 `userlist.txt`를 담은 auth Secret을 명시해야 하며,
  operator는 ConfigMap/Deployment/Service를 생성한다. `spec.pgbouncer.exporter`
  를 지정하면 exporter sidecar와 `metrics` Service port도 함께 생성한다.
- `PostgresDatabase`는 ready primary Pod의 `postgres` 컨테이너에서 `psql`을 실행해
  database, tablespace, schema, extension, FDW, foreign server 선언을 적용하고 status `applied`를 기록한다.
  database/schema privilege grant/revoke도 선언적으로 적용한다.
  `databaseReclaimPolicy=delete`인 CR은 삭제 시 실제 database를 drop한 뒤 finalizer를 제거한다.
- `PostgresUser`는 ready primary Pod의 `postgres` 컨테이너에서 `psql`을 실행해
  role flags, `inRoles`, `connectionLimit`, `validUntil`, password Secret 또는 `disablePassword`를 적용하고 status `applied`를 기록한다.
  참조 Secret 변경 시 해당 `PostgresUser`를 다시 reconcile 하며, 성공 반영한 Secret revision을 `status.passwordSecretResourceVersion`에 기록한다.
- `PostgresCluster.status.managedRolesStatus`는 해당 클러스터를 참조하는 `PostgresUser`들을 `byStatus`, `cannotReconcile`, `passwordStatus`로 집계한다.

```bash
helm lint --strict ./charts/postgres-operator
helm template --include-crds gate ./charts/postgres-operator
helm template monitor ./charts/postgres-operator \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.prometheusRule.enabled=true \
  --set metrics.grafanaDashboards.enabled=true
kubectl get crd postgresclusters.postgres.keiailab.io backupjobs.postgres.keiailab.io scheduledbackups.postgres.keiailab.io poolers.postgres.keiailab.io postgresdatabases.postgres.keiailab.io postgresusers.postgres.keiailab.io
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_dev.yaml
kubectl apply -f config/samples/postgres_v1alpha1_postgresuser.yaml
kubectl apply -f config/samples/postgres_v1alpha1_postgresdatabase.yaml
```

## 제거

```bash
helm uninstall postgres-operator -n postgres-operator-system
```

Helm은 CRD를 uninstall 시 자동 삭제하지 않는다. CRD까지 제거하려면 명시적으로 삭제한다.

```bash
kubectl delete crd postgresclusters.postgres.keiailab.io
kubectl delete crd backupjobs.postgres.keiailab.io
kubectl delete crd scheduledbackups.postgres.keiailab.io
kubectl delete crd poolers.postgres.keiailab.io
kubectl delete crd postgresdatabases.postgres.keiailab.io
kubectl delete crd postgresusers.postgres.keiailab.io
```
