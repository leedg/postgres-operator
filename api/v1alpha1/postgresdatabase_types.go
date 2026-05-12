/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DatabaseEnsure 는 PostgreSQL database/schema/extension 의 선언적 존재 상태다.
// +kubebuilder:validation:Enum=present;absent
type DatabaseEnsure string

const (
	DatabaseEnsurePresent DatabaseEnsure = "present"
	DatabaseEnsureAbsent  DatabaseEnsure = "absent"
)

// DatabaseReclaimPolicy 는 PostgresDatabase CR 삭제 시 PostgreSQL database 처리 정책이다.
// +kubebuilder:validation:Enum=retain;delete
type DatabaseReclaimPolicy string

const (
	DatabaseReclaimRetain DatabaseReclaimPolicy = "retain"
	DatabaseReclaimDelete DatabaseReclaimPolicy = "delete"
)

// DatabaseUsageType 는 FDW/server USAGE 권한 reconcile 동작이다.
// +kubebuilder:validation:Enum=grant;revoke
type DatabaseUsageType string

const (
	DatabaseUsageGrant  DatabaseUsageType = "grant"
	DatabaseUsageRevoke DatabaseUsageType = "revoke"
)

// DatabaseClusterRef 는 같은 namespace 의 PostgresCluster 참조다.
type DatabaseClusterRef struct {
	// Name 은 PostgresCluster.metadata.name 이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// DatabaseExtensionSpec 은 target database 안에서 관리할 extension 이다.
type DatabaseExtensionSpec struct {
	// Name 은 PostgreSQL extension 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Ensure 는 extension 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// Version 은 설치 또는 업그레이드할 extension version 이다.
	// +optional
	Version string `json:"version,omitempty"`

	// Schema 는 extension 을 배치할 schema 다.
	// +optional
	Schema string `json:"schema,omitempty"`
}

// DatabaseSchemaSpec 은 target database 안에서 관리할 schema 이다.
type DatabaseSchemaSpec struct {
	// Name 은 PostgreSQL schema 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Owner 는 schema owner role 이다.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Ensure 는 schema 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// Privileges 는 이 schema 에 대한 role privilege grant/revoke 목록이다.
	// +optional
	Privileges []DatabaseGrantSpec `json:"privileges,omitempty"`
}

// DatabaseOptionSpec 은 FDW/server option 하나의 선언 상태다.
type DatabaseOptionSpec struct {
	// Name 은 option 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Value 는 option 값이다. ensure=absent 에서는 생략할 수 있다.
	// +optional
	Value string `json:"value,omitempty"`

	// Ensure 는 option 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`
}

// DatabaseUsageSpec 은 FDW/server 에 대한 role USAGE 권한 reconcile 항목이다.
type DatabaseUsageSpec struct {
	// Name 은 권한을 부여하거나 회수할 PostgreSQL role 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type 은 grant 또는 revoke 다. 빈 값이면 grant.
	// +kubebuilder:default=grant
	// +optional
	Type DatabaseUsageType `json:"type,omitempty"`
}

// DatabaseGrantSpec 은 database 또는 schema privilege grant/revoke 항목이다.
type DatabaseGrantSpec struct {
	// Role 은 권한을 부여하거나 회수할 PostgreSQL role 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Role string `json:"role"`

	// Privileges 는 대상 객체에 적용할 PostgreSQL privilege 목록이다.
	// database 대상: CONNECT, CREATE, TEMPORARY, TEMP, ALL PRIVILEGES
	// schema 대상: USAGE, CREATE, ALL PRIVILEGES
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Privileges []string `json:"privileges"`

	// Type 은 grant 또는 revoke 다. 빈 값이면 grant.
	// +kubebuilder:default=grant
	// +optional
	Type DatabaseUsageType `json:"type,omitempty"`
}

// DatabaseFDWSpec 은 target database 안에서 관리할 foreign data wrapper 이다.
type DatabaseFDWSpec struct {
	// Name 은 foreign data wrapper 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Ensure 는 FDW 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// Handler 는 FDW handler function 이름이다. "-" 는 NO HANDLER 를 의미한다.
	// +optional
	Handler string `json:"handler,omitempty"`

	// Validator 는 FDW validator function 이름이다. "-" 는 NO VALIDATOR 를 의미한다.
	// +optional
	Validator string `json:"validator,omitempty"`

	// Owner 는 FDW owner role 이다.
	// +optional
	Owner string `json:"owner,omitempty"`

	// Options 는 FDW option 목록이다.
	// +optional
	Options []DatabaseOptionSpec `json:"options,omitempty"`

	// Usage 는 FDW USAGE 권한 목록이다.
	// +optional
	Usage []DatabaseUsageSpec `json:"usage,omitempty"`
}

// DatabaseServerSpec 은 target database 안에서 관리할 foreign server 이다.
type DatabaseServerSpec struct {
	// Name 은 foreign server 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// FDW 는 이 server 가 사용할 foreign data wrapper 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	FDW string `json:"fdw"`

	// Ensure 는 server 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// Options 는 server option 목록이다.
	// +optional
	Options []DatabaseOptionSpec `json:"options,omitempty"`

	// Usage 는 server USAGE 권한 목록이다.
	// +optional
	Usage []DatabaseUsageSpec `json:"usage,omitempty"`
}

// PostgresDatabaseSpec 은 CNPG Database CRD 의 핵심 운영 표면을 본 operator 모델로 이식한다.
//
// +kubebuilder:validation:XValidation:rule="self.name != 'postgres' && self.name != 'template0' && self.name != 'template1'",message="postgres, template0, template1 are reserved database names"
type PostgresDatabaseSpec struct {
	// Cluster 는 database 를 생성할 PostgresCluster 이다.
	// +kubebuilder:validation:Required
	Cluster DatabaseClusterRef `json:"cluster"`

	// Name 은 PostgreSQL database 이름이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Owner 는 database owner role 이다.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	// Ensure 는 database 존재 상태다. 빈 값이면 present.
	// +kubebuilder:default=present
	// +optional
	Ensure DatabaseEnsure `json:"ensure,omitempty"`

	// DatabaseReclaimPolicy 는 CR 삭제 시 database 처리 정책이다. 빈 값이면 retain.
	// +kubebuilder:default=retain
	// +optional
	DatabaseReclaimPolicy DatabaseReclaimPolicy `json:"databaseReclaimPolicy,omitempty"`

	// Tablespace 는 database 의 기본 tablespace 이다.
	// +optional
	Tablespace string `json:"tablespace,omitempty"`

	// Extensions 는 target database 안에서 선언적으로 관리할 extension 목록이다.
	// +optional
	Extensions []DatabaseExtensionSpec `json:"extensions,omitempty"`

	// Schemas 는 target database 안에서 선언적으로 관리할 schema 목록이다.
	// +optional
	Schemas []DatabaseSchemaSpec `json:"schemas,omitempty"`

	// FDWs 는 target database 안에서 선언적으로 관리할 foreign data wrapper 목록이다.
	// +optional
	FDWs []DatabaseFDWSpec `json:"fdws,omitempty"`

	// Servers 는 target database 안에서 선언적으로 관리할 foreign server 목록이다.
	// +optional
	Servers []DatabaseServerSpec `json:"servers,omitempty"`

	// Privileges 는 database 객체 자체에 대한 role privilege grant/revoke 목록이다.
	// +optional
	Privileges []DatabaseGrantSpec `json:"privileges,omitempty"`
}

// PostgresDatabaseStatus 는 database reconcile 관찰 상태다.
type PostgresDatabaseStatus struct {
	// Applied 는 마지막 observedGeneration 이 PostgreSQL 에 성공적으로 반영됐는지 여부다.
	// +optional
	Applied bool `json:"applied,omitempty"`

	// ObservedGeneration 은 마지막 처리 generation 이다.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Message 는 마지막 reconcile 요약 또는 실패 원인이다.
	// +optional
	Message string `json:"message,omitempty"`

	// Conditions 는 K8s 표준 상태다.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=pgdb,categories=postgres;database;all
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.cluster.name`
// +kubebuilder:printcolumn:name="Database",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name="Applied",type=boolean,JSONPath=`.status.applied`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PostgresDatabase struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresDatabaseSpec   `json:"spec,omitempty"`
	Status PostgresDatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type PostgresDatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresDatabase `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresDatabase{}, &PostgresDatabaseList{})
}
