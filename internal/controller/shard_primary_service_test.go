/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
)

func TestPrimaryEndpointHost(t *testing.T) {
	cases := map[string]string{
		"demo-shard-0-0.demo-shard-0-headless.default.svc.cluster.local:5432": "demo-shard-0-0.demo-shard-0-headless.default.svc.cluster.local",
		"host:5432": "host",
		"host":      "host",
		"":          "",
	}
	for in, want := range cases {
		if got := primaryEndpointHost(in); got != want {
			t.Errorf("primaryEndpointHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildShardPrimaryService(t *testing.T) {
	cluster := &postgresv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "default"},
	}
	svc := buildShardPrimaryService(cluster, ShardPrimaryServiceName("demo", "shard-0"), "demo-shard-0-0.hdl.default.svc.cluster.local")
	if svc.Name != "demo-shard-0-primary" {
		t.Fatalf("name = %q, want demo-shard-0-primary", svc.Name)
	}
	if svc.Spec.Type != corev1.ServiceTypeExternalName {
		t.Fatalf("type = %q, want ExternalName", svc.Spec.Type)
	}
	if svc.Spec.ExternalName != "demo-shard-0-0.hdl.default.svc.cluster.local" {
		t.Fatalf("externalName = %q", svc.Spec.ExternalName)
	}
	// ExternalName Service 는 selector/ports 를 갖지 않는다.
	if svc.Spec.Selector != nil || len(svc.Spec.Ports) != 0 {
		t.Fatalf("ExternalName Service must have no selector/ports: %+v", svc.Spec)
	}
}

func TestReconcileShardPrimaryServices_PublishAndFailoverUpdate(t *testing.T) {
	scheme := newScheme(t)
	ns := "default"
	cluster := &postgresv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: ns},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

	svcKey := client.ObjectKey{Namespace: ns, Name: ShardPrimaryServiceName("demo", "shard-0")}

	// 1) 초기 primary(pod-0) → ExternalName Service publish.
	shards := []postgresv1alpha1.ShardStatus{{
		Name:    "shard-0",
		Primary: &postgresv1alpha1.ShardEndpoint{Pod: "demo-shard-0-0", Endpoint: "demo-shard-0-0.hdl.default.svc.cluster.local:5432", Ready: true},
	}}
	if err := r.reconcileShardPrimaryServices(context.Background(), cluster, shards); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var svc corev1.Service
	if err := c.Get(context.Background(), svcKey, &svc); err != nil {
		t.Fatalf("primary Service not created: %v", err)
	}
	if svc.Spec.ExternalName != "demo-shard-0-0.hdl.default.svc.cluster.local" {
		t.Fatalf("initial externalName = %q", svc.Spec.ExternalName)
	}

	// 2) failover: primary 가 pod-1 로 승격 → ExternalName 이 새 primary 로 갱신되어야 함.
	shards[0].Primary = &postgresv1alpha1.ShardEndpoint{Pod: "demo-shard-0-1", Endpoint: "demo-shard-0-1.hdl.default.svc.cluster.local:5432", Ready: true}
	if err := r.reconcileShardPrimaryServices(context.Background(), cluster, shards); err != nil {
		t.Fatalf("reconcile (failover): %v", err)
	}
	if err := c.Get(context.Background(), svcKey, &svc); err != nil {
		t.Fatalf("get after failover: %v", err)
	}
	if svc.Spec.ExternalName != "demo-shard-0-1.hdl.default.svc.cluster.local" {
		t.Fatalf("failover externalName = %q, want pod-1 (DNS failover-follow)", svc.Spec.ExternalName)
	}
}

func TestReconcileShardPrimaryServices_SkipNotReadyOrNoPrimary(t *testing.T) {
	scheme := newScheme(t)
	ns := "default"
	cluster := &postgresv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: ns}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

	shards := []postgresv1alpha1.ShardStatus{
		{Name: "shard-0", Primary: nil}, // primary 부재.
		{Name: "shard-1", Primary: &postgresv1alpha1.ShardEndpoint{Pod: "p", Endpoint: "p:5432", Ready: false}}, // not-ready.
	}
	if err := r.reconcileShardPrimaryServices(context.Background(), cluster, shards); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var list corev1.ServiceList
	if err := c.List(context.Background(), &list, client.InNamespace(ns)); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected no primary Service (skip not-ready/no-primary), got %d", len(list.Items))
	}
}
