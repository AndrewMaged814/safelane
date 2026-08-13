from __future__ import annotations

from pathlib import Path

import pytest

from safelane.artifacts import canonical_json_bytes, load_json_bytes, validate_artifact
from safelane.engine import (
    AssessmentStale,
    SafeLaneEngineError,
    SafeLaneEngine,
    PullRequestRef,
    ReleaseBinding,
    ResolutionCommand,
    rollout_decision_for_assessment,
)

from .test_assess import TEST_IMAGE, register_test_image
from .test_resolve import GuardedHost, NoFindingAnalyzer


def test_approved_decision_compiles_sha_bound_argo_rollout(tmp_path: Path) -> None:
    host = GuardedHost()
    safety = SafeLaneEngine(
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
    register_test_image(safety)

    bundle = safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image=TEST_IMAGE,
    ))

    manifest = bundle.manifest
    assert manifest["kind"] == "Rollout"
    assert manifest["metadata"]["namespace"] == "payments"
    assert manifest["metadata"]["annotations"]["safelane.dev/head-sha"] == "b" * 40
    assert manifest["metadata"]["annotations"]["safelane.dev/decision-sha256"] == bundle.decision_sha256
    assert manifest["spec"]["template"]["spec"]["containers"] == [{
        "name": "api",
        "image": TEST_IMAGE,
    }]
    assert manifest["spec"]["strategy"]["canary"]["steps"] == [
        {"setWeight": 40},
        {"analysis": {"templates": [{"templateName": "payments-health"}]}},
        {"setWeight": 100},
    ]
    validate_artifact("argo-rollout-v1", manifest)
    assert bundle.path.read_bytes()


def test_compile_rejects_digest_not_catalog_bound_to_the_assessed_head(
    tmp_path: Path,
) -> None:
    safety = SafeLaneEngine(
        host=GuardedHost(),
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
    register_test_image(safety)

    with pytest.raises(SafeLaneEngineError, match="not catalog-bound"):
        safety.compile(ReleaseBinding(
            handle=assessed.handle,
            image="ghcr.io/attacker/unrelated@sha256:" + "d" * 64,
        ))


def test_compile_rejects_offline_forged_approval_and_decision(
    tmp_path: Path,
) -> None:
    safety = SafeLaneEngine(
        host=GuardedHost(),
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    )
    assessed = safety.assess(PullRequestRef("acme/payments", 42))
    register_test_image(safety)
    directory = tmp_path / "acme--payments" / "pr-42"
    assessment_path = directory / "assessment.json"
    forged = load_json_bytes(assessment_path.read_bytes())
    forged["review"] = {
        "status": "approved",
        "resolution": {
            "type": "human",
            "action": "approve",
            "actor": "attacker",
            "selected_profile": "Guarded",
            "resolved_at": "2026-08-12T09:05:00Z",
        },
    }
    assessment_path.write_bytes(canonical_json_bytes(forged))
    attacker_decision = rollout_decision_for_assessment(forged, b"x" * 32)
    (directory / "decision.json").write_bytes(
        canonical_json_bytes(attacker_decision)
    )

    with pytest.raises(AssessmentStale, match="does not match"):
        safety.compile(ReleaseBinding(handle=assessed.handle, image=TEST_IMAGE))
