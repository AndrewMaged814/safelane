from __future__ import annotations

from pathlib import Path

from safelane.artifacts import canonical_json_bytes
from safelane.change_safety import ChangeSafety
from safelane.cli import _repository_state_root
from safelane.repository_studio import RepositoryStudioService

from .test_assess import PullRequestRef, TEST_IMAGE, register_test_image
from .test_resolve import GuardedHost, NoFindingAnalyzer


class OpenPullRequests(GuardedHost):
    repository = "acme/payments"

    def list_open_pull_requests(self):
        snapshot = self.snapshot
        return [{
            "number": snapshot.number,
            "title": snapshot.title,
            "url": snapshot.url,
            "author": snapshot.author,
            "base_ref": snapshot.base_ref,
            "head_ref": snapshot.head_ref,
            "base_sha": snapshot.base_sha,
            "head_sha": snapshot.head_sha,
            "updated_at": snapshot.updated_at,
            "is_draft": snapshot.is_draft,
        }]


def test_cli_assessment_is_the_same_bytes_studio_reads_by_default(
    tmp_path: Path,
) -> None:
    host = OpenPullRequests()
    host.local_path = tmp_path
    state_root = _repository_state_root(host, None)
    cli_workflow = ChangeSafety(
        host=host,
        state_dir=state_root,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    )
    cli_outcome = cli_workflow.assess(PullRequestRef("acme/payments", 42))
    assessment_path = state_root / "acme--payments" / "pr-42" / "assessment.json"
    cli_bytes = assessment_path.read_bytes()

    def unexpected_analyzer(policy):
        raise AssertionError("Studio should reuse the CLI assessment")

    studio_workflow = ChangeSafety(
        host=host,
        state_dir=state_root,
        analyzer_factory=unexpected_analyzer,
        clock=lambda: (_ for _ in ()).throw(
            AssertionError("Studio should not timestamp a duplicate assessment")
        ),
    )
    studio = RepositoryStudioService(
        provider=host,
        workflow=studio_workflow,
        state_root=state_root,
    )

    assert studio.assessment(42) == cli_outcome.assessment
    assert assessment_path.read_bytes() == cli_bytes


def test_studio_projects_canonical_assessment_and_resolution(tmp_path: Path) -> None:
    host = OpenPullRequests()
    workflow = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=iter([
            "2026-08-12T09:00:00Z",
            "2026-08-12T09:05:00Z",
        ]).__next__,
    )
    studio = RepositoryStudioService(
        provider=host,
        workflow=workflow,
        state_root=tmp_path,
    )

    dashboard = studio.dashboard()
    detail = studio.assessment(42)
    resolved = studio.resolve(42, {
        "action": "approve",
        "actor": "andrew",
        "selected_profile": "Guarded",
        "assessment_id": detail["assessment_id"],
        "assessment_result_sha256": detail["assessment_result_sha256"],
    }, approval_token=studio.approval_token)

    assert dashboard["changes"][0]["profile"] == "Guarded"
    assert detail["schema_version"] == "change-assessment-v1"
    assert resolved["review"]["status"] == "approved"
    assert resolved["decision"]["schema_version"] == "rollout-decision-v1"
    register_test_image(workflow)

    release = studio.compile(42, {
        "image": TEST_IMAGE,
    }, approval_token=studio.approval_token)

    assert release["manifest"]["kind"] == "Rollout"
    assert release["path"].endswith("rollout.yaml")

    receipt = studio.record_outcome(42, {
        "rollout_uid": "rollout-uid-123",
        "result": "succeeded",
        "stages": [
            {"set_weight": 40, "outcome": "succeeded", "analysis_outcome": "passed"},
            {"set_weight": 100, "outcome": "succeeded", "analysis_outcome": "not_run"},
        ],
        "incident_within_24h": False,
    }, approval_token=studio.approval_token)

    assert receipt["profile"] == "Guarded"
    assert studio.outcomes()["total"] == 1

    other = {
        **receipt,
        "repository": "acme/catalog",
        "pull_request": 7,
        "assessment_id": "acme/catalog#7@" + "d" * 40 + ":catalog-1",
        "rollout_uid": "catalog-rollout-1",
    }
    other_path = (
        tmp_path / "acme--catalog" / "pr-7" / "outcomes" / "catalog-rollout-1.json"
    )
    other_path.parent.mkdir(parents=True)
    other_path.write_bytes(canonical_json_bytes(other))

    assert studio.outcomes()["total"] == 1
