/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package v1alpha1은 keiailab/postgres-operator의 v1alpha1 API 그룹을 정의한다.
//
// 본 패키지의 모든 타입 시그니처는 RFC 0001(CRD Schema v1alpha1)에서 동결되었다.
// 의미론(reconciler 동작, webhook 검증)은 Pillar별 후속 RFC에서 확정된다.
//
// +kubebuilder:object:generate=true
// +groupName=postgres.keiailab.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion은 본 그룹의 그룹/버전 식별자다.
	GroupVersion = schema.GroupVersion{Group: "postgres.keiailab.io", Version: "v1alpha1"}

	// SchemeBuilder는 본 패키지의 Go 타입을 GroupVersion에 등록한다.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme은 cmd/main.go의 init()에서 호출되어 매니저 스키마에 본 패키지
	// 타입을 등록한다.
	AddToScheme = SchemeBuilder.AddToScheme
)
