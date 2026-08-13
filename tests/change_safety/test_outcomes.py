from __future__ import annotations

from dataclasses import replace
from pathlib import Path

import pytest
import yaml

from safelane.artifacts import (
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from safelane.change_safety import (
    ChangeSafety,
    PullRequestRef,
    ReleaseBinding,
    ResolutionCommand,
)
from safelane.outcomes import (
    OutcomeError,
    OutcomeLedger,
    OutcomeObservation,
    StageObservation,
)

from .test_assess import TEST_IMAGE, register_test_image
from .test_resolve import GuardedHost, NoFindingAnalyzer


def test_rollout_outcome_receipt_binds_release_and_feeds_calibration(tmp_path: Path) -> None:
    safety = ChangeSafety(
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
    bundle = safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image=TEST_IMAGE,
    ))
    ledger = OutcomeLedger(
        state_dir=tmp_path,
        clock=lambda: "2026-08-12T10:00:00Z",
    )

    observation = OutcomeObservation(
        repository="acme/payments",
        pull_request=42,
        rollout_uid="rollout-uid-123",
        result="succeeded",
        stages=(
            StageObservation(40, "succeeded", "passed"),
            StageObservation(100, "succeeded", "not_run"),
        ),
        incident_within_24h=False,
    )
    receipt = ledger.record(observation)

    assert receipt["assessment_id"] == assessed.handle.assessment_id
    assert receipt["decision_sha256"] == bundle.decision_sha256
    assert receipt["head_sha"] == "b" * 40
    assert receipt["profile"] == "Guarded"
    validate_artifact("rollout-outcome-v1", receipt)
    assert ledger.record(observation) == receipt
    with pytest.raises(OutcomeError, match="already has a different receipt"):
        ledger.record(replace(
            observation,
            result="failed",
            stages=(
                StageObservation(40, "failed", "failed"),
                StageObservation(100, "skipped", "not_run"),
            ),
        ))
    assert ledger.summary() == {
        "total": 1,
        "by_tier": {
            "guarded": {
                "total": 1,
                "succeeded": 1,
                "failed_or_aborted": 0,
                "incidents_within_24h": 0,
            }
        },
        "by_rule": {
            "scope.guarded": {
                "total": 1,
                "failed_or_aborted": 0,
                "incidents_within_24h": 0,
            }
        },
        "by_finding": {},
    }


def test_outcome_rejects_assessment_content_changed_after_compilation(
    tmp_path: Path,
) -> None:
    safety = ChangeSafety(
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
    safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image=TEST_IMAGE,
    ))
    assessment_path = tmp_path / "acme--payments" / "pr-42" / "assessment.json"
    assessment = load_json_bytes(assessment_path.read_bytes())
    assessment["policy_rule_ids"] = ["scope.safe"]
    assessment_path.write_bytes(canonical_json_bytes(assessment))

    with pytest.raises(OutcomeError, match="missing or invalid"):
        OutcomeLedger(state_dir=tmp_path).record(OutcomeObservation(
            repository="acme/payments",
            pull_request=42,
            rollout_uid="rollout-uid-forged",
            result="succeeded",
            stages=(
                StageObservation(40, "succeeded", "passed"),
                StageObservation(100, "succeeded", "not_run"),
            ),
            incident_within_24h=False,
        ))


def test_outcome_rejects_decision_not_exactly_derived_from_assessment(
    tmp_path: Path,
) -> None:
    safety = ChangeSafety(
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
    safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image=TEST_IMAGE,
    ))
    directory = tmp_path / "acme--payments" / "pr-42"
    decision_path = directory / "decision.json"
    manifest_path = directory / "release" / "rollout.yaml"
    decision = load_json_bytes(decision_path.read_bytes())
    decision["resolution"]["actor"] = "forged-reviewer"
    manifest = load_yaml_bytes(manifest_path.read_bytes())
    manifest["metadata"]["annotations"]["safelane.dev/decision-sha256"] = sha256(
        decision
    )
    decision_path.write_bytes(canonical_json_bytes(decision))
    manifest_path.write_bytes(
        yaml.safe_dump(manifest, sort_keys=False).encode("utf-8")
    )

    with pytest.raises(OutcomeError, match="authorization is inconsistent"):
        OutcomeLedger(state_dir=tmp_path).record(OutcomeObservation(
            repository="acme/payments",
            pull_request=42,
            rollout_uid="rollout-uid-forged-decision",
            result="succeeded",
            stages=(
                StageObservation(40, "succeeded", "passed"),
                StageObservation(100, "succeeded", "not_run"),
            ),
            incident_within_24h=False,
        ))


def test_outcome_rejects_success_that_contains_failed_or_skipped_stages(
    tmp_path: Path,
) -> None:
    safety = ChangeSafety(
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
    safety.compile(ReleaseBinding(handle=assessed.handle, image=TEST_IMAGE))

    with pytest.raises(OutcomeError, match="successful rollout"):
        OutcomeLedger(state_dir=tmp_path).record(OutcomeObservation(
            repository="acme/payments",
            pull_request=42,
            rollout_uid="contradictory-success",
            result="succeeded",
            stages=(
                StageObservation(40, "failed", "failed"),
                StageObservation(100, "skipped", "not_run"),
            ),
            incident_within_24h=False,
        ))
