from __future__ import annotations

import os
import subprocess
from pathlib import Path


_WARMUP = '''"""Five-replica SafeLane demo service."""

from __future__ import annotations

from fastapi import FastAPI

app = FastAPI()

SERVICE_NAME = "demo-api"

@app.get("/ready")
def ready() -> dict[str, bool]:
    return {"ready": True}

# Public compatibility contract used by the trusted probe.

@app.get("/v1/quote")
def quote() -> dict[str, str]:
    return {"quote": "EGP 50.00"}
'''

_FAST = '''"""Five-replica SafeLane demo service."""

from __future__ import annotations

from fastapi import FastAPI

app = FastAPI()

SERVICE_NAME = "demo-api"

@app.get("/ready")
def ready() -> dict[str, bool]:
    return {"ready": True}

# Public compatibility contract used by the trusted probe.

@app.get("/v1/quote")
def quote() -> dict[str, str]:
    payload = {"quote": "EGP 50.00"}
    return dict(payload)
'''

_STRICT = '''"""Five-replica SafeLane demo service."""

from __future__ import annotations

from fastapi import FastAPI

app = FastAPI()

SERVICE_NAME = "demo-api"

@app.get("/ready")
def ready() -> dict[str, bool]:
    return {"ready": True}

# Public compatibility contract used by the trusted probe.

# Version two replaces the consumer-facing route.
@app.get("/v2/quote")
def quote() -> dict[str, str]:
    payload = {"quote": "EGP 50.00"}
    return dict(payload)
'''


def _run(repo: Path, *args: str, env: dict[str, str] | None = None) -> str:
    result = subprocess.run(
        ["git", "-C", str(repo), *args],
        check=True,
        capture_output=True,
        text=True,
        env=env,
    )
    return result.stdout.strip()


def create_demo_repository(target: Path) -> dict[str, str]:
    """Create the frozen three-revision demo repository at an empty target."""
    if target.exists() and any(target.iterdir()):
        raise ValueError(f"demo repository target is not empty: {target}")
    target.mkdir(parents=True, exist_ok=True)
    _run(target, "init", "--initial-branch=main")
    _run(target, "config", "user.name", "SafeLane Fixture")
    _run(target, "config", "user.email", "fixture@safelane.dev")
    app = target / "src" / "demo_api" / "app.py"
    app.parent.mkdir(parents=True)

    revisions: dict[str, str] = {}
    commits = [
        ("warmup", "2026-08-09T08:00:00Z", "demo: add healthy warm-up revision", _WARMUP),
        ("fast", "2026-08-09T08:01:00Z", "demo: copy quote response before return", _FAST),
        ("strict", "2026-08-09T08:02:00Z", "demo: rename public quote route", _STRICT),
    ]
    for name, timestamp, message, source in commits:
        app.write_text(source, encoding="utf-8", newline="\n")
        _run(target, "add", "src/demo_api/app.py")
        commit_env = os.environ.copy()
        commit_env.update(
            {
                "GIT_AUTHOR_NAME": "SafeLane Fixture",
                "GIT_AUTHOR_EMAIL": "fixture@safelane.dev",
                "GIT_COMMITTER_NAME": "SafeLane Fixture",
                "GIT_COMMITTER_EMAIL": "fixture@safelane.dev",
                "GIT_AUTHOR_DATE": timestamp,
                "GIT_COMMITTER_DATE": timestamp,
                "TZ": "UTC",
            }
        )
        _run(target, "commit", "-m", message, env=commit_env)
        revisions[name] = _run(target, "rev-parse", "HEAD")
    return revisions
