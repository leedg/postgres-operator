- PodSecurity restricted enforcement
- NetworkPolicy default-deny + 명시 ingress/egress
- TLS auto-issuance (cert-manager) for client + replication
- Secret rotation (admin / replication / pgbouncer)
- SBOM + cosign signing

## Operational tasks

### Secret rotation

```bash
kubectl annotate postgrescluster <name> \
    postgres.keiailab.io/rotate-secrets=$(date +%s)
# operator reconcile → new Secret → rolling restart
```

### ExternalSecret readiness

```bash
kubectl get externalsecret -n <ns>
kubectl describe externalsecret quickstart-app-password -n <ns>
kubectl get secret quickstart-app-password -n <ns>
```

Check `ExternalSecret` readiness before decoding Secret data or rerunning
PostgresUser / Pooler reconciliation. See
[credential-sourcing.md](credential-sourcing.md) for the standard Infisical
mapping.

### NetworkPolicy audit

```bash
kubectl get networkpolicy -n <ns>
# 각 NetworkPolicy default-deny 적용 확인:
kubectl exec -it <test-pod> -- nc -zv <db-pod> 5432
```

### Image signature verify

```bash
cosign verify ghcr.io/keiailab/postgres-operator:<tag> \
    --certificate-identity-regexp 'https://github.com/keiailab/postgres-operator/.*' \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

## References

- ROADMAP.md G2 (Security defaults hardening) + G6 (SBOM + signing)
- D.11.5 SBOM + cosign
