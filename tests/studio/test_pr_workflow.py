from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from safelane.pr_studio import (
    GitHubPullRequestProvider,
    PullRequestAssessmentEngine,
    PullRequestStudioService,
)


ROOT = Path(__file__).resolve().parents[2]


class FakeGitHub:
    def __init__(self, repository: str = "acme/payments") -> None:
        self.repository = repository
        self.diff_requests: list[tuple[int, str, str]] = []
        self.head_sha = "b" * 40

    def list_open_pull_requests(self) -> list[dict[str, Any]]:
        return [
            {
                "number": 42,
                "title": "Remove retry ceiling",
                "url": "https://github.com/acme/payments/pull/42",
                "author": "andrew",
                "head_ref": "feat/adaptive-retries",
                "base_ref": "main",
                "base_sha": "a" * 40,
                "head_sha": self.head_sha,
                "updated_at": "2026-08-12T08:00:00Z",
                "is_draft": False,
            }
        ]

    def pull_request_diff(self, number: int, base_sha: str, head_sha: str) -> bytes:
        self.diff_requests.append((number, base_sha, head_sha))
        return b"diff --git a/retries.py b/retries.py\n-old_limit = 5\n+old_limit = None\n"


class FakeAssessor:
    def __init__(self, expected_repository: str = "acme/payments") -> None:
        self.expected_repository = expected_repository
        self.policy = {"policy_version": "2026.08.3"}

    def assess(
        self, repository: str, pull_request: dict[str, Any], diff: bytes
    ) -> dict[str, Any]:
        assert repository == self.expected_repository
        assert pull_request["number"] == 42
        assert b"old_limit = None" in diff
        return {
            "tier": "risky",
            "profile": "Strict",
            "reason": "Retry protection was removed.",
            "confidence": "high",
            "findings": [],
            "rollout_options": [{"name": "Strict"}],
            "evidence": {"ai_status": "complete"},
        }


def test_dashboard_discovers_and_assesses_only_open_pull_requests(tmp_path: Path) -> None:
    github = FakeGitHub()
    service = PullRequestStudioService(github, tmp_path / "studio", FakeAssessor())

    snapshot = service.dashboard()

    assert snapshot["repository"] == "acme/payments"
    assert snapshot["counts"] == {"needs_review": 1, "resolved": 0}
    assert snapshot["changes"] == [
        {
            "number": 42,
            "title": "Remove retry ceiling",
            "author": "andrew",
            "head_ref": "feat/adaptive-retries",
            "updated_at": "2026-08-12T08:00:00Z",
            "tier": "risky",
            "profile": "Strict",
            "reason": "Retry protection was removed.",
            "review_status": "unresolved",
            "head_sha": "b" * 40,
        }
    ]
    assert github.diff_requests == [(42, "a" * 40, "b" * 40)]

    detail = service.assessment(42)
    second_snapshot = service.dashboard()

    assert detail["assessment_id"] == f"acme/payments#42@{'b' * 40}"
    assert detail["schema_version"] == "studio-pr-assessment-v2"
    assert detail["artifact_scope"] == "studio_local_review_only"
    assert detail["policy_version"] == "2026.08.3"
    assert detail["assessment_input_sha256"].startswith("sha256:")
    assert detail["change"]["url"].endswith("/pull/42")
    assert detail["review"] == {"status": "unresolved", "resolution": None}
    assert second_snapshot == snapshot
    assert github.diff_requests == [(42, "a" * 40, "b" * 40)]


def test_remote_github_repository_is_normalized_and_open_prs_are_mapped() -> None:
    calls: list[tuple[str, ...]] = []

    def run(arguments: tuple[str, ...], cwd: Path | None = None) -> bytes:
        assert cwd is None
        calls.append(arguments)
        if arguments[:2] == ("pr", "list"):
            return b'''[{"number":7,"title":"Bound retries","url":"https://github.com/acme/payments/pull/7","author":{"login":"andrew"},"headRefName":"fix/retries","baseRefName":"main","headRefOid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","baseRefOid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","updatedAt":"2026-08-12T09:00:00Z","isDraft":false}]'''
        if arguments[0] == "api":
            return b""
        raise AssertionError(arguments)

    provider = GitHubPullRequestProvider(
        "https://github.com/acme/payments.git", command_runner=run
    )

    pull_requests = provider.list_open_pull_requests()
    diff = provider.pull_request_diff(7, "a" * 40, "b" * 40)

    assert provider.repository == "acme/payments"
    assert pull_requests[0]["author"] == "andrew"
    assert pull_requests[0]["head_sha"] == "b" * 40
    assert diff == b""
    assert calls == [
        (
            "pr", "list", "--repo", "acme/payments", "--state", "open",
            "--limit", "100", "--json",
            "number,title,url,author,headRefName,baseRefName,headRefOid,baseRefOid,updatedAt,isDraft,state",
        ),
        (
            "api", f"repos/acme/payments/compare/{'a' * 40}...{'b' * 40}",
            "--header", "Accept: application/vnd.github.v3.diff",
        ),
    ]


def test_real_diff_is_classified_with_source_verified_ai_evidence() -> None:
    diff = b"""diff --git a/retries.py b/retries.py
index 257cc56..5716ca5 100644
--- a/retries.py
+++ b/retries.py
@@ -1 +1 @@
-retry_limit = 5
+retry_limit = None
"""

    class Analyzer:
        def analyze(self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]) -> dict[str, Any]:
            assert raw_diff == diff
            assert {span["text"] for span in authorized_spans} == {
                "retry_limit = 5", "retry_limit = None"
            }
            return {
                "status": "complete",
                "findings": [
                        {
                            "category": "availability",
                            "severity": "high",
                            "hypothesis_kind": "changed_behavior_may_violate_contract",
                            "verification_intent_kind": "verify_changed_contract_during_rollout",
                            "approval_question_kind": "confirm_contract_is_preserved",
                            "remediation_kind": "preserve_previous_contract_or_add_compatibility",
                            "spans": [authorized_spans[0]],
                        }
                ],
            }

    engine = PullRequestAssessmentEngine(
        policy_path=ROOT / "policy.yaml", analyzer=Analyzer()
    )
    result = engine.assess(
        "acme/payments", {"number": 42}, diff
    )

    assert result["tier"] == "risky"
    assert result["profile"] == "Strict"
    assert result["reason"] == "Availability-sensitive change"
    assert result["findings"][0]["title"] == "Availability-sensitive change"
    assert result["findings"][0]["rationale"] == (
        "The local model identified a possible availability impact tied to the "
        "source-verified changed lines below."
    )
    assert result["findings"][0]["source_references_verified"] is True
    assert result["findings"][0]["safety_case"] == {
        "hypothesis_kind": "changed_behavior_may_violate_contract",
        "verification_intent_kind": "verify_changed_contract_during_rollout",
        "approval_question_kind": "confirm_contract_is_preserved",
        "remediation_kind": "preserve_previous_contract_or_add_compatibility",
    }
    assert result["findings"][0]["spans"][0] == {
        "file": "retries.py",
        "side": "removed",
        "line": 1,
        "text": "retry_limit = 5",
    }


def test_approval_resolves_only_the_exact_open_pr_revision(tmp_path: Path) -> None:
    github = FakeGitHub()
    service = PullRequestStudioService(github, tmp_path / "studio", FakeAssessor())
    assessment = service.assessment(42)

    resolved = service.approve(
        42,
        {
            "selected_profile": "Strict",
            "assessment_id": assessment["assessment_id"],
            "head_sha": assessment["change"]["head_sha"],
            "policy_version": assessment["policy_version"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
        },
        approval_token=service.approval_token,
    )

    assert resolved["review"]["status"] == "resolved"
    assert resolved["review"]["resolution"]["selected_profile"] == "Strict"
    assert resolved["decision_path"].endswith("pr-42.json")
    decision = json.loads(
        (tmp_path / "studio" / "decisions" / "pr-42.json").read_text()
    )
    assert decision["policy_version"] == "2026.08.3"
    assert decision["assessment_input_sha256"].startswith("sha256:")
    assert decision["schema_version"] == "studio-pr-review-v1"
    assert decision["authorization_scope"] == "studio_local_review_only"
    assert service.dashboard()["counts"] == {"needs_review": 0, "resolved": 1}

    github.head_sha = "c" * 40
    refreshed = service.dashboard()

    assert refreshed["counts"] == {"needs_review": 1, "resolved": 0}
    assert refreshed["changes"][0]["head_sha"] == "c" * 40
    assert not (tmp_path / "studio" / "decisions" / "pr-42.json").exists()


def test_safe_pr_resolves_automatically_with_a_matching_local_decision(
    tmp_path: Path,
) -> None:
    class SafeAssessor(FakeAssessor):
        def assess(
            self, repository: str, pull_request: dict[str, Any], diff: bytes
        ) -> dict[str, Any]:
            result = super().assess(repository, pull_request, diff)
            return {
                **result,
                "tier": "safe",
                "profile": "Fast",
                "reason": "This PR is inside the bounded Fast scope.",
                "rollout_options": [{"name": "Fast"}],
            }

    service = PullRequestStudioService(
        FakeGitHub(), tmp_path / "studio", SafeAssessor()
    )

    snapshot = service.dashboard()

    assert snapshot["counts"] == {"needs_review": 0, "resolved": 1}
    assert (tmp_path / "studio" / "decisions" / "pr-42.json").is_file()


def test_incompletely_mapped_diff_cannot_enter_the_fast_lane() -> None:
    class NoFindings:
        def analyze(
            self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]
        ) -> dict[str, Any]:
            return {"status": "complete", "findings": []}

    engine = PullRequestAssessmentEngine(
        policy_path=ROOT / "policy.yaml", analyzer=NoFindings()
    )
    malformed = b"""diff --git a/retries.py b/renamed.py
--- a/retries.py
+++ b/renamed.py
@@ -1 +1 @@
-retry_limit = 5
+retry_limit = 6
"""

    result = engine.assess("acme/payments", {"number": 42}, malformed)

    assert result["evidence"]["all_paths_recognized"] is False
    assert result["tier"] == "guarded"
    assert result["profile"] == "Guarded"


def test_small_mapped_diff_with_complete_empty_ai_evidence_can_enter_fast() -> None:
    class NoFindings:
        def analyze(
            self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]
        ) -> dict[str, Any]:
            return {"status": "complete", "findings": []}

    engine = PullRequestAssessmentEngine(
        policy_path=ROOT / "policy.yaml", analyzer=NoFindings()
    )
    mapped = b"""diff --git a/src/demo_api/README.md b/src/demo_api/README.md
--- a/src/demo_api/README.md
+++ b/src/demo_api/README.md
@@ -1 +1 @@
-old copy
+clearer copy
"""

    result = engine.assess("acme/payments", {"number": 42}, mapped)

    assert result["evidence"]["all_paths_recognized"] is True
    assert result["tier"] == "safe"
    assert result["profile"] == "Fast"


def test_small_documentation_only_diff_can_enter_fast_for_any_repository() -> None:
    class NoFindings:
        def analyze(
            self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]
        ) -> dict[str, Any]:
            return {"status": "complete", "findings": []}

    engine = PullRequestAssessmentEngine(
        policy_path=ROOT / "policy.yaml", analyzer=NoFindings()
    )
    documentation = b"""diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
@@ -1 +1 @@
-old copy
+clearer copy
"""

    result = engine.assess("another/repository", {"number": 7}, documentation)

    assert result["evidence"]["all_paths_recognized"] is True
    assert result["tier"] == "safe"
    assert result["profile"] == "Fast"


def test_repository_can_be_switched_through_the_studio_service(tmp_path: Path) -> None:
    created: list[str] = []

    def provider_factory(source: str) -> FakeGitHub:
        created.append(source)
        return FakeGitHub(source)

    service = PullRequestStudioService(
        FakeGitHub(),
        tmp_path / "state" / "acme--payments",
        FakeAssessor("acme/catalog"),
        state_root=tmp_path / "state",
        provider_factory=provider_factory,
    )

    snapshot = service.connect(
        {"repository": "acme/catalog"},
        approval_token=service.approval_token,
    )

    assert created == ["acme/catalog"]
    assert snapshot["repository"] == "acme/catalog"
    assert service.workspace == (tmp_path / "state" / "acme--catalog").resolve()
    assert snapshot["changes"][0]["title"] == "Remove retry ceiling"
