# RFC 0001 — CRD Schema (v1alpha1)

- **Status**: Draft
- **Submitted**: 2026-04-26
- **Authors**: @keiailab/maintainers (TBD assigned)
- **Comment window**: 14 days (closes 2026-05-10)
- **Approval criteria**: ≥2/3 of maintainers in favor (GOVERNANCE.md "architecture change" procedure)
- **Related**: ADR 0001 (Citus standard + QueryRouter), ADR 0002 (no Patroni), ADR 0003 (QueryRouter Stateless)
- **Prior artifact**: Analysis plan A1
- **Following RFCs**: 0002 (Metadata Sync), 0005 (Router Statelessness Gates), 0006 (Security/RBAC), 0007 (Observability), 0008 (Distributed Tables)

## Context

Phase 1 produces the `PostgresCluster` CRD definition and static bootstrap (StatefulSet/Deployment creation). Before Phase 1 coding begins, the **group/version/name, required fields, and validation rules** of all CRDs must be fixed in order to prevent the following.

1. **Irreversible decisions that affect the entirety of Phases 1~13** — changes to group name/version break compatibility post v1 GA.
2. **Inter-controller dependencies** — `DistributedTable` reconcilers hold `PostgresCluster` as owner reference, so naming and identification conventions of the two CRDs must be consistent.
3. **Webhook validation gaps** — if the 5 statelessness conditions in ADR 0003 are not expressed at the CRD schema layer, there is risk of runtime bypass.

This RFC finalizes the skeleton of every v1alpha1 CRD at once. The **semantics of non-Phase 1 CRDs (e.g., DistributedTable colocation policy)** are delegated to following RFCs (such as 0008), but **type signatures and field names are frozen in this RFC**.

## Decision

### 1. Group, version, and domain

| Item | Value | Rationale |
|---|---|---|
| API Group | `postgres.keiailab.io` | ADR 0001 §CRD root representation example, `PROJECT.domain=keiailab.io` |
| Version | `v1alpha1` | breaking changes allowed pre-GA |
| Layout | `go.kubebuilder.io/v4`, multigroup=false | the `PROJECT` file as is |
| Scope | Namespaced (all CRDs) | eases multi-tenancy and RBAC separation |

### 2. CRD list (v1alpha1 freeze targets)

| Kind | Introduced in Phase | Owner | Scope of this RFC |
|---|---|---|---|
| `PostgresCluster` | 1 | (root) | **Detailed schema frozen** |
| `DistributedTable` | 5 | `PostgresCluster` | Only type signature frozen; semantics in RFC 0008 |
| `ReferenceTable` | 5 | `PostgresCluster` | Only type signature frozen; semantics in RFC 0008 |
| `RebalanceJob` | 6 | `PostgresCluster` | Only type signature frozen |
| `ShardPlacementPolicy` | 7 | `PostgresCluster` | Only type signature frozen |
| `BackupJob` | 4 | `PostgresCluster` | Only type signature frozen; semantics in RFC 0004 |
| `PgUser` | 8 | `PostgresCluster` | Only type signature frozen; RBAC in RFC 0006 |
| `PgDatabase` | 8 | `PostgresCluster` | Only type signature frozen |

### 3. PostgresCluster detailed schema (Phase 1 minimum set)

```go
// api/v1alpha1/postgrescluster_types.go (conceptual draft)

type PostgresCluster struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   PostgresClusterSpec   `json:"spec"`
    Status PostgresClusterStatus `json:"status,omitempty"`
}

type PostgresClusterSpec struct {
    // Required: PG/Citus version. Must match a supported combination in matrix.go.
    Version VersionSpec `json:"version"`

    // Required: Coordinator HA RS.
    Coordinator CoordinatorSpec `json:"coordinator"`

    // Required: 1+ Worker pool. Each pool is its own HA RS.
    Workers []WorkerPoolSpec `json:"workers"`

    // Required: stateless QueryRouter pool (replicas >= 1).
    Routers RouterSpec `json:"routers"`

    // Optional: PG/Citus extensions to enable (e.g., pgvector, pg_cron, postgis).
    Extensions []ExtensionSpec `json:"extensions,omitempty"`

    // Optional: development | production. Default production.
    // development relaxes members lower bound and storage validation (for quickstart).
    Deployment DeploymentMode `json:"deployment,omitempty"`
}

type VersionSpec struct {
    // "16" | "17" | "18". "18" requires feature gate "PostgresEighteen".
    Postgres string `json:"postgres"`
    // "12.1" | "13.0" etc. at minor granularity.
    Citus string `json:"citus"`
}

type CoordinatorSpec struct {
    // Only odd allowed (split-brain prevention, ADR 0003 §enforcement).
    // production: members >= 3 recommended. development: members=1 allowed.
    Members int32 `json:"members"`
    Storage StorageSpec `json:"storage"`
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`
    // Optional: ShouldHaveShards. Default false (ADR 0003 §Coordinator).
    ShouldHaveShards *bool `json:"shouldHaveShards,omitempty"`
}

type WorkerPoolSpec struct {
    // Pool identifier. Unique within a cluster. DNS-1123 label.
    Name string `json:"name"`
    // Only odd allowed. production: members >= 3.
    Members int32 `json:"members"`
    Storage StorageSpec `json:"storage"`
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type RouterSpec struct {
    // replicas >= 1. When HPA is attached, manage via a separate HPA CR (no HPA field in this CRD).
    Replicas int32 `json:"replicas"`
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`
    // PgBouncer sidecar settings.
    PgBouncer PgBouncerSpec `json:"pgbouncer,omitempty"`

    // ❌ Storage field intentionally absent — ADR 0003 statelessness enforced.
    // ❌ ShouldHaveShards field absent — always false enforced.
}

type StorageSpec struct {
    Size resource.Quantity `json:"size"`
    StorageClassName *string `json:"storageClassName,omitempty"`
    AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

type ExtensionSpec struct {
    Name string `json:"name"`              // e.g., "pgvector"
    Version string `json:"version,omitempty"`
}

type PgBouncerSpec struct {
    // transaction (default) | session | statement.
    PoolMode string `json:"poolMode,omitempty"`
    // Backend connection upper limit (per Pod).
    MaxClientConn *int32 `json:"maxClientConn,omitempty"`
}

type DeploymentMode string
const (
    DeploymentProduction DeploymentMode = "production" // default
    DeploymentDevelopment DeploymentMode = "development"
)
```

#### Status

```go
type PostgresClusterStatus struct {
    // Standard conditions: Ready, CoordinatorReady, WorkersReady, RoutersReady, MetadataInSync.
    Conditions []metav1.Condition `json:"conditions,omitempty"`

    // Current topology state (reflects metadata sync results).
    Topology TopologyStatus `json:"topology,omitempty"`

    // Active channel (stable | beta | preview-pg18 in matrix.go).
    Channel string `json:"channel,omitempty"`

    // Hash of the last reconciled spec (drift tracking).
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type TopologyStatus struct {
    Coordinator NodeStatus `json:"coordinator"`
    Workers []WorkerPoolStatus `json:"workers,omitempty"`
    Routers RouterPoolStatus `json:"routers"`
}

type NodeStatus struct {
    Primary string `json:"primary,omitempty"`         // Pod name
    Replicas []string `json:"replicas,omitempty"`
    LeaseHolder string `json:"leaseHolder,omitempty"` // K8s lease holder
}

type WorkerPoolStatus struct {
    Name string `json:"name"`
    NodeStatus
    // (host, port, shouldhaveshards) registered in pg_dist_node.
    DistNode *DistNodeRef `json:"distNode,omitempty"`
}

type RouterPoolStatus struct {
    ReadyReplicas int32 `json:"readyReplicas"`
    // Largest router_metadata_lag_seconds (max across all router pods).
    MaxMetadataLagSeconds *float64 `json:"maxMetadataLagSeconds,omitempty"`
}

type DistNodeRef struct {
    GroupId int32 `json:"groupId"`
    NodeName string `json:"nodeName"`
    NodePort int32 `json:"nodePort"`
    ShouldHaveShards bool `json:"shouldHaveShards"`
}
```

### 4. Validation rules (Validating Webhook candidates)

The following table partially overlaps with RFC 0005 (Router Statelessness Gates), but specifies all **constraints expressible at the CRD schema layer**.

| Rule | Reason | Source |
|---|---|---|
| `routers` lacks `volumeClaimTemplates`/`storage` fields → enforced at type level | ADR 0003 statelessness | Schema (field itself absent) |
| `coordinator.members` is odd, ≥1 | Split-brain prevention | ADR 0003 §enforcement, webhook |
| `workers[].members` is odd, ≥1 | Same | webhook |
| `routers.replicas` ≥ 1 | Availability | webhook |
| `(spec.version.postgres, spec.version.citus)` passes `matrix.IsSupported` | Compatibility | webhook + `internal/version/matrix.go:52` |
| `spec.version.postgres="18"` only allowed when feature gate `PostgresEighteen` is active | Isolated channel | `FeatureGate` branch in matrix.go |
| `workers[].name` unique within a cluster, DNS-1123 label | Identification | webhook |
| With `deployment=production`, `coordinator.members ≥ 3` and `workers[].members ≥ 3` | Operational safety | webhook |
| With `deployment=development`, only `routers.replicas`≥1 enforced; the rest relaxed | Quickstart UX | webhook |
| `extensions[].name` is on the allowlist (`pgvector`, `pg_cron`, `postgis`, ...) | Security and compatibility | webhook (expanded in Phase 12) |

### 5. Type signatures of other CRDs (semantics in following RFCs)

```go
// DistributedTable — Phase 5, semantics finalized in RFC 0008
type DistributedTableSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Database string `json:"database"`
    Schema string `json:"schema,omitempty"`        // default "public"
    Table string `json:"table"`
    Distribution DistributionSpec `json:"distribution"`
    ReplicationFactor *int32 `json:"replicationFactor,omitempty"`
}

type DistributionSpec struct {
    // Distribution column name. Empty string allowed for schema-based sharding.
    Column string `json:"column,omitempty"`
    // Number of shards. Default decided in RFC 0008.
    ShardCount *int32 `json:"shardCount,omitempty"`
    // Colocation group name.
    ColocationGroup string `json:"colocationGroup,omitempty"`
}

// ReferenceTable — Phase 5
type ReferenceTableSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Database string `json:"database"`
    Schema string `json:"schema,omitempty"`
    Table string `json:"table"`
}

// RebalanceJob — Phase 6
type RebalanceJobSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Window WindowSpec `json:"window"`
    // by_shard_count | by_disk_size
    Strategy string `json:"strategy"`
}
type WindowSpec struct {
    Cron string `json:"cron"`               // e.g., "0 2 * * *"
    DurationSeconds int32 `json:"durationSeconds"`
}

// ShardPlacementPolicy — Phase 7
type ShardPlacementPolicySpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Zones []ZoneSpec `json:"zones"`
    TagSelectors map[string]string `json:"tagSelectors,omitempty"`
}
type ZoneSpec struct {
    Name string `json:"name"`
    NodeSelector map[string]string `json:"nodeSelector"`
}

// BackupJob — Phase 4, RFC 0004
type BackupJobSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Storage BackupStorageSpec `json:"storage"`
    Schedule string `json:"schedule,omitempty"` // cron, empty string = one-shot
    Retention RetentionSpec `json:"retention"`
}
type BackupStorageSpec struct {
    // s3 | gcs | azure | pvc
    Type string `json:"type"`
    // Credentials, bucket, prefix are sub-structs per Type (finalized in RFC 0004).
    S3 *S3StorageSpec `json:"s3,omitempty"`
    PVC *PVCStorageSpec `json:"pvc,omitempty"`
}
type S3StorageSpec struct {
    Bucket string `json:"bucket"`
    Prefix string `json:"prefix,omitempty"`
    Region string `json:"region"`
    // SecretRef holds access_key/secret_key. Allowed nil with IRSA.
    SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}
type PVCStorageSpec struct {
    ClaimName string `json:"claimName"`
}
type RetentionSpec struct {
    KeepDays *int32 `json:"keepDays,omitempty"`
    KeepCount *int32 `json:"keepCount,omitempty"`
}

// PgUser, PgDatabase — Phase 8, RBAC policy in RFC 0006
type PgUserSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Username string `json:"username"`
    PasswordSecretRef corev1.SecretKeySelector `json:"passwordSecretRef"`
    // GRANT scope defined in RFC 0006. This RFC only freezes fields.
    Privileges []PrivilegeSpec `json:"privileges,omitempty"`
}
type PgDatabaseSpec struct {
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`
    Name string `json:"name"`
    Owner *string `json:"owner,omitempty"` // PgUser name
    Encoding string `json:"encoding,omitempty"` // default UTF8
    Locale string `json:"locale,omitempty"`
}
type PrivilegeSpec struct {
    On string `json:"on"`              // database | schema | table
    Target string `json:"target"`      // ref name
    Grants []string `json:"grants"`    // SELECT | INSERT | ALL | ...
}
```

### 6. Owner Reference convention

- All non-`PostgresCluster` CRDs reference the parent via `spec.clusterRef.name`.
- Reconcilers automatically set `metav1.OwnerReference`(controller=true) at creation time.
- Cascade delete is delegated to K8s GC (no separate finalizer). However, `BackupJob` holds an external storage cleanup finalizer (Phase 4 decision).

### 7. Open questions (deferred to following RFCs)

| Question | Delegation target |
|---|---|
| Default for `DistributionSpec.ShardCount` | RFC 0008 |
| First GA tool for `BackupStorageSpec.Type` (recommend pgBackRest) | RFC 0004 |
| Safe GRANT allowlist for `PgUser.Privileges` | RFC 0006 |
| Lease duration/renew values for Coordinator failover | RFC 0003 |
| Allowlist for `extensions[]` | RFC 0012 (Phase 12) |
| Recommended default for `RouterSpec.PgBouncer.MaxClientConn` | RFC 0007 (decided together with observability) |

## Rationale

### Why freeze all CRDs at once
- Group/version/Owner conventions must maintain compatibility until v1 GA. Risk of conflict from per-Phase decisions.
- Freezing type signatures lets following RFCs focus on semantics, raising RFC processing speed.
- Guarantees the `clusterRef` pattern is applied consistently across all child CRDs.

### Why we do not put `Storage`/`ShouldHaveShards` fields on RouterSpec
- The statelessness enforcement in ADR 0003 is more powerful as **type absence** than as webhook rejection.
- Without the type, users cannot write it in YAML, and the controller cannot process it in code paths.

### Why we enforce `members` as odd
- Explicitly stated in ADR 0003 §enforcement. K8s lease-based election risks split-brain with even numbers.

### `deployment` mode separation
- ADR 0003 §tradeoffs: guarantee a 5-minute quickstart in development. The webhook applies different validations per mode.

## Tradeoffs

- **Cost of changes during alpha**: since v1alpha1 allows free changes, but to change field names frozen by this RFC in subsequent minors requires alpha→alpha conversion.
- **Awkwardness of OwnerReference automation**: if `OwnerReferences` is automatically filled while the user specifies `clusterRef`, it may be counterintuitive. Mitigated by documentation.
- **Cognitive burden of separating type signature freeze from semantics**: situations like "this field exists in v1alpha1 but its behavior is only enabled in Phase N" occur. Mark each field with a `// Available: Phase N` comment.

## Enforcement mechanism

1. Reflect the type definitions of this RFC directly in `api/v1alpha1/*_types.go`.
2. Implement the §4 validation rules in `internal/webhook/postgrescluster_webhook.go`.
3. Auto-generate CRD YAML via `make manifests` and commit to `config/crd/bases/`.
4. Include ≥1 case of each validation rule violation in e2e tests (`test/e2e/`).
5. Auto-generate `docs/api/v1alpha1.md` (using `controller-gen` or `crd-ref-docs`).
6. CI gates: `golangci-lint`, `kubebuilder validate`, `make test` (coverage ≥ 80% per CONTRIBUTING.md).

## Consequences

- Freeze all CRD type signatures in v1alpha1 → guarantees type compatibility for Phase 1~13 reconciler writing.
- Only `PostgresCluster` has semantics fixed; the rest are deferred to following RFCs.
- Changes to this RFC (adding/removing fields, type changes) are handled per GOVERNANCE.md "minor change" (adding fields) or "architecture change" (removing fields/type changes).

## Verification (How to verify)

After this RFC is adopted, verify with the following:

```bash
cd /Users/phil/WorkSpace/public/postgresql-operator

# 1) Does CRD manifest generation from type definitions not break?
make manifests generate

# 2) Webhook unit tests (cases per validation rule)
make test

# 3) Sample CR apply/reject e2e
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_min.yaml      # accepted
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_router_pvc.yaml # expected rejection
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_even_members.yaml # expected rejection

# 4) Matrix verification
go test ./internal/version/... -run TestIsSupported
```

## Appendix A — Sample CR (development mode)

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata:
  name: quickstart
  namespace: default
spec:
  deployment: development
  version:
    postgres: "17"
    citus: "13.0"
  coordinator:
    members: 1
    storage:
      size: 10Gi
  workers:
  - name: pool-a
    members: 1
    storage:
      size: 20Gi
  routers:
    replicas: 1
```

## Appendix B — Sample CR (production mode)

```yaml
apiVersion: postgres.keiailab.io/v1alpha1
kind: PostgresCluster
metadata:
  name: prod-cluster
  namespace: pg-system
spec:
  deployment: production
  version:
    postgres: "17"
    citus: "13.0"
  coordinator:
    members: 3
    storage:
      size: 100Gi
      storageClassName: fast-ssd
    resources:
      requests: { cpu: "2", memory: "8Gi" }
  workers:
  - name: pool-a
    members: 3
    storage:
      size: 500Gi
      storageClassName: fast-ssd
    resources:
      requests: { cpu: "4", memory: "16Gi" }
  - name: pool-b
    members: 3
    storage:
      size: 500Gi
      storageClassName: fast-ssd
  routers:
    replicas: 3
    pgbouncer:
      poolMode: transaction
      maxClientConn: 1000
  extensions:
  - name: pgvector
    version: "0.7"
```

## Appendix C — Change history

| Date | Change | Author |
|---|---|---|
| 2026-04-26 | Draft submitted | @keiailab/maintainers |
| 2026-04-27 | Git tracking started + Addendum D added (Pillar mapping, delegation to RFC 0009, reflection of mission redefinition) | @keiailab/maintainers |

---

## Appendix D — Following RFC delegation + Pillar mapping (added 2026-04-27)

This RFC was written based on ADR 0001 v1 ("Citus + Stateless QueryRouter single differentiator"). In ADR 0001 v2 (2026-04-27) the mission was redefined to the three axes **"PGO-class full stack + Citus first-class + Plugin SDK"**, but **the type signatures frozen by this RFC are all still valid**. The scope of impact is limited to the following two.

### D.1 `QueryRouter` representation — delegated to RFC 0009

§3 of this RFC defined `RouterSpec` as a `PostgresClusterSpec.routers` subfield. With the Plugin SDK (`RouterPlugin`) introduction decided in ADR 0001 v2, **whether to separate into its own `QueryRouter` CRD** becomes a new subject of review. The decision is delegated to RFC 0009.

| Option | Pros | Cons |
|---|---|---|
| Subfield (current RFC) | Topology completed in the single `PostgresCluster` CR | RouterPlugin tightly coupled with the PostgresCluster reconciler |
| Separate CRD | RouterPlugin has independent reconciler lifecycle; multiple router pools possible | Need to synchronize two CRs; owner reference complexity |

### D.2 Pillar mapping

Pillar assignment (plan §10) of the 8 CRDs frozen by this RFC:

| CRD | Pillar | Introduction milestone |
|---|---|---|
| `PostgresCluster` | P1 | v0.1 alpha (P1-T1) |
| `BackupJob` | P4 | v0.5 beta |
| `PgUser`, `PgDatabase` | P8 | v0.5 beta |
| `ClusterUpgrade` | P9 | v0.7 beta (not in RFC 0001, follow-up RFC 0010) |
| `DistributedTable`, `ReferenceTable` | P11 | v0.5 beta (M1) |
| `RebalanceJob` | P11 | v0.7 beta |
| `ShardPlacementPolicy` | P11 | v0.7 beta |
| `QueryRouter` (when RFC 0009 decides) | P12 | v0.7 beta |

### D.3 Relationship to the Plugin SDK interface freeze

The 5 interfaces (`BackupPlugin`/`ExporterPlugin`/`ExtensionPlugin`/`RouterPlugin`/`AuthPlugin`) in `internal/plugin/api.go` based on ADR 0005 (to be written) do not directly reference the CRD signatures of this RFC. The interfaces are an internal calling convention for reconcilers, and CRD signatures are the user surface. The two freezes can **proceed independently**.

### D.4 Comment window

Original deadline 2026-05-10. This Addendum does not change body signatures, so the comment window does not need to restart (corresponds to the GOVERNANCE.md "supplementary change" category).
