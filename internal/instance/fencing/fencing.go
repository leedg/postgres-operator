/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package fencing은 PVC label 기반 split-brain 방지 메커니즘을 제공한다
// (Pillar P2-T2, RFC 0003 부록 A).
//
// 모델 — PG primary가 lease를 잃었을 때, instance manager는 자신의 데이터
// PVC에 fence label을 부착한다. 이후 같은 PVC를 마운트하려는 새 Pod은
// promote 직전에 fence label 부재를 확인해야 한다. 좀비 Pod이 살아돌아와
// 같은 PVC에 두 PG가 동시 마운트되는 split-brain은 K8s API server를 단일
// 진실 출처로 두는 본 메커니즘으로 차단된다.
//
// 본 패키지는 *RBAC를 가진 Pod 자신*이 자기 PVC를 patch 하는 권한 모델을
// 가정한다. 클러스터 controller가 patch 하는 모델은 ADR 0002 §결과의
// "K8s API as DCS" 원칙과 충돌하므로 채택하지 않는다.
package fencing

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// FenceLabelKey는 PVC를 fenced 상태로 표시하는 label key다(RFC 0003 부록 A §1).
const FenceLabelKey = "postgres.keiailab.io/fenced"

// FenceLabelValue는 fenced 상태의 label value다.
const FenceLabelValue = "true"

// ErrFenced는 자기 PVC가 fenced 상태일 때 promote를 거부할 때 반환된다.
// instance manager는 본 에러를 받으면 *exit non-zero*로 응답해 운영자가
// 수동으로 fence를 해제(또는 PVC 교체)할 때까지 leadership 점유를 거절한다.
var ErrFenced = errors.New("fencing: PVC is fenced — refusing to promote")

// Fencer는 PVC fence 상태 조회/설정을 추상화한다.
//
// 단일 PVC 스코프로 설계됐으며 단일 Pod 인스턴스가 자기 자신의 PVC만
// 다루는 운영 모델을 따른다. 멀티 PVC를 한 번에 다루는 트랜잭션은 본
// 인터페이스에 포함하지 않는다 — split-brain 방지는 *각 Pod이 자기*
// 결정을 기록하는 분산 모델로 충분.
type Fencer interface {
	// MarkFenced는 PVC에 fence label을 부착한다. 이미 fenced면 no-op(idempotent).
	MarkFenced(ctx context.Context) error

	// Unfence는 PVC에서 fence label을 제거한다. 운영자 수동 회복용.
	Unfence(ctx context.Context) error

	// IsFenced는 PVC의 fence label 존재 여부를 반환한다.
	// PVC 자체가 존재하지 않으면 (false, error). 호출자가 분기 처리.
	IsFenced(ctx context.Context) (bool, error)

	// VerifyNotFenced는 fenced=true이면 ErrFenced를 반환한다. promote 직전
	// 호출용 헬퍼.
	VerifyNotFenced(ctx context.Context) error
}

// PVCName은 StatefulSet VolumeClaimTemplates 컨벤션에 따른 PVC 이름을
// 합성한다(`<vct-name>-<sts-pod-name>`).
//
// 본 오퍼레이터의 buildPGStatefulSet(internal/controller/builders.go)은
// VCT 이름 "data"를 사용하므로 instance manager Pod명이 `<sts>-<ord>`이면
// PVC 이름은 `data-<sts>-<ord>`다.
func PVCName(podName string) string {
	return "data-" + podName
}

// RealConfig는 NewReal 입력이다.
type RealConfig struct {
	Client    kubernetes.Interface
	Namespace string
	PVCName   string
}

// Real은 K8s API에 대한 실제 Fencer 구현이다.
type Real struct {
	cs  kubernetes.Interface
	ns  string
	pvc string
}

// NewReal은 PVCName/Namespace 검증 후 Real을 만든다.
func NewReal(cfg RealConfig) (*Real, error) {
	if cfg.Client == nil {
		return nil, errors.New("fencing: Client must not be nil")
	}
	if cfg.Namespace == "" {
		return nil, errors.New("fencing: Namespace must not be empty")
	}
	if cfg.PVCName == "" {
		return nil, errors.New("fencing: PVCName must not be empty")
	}
	return &Real{cs: cfg.Client, ns: cfg.Namespace, pvc: cfg.PVCName}, nil
}

// MarkFenced는 strategic merge patch로 fence label을 부착한다.
// 동일 label 재부착은 K8s API가 no-op으로 처리하므로 idempotent.
func (r *Real) MarkFenced(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"metadata":{"labels":{%q:%q}}}`,
		FenceLabelKey, FenceLabelValue,
	)
	_, err := r.cs.CoreV1().PersistentVolumeClaims(r.ns).Patch(
		ctx, r.pvc, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("fencing: MarkFenced %s/%s: %w", r.ns, r.pvc, err)
	}
	return nil
}

// Unfence는 strategic merge patch로 label key를 null로 설정해 제거한다.
// 부재 시 no-op이 되도록 K8s가 보장.
func (r *Real) Unfence(ctx context.Context) error {
	patch := fmt.Sprintf(
		`{"metadata":{"labels":{%q:null}}}`,
		FenceLabelKey,
	)
	_, err := r.cs.CoreV1().PersistentVolumeClaims(r.ns).Patch(
		ctx, r.pvc, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("fencing: Unfence %s/%s: %w", r.ns, r.pvc, err)
	}
	return nil
}

// IsFenced는 PVC 객체를 GET하여 label 존재 여부를 본다.
func (r *Real) IsFenced(ctx context.Context) (bool, error) {
	pvc, err := r.cs.CoreV1().PersistentVolumeClaims(r.ns).Get(ctx, r.pvc, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, fmt.Errorf("fencing: PVC %s/%s not found: %w", r.ns, r.pvc, err)
		}
		return false, fmt.Errorf("fencing: Get %s/%s: %w", r.ns, r.pvc, err)
	}
	return isLabeledFenced(pvc), nil
}

// VerifyNotFenced는 IsFenced=true이면 ErrFenced를 반환한다. PVC 부재는
// 본 함수에서 fenced로 간주하지 않고 IsFenced의 에러를 전파(부재 자체가
// 운영 이슈로 별도 처리).
func (r *Real) VerifyNotFenced(ctx context.Context) error {
	fenced, err := r.IsFenced(ctx)
	if err != nil {
		return err
	}
	if fenced {
		return ErrFenced
	}
	return nil
}

func isLabeledFenced(pvc *corev1.PersistentVolumeClaim) bool {
	if pvc == nil {
		return false
	}
	return pvc.Labels[FenceLabelKey] == FenceLabelValue
}

// Compile-time guard.
var _ Fencer = (*Real)(nil)
