/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package controller

import (
	"fmt"

	commonslabels "github.com/keiailab/operator-commons/pkg/labels"
)

// 본 파일은 reconciler가 생성하는 K8s 자원 이름을 단일 출처로 모은다.
// 명명 규약은 RFC 0001 PostgresCluster CRD v2 의 shard ordinal 모델을 따른다:
//   - 자원 이름은 PostgresCluster.metadata.name 접두사를 사용한다.
//   - 역할은 "shard" 또는 "router".
//   - shard 식별자는 0-based ordinal (e.g. shard-0, shard-1).
//   - K8s lease는 ADR 0002 명명 규약 "<cluster>-<role>-primary"를 따름.

// ShardStatefulSetName은 shard ordinal 의 StatefulSet 이름을 반환한다.
// StatefulSet 의 pod 들은 <name>-0 (primary), <name>-1.. (async replicas) 가 된다.
func ShardStatefulSetName(cluster string, ordinal int32) string {
	return fmt.Sprintf("%s-shard-%d", cluster, ordinal)
}

// ShardServiceName은 shard 의 headless Service 이름을 반환한다.
// pod 의 안정적 DNS = <pod>.<service>.<ns>.svc.cluster.local.
func ShardServiceName(cluster string, ordinal int32) string {
	return fmt.Sprintf("%s-shard-%d-headless", cluster, ordinal)
}

// ShardConfigMapName은 shard 의 postgresql.conf 등을 담는 ConfigMap 이름.
func ShardConfigMapName(cluster string, ordinal int32) string {
	return fmt.Sprintf("%s-shard-%d-config", cluster, ordinal)
}

// RouterDeploymentName은 QueryRouter Deployment 이름을 반환한다.
// PVC 부재(ADR 0003)이므로 Deployment를 사용한다(StatefulSet 아님).
func RouterDeploymentName(cluster string) string {
	return fmt.Sprintf("%s-router", cluster)
}

// RouterServiceName은 클라이언트 진입점이 되는 ClusterIP Service 이름을 반환한다.
// 사용자가 "host=<cluster>-router" 형태로 접속.
func RouterServiceName(cluster string) string {
	return fmt.Sprintf("%s-router", cluster)
}

// RouterConfigMapName은 라우터 PgBouncer 등의 설정을 담는 ConfigMap.
func RouterConfigMapName(cluster string) string {
	return fmt.Sprintf("%s-router-config", cluster)
}

// PoolerDeploymentName 은 Pooler CR 이 소유하는 PgBouncer Deployment 이름이다.
func PoolerDeploymentName(pooler string) string {
	return fmt.Sprintf("%s-pooler", pooler)
}

// PoolerServiceName 은 application 이 접속하는 PgBouncer Service 이름이다.
func PoolerServiceName(pooler string) string {
	return fmt.Sprintf("%s-pooler", pooler)
}

// PoolerConfigMapName 은 pgbouncer.ini 를 담는 ConfigMap 이름이다.
func PoolerConfigMapName(pooler string) string {
	return fmt.Sprintf("%s-pooler-config", pooler)
}

// PoolerPDBName 은 Pooler CR 이 소유하는 PodDisruptionBudget 이름이다.
func PoolerPDBName(pooler string) string {
	return fmt.Sprintf("%s-pooler-pdb", pooler)
}

// InstanceServiceAccountName 은 cluster 단위 instance manager ServiceAccount 이름.
// 모든 shard Pod 가 동일 SA 를 공유 — namespace 안 leases + 자기 PVC patch 권한.
func InstanceServiceAccountName(cluster string) string {
	return fmt.Sprintf("%s-instance", cluster)
}

// InstanceRoleName 은 InstanceServiceAccount 에 부착되는 Role 이름.
func InstanceRoleName(cluster string) string {
	return fmt.Sprintf("%s-instance", cluster)
}

// InstanceRoleBindingName 은 SA↔Role 결합 RoleBinding 이름.
func InstanceRoleBindingName(cluster string) string {
	return fmt.Sprintf("%s-instance", cluster)
}

// SelectorLabels는 부모 PostgresCluster + 역할 + shard ordinal 식별 레이블이다.
// reconciler가 Service의 selector와 Pod template label에 동일하게 적용한다.
//
// router 처럼 shard 차원이 없는 자원은 ordinal=-1 을 전달한다 — 이때 pool 레이블
// 자체가 부재한다 (router 의 backend 는 모든 shard 이므로 ordinal 분리 무의미).
//
// iteration 28 (2026-05-07): operator-commons/pkg/labels 위임 — 4-key
// app.kubernetes.io/* convention 통일. postgres-specific shard label 은 별도 추가.
func SelectorLabels(cluster, role string, shardOrdinal int32) map[string]string {
	out := commonslabels.Set{
		Name:      "postgrescluster",
		Instance:  cluster,
		Component: role,
		ManagedBy: "keiailab-postgres-operator",
		// Version + PartOf 미지정 → 4-key (기존 동작 보존)
	}.All()
	if shardOrdinal >= 0 {
		out["postgres.keiailab.io/shard"] = fmt.Sprintf("%d", shardOrdinal)
	}
	return out
}
