from __future__ import annotations

from dataclasses import replace
from pathlib import Path

import pytest
import safelane.change_safety as change_safety_module

from safelane.engine import (
    AssessmentHandle,
    AssessmentStale,
    SafeLaneEngine,
    PullRequestRef,
    ReleaseBinding,
    ResolutionCommand,
)

from .test_assess import FakePullRequestHost, TEST_IMAGE, register_test_image


class NoFindingAnalyzer:
    def analyze(self, raw_diff: bytes, authorized_spans: list[dict]) -> dict:
        assert raw_diff
        assert authorized_spans
        return {"status": "complete", "findings": []}


class GuardedHost(FakePullRequestHost):
    def diff(self, snapshot):
        assert snapshot is self.snapshot
        return b'''diff --git a/src/payments/a.py b/src/payments/a.py
new file mode 100644
--- /dev/null
+++ b/src/payments/a.py
@@ -0,0 +1 @@
+a = 1
diff --git a/src/payments/b.py b/src/payments/b.py
new file mode 100644
--- /dev/null
+++ b/src/payments/b.py
@@ -0,0 +1 @@
+b = 1
diff --git a/src/payments/c.py b/src/payments/c.py
new file mode 100644
--- /dev/null
+++ b/src/payments/c.py
@@ -0,0 +1 @@
+c = 1
'''


def test_approval_binds_decision_to_current_assessment_and_actor(tmp_path: Path) -> None:
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

    resolved = safety.resolve(ResolutionCommand(
        handle=assessed.handle,
        action="approve",
        selected_profile="Guarded",
        actor="andrew",
    ))

    assert resolved.assessment["review"] == {
        "status": "approved",
        "resolution": {
            "type": "human",
            "action": "approve",
            "actor": "andrew",
            "selected_profile": "Guarded",
            "resolved_at": "2026-08-12T09:05:00Z",
        },
    }
    assert resolved.decision is not None
    assert resolved.decision["head_sha"] == "b" * 40
    assert resolved.decision["assessment_result_sha256"] == assessed.handle.assessment_result_sha256


def test_rejection_records_review_but_emits_no_rollout_decision(tmp_path: Path) -> None:
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

    resolved = safety.resolve(ResolutionCommand(
        handle=AssessmentHandle(
            assessed.handle.assessment_id,
            assessed.handle.assessment_result_sha256,
        ),
        action="reject",
        selected_profile=None,
        actor="andrew",
    ))

    assert resolved.assessment["review"]["status"] == "rejected"
    assert resolved.assessment["review"]["resolution"]["action"] == "reject"
    assert resolved.decision is None


def test_new_head_removes_prior_authorizing_decision(tmp_path: Path) -> None:
    host = GuardedHost()
    safety = SafeLaneEngine(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=iter([
            "2026-08-12T09:00:00Z",
            "2026-08-12T09:05:00Z",
            "2026-08-12T09:10:00Z",
        ]).__next__,
    )
    first = safety.assess(PullRequestRef("acme/payments", 42))
    safety.resolve(ResolutionCommand(
        handle=first.handle,
        action="approve",
        selected_profile="Guarded",
        actor="andrew",
    ))
    decision_path = tmp_path / "acme--payments" / "pr-42" / "decision.json"
    release_path = tmp_path / "acme--payments" / "pr-42" / "release" / "rollout.yaml"
    assert decision_path.exists()
    register_test_image(safety)
    safety.compile(ReleaseBinding(
        handle=first.handle,
        image=TEST_IMAGE,
    ))
    assert release_path.exists()

    host.snapshot = replace(host.snapshot, head_sha="c" * 40)
    second = safety.assess(PullRequestRef("acme/payments", 42))

    assert second.assessment["change"]["head_sha"] == "c" * 40
    assert not decision_path.exists()
    assert not release_path.exists()


def test_base_revision_move_invalidates_review_even_when_policy_bytes_match(
    tmp_path: Path,
) -> None:
    host = GuardedHost()
    safety = SafeLaneEngine(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    )
    assessed = safety.assess(PullRequestRef("acme/payments", 42))
    moved_base = "d" * 40
    host.files[(moved_base, ".safelane/policy.yaml")] = host.files[
        ("a" * 40, ".safelane/policy.yaml")
    ]
    host.files[(moved_base, ".safelane/trusted-probes.yaml")] = host.files[
        ("a" * 40, ".safelane/trusted-probes.yaml")
    ]
    host.snapshot = replace(host.snapshot, base_sha=moved_base)

    with pytest.raises(AssessmentStale, match="base revision"):
        safety.resolve(ResolutionCommand(
            handle=assessed.handle,
            action="approve",
            selected_profile="Guarded",
            actor="andrew",
        ))


def test_interrupted_approval_never_leaves_a_compilable_authorization(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
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
    register_test_image(safety)
    real_write = change_safety_module._atomic_write

    def interrupted_write(path: Path, data: bytes) -> None:
        if path.name == "assessment.json" and b'"status": "approved"' in data:
            raise OSError("simulated assessment publication failure")
        real_write(path, data)

    monkeypatch.setattr(change_safety_module, "_atomic_write", interrupted_write)
    with pytest.raises(OSError, match="publication failure"):
        safety.resolve(ResolutionCommand(
            handle=assessed.handle,
            action="approve",
            selected_profile="Guarded",
            actor="andrew",
        ))

    with pytest.raises(AssessmentStale, match="requires an approved assessment"):
        safety.compile(ReleaseBinding(handle=assessed.handle, image=TEST_IMAGE))
