#!/usr/bin/env python3
"""Check that public feature documentation matches repository metadata."""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ERRORS: list[str] = []


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def fail(message: str) -> None:
    ERRORS.append(message)


def check_contains(path: str, needles: list[str]) -> None:
    text = read(path)
    for needle in needles:
        if needle not in text:
            fail(f"{path}: missing {needle!r}")


def main() -> int:
    readme = read("README.md")
    kinds: set[str] = set()
    for crd in (ROOT / "config/crd/bases").glob("*.yaml"):
        match = re.search(r"(?m)^\s{4}kind:\s*(\w+)\s*$", crd.read_text(encoding="utf-8"))
        if not match:
            fail(f"{crd.relative_to(ROOT)}: spec.names.kind not found")
        else:
            kinds.add(match.group(1))

    count_match = re.search(r"The chart installs \*\*(\d+) CRDs\*\*:", readme)
    if not count_match or int(count_match.group(1)) != len(kinds):
        fail(f"README.md: CRD count must be {len(kinds)}")
    table_match = re.search(r"The chart installs .*?\n\n(.*?)\n\n## Status", readme, re.S)
    documented = set(re.findall(r"(?m)^\| `([^`]+)` \|", table_match.group(1) if table_match else ""))
    if documented != kinds:
        fail(f"README.md: CRD table mismatch; missing={sorted(kinds-documented)}, extra={sorted(documented-kinds)}")

    latest_tag = subprocess.check_output(
        ["git", "tag", "--sort=-version:refname"], cwd=ROOT, text=True
    ).splitlines()[0]
    chart = read("charts/postgres-operator/Chart.yaml")
    chart_version = re.search(r"(?m)^version:\s*(\S+)", chart).group(1)
    app_version = re.search(r'(?m)^appVersion:\s*"?([^"\s]+)', chart).group(1)
    if latest_tag != f"v{app_version}":
        fail(f"Chart appVersion {app_version} does not match latest operator tag {latest_tag}")
    for path in ("README.md", "docs/PROJECT_OVERVIEW.md"):
        check_contains(path, [latest_tag, chart_version])

    check_contains("README.md", ["`ShardRange`", "`ShardSplitJob`", "`pg-router`", "GitHub", "canonical"])
    check_contains("README.md", ["ordinal `shard-N`", "cannot yet be selected as a later split source"])
    check_contains("docs/FEATURE_DEEP_DIVE.md", ["SnapshotWAL", "no-op placeholder", "snapshotLSN", "Bootstrap", "InitialCopy", "CDCCatchup", "RoutingUpdate", "Cleanup", "Promote"])
    snapshot_description = "현재 SnapshotWAL 은 no-op 이므로 컨트롤러가 이 값을 채우지 않는다."
    check_contains("api/v1alpha1/shardsplitjob_types.go", ["현재는 호환성을 위해 유지하는 no-op 전이 단계", snapshot_description])
    check_contains("config/crd/bases/postgres.keiailab.io_shardsplitjobs.yaml", [snapshot_description])
    check_contains("charts/postgres-operator/crds/postgres.keiailab.io_shardsplitjobs.yaml", [snapshot_description])
    check_contains("docs/sharding/SHARDING.md", ["no-op 예약 단계", "현재 구현 보장이 아니다"])
    check_contains("internal/router/resharding.go", ["SnapshotWAL은 현재 부수효과가 없는 예약 전이 단계다."])
    check_contains("docs/ROADMAP.md", ["현재는 no-op 예약 단계이며 `status.snapshotLSN`을 채우지 않는다."])
    check_contains("docs/ROADMAP.md", ["AllowForwardOnly=true", "완전한 post-cutover 자동 rollback 보장이 아니다."])
    if "internal/controller/shardsplit/" in read("api/v1alpha1/shardsplitjob_types.go"):
        fail("api/v1alpha1/shardsplitjob_types.go: references removed controller path")
    if "internal/controller/shardsplit/" in read("docs/ROADMAP.md"):
        fail("docs/ROADMAP.md: references removed controller path")
    check_contains("docs/kb/adr/0028-postgres-first-then-commons-dedup.md", ["해당 패키지는 이후 제거됐고", "현행 파일 경로를 주장하지 않는다"])
    check_contains("docs/FEATURE_DEEP_DIVE.md", ["cluster: my-cluster", "keyspace: orders", "vindex:", "lo:", "hi:", "sources: [shard-0]", "targets:", "shardID:"])
    check_contains("docs/PROJECT_OVERVIEW.md", ["ShardSplitJobReconciler", "pg-router", "CRD 목록 (10종)", "[현재 beta]"])

    if "ShardSplitJobReconciler" not in read("cmd/main.go"):
        fail("cmd/main.go: ShardSplitJobReconciler is not registered")
    stale_patterns = {
        "README.md": [r"no controllers exist", r"design-only"],
        "docs/FEATURE_DEEP_DIVE.md": [r"빈 scaffolding", r"미래 샤딩 설계"],
        "docs/PROJECT_OVERVIEW.md": [r"컨트롤러 미구현", r"CRD만, 컨트롤러 구현 예정", r"컨트롤러 없음", r"로드맵 전용", r"\[현재 GA\]", r"설계 단계"],
    }
    for path, patterns in stale_patterns.items():
        text = read(path)
        for pattern in patterns:
            if re.search(pattern, text, re.I):
                fail(f"{path}: stale claim matches {pattern!r}")

    for path, target in (
        ("docs/kb/adr/INDEX.md", "0008-operator-commons-adoption.md"),
        ("docs/BRANDING.md", "branding/symbol.png"),
        ("docs/BRANDING.md", "../LICENSE"),
    ):
        if not (ROOT / Path(path).parent / target).resolve().is_file():
            fail(f"{path}: linked target does not exist: {target}")

    artifacthub_block = re.search(r"artifacthub.io/crds: \|\n(.*?)\n  artifacthub.io/crdsExamples:", chart, re.S)
    artifacthub_kinds = set(re.findall(r"(?m)^    - kind: (\w+)$", artifacthub_block.group(1) if artifacthub_block else ""))
    if artifacthub_kinds != kinds:
        fail(f"Chart.yaml artifacthub.io/crds mismatch; missing={sorted(kinds-artifacthub_kinds)}, extra={sorted(artifacthub_kinds-kinds)}")
    changes = re.search(r"artifacthub.io/changes: \|\n(.*?)(?:\n\S|\Z)", chart, re.S)
    if not changes or app_version not in changes.group(1) or "0.4.0-beta.1" in changes.group(1):
        fail("Chart.yaml artifacthub.io/changes does not describe current appVersion")

    if ERRORS:
        for error in ERRORS:
            print(f"FAIL: {error}", file=sys.stderr)
        return 1
    print(
        f"PASS: {len(kinds)} CRDs, operator {latest_tag}, chart {chart_version}, "
        "implemented sharding docs, and audited links are synchronized"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
