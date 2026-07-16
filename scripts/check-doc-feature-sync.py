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
    check_contains("README.md", ["merge", "multiple sources are rejected", "automatic rollback are not implemented"])
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

    architecture_docs = (
        "docs/ARCHITECTURE.md",
        "docs/ARCHITECTURE.ko.md",
        "docs/ARCHITECTURE.ja.md",
        "docs/ARCHITECTURE.zh.md",
    )
    translated_readmes = (
        "docs/README.ko.md",
        "docs/README.ja.md",
        "docs/README.zh.md",
    )
    for path in architecture_docs + translated_readmes:
        check_contains(path, [app_version, chart_version, "ShardRange", "ShardSplitJob"])
        text = read(path)
        if not re.search(r"10 (?:owned )?CRD|10종|10 個|10 个", text):
            fail(f"{path}: current 10-CRD count is missing")
        stale_current_patterns = [r"v0\.4\.0-beta\.1", r"design-only", r"설계만", r"ランタイムコードはまだありません", r"暂无运行时代码"]
        if path in architecture_docs:
            stale_current_patterns.extend((r"8 (?:owned )?CRD", r"8종", r"8つ", r"8 个"))
        for pattern in stale_current_patterns:
            if re.search(pattern, text, re.I):
                fail(f"{path}: stale current-state claim matches {pattern!r}")

    future_markers = {
        "docs/ARCHITECTURE.md": "future target:",
        "docs/ARCHITECTURE.ko.md": "향후 목표:",
        "docs/ARCHITECTURE.ja.md": "将来目標:",
        "docs/ARCHITECTURE.zh.md": "未来目标:",
    }
    for path in architecture_docs:
        check_contains(path, ["distributed transaction coordinator", future_markers[path]])
    for path in translated_readmes:
        check_contains(path, ["License-MIT-blue.svg", "OperatorHub bundle", "2PC + saga"])
        marker = "향후 목표:" if ".ko." in path else "将来目標:" if ".ja." in path else "未来目标:"
        check_contains(path, [marker])
        if not re.search(r"8 (?:個|个)?\s*CRD", read(path)):
            fail(f"{path}: OperatorHub's current 8-CRD boundary is missing")
        if "License-Apache_2.0" in read(path):
            fail(f"{path}: license badge must match MIT LICENSE")

    controller = read("internal/controller/shardsplitjob_controller.go")
    for guard in (
        "if reason := unsupportedSplitSpecReason(&ssj); reason != \"\"",
        "func unsupportedSplitSpecReason",
        "if ssj.Spec.Direction == postgresv1alpha1.ShardSplitDirectionMerge",
        "if len(ssj.Spec.Sources) != 1",
    ):
        if guard not in controller:
            fail(f"internal/controller/shardsplitjob_controller.go: missing fail-closed guard {guard!r}")
    global_guard = controller.find('if reason := unsupportedSplitSpecReason(&ssj); reason != ""')
    first_phase_effect = controller.find("if ssj.Status.Phase == postgresv1alpha1.ShardSplitPhaseBootstrap")
    if global_guard < 0 or first_phase_effect < 0 or global_guard > first_phase_effect:
        fail("ShardSplitJob global support guard must run before every operational phase effect")

    check_contains("api/v1alpha1/shardsplitjob_types.go", [
        "!has(self.direction) || self.direction == 'split'",
        "size(self.sources) == 1",
        "merge direction is not implemented",
        'self == oldSelf',
        "spec is immutable after creation",
    ])
    for path in (
        "config/crd/bases/postgres.keiailab.io_shardsplitjobs.yaml",
        "charts/postgres-operator/crds/postgres.keiailab.io_shardsplitjobs.yaml",
    ):
        check_contains(path, [
            "!has(self.direction) || self.direction == ''split''",
            "size(self.sources) == 1",
            "merge direction is not implemented",
            "self == oldSelf",
            "spec is immutable after creation",
        ])
    source_crd = (ROOT / "config/crd/bases/postgres.keiailab.io_shardsplitjobs.yaml").read_bytes()
    chart_crd = (ROOT / "charts/postgres-operator/crds/postgres.keiailab.io_shardsplitjobs.yaml").read_bytes()
    if source_crd != chart_crd:
        fail("ShardSplitJob source and chart CRDs must be byte-identical")

    bundle_kinds: set[str] = set()
    for crd in (ROOT / "bundle/manifests").glob("postgres.keiailab.io_*.yaml"):
        match = re.search(r"(?m)^\s{4}kind:\s*(\w+)\s*$", crd.read_text(encoding="utf-8"))
        if match:
            bundle_kinds.add(match.group(1))
    if len(bundle_kinds) != 8 or {"ShardRange", "ShardSplitJob"} & bundle_kinds:
        fail(f"bundle/manifests: expected 8 legacy CRDs without sharding, got {sorted(bundle_kinds)}")

    for path in ("docs/FEATURE_DEEP_DIVE.md", "docs/ROADMAP.md"):
        if "shardsplitjob_cdc.go" in read(path):
            fail(f"{path}: references nonexistent shardsplitjob_cdc.go")
    check_contains("docs/kb/adr/0027-non-ordinal-reshard-target-shard-identity.md", [
        "Current implementation note (2026-07-16)",
        "자동 rollback, merge는 구현되지 않았다",
    ])

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
