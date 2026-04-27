/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package controller는 keiailab/postgres-operator의 reconciler들을 보유한다.
//
// 본 파일은 Pillar P1-T2의 골격이다. 현재는 PostgresCluster CR 변화에 반응하는
// Reconcile 메서드만 stub 형태로 두며, StatefulSet/Service/ConfigMap 생성과
// 상태 전이 로직은 P1-M1 마일스톤에서 채워진다.
package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	"github.com/keiailab/postgres-operator/internal/plugin"
)

// PostgresClusterReconciler는 PostgresCluster CR을 reconcile한다.
//
// Plugins 필드는 의도적이다 — 본 reconciler는 Backup/Exporter/Extension/Router/
// Auth 구체 구현을 직접 import 하지 않고 Plugin SDK Registry를 통해서만 호출한다
// (ADR 0005 §강제 메커니즘).
type PostgresClusterReconciler struct {
	client.Client
	Scheme  *runtime.Scheme
	Plugins *plugin.Registry
}

// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=postgres.keiailab.io,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile은 PostgresCluster CR 변화에 반응한다.
// 현재는 P1-T2 골격으로 CR 존재 확인과 Status.ObservedGeneration 갱신 의도만 표시.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("postgrescluster", req.NamespacedName)

	var cluster postgresv1alpha1.PostgresCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to fetch PostgresCluster")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Info("Observed PostgresCluster",
		"generation", cluster.Generation,
		"observedGeneration", cluster.Status.ObservedGeneration,
		"workers", len(cluster.Spec.Workers),
	)

	return ctrl.Result{}, nil
}

// SetupWithManager는 본 reconciler를 controller-runtime Manager에 등록한다.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&postgresv1alpha1.PostgresCluster{}).
		Named("postgrescluster").
		Complete(r)
}
