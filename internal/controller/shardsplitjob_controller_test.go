/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package controller

import (
	"context"
	"strings"
	"testing"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func ssjWith(phase postgresv1alpha1.ShardSplitJobPhase, fwdOnly bool, targets []postgresv1alpha1.ShardSplitTarget) *postgresv1alpha1.ShardSplitJob {
	return &postgresv1alpha1.ShardSplitJob{
		Spec: postgresv1alpha1.ShardSplitJobSpec{
			Direction:        postgresv1alpha1.ShardSplitDirectionSplit,
			Sources:          []string{"shard-0"},
			AllowForwardOnly: fwdOnly,
			Targets:          targets,
		},
		Status: postgresv1alpha1.ShardSplitJobStatus{Phase: phase},
	}
}

func twoTargets() []postgresv1alpha1.ShardSplitTarget {
	return []postgresv1alpha1.ShardSplitTarget{
		{ShardID: "t0", Ranges: []postgresv1alpha1.ShardRangeEntry{{Lo: "0x00000000", Hi: "0x7fffffff", Shard: "t0"}}},
		{ShardID: "t1", Ranges: []postgresv1alpha1.ShardRangeEntry{{Lo: "0x80000000", Hi: "0xffffffff", Shard: "t1"}}},
	}
}

func overlapTargets() []postgresv1alpha1.ShardSplitTarget {
	return []postgresv1alpha1.ShardSplitTarget{
		{ShardID: "t0", Ranges: []postgresv1alpha1.ShardRangeEntry{{Lo: "0x00000000", Hi: "0x80000000", Shard: "t0"}}},
		{ShardID: "t1", Ranges: []postgresv1alpha1.ShardRangeEntry{{Lo: "0x7fffffff", Hi: "0xffffffff", Shard: "t1"}}},
	}
}

func TestShardSplitJob_nextPhase(t *testing.T) {
	r := &ShardSplitJobReconciler{}
	P := postgresv1alpha1.ShardSplitJobPhase("")
	_ = P
	cases := []struct {
		name string
		ssj  *postgresv1alpha1.ShardSplitJob
		want postgresv1alpha1.ShardSplitJobPhase
	}{
		{"pending valid → SnapshotWAL", ssjWith(postgresv1alpha1.ShardSplitPhasePending, false, twoTargets()), postgresv1alpha1.ShardSplitPhaseSnapshotWAL},
		{"pending overlap → Failed", ssjWith(postgresv1alpha1.ShardSplitPhasePending, false, overlapTargets()), postgresv1alpha1.ShardSplitPhaseFailed},
		{"initialcopy → CDCCatchup", ssjWith(postgresv1alpha1.ShardSplitPhaseInitialCopy, false, twoTargets()), postgresv1alpha1.ShardSplitPhaseCDCCatchup},
		{"cutover reversible → RoutingUpdate", ssjWith(postgresv1alpha1.ShardSplitPhaseCutover, false, twoTargets()), postgresv1alpha1.ShardSplitPhaseRoutingUpdate},
		{"cutover forward-only → Failed (비가역 거부)", ssjWith(postgresv1alpha1.ShardSplitPhaseCutover, true, twoTargets()), postgresv1alpha1.ShardSplitPhaseFailed},
		{"cleanup → Promote", ssjWith(postgresv1alpha1.ShardSplitPhaseCleanup, false, twoTargets()), postgresv1alpha1.ShardSplitPhasePromote},
		{"promote → Completed", ssjWith(postgresv1alpha1.ShardSplitPhasePromote, false, twoTargets()), postgresv1alpha1.ShardSplitPhaseCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := r.nextPhase(tc.ssj)
			if got != tc.want {
				t.Fatalf("nextPhase = %q, want %q", got, tc.want)
			}
		})
	}
}
func TestShardSplitJob_nextPhase_RejectsUnsupportedSourceShapes(t *testing.T) {
	r := &ShardSplitJobReconciler{}
	cases := []struct {
		name      string
		direction postgresv1alpha1.ShardSplitDirection
		sources   []string
	}{
		{"merge is not implemented", postgresv1alpha1.ShardSplitDirectionMerge, []string{"shard-0", "shard-1"}},
		{"split requires exactly one source", postgresv1alpha1.ShardSplitDirectionSplit, []string{"shard-0", "shard-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ssj := ssjWith(postgresv1alpha1.ShardSplitPhasePending, false, twoTargets())
			ssj.Spec.Direction = tc.direction
			ssj.Spec.Sources = tc.sources
			got, reason := r.nextPhase(ssj)
			if got != postgresv1alpha1.ShardSplitPhaseFailed {
				t.Fatalf("nextPhase = %q, want Failed", got)
			}
			if reason == "" {
				t.Fatal("unsupported source shape must include a failure reason")
			}
		})
	}
}

func TestReconcileShardSplitJob_RejectsUnsupportedShapeBeforePhaseEffects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		direction postgresv1alpha1.ShardSplitDirection
		sources   []string
		want      string
	}{
		{"in-flight merge", postgresv1alpha1.ShardSplitDirectionMerge, []string{"shard-0", "shard-1"}, "merge direction is not implemented"},
		{"in-flight multi-source split", postgresv1alpha1.ShardSplitDirectionSplit, []string{"shard-0", "shard-1"}, "split requires exactly one source"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scheme := newScheme(t)
			job := ssjWith(postgresv1alpha1.ShardSplitPhaseBootstrap, false, twoTargets())
			job.ObjectMeta = metav1.ObjectMeta{Name: "unsafe", Namespace: "default"}
			job.Spec.Direction = tc.direction
			job.Spec.Sources = tc.sources
			client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(job).WithObjects(job).Build()
			r := &ShardSplitJobReconciler{Client: client, Scheme: scheme}

			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: job.Name, Namespace: job.Namespace}}); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}

			var got postgresv1alpha1.ShardSplitJob
			if err := client.Get(context.Background(), types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, &got); err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got.Status.Phase != postgresv1alpha1.ShardSplitPhaseFailed || !strings.Contains(got.Status.FailureReason, tc.want) {
				t.Fatalf("status = (%q, %q), want Failed containing %q", got.Status.Phase, got.Status.FailureReason, tc.want)
			}
			var configMaps corev1.ConfigMapList
			if err := client.List(context.Background(), &configMaps); err != nil {
				t.Fatalf("List(ConfigMap) error = %v", err)
			}
			if len(configMaps.Items) != 0 {
				t.Fatalf("unsupported request created %d ConfigMaps", len(configMaps.Items))
			}
		})
	}
}
