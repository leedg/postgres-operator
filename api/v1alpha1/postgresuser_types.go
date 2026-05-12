/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgresUserSpec 은 CNPG managed.roles 의 핵심 운영 표면을 별도 CRD 로 제공한다.
//
// +kubebuilder:validation:XValidation:rule="self.name != 'postgres' && !self.name.startsWith('pg_')",message="postgres and pg_* are reserved role names"
// +kubebuilder:validation:XValidation:rule="!(has(self.passwordSecretRef) && self.disablePassword)",message="passwordSecretRef and disablePassword cannot both be set"
type PostgresUserSpec struct {
	// Cluster 는 role 을 생성할 PostgresCluster 이다.
	// +kubebuilder:validation:Required
	Cluster DatabaseClusterRef `json:"cluster"`

	// Name 은 PostgreSQL role 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Ensure 는 role 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// Login 은 role 이 LOGIN 권한을 갖는지 여부다.
	// +optional
	Login bool `json:"login,omitempty"`

	// Superuser 는 role 이 SUPERUSER 권한을 갖는지 여부다.
	// +optional
	Superuser bool `json:"superuser,omitempty"`

	// CreateDB 는 role 이 CREATEDB 권한을 갖는지 여부다.
	// +optional
	CreateDB bool `json:"createdb,omitempty"`

	// CreateRole 은 role 이 CREATEROLE 권한을 갖는지 여부다.
	// +optional
	CreateRole bool `json:"createrole,omitempty"`

	// Replication 은 role 이 REPLICATION 권한을 갖는지 여부다.
	// +optional
	Replication bool `json:"replication,omitempty"`

	// BypassRLS 는 role 이 row-level security 를 우회할 수 있는지 여부다.
	// +optional
	BypassRLS bool `json:"bypassrls,omitempty"`

	// Inherit 는 role privilege inheritance 여부다. nil 이면 PostgreSQL 기본값인 true 로 reconcile 한다.
	// +optional
	Inherit *bool `json:"inherit,omitempty"`

	// ConnectionLimit 은 role connection limit 이다. nil 이면 변경하지 않고, -1 은 제한 없음을 뜻한다.
	// +kubebuilder:validation:Minimum=-1
	// +optional
	ConnectionLimit *int32 `json:"connectionLimit,omitempty"`

	// InRoles 는 이 role 이 member 로 들어갈 parent role 목록이다.
	// +optional
	InRoles []string `json:"inRoles,omitempty"`

	// PasswordSecretRef 는 data.username/data.password 값을 PostgreSQL role password 로 반영할 Secret 이다.
	// data.username 은 spec.name 과 일치해야 한다.
	// +optional
	PasswordSecretRef *corev1.LocalObjectReference `json:"passwordSecretRef,omitempty"`

	// DisablePassword 는 password 를 NULL 로 설정해 password login 을 비활성화한다.
	// +optional
	DisablePassword bool `json:"disablePassword,omitempty"`

	// ValidUntil 은 role password 만료 시각이다. PostgreSQL 이 허용하는 timestamp 또는 infinity 를 사용한다.
	// +optional
	ValidUntil string `json:"validUntil,omitempty"`
}

// PostgresUserStatus 는 role reconcile 관찰 상태다.
type PostgresUserStatus struct {
	// Applied 는 마지막 observedGeneration 이 PostgreSQL 에 성공적으로 반영됐는지 여부다.
	// +optional
	Applied bool `json:"applied,omitempty"`

	// ObservedGeneration 은 마지막 처리 generation 이다.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Message 는 마지막 reconcile 요약 또는 실패 원인이다.
	// +optional
	Message string `json:"message,omitempty"`

	// PasswordSecretResourceVersion 은 마지막으로 성공 반영한 password Secret resourceVersion 이다.
	// +optional
	PasswordSecretResourceVersion string `json:"passwordSecretResourceVersion,omitempty"`

	// Conditions 는 K8s 표준 상태다.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=pguser,categories=postgres;role;all
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.cluster.name`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Login",type=boolean,JSONPath=`.spec.login`
// +kubebuilder:printcolumn:name="Applied",type=boolean,JSONPath=`.status.applied`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PostgresUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresUserSpec   `json:"spec,omitempty"`
	Status PostgresUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PostgresUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresUser{}, &PostgresUserList{})
}
