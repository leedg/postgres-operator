/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package fencing

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// 본 파일은 Pillar P2-T2 fencing의 단위 회귀다. RFC 0003 부록 A의 결정을
// 코드 차원에서 강제한다.

// ----------------------------------------------------------------------------
// 명명 규약
// ----------------------------------------------------------------------------

func TestPVCName_AppendsDataPrefix(t *testing.T) {
	got := PVCName("orders-coordinator-0")
	want := "data-orders-coordinator-0"
	if got != want {
		t.Errorf("PVCName = %q, want %q", got, want)
	}
}

// ----------------------------------------------------------------------------
// Real 입력 검증
// ----------------------------------------------------------------------------

func TestNewReal_RejectsEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		cfg  RealConfig
	}{
		{"nil client", RealConfig{Namespace: "default", PVCName: "data-x-0"}},
		{"empty namespace", RealConfig{Client: fake.NewClientset(), PVCName: "data-x-0"}},
		{"empty pvc", RealConfig{Client: fake.NewClientset(), Namespace: "default"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewReal(c.cfg); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Real fence 라이프사이클 (fake clientset)
//
// fake clientset은 strategic merge patch를 처리하므로 K8s API server 없이
// label patch 회귀가 가능하다. 실 K8s API에 대한 회귀는 internal/controller
// envtest에서 수행한다(향후 통합 테스트 추가 가능).
// ----------------------------------------------------------------------------

func newPVC(name string, labels map[string]string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}
}

func TestReal_MarkFenced_AddsLabel(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(newPVC("data-orders-coordinator-0", nil))

	r, err := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-orders-coordinator-0"})
	if err != nil {
		t.Fatal(err)
	}

	if err := r.MarkFenced(ctx); err != nil {
		t.Fatalf("MarkFenced: %v", err)
	}

	pvc, err := cs.CoreV1().PersistentVolumeClaims("default").Get(ctx, "data-orders-coordinator-0", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if pvc.Labels[FenceLabelKey] != FenceLabelValue {
		t.Errorf("label %s = %q, want %q", FenceLabelKey, pvc.Labels[FenceLabelKey], FenceLabelValue)
	}
}

func TestReal_MarkFenced_Idempotent(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(newPVC("data-x-0", map[string]string{FenceLabelKey: FenceLabelValue}))

	r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-x-0"})
	if err := r.MarkFenced(ctx); err != nil {
		t.Fatalf("MarkFenced(already-fenced): %v", err)
	}
	if err := r.MarkFenced(ctx); err != nil {
		t.Fatalf("MarkFenced(2nd call): %v", err)
	}
}

func TestReal_Unfence_RemovesLabel(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(newPVC("data-x-0", map[string]string{
		FenceLabelKey: FenceLabelValue,
		"app":         "postgres",
	}))

	r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-x-0"})
	if err := r.Unfence(ctx); err != nil {
		t.Fatalf("Unfence: %v", err)
	}

	pvc, _ := cs.CoreV1().PersistentVolumeClaims("default").Get(ctx, "data-x-0", metav1.GetOptions{})
	if _, ok := pvc.Labels[FenceLabelKey]; ok {
		t.Errorf("fence label still present after Unfence: %v", pvc.Labels)
	}
	if pvc.Labels["app"] != "postgres" {
		t.Errorf("Unfence wiped unrelated labels: %v", pvc.Labels)
	}
}

func TestReal_IsFenced_Reflects(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"no labels", nil, false},
		{"unrelated only", map[string]string{"app": "postgres"}, false},
		{"fenced=true", map[string]string{FenceLabelKey: FenceLabelValue}, true},
		{"fenced=false-string", map[string]string{FenceLabelKey: "false"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cs := fake.NewClientset(newPVC("data-x-0", c.labels))
			r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-x-0"})
			got, err := r.IsFenced(ctx)
			if err != nil {
				t.Fatalf("IsFenced: %v", err)
			}
			if got != c.want {
				t.Errorf("IsFenced = %v, want %v", got, c.want)
			}
		})
	}
}

func TestReal_IsFenced_NotFound(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset() // PVC 부재
	r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-missing-0"})

	_, err := r.IsFenced(ctx)
	if err == nil {
		t.Fatal("expected NotFound error")
	}
}

func TestReal_VerifyNotFenced_ReturnsErrFenced(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(newPVC("data-x-0", map[string]string{FenceLabelKey: FenceLabelValue}))
	r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-x-0"})

	err := r.VerifyNotFenced(ctx)
	if !errors.Is(err, ErrFenced) {
		t.Errorf("VerifyNotFenced = %v, want ErrFenced", err)
	}
}

func TestReal_VerifyNotFenced_PassesWhenClean(t *testing.T) {
	ctx := context.Background()
	cs := fake.NewClientset(newPVC("data-x-0", nil))
	r, _ := NewReal(RealConfig{Client: cs, Namespace: "default", PVCName: "data-x-0"})

	if err := r.VerifyNotFenced(ctx); err != nil {
		t.Errorf("VerifyNotFenced(clean) = %v, want nil", err)
	}
}

// ----------------------------------------------------------------------------
// Mock 동작
// ----------------------------------------------------------------------------

func TestMock_LifecycleAndCounters(t *testing.T) {
	ctx := context.Background()
	m := NewMock()

	// 초기 상태 — unfenced
	if got, _ := m.IsFenced(ctx); got {
		t.Error("initial IsFenced = true, want false")
	}

	// MarkFenced → fenced
	if err := m.MarkFenced(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyNotFenced(ctx); !errors.Is(err, ErrFenced) {
		t.Errorf("VerifyNotFenced after MarkFenced = %v, want ErrFenced", err)
	}

	// Unfence → unfenced
	if err := m.Unfence(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyNotFenced(ctx); err != nil {
		t.Errorf("VerifyNotFenced after Unfence = %v, want nil", err)
	}

	// SetFenced(true) → 직접 전이
	m.SetFenced(true)
	if got, _ := m.IsFenced(ctx); !got {
		t.Error("SetFenced(true) did not flip flag")
	}

	mark, unfence, is := m.Calls()
	if mark != 1 || unfence != 1 || is < 3 {
		t.Errorf("call counts = mark=%d unfence=%d is=%d", mark, unfence, is)
	}
}

// ----------------------------------------------------------------------------
// 인터페이스 일관성
// ----------------------------------------------------------------------------

func TestImplementations_SatisfyInterface(t *testing.T) {
	var _ Fencer = (*Real)(nil)
	var _ Fencer = (*Mock)(nil)
}
