from __future__ import annotations

from pathlib import Path

from safelane.artifacts import validate_artifact
from safelane.change_safety import (
    ChangeSafety,
    PullRequestRef,
    ReleaseBinding,
    ResolutionCommand,
)
from safelane.outcomes import OutcomeLedger, OutcomeObservation, StageObservation

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
    bundle = safety.compile(ReleaseBinding(
        handle=assessed.handle,
        image="ghcr.io/acme/payments@sha256:" + "c" * 64,
    ))
    ledger = OutcomeLedger(
        state_dir=tmp_path,
        clock=lambda: "2026-08-12T10:00:00Z",
    )

    receipt = ledger.record(OutcomeObservation(
        repository="acme/payments",
        pull_request=42,
        rollout_uid="rollout-uid-123",
        result="succeeded",
        stages=(
            StageObservation(40, "succeeded", "passed"),
            StageObservation(100, "succeeded", "not_run"),
        ),
        incident_within_24h=False,
    ))

    assert receipt["assessment_id"] == assessed.handle.assessment_id
    assert receipt["decision_sha256"] == bundle.decision_sha256
    assert receipt["head_sha"] == "b" * 40
    assert receipt["profile"] == "Guarded"
    validate_artifact("rollout-outcome-v1", receipt)
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
    }

