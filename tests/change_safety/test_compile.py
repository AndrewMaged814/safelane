from __future__ import annotations

from pathlib import Path

from safelane.artifacts import validate_artifact
from safelane.change_safety import (
    ChangeSafety,
    PullRequestRef,
    ReleaseBinding,
    ResolutionCommand,
)

from .test_resolve import GuardedHost, NoFindingAnalyzer


def test_approved_decision_compiles_sha_bound_argo_rollout(tmp_path: Path) -> None:
    host = GuardedHost()
    safety = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=iter([
            "2026-08-12T09:00:00Z",
            "2026-08-12T09:05:00Z",
        ]).__next__,
    )
    assessed = safety.assess(PullRequestRef("acme/payments", 42))
    safety.resolve(ResolutionCommand(
        handle=assessed.handle,
        action="approve",
        selected_profile="Guarded",
        actor="andrew",
    ))

    bundle = safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image="ghcr.io/acme/payments@sha256:" + "c" * 64,
    ))

    manifest = bundle.manifest
    assert manifest["kind"] == "Rollout"
    assert manifest["metadata"]["namespace"] == "payments"
    assert manifest["metadata"]["annotations"]["safelane.dev/head-sha"] == "b" * 40
    assert manifest["metadata"]["annotations"]["safelane.dev/decision-sha256"] == bundle.decision_sha256
    assert manifest["spec"]["template"]["spec"]["containers"] == [{
        "name": "api",
        "image": "ghcr.io/acme/payments@sha256:" + "c" * 64,
    }]
    assert manifest["spec"]["strategy"]["canary"]["steps"] == [
        {"setWeight": 40},
        {"analysis": {"templates": [{"templateName": "payments-api-contract"}]}},
        {"setWeight": 100},
    ]
    validate_artifact("argo-rollout-v1", manifest)
    assert bundle.path.read_bytes()

