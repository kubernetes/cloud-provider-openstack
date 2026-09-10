#!/usr/bin/env python3

# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# /// script
# requires-python = ">=3.11"
# dependencies = [
#   "rich",
#   "ruamel.yaml",
# ]
# ///
#
# Dependencies are resolved automatically via the PEP 723 metadata above.
# Run with uv:
#   uv run hack/bump-release.py
#
# Or with pipx:
#   pipx run hack/bump-release.py

import re
import subprocess
import sys
from pathlib import Path

from rich.console import Console
from rich.prompt import Confirm
from ruamel.yaml import YAML

console = Console()

CHART_FILES = sorted(Path("charts").glob("*/Chart.yaml"))
RELEASE_DIRS = [Path("docs"), Path("manifests"), Path("examples")]
TEXT_SUFFIXES = {".yaml", ".yml", ".md", ".sh", ".json", ".txt", ".conf"}

APP_VERSION_RE = re.compile(r"^v(?P<major>\d+)\.(?P<x>\d+)\.(?P<y>\d+)$")
VERSION_RE = re.compile(r"^(?P<major>\d+)\.(?P<x>\d+)\.(?P<z>\d+)$")
VERSION_MASTER_RE = re.compile(r"^(?P<major>\d+)\.(?P<x>\d+)\.0-dev$")


def git(*args: str) -> str:
    return subprocess.run(
        ["git", *args], capture_output=True, text=True, check=True
    ).stdout.strip()


def git_check(*args: str) -> bool:
    return (
        subprocess.run(["git", *args], capture_output=True, check=False).returncode == 0
    )


def find_base_branch() -> tuple[str, str] | tuple[None, None]:
    """Return (branch_name, merge_base_commit) for the closest ancestor of HEAD.

    branch_name is 'master' or a 'release-N.N' name. Checks all remotes so
    the result is not tied to a specific remote name.
    """
    # Build a map of logical branch name -> remote refs across all remotes.
    # A logical branch (e.g. "release-1.36") may exist on several remotes.
    candidates: dict[str, list[str]] = {}
    for ref in git(
        "for-each-ref", "--format=%(refname:short)", "refs/remotes/"
    ).splitlines():
        if ref.endswith("/HEAD"):
            continue
        logical = ref.split("/", 1)[-1]
        if logical == "master" or re.fullmatch(r"release-\d+\.\d+", logical):
            candidates.setdefault(logical, []).append(ref)

    best_name: str | None = None
    best_merge_base: str | None = None

    for logical_name, refs in candidates.items():
        for full_ref in refs:
            try:
                mb = git("merge-base", "HEAD", full_ref)
            except subprocess.CalledProcessError:
                continue
            if best_merge_base is None or git_check(
                "merge-base", "--is-ancestor", best_merge_base, mb
            ):
                best_name, best_merge_base = logical_name, mb

    if best_name is None:
        return None, None
    return best_name, best_merge_base


def load_chart(path: Path) -> tuple[YAML, dict]:
    yaml = YAML()
    yaml.preserve_quotes = True
    with path.open() as f:
        return yaml, yaml.load(f)


def read_app_version() -> str:
    """Return appVersion, validating all charts agree on it."""
    _, ref = load_chart(CHART_FILES[0])
    app_version = ref["appVersion"]

    for path in CHART_FILES[1:]:
        _, data = load_chart(path)
        if data["appVersion"] != app_version:
            sys.exit(
                f"ERROR: {path} has a different appVersion from {CHART_FILES[0]}:\n"
                f"  {data['appVersion']!r} vs {app_version!r}"
            )

    return app_version


def update_chart(path: Path, new_app_version: str, new_version: str) -> None:
    yaml, data = load_chart(path)
    data["appVersion"] = new_app_version
    data["version"] = new_version
    with path.open("w") as f:
        yaml.dump(data, f)


def update_files(old_version: str, new_version: str) -> None:
    """Replace old_version with new_version in all text files under RELEASE_DIRS."""
    for directory in RELEASE_DIRS:
        for path in sorted(directory.rglob("*")):
            if not path.is_file() or path.suffix not in TEXT_SUFFIXES:
                continue
            text = path.read_text()
            if old_version in text:
                path.write_text(text.replace(old_version, new_version))
                console.print(f"  Updated [dim]{path}[/dim]")


def release_master() -> None:
    app_version = read_app_version()

    m_app = APP_VERSION_RE.match(app_version)
    if not m_app:
        sys.exit(f"ERROR: Expected appVersion to match 'v1.X.Y', got: {app_version!r}")

    app_x = int(m_app.group("x"))
    next_x = app_x + 1
    next_tag = f"1.{next_x}.0"
    new_app_version = f"v{next_tag}"

    console.print("[dim]Detected base branch:[/dim] [bold]master[/bold]")
    console.print(
        "[dim]Note: CPO patch releases are made from [bold]release-X.Y[/bold] branches. "
        "Continuing will bump the Helm charts and all image references for a new Kubernetes "
        f"major version ([bold]{next_tag}[/bold]). This is typically done just before "
        "cutting a new release branch.[/dim]"
    )
    console.print()
    if not Confirm.ask(f"Release Kubernetes major version {next_tag}?", default=False):
        print("Exiting.")
        sys.exit(0)
    print()

    print(f"Releasing {next_tag}")
    print(f"  appVersion: {app_version} -> {new_app_version}")

    for path in CHART_FILES:
        _, data = load_chart(path)
        current_version = data["version"]

        m_ver = VERSION_MASTER_RE.match(current_version)
        if not m_ver:
            sys.exit(
                f"ERROR: {path.parent.name}: expected 'MAJOR.{next_x}.0-dev', "
                f"got: {current_version!r}"
            )
        if int(m_ver.group("x")) != next_x:
            sys.exit(
                f"ERROR: {path.parent.name}: expected version minor {next_x}, "
                f"got: {current_version!r}"
            )

        new_version = f"{m_ver.group('major')}.{next_x}.0"
        print(f"  {path.parent.name}: version {current_version} -> {new_version}")
        update_chart(path, new_app_version, new_version)

    print("Updating image references:")
    update_files(app_version, new_app_version)


def changed_files(merge_base: str) -> list[str]:
    return git("diff", "--name-only", merge_base, "HEAD").splitlines()


def release_stable_full(base_branch: str) -> None:
    """Full CPO release: bump appVersion, chart versions, and image references."""
    app_version = read_app_version()

    m_app = APP_VERSION_RE.match(app_version)
    if not m_app:
        sys.exit(f"ERROR: Expected appVersion to match 'v1.X.Y', got: {app_version!r}")

    app_x, app_y = int(m_app.group("x")), int(m_app.group("y"))
    new_app_version = f"v{m_app.group('major')}.{app_x}.{app_y + 1}"

    console.print(f"[dim]Detected base branch:[/dim] [bold]{base_branch}[/bold]")
    console.print(
        f"[dim]Changes to CPO code detected. Continuing will release a new CPO patch version "
        f"([bold]{new_app_version}[/bold]) and bump all Helm chart versions.[/dim]"
    )
    console.print()
    if not Confirm.ask(f"Release CPO {new_app_version}?", default=False):
        print("Exiting.")
        sys.exit(0)
    print()

    print("Bumping versions:")
    print(f"  appVersion: {app_version} -> {new_app_version}")

    for path in CHART_FILES:
        _, data = load_chart(path)
        current_version = data["version"]

        m_ver = VERSION_RE.match(current_version)
        if not m_ver:
            sys.exit(
                f"ERROR: {path.parent.name}: expected version 'MAJOR.X.Z' (no -dev suffix), "
                f"got: {current_version!r}"
            )
        if int(m_ver.group("x")) != app_x:
            sys.exit(
                f"ERROR: {path.parent.name}: minor version mismatch with "
                f"appVersion ({app_version}): {current_version!r}"
            )

        new_version = f"{m_ver.group('major')}.{app_x}.{int(m_ver.group('z')) + 1}"
        print(f"  {path.parent.name}: version {current_version} -> {new_version}")
        update_chart(path, new_app_version, new_version)

    print("Updating image references:")
    update_files(app_version, new_app_version)


def release_stable_chart_only(
    base_branch: str, files: list[str]
) -> None:
    """Chart-only release: bump chart versions without touching appVersion or manifests."""
    changed_chart_names = {
        f.split("/")[1] for f in files if f.startswith("charts/") and f.count("/") >= 2
    }
    affected = [p for p in CHART_FILES if p.parent.name in changed_chart_names]
    if not affected:
        print("No chart files changed. Nothing to release.")
        sys.exit(0)

    app_version = read_app_version()

    m_app = APP_VERSION_RE.match(app_version)
    if not m_app:
        sys.exit(f"ERROR: Expected appVersion to match 'v1.X.Y', got: {app_version!r}")

    app_x = int(m_app.group("x"))

    console.print(f"[dim]Detected base branch:[/dim] [bold]{base_branch}[/bold]")
    console.print(
        "[dim]No changes to CPO code detected. Continuing will bump only the Helm chart "
        "versions for changed charts (appVersion and image references will not change).[/dim]"
    )
    console.print()
    if not Confirm.ask("Release chart-only update?", default=False):
        print("Exiting.")
        sys.exit(0)
    print()

    print("Bumping chart versions:")

    for path in affected:
        _, data = load_chart(path)
        current_version = data["version"]

        m_ver = VERSION_RE.match(current_version)
        if not m_ver:
            sys.exit(
                f"ERROR: {path.parent.name}: expected version 'MAJOR.X.Z' (no -dev suffix), "
                f"got: {current_version!r}"
            )
        if int(m_ver.group("x")) != app_x:
            sys.exit(
                f"ERROR: {path.parent.name}: minor version mismatch with "
                f"appVersion ({app_version}): {current_version!r}"
            )

        new_version = f"{m_ver.group('major')}.{app_x}.{int(m_ver.group('z')) + 1}"
        print(f"  {path.parent.name}: version {current_version} -> {new_version}")
        update_chart(path, app_version, new_version)


def release_stable(base_branch: str, merge_base: str) -> None:
    files = changed_files(merge_base)
    has_code_changes = any(
        f.startswith("cmd/") or f.startswith("pkg/") for f in files
    )
    has_chart_changes = any(f.startswith("charts/") for f in files)

    if not has_code_changes and not has_chart_changes:
        print(
            f"No changes to CPO code or charts detected since branching from {base_branch}. "
            "Nothing to release."
        )
        sys.exit(0)

    if has_code_changes:
        release_stable_full(base_branch)
    else:
        release_stable_chart_only(base_branch, files)


def main() -> None:
    if not CHART_FILES:
        sys.exit(
            "ERROR: No Chart.yaml files found under charts/. Run from the repo root."
        )

    base_branch, merge_base = find_base_branch()

    if base_branch == "master":
        release_master()
    elif base_branch is not None and merge_base is not None:
        release_stable(base_branch, merge_base)
    else:
        sys.exit(
            "ERROR: Could not determine a base branch.\n"
            "Releases should be made from master or a stable branch (release-X.Y)."
        )

    print("Done.")


if __name__ == "__main__":
    main()
