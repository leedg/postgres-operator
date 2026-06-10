# Credential Sourcing Runbook

## Scope

Use External Secrets Operator with the KeiaiLab Infisical `ClusterSecretStore`
to materialize the Kubernetes Secrets referenced by `PostgresUser`, `Pooler`,
and external replica bootstrap specs. The operator CRDs continue to consume
ordinary Kubernetes Secret names, so no CRD migration is required.

## Helm Values

```yaml
externalSecrets:
  enabled: true
  secretStoreKind: ClusterSecretStore
  secretStoreName: infisical
  postgresUser:
    enabled: true
    targetName: quickstart-app-password
    usernameRemoteKey: /data/postgres/users/app/username
    passwordRemoteKey: /data/postgres/users/app/password
  poolerAuth:
    enabled: true
    targetName: quickstart-pooler-auth
    userlistRemoteKey: /data/postgres/pooler/userlist.txt
  replicaSourcePassword:
    enabled: true
    targetName: primary-eu-password
    passwordRemoteKey: /data/postgres/replica/source/password
```

## CRD Wiring

| Use | CRD field | Required Secret keys |
|---|---|---|
| Managed role password | `PostgresUser.spec.passwordSecretRef.name` | `username`, `password` |
| PgBouncer supplied auth | `Pooler.spec.pgbouncer.authSecretRef.name` | `userlist.txt` |
| External replica source | `PostgresCluster.spec.externalClusters[].password` | `password` |

`PostgresUser` requires `data.username` to match `spec.name`; this is checked
by the reconciler before it applies password SQL.

## Verify

```bash
kubectl get externalsecret -n <ns>
kubectl describe externalsecret quickstart-app-password -n <ns>
kubectl get secret quickstart-app-password -n <ns>
kubectl get postgresuser quickstart-app -n <ns> -o yaml | yq '.status'
```

For Pooler auth, verify the ExternalSecret first, then check that the Pooler
hash changes after the Secret refresh:

```bash
kubectl get externalsecret quickstart-pooler-auth -n <ns>
kubectl get pooler quickstart-rw -n <ns> -o jsonpath='{.status.configHash}'
```

## Rollback

Disable only the chart materialization and keep the already-created Kubernetes
Secret in place:

```bash
helm upgrade <release> charts/postgres-operator \
  --reuse-values \
  --set externalSecrets.enabled=false
```

Then confirm the CRDs still reference an existing Secret name:

```bash
kubectl get postgresuser,pooler,postgrescluster -n <ns> -o yaml | grep -E 'passwordSecretRef|authSecretRef|password:'
```
