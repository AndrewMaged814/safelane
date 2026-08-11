from __future__ import annotations

import json
import subprocess
from pathlib import Path

from safelane.demo_repository import create_demo_repository


def git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def test_demo_repository_has_frozen_linear_warmup_fast_strict_history(tmp_path: Path) -> None:
    repo = tmp_path / "demo-repository"

    revisions = create_demo_repository(repo)

    assert list(revisions) == ["warmup", "fast", "strict"]
    assert git(repo, "rev-list", "--count", "HEAD") == "3"
    assert git(repo, "rev-parse", f'{revisions["fast"]}^') == revisions["warmup"]
    assert git(repo, "rev-parse", f'{revisions["strict"]}^') == revisions["fast"]
    assert git(repo, "show", f'{revisions["fast"]}:src/demo_api/app.py').count('/v1/quote') == 1
    strict_source = git(repo, "show", f'{revisions["strict"]}:src/demo_api/app.py')
    assert '/v1/quote' not in strict_source
    assert strict_source.count('/v2/quote') == 1

    manifest = json.loads((Path("demo") / "revisions.json").read_text(encoding="utf-8"))
    assert revisions == manifest


def test_additive_route_is_evaluation_only() -> None:
    diff = (Path("demo") / "evaluation" / "additive-route.diff").read_text(encoding="utf-8")

    assert '+@app.get("/v2/quote")' in diff
    assert '-@app.get("/v1/quote")' not in diff
    assert "/v1/quote" in diff
