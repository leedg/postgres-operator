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
    check_contains("docs/FEATURE_DEEP_DIVE.md", ["SnapshotWAL", "Bootstrap", "InitialCopy", "CDCCatchup", "RoutingUpdate", "Cleanup", "Promote"])
    check_contains("docs/PROJECT_OVERVIEW.md", ["ShardSplitJobReconciler", "pg-router"])

    if "ShardSplitJobReconciler" not in read("cmd/main.go"):
        fail("cmd/main.go: ShardSplitJobReconciler is not registered")
    stale_patterns = {
        "README.md": [r"no controllers exist", r"design-only"],
        "docs/FEATURE_DEEP_DIVE.md": [r"빈 scaffolding", r"미래 샤딩 설계"],
        "docs/PROJECT_OVERVIEW.md": [r"컨트롤러 미구현", r"CRD만, 컨트롤러 구현 예정"],
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
