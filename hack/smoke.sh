#!/usr/bin/env bash
# hack/smoke.sh — kind 환경에서 quickstart sample 을 적용하고 postgres Pod 가
# Ready 가 될 때까지 검증하는 스모크 스크립트.
#
# 의존:
#   - kind, kubectl, helm, docker
#   - GHCR 또는 로컬 빌드의 operator + pg 이미지
#
# 사용:
#   ./hack/smoke.sh [--keep]  # --keep 이면 종료 후에도 kind cluster 유지
#
# 흐름:
#   1. kind cluster 생성 (이미 있으면 재사용)
#   2. operator + PG image 로컬 빌드 후 kind 에 load
#   3. CRD + operator 설치 (kustomize 또는 helm)
#   4. quickstart sample apply
#   5. Pod Ready 대기 (5분 timeout)
#   6. psql round-trip 검증 (`kubectl exec ... -- psql -c 'SELECT 1'`)
#   7. cleanup (--keep 미지정 시 cluster 삭제)

set -euo pipefail

KEEP=0
if [[ "${1:-}" == "--keep" ]]; then
    KEEP=1
fi

CLUSTER_NAME="${CLUSTER_NAME:-postgres-operator-smoke}"
# NS 는 sample CR (config/samples/...) 의 metadata.namespace 와 정합돼야 한다.
# sample 이 'default' 로 hardcoded 이므로 env override 를 받지 않는다 (false-negative
# 회피 — 이전 cycle 에서 부모 shell 의 NS=dev 가 STS wait 를 dev 로 보내 timeout).
NS="default"
CR_NAME="${CR_NAME:-quickstart}"
PG_IMG="${PG_IMG:-ghcr.io/keiailab/pg:18}"
# install.yaml 이 config/manager/kustomization.yaml 의 newTag 를 사용하고, 그 값은
# charts/postgresql-operator/Chart.yaml 의 appVersion 과 동기화돼 있다 (Makefile §3 IMAGE_TAG).
# smoke.sh 가 다른 태그 (예: ":smoke") 로 빌드/로드하면 kubelet 이 install.yaml 의 태그를
# pull 하려다 실패한다. drift 방지를 위해 단일 출처에서 태그 도출.
OPERATOR_TAG="${OPERATOR_TAG:-$(awk '/^appVersion:/ { gsub(/"/, "", $2); print $2; exit }' charts/postgresql-operator/Chart.yaml)}"
OPERATOR_IMG="${OPERATOR_IMG:-ghcr.io/keiailab/postgres-operator:${OPERATOR_TAG}}"

log() { printf '\n[smoke] %s\n' "$*" >&2; }

cleanup() {
    if [[ "$KEEP" == "0" ]]; then
        log "Deleting kind cluster $CLUSTER_NAME"
        kind delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || true
    else
        log "Cluster $CLUSTER_NAME 유지 (--keep). 수동 삭제: kind delete cluster --name $CLUSTER_NAME"
    fi
}
trap cleanup EXIT

# 1. kind cluster
if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
    log "Creating kind cluster $CLUSTER_NAME"
    kind create cluster --name "$CLUSTER_NAME"
else
    log "Reusing existing kind cluster $CLUSTER_NAME"
fi
kubectl cluster-info --context "kind-${CLUSTER_NAME}"

# 2. images — local build + kind load
log "Building operator image $OPERATOR_IMG"
docker build -t "$OPERATOR_IMG" .
log "Building PG image $PG_IMG"
docker build -f Dockerfile.pg --build-arg PG_MAJOR=18 -t "$PG_IMG" .
log "Loading images into kind"
kind load docker-image "$OPERATOR_IMG" --name "$CLUSTER_NAME"
kind load docker-image "$PG_IMG" --name "$CLUSTER_NAME"

# 3. CRD + operator 설치 (kustomize 결과 dist/install.yaml 사용)
log "Generating dist/install.yaml + applying"
make build-installer >/dev/null
# operator image override — local kind 에서는 IfNotPresent 로 로딩 이미지 사용.
kubectl apply -f dist/install.yaml

# operator Pod Ready 대기
log "Waiting for operator manager Pod"
kubectl -n postgresql-operator-system wait --for=condition=Available deployment \
    -l control-plane=controller-manager --timeout=180s

# 4. sample CR
log "Applying quickstart sample"
kubectl apply -f config/samples/postgres_v1alpha1_postgrescluster_dev.yaml

# 5. Pod Ready 대기 (5분 timeout — initdb + 첫 부팅 여유)
STS_NAME="${CR_NAME}-shard-0"
log "Waiting for StatefulSet $STS_NAME to have ReadyReplicas >= 1"
end=$(( $(date +%s) + 300 ))
while [[ $(date +%s) -lt $end ]]; do
    ready=$(kubectl -n "$NS" get sts "$STS_NAME" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)
    if [[ "${ready:-0}" -ge 1 ]]; then
        break
    fi
    sleep 5
done
if [[ "${ready:-0}" -lt 1 ]]; then
    log "ERROR: StatefulSet did not become Ready in 5 minutes"
    kubectl -n "$NS" describe sts "$STS_NAME" || true
    kubectl -n "$NS" get pods -l "app.kubernetes.io/instance=${CR_NAME}" -o wide || true
    kubectl -n "$NS" logs "${STS_NAME}-0" -c postgres --tail=200 || true
    exit 1
fi

# 6. psql round-trip
POD="${STS_NAME}-0"
log "Running psql round-trip in $POD"
out=$(kubectl -n "$NS" exec "$POD" -c postgres -- psql -h /var/run/postgresql -U postgres -d postgres -At -c 'SELECT 1' 2>&1 || true)
if [[ "$out" != "1" ]]; then
    log "ERROR: psql round-trip failed: $out"
    exit 1
fi

log "SUCCESS — quickstart cluster Ready, psql SELECT 1 = 1"
log "Cluster status:"
kubectl -n "$NS" get postgrescluster "$CR_NAME" -o yaml | tail -40
