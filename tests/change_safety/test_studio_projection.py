from __future__ import annotations

from pathlib import Path

from safelane.change_safety import ChangeSafety
from safelane.repository_studio import RepositoryStudioService

from .test_assess import PullRequestRef
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

