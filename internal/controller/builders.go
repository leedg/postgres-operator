/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	"github.com/keiailab/postgres-operator/internal/plugin"
)

// 본 파일은 PostgresCluster CR로부터 K8s 하위 자원(StatefulSet, Service,
// ConfigMap, Deployment)의 desired state를 생성하는 순수 함수들의 모음이다.
//
// 설계 원칙:
//   - 모든 함수는 입력에서 출력으로 결정적(stateless, side-effect 0).
//   - controllerutil.SetControllerReference 호출은 reconciler가 담당. 본 함수는
//     ObjectMeta까지만 채운다.
//   - 컨테이너 이미지 lookup은 internal/version/matrix.go의 결과만 사용한다.
//     본 파일에 imageRef:tag 하드코딩 금지.
//   - PostgreSQL 컨테이너 환경 변수, 볼륨 마운트, postgresql.conf의 세부
//     defaulting은 P1-M1 후속 작업에서 보강한다. 현재는 PG가 부팅 가능한 최소
//     스펙만 보장한다.

const (
	// pgContainerName은 PG 컨테이너의 식별자다. status 보고에서 동일 값을 참조.
	pgContainerName = "postgres"

	// pgPort는 PostgreSQL의 표준 포트다.
	pgPort int32 = 5432

	// instanceProbePort 는 instance manager 의 healthz/readyz HTTP 포트.
	instanceProbePort int32 = 8080

	// pgDataMountPath는 PVC가 마운트되는 위치다.
	pgDataMountPath = "/var/lib/postgresql/data"

	// pgDataSubdir 는 PVC root 안 PGDATA subdir. lost+found 충돌 회피.
	pgDataSubdir = pgDataMountPath + "/pgdata"

	// pgConfigMountPath는 ConfigMap이 마운트되는 위치다.
	pgConfigMountPath = "/etc/postgres-operator/conf"

	// pgConfigFile / pgHbaFile 은 ConfigMap mount 안 파일 경로 (instance 의 BinDir/CmdLine 인자).
	pgConfigFile = pgConfigMountPath + "/postgresql.conf"
	pgHbaFile    = pgConfigMountPath + "/pg_hba.conf"

	// pgRunDir 는 Unix socket directory (peer auth). dataplaneEphemeralVolumeMounts 에서
	// emptyDir 로 마운트되며 instance 가 LocalDSN 에서 사용한다.
	pgRunDir = "/var/run/postgresql"

	// postgresUserUID는 PostgreSQL 표준 postgres user의 UID/GID다.
	// ADR 0006에 의해 동결된 데이터플레인 Pod의 runAsUser/runAsGroup/fsGroup 기본값.
	postgresUserUID int64 = 70
)

// pgBinDir 는 base PG image 안 postgres binary 디렉터리. Dockerfile.pg 의
// postgres:${PG_MAJOR}-bookworm 표준 경로 (/usr/lib/postgresql/${PG_MAJOR}/bin).
func pgBinDir(pgMajor string) string {
	return "/usr/lib/postgresql/" + pgMajor + "/bin"
}

// ptrBool/ptrInt64는 외부 의존 없이 inline pointer를 만드는 헬퍼다.
// (K8s API의 *bool/*int64 필드용. k8s.io/utils/ptr import 회피로 SDK 의존 최소화.)
func ptrBool(b bool) *bool    { return &b }
func ptrInt64(i int64) *int64 { return &i }

// storageClassPtr 는 빈 문자열이면 nil (클러스터 default), 아니면 ptr 을 반환한다.
// PVC.StorageClassName 의미: nil = default class, "" = no class, "<name>" = explicit.
// 우리는 빈 문자열을 "default 사용" 으로 해석한다.
func storageClassPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// dataplanePodSecurityContext는 데이터플레인 Pod(PG StatefulSet, Router Deployment)
// 의 PodSecurityContext 기본값을 반환한다. ADR 0006 §결정에 의해 동결.
//
// 구성:
//   - runAsNonRoot=true (root 거부)
//   - runAsUser/Group/FSGroup=70 (PG postgres user)
//   - seccompProfile=RuntimeDefault (커널 syscall 화이트리스트)
//
// 사용자 override는 향후 PostgresCluster.Spec.SecurityContext 필드 + webhook에서
// 처리한다(ADR 0006 §트레이드오프). 현 시점은 *opt-out 강제* — 운영자가 잊으면
// root 가능 상태로 떨어지지 않도록 default를 항상 강제한다.
func dataplanePodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptrBool(true),
		RunAsUser:    ptrInt64(postgresUserUID),
		RunAsGroup:   ptrInt64(postgresUserUID),
		FSGroup:      ptrInt64(postgresUserUID),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// dataplaneContainerSecurityContext는 데이터플레인 Container의 SecurityContext
// 기본값을 반환한다. ADR 0006 §결정.
//
// 구성:
//   - allowPrivilegeEscalation=false (suid/setuid 비활성)
//   - readOnlyRootFilesystem=true (컨테이너 내 임의 바이너리 작성 차단 — 공급망 공격 완화)
//   - capabilities.drop=[ALL] (모든 Linux capability 제거)
//
// readOnlyRootFilesystem 동반: PG가 /tmp, /run, /var/run/postgresql에 socket/lock
// 작성하므로 emptyDir mount 3개 추가(dataplaneEphemeralVolumeMounts/Volumes).
func dataplaneContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptrBool(false),
		ReadOnlyRootFilesystem:   ptrBool(true),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
}

// dataplaneEphemeralVolumeMounts는 readOnlyRootFilesystem=true 동반에 필요한
// 쓰기 가능 mount point들을 반환한다(/tmp, /run, /var/run/postgresql).
func dataplaneEphemeralVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "ephemeral-tmp", MountPath: "/tmp"},
		{Name: "ephemeral-run", MountPath: "/run"},
		{Name: "ephemeral-pg-run", MountPath: "/var/run/postgresql"},
	}
}

// dataplaneEphemeralVolumes는 dataplaneEphemeralVolumeMounts와 짝이 되는
// emptyDir Volume 정의를 반환한다.
func dataplaneEphemeralVolumes() []corev1.Volume {
	return []corev1.Volume{
		{Name: "ephemeral-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "ephemeral-run", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "ephemeral-pg-run", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
}

// renderSharedPreloadLibraries는 enabledNames 에 매칭되는 ExtensionPlugin 만
// 우선순위 순으로 직렬화하여 shared_preload_libraries 값을 만든다 (RFC 0006 R1).
//
// 우선순위는 Registry.EnabledExtensions 가 SharedPreloadOrder 오름차순으로 정렬해
// 반환하는 것으로 보장된다 (ADR 0005). 본 함수는 그 결과를 콤마로 join.
//
// enabledNames 가 비어있거나 reg 가 nil 이면 빈 문자열 반환 — ConfigMap 에서
// shared_preload_libraries 라인 생략 (vanilla PG 부팅 보장).
func renderSharedPreloadLibraries(reg *plugin.Registry, enabledNames []string) string {
	if reg == nil || len(enabledNames) == 0 {
		return ""
	}
	exts, _ := reg.EnabledExtensions(enabledNames)
	names := make([]string, 0, len(exts))
	for _, e := range exts {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

// renderPostgresConf는 postgresql.conf의 본문을 생성한다 (RFC 0006 R1 — per-cluster
// extension list).
func renderPostgresConf(reg *plugin.Registry, enabledExtensions []string) string {
	var sb strings.Builder
	sb.WriteString("# Generated by keiailab-postgres-operator. Do not edit by hand.\n")
	sb.WriteString("listen_addresses = '*'\n")
	sb.WriteString("port = 5432\n")
	// Unix socket 위치 — instance manager 의 LocalDSN 이 본 경로에 의존.
	fmt.Fprintf(&sb, "unix_socket_directories = '%s'\n", pgRunDir)
	// WAL + replication 기본값 — replicas>0 일 때 streaming replication 전제.
	sb.WriteString("wal_level = replica\n")
	sb.WriteString("max_wal_senders = 10\n")
	sb.WriteString("max_replication_slots = 10\n")
	sb.WriteString("hot_standby = on\n")
	if spl := renderSharedPreloadLibraries(reg, enabledExtensions); spl != "" {
		fmt.Fprintf(&sb, "shared_preload_libraries = '%s'\n", spl)
	}
	return sb.String()
}

// renderPGHBAConf 는 pg_hba.conf 본문을 생성한다.
//
// 인증 정책 (alpha 단계 — production 은 추후 ADR + secret 기반 강화):
//   - local Unix socket: trust (instance manager 가 peer auth 로 LocalDSN 사용)
//   - host (cluster 내부 10.0.0.0/8 + 172.16.0.0/12 + 192.168.0.0/16): scram-sha-256
//   - replication: cluster 내부 trust (alpha — secret rotation 후속)
func renderPGHBAConf() string {
	return `# Generated by keiailab-postgres-operator. Do not edit by hand.
# TYPE  DATABASE        USER            ADDRESS                 METHOD
local   all             all                                     trust
host    all             all             10.0.0.0/8              scram-sha-256
host    all             all             172.16.0.0/12           scram-sha-256
host    all             all             192.168.0.0/16          scram-sha-256
host    replication     all             10.0.0.0/8              trust
host    replication     all             172.16.0.0/12           trust
host    replication     all             192.168.0.0/16          trust
`
}

// buildConfigMap은 shard/router 모두에서 동일 패턴으로 사용된다.
// 호출자가 name·role·shardOrdinal 을 정해 넘긴다 (router 의 경우 ordinal=-1).
//
// shard ConfigMap 에는 postgresql.conf + pg_hba.conf 둘 다 들어간다.
// router ConfigMap 은 router 가 PG runtime 이 아니므로 pg_hba 는 생략 가능하나,
// 동일 builder 사용 위해 포함 (router 가 무시).
func buildConfigMap(cluster *postgresv1alpha1.PostgresCluster, name, role string, shardOrdinal int32, reg *plugin.Registry) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    SelectorLabels(cluster.Name, role, shardOrdinal),
		},
		Data: map[string]string{
			"postgresql.conf": renderPostgresConf(reg, cluster.Spec.Extensions),
			"pg_hba.conf":     renderPGHBAConf(),
		},
	}
}

// buildHeadlessService는 StatefulSet과 짝이 되는 ClusterIP=None Service를 만든다.
// 안정적 Pod DNS 제공이 목적이다 — shard 전용 (router 는 buildClientService 사용).
func buildHeadlessService(cluster *postgresv1alpha1.PostgresCluster, name, role string, shardOrdinal int32) *corev1.Service {
	labels := SelectorLabels(cluster.Name, role, shardOrdinal)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports: []corev1.ServicePort{{
				Name:       "postgres",
				Port:       pgPort,
				TargetPort: intstr.FromInt32(pgPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// buildClientService는 라우터의 진입점 Service(ClusterIP)다.
// router 는 shard 차원이 없으므로 SelectorLabels 에 ordinal=-1 을 전달한다.
func buildClientService(cluster *postgresv1alpha1.PostgresCluster, name, role string) *corev1.Service {
	labels := SelectorLabels(cluster.Name, role, -1)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Name:       "postgres",
				Port:       pgPort,
				TargetPort: intstr.FromInt32(pgPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// buildInstanceServiceAccount 는 instance Pod 가 사용할 ServiceAccount 를 만든다.
// cluster 단위 단일 SA — 모든 shard Pod 가 공유 (namespace-scoped).
func buildInstanceServiceAccount(cluster *postgresv1alpha1.PostgresCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InstanceServiceAccountName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    SelectorLabels(cluster.Name, "shard", -1),
		},
	}
}

// buildInstanceRole 는 instance manager 가 K8s API 호출에 필요한 최소 권한 Role.
//
// 권한 스펙 (RFC 0003 election + fencing 정확히 충족):
//   - coordination.k8s.io/leases: leaderelection (get/list/watch/create/update/patch/delete)
//   - core/persistentvolumeclaims: 자기 PVC 의 fence label patch (get/patch)
//   - core/events: instance 가 이벤트 송출 가능하도록 (create/patch — 선택적이나 운영 가시성)
func buildInstanceRole(cluster *postgresv1alpha1.PostgresCluster) *rbacv1.Role {
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InstanceRoleName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    SelectorLabels(cluster.Name, "shard", -1),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"persistentvolumeclaims"},
				Verbs:     []string{"get", "list", "watch", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
			// RFC 0006 R2 — instance manager 가 자기 Pod annotation 에
			// statusapi.Status 를 patch (status feedback channel).
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "patch"},
			},
		},
	}
}

// buildInstanceRoleBinding 는 ServiceAccount ↔ Role 결합 RoleBinding.
func buildInstanceRoleBinding(cluster *postgresv1alpha1.PostgresCluster) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InstanceRoleBindingName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    SelectorLabels(cluster.Name, "shard", -1),
		},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      InstanceServiceAccountName(cluster.Name),
			Namespace: cluster.Namespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     InstanceRoleName(cluster.Name),
		},
	}
}

// buildInitdbContainer 는 PGDATA 가 비어 있으면 initdb 를 수행하는 init container.
//
// 재실행 안전 — PG_VERSION 파일 존재 시 즉시 종료.
// readOnlyRootFilesystem=true 동반 시 initdb 가 /tmp 에만 임시 쓰기 — emptyDir 충분.
// PVC FSGroup=70 으로 mount 되므로 UID 70 으로 쓰기 가능.
func buildInitdbContainer(image, pgMajor string) corev1.Container {
	script := `set -eu
DATA="` + pgDataSubdir + `"
if [ -f "$DATA/PG_VERSION" ]; then
  echo "PGDATA already initialized at $DATA — skipping initdb"
  exit 0
fi
mkdir -p "$DATA"
chmod 0700 "$DATA"
` + pgBinDir(pgMajor) + `/initdb -D "$DATA" --auth-local=trust --auth-host=scram-sha-256 --username=postgres --encoding=UTF8 --locale=C
echo "initdb completed at $DATA"
`
	return corev1.Container{
		Name:            "initdb",
		Image:           image,
		Command:         []string{"sh", "-c"},
		Args:            []string{script},
		SecurityContext: dataplaneContainerSecurityContext(),
		VolumeMounts: append([]corev1.VolumeMount{
			{Name: "data", MountPath: pgDataMountPath},
		}, dataplaneEphemeralVolumeMounts()...),
	}
}

// buildInstanceEnv 는 instance manager (PID 1) 에 주입할 환경 변수 집합을 만든다.
// downward API + spec 매개변수 + 고정 경로의 합산.
func buildInstanceEnv(clusterName string, shardOrdinal int32, pgMajor string) []corev1.EnvVar {
	return []corev1.EnvVar{
		// downward API — Pod / Namespace 식별자.
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		// spec 매개변수 — election lease 명명 + role 분기.
		{Name: "POSTGRES_CLUSTER", Value: clusterName},
		{Name: "POSTGRES_ROLE", Value: "shard"},
		{Name: "POSTGRES_SHARD_ORDINAL", Value: fmt.Sprintf("%d", shardOrdinal)},
		// supervise.Config — image 안 표준 경로 + ConfigMap mount + Unix socket.
		{Name: "POSTGRES_BIN_DIR", Value: pgBinDir(pgMajor)},
		{Name: "POSTGRES_DATA_DIR", Value: pgDataSubdir},
		{Name: "POSTGRES_CONFIG_FILE", Value: pgConfigFile},
		{Name: "POSTGRES_HBA_FILE", Value: pgHbaFile},
		{Name: "POSTGRES_LOCAL_DSN", Value: "host=" + pgRunDir + " user=postgres dbname=postgres"},
	}
}

// buildPGStatefulSet은 단일 shard 의 StatefulSet desired state 를 만든다.
// RFC 0001 PostgresCluster CRD v2 모델에서 role 은 항상 "shard" 이며, shardOrdinal
// 은 0-based 값이다. members 는 primary 1 + async replica N 의 합산이다.
//
// 컨테이너 ENTRYPOINT 는 /usr/local/bin/instance (Dockerfile.pg). instance 가 PID 1
// 으로 동작하면서 buildInstanceEnv 의 env 를 읽어 postgres child 를 fork.
func buildPGStatefulSet(
	cluster *postgresv1alpha1.PostgresCluster,
	name, serviceName string,
	shardOrdinal int32,
	image, configMapName, pgMajor string,
	members int32,
	storage postgresv1alpha1.StorageSpec,
	resources corev1.ResourceRequirements,
) *appsv1.StatefulSet {
	labels := SelectorLabels(cluster.Name, "shard", shardOrdinal)

	pvcAccessModes := storage.AccessModes
	if len(pvcAccessModes) == 0 {
		pvcAccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}

	pvcSpec := corev1.PersistentVolumeClaimSpec{
		AccessModes: pvcAccessModes,
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: storage.Size,
			},
		},
		StorageClassName: storageClassPtr(storage.StorageClass),
	}

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: serviceName,
			Replicas:    &members,
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext:    dataplanePodSecurityContext(),
					ServiceAccountName: InstanceServiceAccountName(cluster.Name),
					InitContainers:     []corev1.Container{buildInitdbContainer(image, pgMajor)},
					Containers: []corev1.Container{{
						Name:            pgContainerName,
						Image:           image,
						Resources:       resources,
						SecurityContext: dataplaneContainerSecurityContext(),
						Env:             buildInstanceEnv(cluster.Name, shardOrdinal, pgMajor),
						Ports: []corev1.ContainerPort{
							{Name: "postgres", ContainerPort: pgPort, Protocol: corev1.ProtocolTCP},
							{Name: "probe", ContainerPort: instanceProbePort, Protocol: corev1.ProtocolTCP},
						},
						// readiness: instance manager 의 /readyz 가 election Status 반영.
						// initialDelaySeconds 30 — initdb + postgres 부팅 + election 부트스트랩 여유.
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/readyz",
									Port: intstr.FromInt32(instanceProbePort),
								},
							},
							InitialDelaySeconds: 30,
							PeriodSeconds:       10,
							TimeoutSeconds:      3,
							FailureThreshold:    3,
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/healthz",
									Port: intstr.FromInt32(instanceProbePort),
								},
							},
							InitialDelaySeconds: 60,
							PeriodSeconds:       30,
							TimeoutSeconds:      5,
							FailureThreshold:    3,
						},
						VolumeMounts: append([]corev1.VolumeMount{
							{Name: "data", MountPath: pgDataMountPath},
							{Name: "config", MountPath: pgConfigMountPath, ReadOnly: true},
						}, dataplaneEphemeralVolumeMounts()...),
					}},
					Volumes: append([]corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
							},
						},
					}}, dataplaneEphemeralVolumes()...),
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "data",
					Labels: labels,
				},
				Spec: pvcSpec,
			}},
		},
	}
}

// buildRouterDeployment는 stateless QueryRouter의 Deployment를 만든다.
// ADR 0003 §강제 메커니즘에 의해 PVC를 절대 마운트하지 않는다(StatefulSet 사용
// 금지). 본 함수는 P12-T2 시점에 cmd/router 바이너리 이미지로 교체된다. 현재는
// PG 베이스 이미지를 그대로 사용하는 placeholder.
func buildRouterDeployment(
	cluster *postgresv1alpha1.PostgresCluster,
	name, configMapName, image string,
	replicas int32,
	resources corev1.ResourceRequirements,
) *appsv1.Deployment {
	labels := SelectorLabels(cluster.Name, "router", -1)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext: dataplanePodSecurityContext(),
					Containers: []corev1.Container{{
						Name:            "router",
						Image:           image,
						Resources:       resources,
						SecurityContext: dataplaneContainerSecurityContext(),
						Ports: []corev1.ContainerPort{{
							Name:          "postgres",
							ContainerPort: pgPort,
							Protocol:      corev1.ProtocolTCP,
						}},
						VolumeMounts: append([]corev1.VolumeMount{
							{Name: "config", MountPath: pgConfigMountPath, ReadOnly: true},
						}, dataplaneEphemeralVolumeMounts()...),
					}},
					Volumes: append([]corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
							},
						},
					}}, dataplaneEphemeralVolumes()...),
				},
			},
		},
	}
}
