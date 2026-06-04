/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Package v1alpha1 defines the v1alpha1 API group of keiailab/postgres-operator.
//
// All type signatures in this package are frozen by RFC 0001 (CRD Schema v1alpha1).
// Semantics (reconciler behavior, webhook validation) are finalized by the per-Pillar
// follow-up RFCs.
//
// +kubebuilder:object:generate=true
// +groupName=postgres.keiailab.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group/version identifier for this API group.
	GroupVersion = schema.GroupVersion{Group: "postgres.keiailab.io", Version: "v1alpha1"}

	// SchemeBuilder registers this package's Go types under GroupVersion.
	//nolint:staticcheck // SA1019: kubebuilder-generated; api packages use sigs.k8s.io/controller-runtime/pkg/scheme as the canonical Builder
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme is invoked from cmd/main.go's init() to register this package's types
	// with the manager's scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
