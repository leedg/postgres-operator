# community-operators 채널 등록 가이드

본 문서는 keiailab/postgres-operator 의 OLM bundle 을
[k8s-operatorhub/community-operators](https://github.com/k8s-operatorhub/community-operators)
저장소에 PR 형태로 등록하는 절차를 정리한다. OperatorHub.io 와
[Artifact Hub OLM 채널](https://artifacthub.io/packages/olm/community-operators/postgresql)
에 자동 동기화되는 것이 목적이다.

## 사전 조건

| 항목 | 상태 |
|---|---|
| OLM bundle 생성 (operator-sdk) | ✅ `make bundle VERSION=0.3.0-alpha.18` |
| bundle validate (default suite) | ✅ `operator-sdk bundle validate ./bundle` |
| bundle validate (operatorframework suite) | ✅ `make validate` 게이트가 자동 실행 |
| CSV alm-examples 7 종 owned CRD 정합 | ✅ T26 (2026-05-12) |
| CSV `customresourcedefinitions.owned` 8 종 description | ✅ T26 (2026-05-12) |
| CHANGELOG `[0.3.0-alpha.N]` entry | ✅ release tag 시점 동기 |
| Chart appVersion ↔ kustomize newTag ↔ dist image tag 정합 | ✅ `make validate` 가 drift assertion |
| LICENSE / SECURITY / SUPPORT / CONTRIBUTING / CODE_OF_CONDUCT | ✅ 표준 부합성 |

## bundle 이미지 빌드 + push

bundle 이미지는 `bundle.Dockerfile` 을 기반으로 단일 amd64 platform 으로
빌드한다 (multi-arch 금지 — RFC-0002 §2).

```bash
make bundle VERSION=0.3.0-alpha.18
make bundle-build VERSION=0.3.0-alpha.18

docker push ghcr.io/keiailab/postgres-operator-bundle:0.3.0-alpha.18
```

`bundle-build` 는 `docker buildx build --platform linux/amd64 -f bundle.Dockerfile`
로 single-arch 이미지를 만든다. push 권한은 maintainer 한정.

## community-operators PR 절차

`k8s-operatorhub/community-operators` 의 `operators/postgres-operator/<version>/`
디렉토리에 본 repo 의 `bundle/` 을 byte-identical 로 복사한다.

```bash
# 1. fork + clone
gh repo fork k8s-operatorhub/community-operators --clone --remote
cd community-operators

# 2. 신규 버전 디렉토리
mkdir -p operators/postgres-operator/0.3.0-alpha.18
cp -r /path/to/postgres-operator/bundle/* \
      operators/postgres-operator/0.3.0-alpha.18/

# 3. ci.yaml (community-operators 메타데이터)
cat <<'YAML' > operators/postgres-operator/ci.yaml
---
updateGraph: replaces-mode
reviewers:
  - eightynine01
YAML

# 4. PR 본문 — 본 repo CHANGELOG[0.3.0-alpha.N] 내용 발췌
gh pr create \
  --repo k8s-operatorhub/community-operators \
  --base main \
  --title "operator postgres-operator (0.3.0-alpha.18)" \
  --body-file /path/to/postgres-operator/.github/PULL_REQUEST_TEMPLATE.md
```

community-operators 자동 CI 가 다음을 자동 검증한다:
- `operator-sdk bundle validate` (default + operatorframework suite).
- bundle 이미지 빌드 가능성.
- 채널 graph (`replaces` / `skips` / `skipRange`) 정합성.
- 정책 부합성 (LICENSE / `category` / `displayName` / `description`).

PR 머지 후 1-2 시간 안에 Artifact Hub OLM 채널과 OperatorHub.io 에 노출된다.

## upgrade graph 운영

`metadata.annotations` 의 `operators.operatorframework.io.bundle.channels.v1=alpha`
와 `default-channel=alpha` 가 본 repo bundle metadata 에 이미 들어 있다.
다음 릴리스부터는 CSV `spec.replaces` 또는 `spec.skips` 를 명시해서
0.3.0-alpha.18 → 0.3.0-alpha.19 의 upgrade path 를 OLM 이 추적할 수 있게 한다.

```yaml
# config/manifests/bases/postgres-operator.clusterserviceversion.yaml
spec:
  replaces: postgres-operator.v0.3.0-alpha.18
```

## 검증

PR 머지 후 약 1-2 시간 뒤:

```bash
# Artifact Hub OLM 채널 — version 노출 확인
curl -s "https://artifacthub.io/api/v1/packages/olm/community-operators/postgres-operator" \
  | jq '{available_versions: .available_versions[].version}'

# OperatorHub.io — UI 노출 확인 (browser)
open "https://operatorhub.io/operator/postgres-operator"
```

## 회귀 차단

향후 누군가가 OLM bundle 을 수정한 채 community-operators 에 PR 을 보내는
회귀를 차단하기 위해, 본 repo `make validate` 가 다음을 강제한다:

- `bundle/manifests/postgres.keiailab.io_*.yaml` 카운트 ≥ 8 (T26).
- `operator-sdk bundle validate ./bundle` 0 error (T26).
- `operator-sdk bundle validate --select-optional suite=operatorframework`
  0 error (T26).
- Chart appVersion ↔ kustomize newTag ↔ dist image tag 동기 (T26).

이 4 게이트가 통과한 상태에서만 release tag → bundle build → PR 흐름을
진행한다.

## 관련

- [ADR-0013](../kb/adr/_archive/v0.x/0013-operatorhub-bundle.md) — OperatorHub bundle scaffold.
- [RFC-0002](../rfcs/_archive/v0.x/) — GitHub Actions 금지 + 로컬 4 계층 게이트.
- [CHANGELOG.md](../../CHANGELOG.md) — 매 alpha cut 의 변경 요약.
- [release-process.md](../releases/release-process.md) — release tag 절차.
