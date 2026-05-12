/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"fmt"
	"strings"

	postgresv1alpha1 "github.com/keiailab/postgres-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

type replicaBootstrapConfig struct {
	Endpoint    string
	User        string
	DBName      string
	SSLMode     string
	Password    *corev1.SecretKeySelector
	SSLKey      *corev1.SecretKeySelector
	SSLCert     *corev1.SecretKeySelector
	SSLRootCert *corev1.SecretKeySelector
}

func standaloneReplicaEnabled(cluster *postgresv1alpha1.PostgresCluster) bool {
	return cluster != nil && cluster.Spec.Replica != nil && cluster.Spec.Replica.Enabled
}

func replicaBootstrapConfigForCluster(cluster *postgresv1alpha1.PostgresCluster) (*replicaBootstrapConfig, error) {
	if !standaloneReplicaEnabled(cluster) {
		return nil, nil
	}
	if cluster.Spec.Replica.Source == "" {
		return nil, fmt.Errorf("replica.source is required when replica.enabled=true")
	}
	if cluster.Spec.Bootstrap == nil || cluster.Spec.Bootstrap.PgBaseBackup == nil {
		return nil, fmt.Errorf("bootstrap.pg_basebackup.source is required when replica.enabled=true")
	}
	if cluster.Spec.Bootstrap.PgBaseBackup.Source != cluster.Spec.Replica.Source {
		return nil, fmt.Errorf("bootstrap.pg_basebackup.source %q must match replica.source %q",
			cluster.Spec.Bootstrap.PgBaseBackup.Source, cluster.Spec.Replica.Source)
	}
	source, ok := externalClusterByName(cluster.Spec.ExternalClusters, cluster.Spec.Replica.Source)
	if !ok {
		return nil, fmt.Errorf("externalClusters does not define replica source %q", cluster.Spec.Replica.Source)
	}
	host := strings.TrimSpace(source.ConnectionParameters["host"])
	if host == "" {
		return nil, fmt.Errorf("externalCluster %q connectionParameters.host is required", source.Name)
	}
	port := strings.TrimSpace(source.ConnectionParameters["port"])
	if port == "" {
		port = "5432"
	}
	user := strings.TrimSpace(source.ConnectionParameters["user"])
	if user == "" {
		user = postgresUserReservedNamePostgres
	}
	dbName := strings.TrimSpace(source.ConnectionParameters["dbname"])
	if dbName == "" {
		dbName = postgresDatabaseReservedNamePostgres
	}
	sslMode := strings.TrimSpace(source.ConnectionParameters["sslmode"])
	if sslMode == "" {
		sslMode = "prefer"
	}
	for label, ref := range map[string]*corev1.SecretKeySelector{
		"password":    source.Password,
		"sslKey":      source.SSLKey,
		"sslCert":     source.SSLCert,
		"sslRootCert": source.SSLRootCert,
	} {
		if err := validateSecretKeySelector(label, ref); err != nil {
			return nil, err
		}
	}
	return &replicaBootstrapConfig{
		Endpoint:    host + ":" + port,
		User:        user,
		DBName:      dbName,
		SSLMode:     sslMode,
		Password:    source.Password,
		SSLKey:      source.SSLKey,
		SSLCert:     source.SSLCert,
		SSLRootCert: source.SSLRootCert,
	}, nil
}

func validateSecretKeySelector(label string, ref *corev1.SecretKeySelector) error {
	if ref == nil {
		return nil
	}
	if strings.TrimSpace(ref.Name) == "" {
		return fmt.Errorf("externalCluster %s.name is required when %s is set", label, label)
	}
	if strings.TrimSpace(ref.Key) == "" {
		return fmt.Errorf("externalCluster %s.key is required when %s is set", label, label)
	}
	return nil
}

func externalClusterByName(
	items []postgresv1alpha1.ExternalClusterSpec,
	name string,
) (postgresv1alpha1.ExternalClusterSpec, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return postgresv1alpha1.ExternalClusterSpec{}, false
}
