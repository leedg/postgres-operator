/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"strings"
	"testing"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestReplicaBootstrapConfigForCluster_RejectsIncompleteSecretSelectors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		edit func(*postgresv1alpha1.ExternalClusterSpec)
		want string
	}{
		{
			name: "password missing key",
			edit: func(source *postgresv1alpha1.ExternalClusterSpec) {
				source.Password = &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "source-password"},
				}
			},
			want: "password.key is required",
		},
		{
			name: "sslKey missing name",
			edit: func(source *postgresv1alpha1.ExternalClusterSpec) {
				source.SSLKey = &corev1.SecretKeySelector{Key: "tls.key"}
			},
			want: "sslKey.name is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cluster := replicaClusterForConfigTest()
			tc.edit(&cluster.Spec.ExternalClusters[0])

			_, err := replicaBootstrapConfigForCluster(cluster)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestReplicaBootstrapConfigForCluster_AcceptsPasswordAndTLSSecretSelectors(t *testing.T) {
	t.Parallel()

	cluster := replicaClusterForConfigTest()
	source := &cluster.Spec.ExternalClusters[0]
	source.Password = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "source-password"},
		Key:                  "password",
	}
	source.SSLKey = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "source-replication"},
		Key:                  "tls.key",
	}
	source.SSLCert = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "source-replication"},
		Key:                  "tls.crt",
	}
	source.SSLRootCert = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "source-ca"},
		Key:                  "ca.crt",
	}

	config, err := replicaBootstrapConfigForCluster(cluster)
	if err != nil {
		t.Fatalf("replicaBootstrapConfigForCluster returned error: %v", err)
	}
	if config.Password.Name != "source-password" || config.Password.Key != "password" {
		t.Fatalf("password selector = %+v", config.Password)
	}
	if config.SSLKey.Name != "source-replication" || config.SSLKey.Key != "tls.key" {
		t.Fatalf("sslKey selector = %+v", config.SSLKey)
	}
	if config.SSLCert.Name != "source-replication" || config.SSLCert.Key != "tls.crt" {
		t.Fatalf("sslCert selector = %+v", config.SSLCert)
	}
	if config.SSLRootCert.Name != "source-ca" || config.SSLRootCert.Key != "ca.crt" {
		t.Fatalf("sslRootCert selector = %+v", config.SSLRootCert)
	}
}

func replicaClusterForConfigTest() *postgresv1alpha1.PostgresCluster {
	return &postgresv1alpha1.PostgresCluster{
		Spec: postgresv1alpha1.PostgresClusterSpec{
			ExternalClusters: []postgresv1alpha1.ExternalClusterSpec{{
				Name: "source",
				ConnectionParameters: map[string]string{
					"host": "source-rw.default.svc",
				},
			}},
			Bootstrap: &postgresv1alpha1.BootstrapSpec{
				PgBaseBackup: &postgresv1alpha1.PgBaseBackupBootstrapSpec{Source: "source"},
			},
			Replica: &postgresv1alpha1.ReplicaClusterSpec{
				Enabled: true,
				Source:  "source",
			},
		},
	}
}
