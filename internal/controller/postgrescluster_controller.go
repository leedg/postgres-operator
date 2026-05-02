/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package controller 는 keiailab/postgres-operator 의 reconciler 들을 보유한다.
//
// 본 파일은 RFC 0001 (PostgresCluster CRD v2) schema 도입 직후의 *최소 reconcile*
// 본체다 (F01a). PostgresCluster CR 을 fetch 하여 PostgresVersion matrix lookup 만
// 수행하고 status (ObservedGeneration, Phase=Provisioning, Ready=False reason=
// DeferredToF01b) 만 갱신한다. 실제 desired state (StatefulSet, Service, ConfigMap,
// Deployment) 생성은 F01b 에서 새 ShardsSpec / RouterSpec 기반 builder 와 함께
// 도입된다.
//
// 본 단계에서는 다음 helper 들이 이미 정의되어 있으나 호출되지 않는다:
//   - buildConfigMap / buildHeadlessService / buildClientService
//   - buildPGStatefulSet / buildRouterDeployment
//
// 위 helper 들은 builders_test.go 가 직접 호출하여 unit test 보존을 통해
// `unused` linter 경고를 회피한다. F01b 에서 본 reconcile 본체가 위 helper
// 들을 새 spec 의 shard 토폴로지 기준으로 호출하도록 재작성된다.
package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	"github.com/keiailab/postgres-operator/internal/plugin"
)

// PostgresClusterReconciler 는 PostgresCluster CR 을 reconcile 한다.
type PostgresClusterReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Plugins *plugin.Registry

	// FeatureGates 는 PG18 같은 격리 채널 활성화 결정에 사용된다.
	// nil 이면 빈 맵으로 취급 (기본 비활성).
	FeatureGates map[string]bool
}

// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets;deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile 은 PostgresCluster CR 변화에 반응한다.
//
// F01a 의 본체는 *최소 동작* 이다 — 새 spec schema 가 reconciler / builder /
// envtest 모두에 단계적으로 흘러갈 수 있도록 type-only 변환의 컴파일 통과를
// 보장한다. 실제 desired state 생성은 F01b 에서 도입된다.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("postgrescluster", req.NamespacedName)

	var cluster postgresv1alpha1.PostgresCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to fetch PostgresCluster")
		return ctrl.Result{}, err
	}

	pgVersion := cluster.Spec.PostgresVersion
	if pgVersion == "" {
		pgVersion = "18"
	}

	if _, ok := lookupCombo(pgVersion, r.FeatureGates); !ok {
		setCondition(&cluster.Status.Conditions, ConditionReady, metav1.ConditionFalse, ReasonVersionRejected,
			fmt.Sprintf("PG=%q is not in supported matrix (or feature gate missing)", pgVersion))
		cluster.Status.Phase = postgresv1alpha1.ClusterPhaseDegraded
		cluster.Status.ObservedGeneration = cluster.Generation
		if err := r.Status().Update(ctx, &cluster); err != nil {
			logger.Error(err, "Failed to update status with version rejection")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// F01a: 실제 reconcile 행위 미정의 — F01b 에서 ShardsSpec 기반 builder 호출.
	cluster.Status.Phase = postgresv1alpha1.ClusterPhaseProvisioning
	setCondition(&cluster.Status.Conditions, ConditionReady, metav1.ConditionFalse, ReasonNotApplicable,
		"reconcile body deferred to F01b (RFC 0001 spec → desired state generation)")
	cluster.Status.ObservedGeneration = cluster.Generation

	if err := r.Status().Update(ctx, &cluster); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Error(err, "Failed to update PostgresCluster status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager 는 본 reconciler 를 controller-runtime Manager 에 등록한다.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1alpha1.PostgresCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("postgrescluster").
		Complete(r)
}
