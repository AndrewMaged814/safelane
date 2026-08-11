from __future__ import annotations

from pathlib import Path

from safelane.change_safety import (
    AssessmentHandle,
    ChangeSafety,
    PullRequestRef,
    ResolutionCommand,
)

from .test_assess import FakePullRequestHost


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
