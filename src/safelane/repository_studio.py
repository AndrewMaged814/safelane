from __future__ import annotations

import secrets
from pathlib import Path
from typing import Any, Callable, Protocol

from .change_safety import (
    AssessmentHandle,
    ChangeSafety,
    ChangeSafetyError,
    PullRequestRef,
    ResolutionCommand,
)
from .pr_studio import GitHubPullRequestProvider, PullRequestStudioError


class OpenPullRequestProvider(Protocol):
    repository: str

    def list_open_pull_requests(self) -> list[dict[str, Any]]: ...


WorkflowFactory = Callable[[OpenPullRequestProvider, Path], ChangeSafety]


class RepositoryStudioService:
    """Studio projection over the canonical ChangeSafety workflow."""

    def __init__(
        self,
        *,
        provider: OpenPullRequestProvider,
        workflow: ChangeSafety,
        state_root: Path,
        workflow_factory: WorkflowFactory | None = None,
        provider_factory: Callable[[str], OpenPullRequestProvider] | None = None,
        reviewer: str = "local-reviewer",
    ) -> None:
        self.provider = provider
        self.workflow = workflow
        self.state_root = state_root.resolve()
        self.state_root.mkdir(parents=True, exist_ok=True)
        self.workspace = self.state_root / provider.repository.replace("/", "--")
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.approval_token = secrets.token_urlsafe(32)
        self._workflow_factory = workflow_factory
        self._provider_factory = provider_factory or GitHubPullRequestProvider
        self._open: dict[int, dict[str, Any]] = {}
        self.reviewer = reviewer

    def dashboard(self) -> dict[str, Any]:
        rows: list[dict[str, Any]] = []
        current: dict[int, dict[str, Any]] = {}
        try:
            pull_requests = self.provider.list_open_pull_requests()
            for pull_request in pull_requests:
                outcome = self.workflow.assess(PullRequestRef(
                    self.provider.repository, pull_request["number"]
                ))
                assessment = outcome.assessment
                current[pull_request["number"]] = assessment
                rows.append(_row(assessment))
        except (ChangeSafetyError, OSError, KeyError, TypeError) as exc:
            raise PullRequestStudioError(str(exc)) from exc
        self._open = current
        rows.sort(key=lambda row: (
            row["review_status"] != "unresolved",
            {"risky": 0, "guarded": 1, "safe": 2}[row["tier"]],
            row["updated_at"],
        ))
        return {
            "repository": self.provider.repository,
            "reviewer": self.reviewer,
            "counts": {
                "needs_review": sum(
                    row["review_status"] == "unresolved" for row in rows
                ),
                "resolved": sum(
                    row["review_status"] != "unresolved" for row in rows
                ),
            },
            "changes": rows,
        }

    def assessment(self, number: int) -> dict[str, Any]:
        self.dashboard()
        try:
            return self._open[number]
        except KeyError as exc:
            raise PullRequestStudioError("open pull request was not found") from exc

    def profiles(self) -> dict[str, Any]:
        self.dashboard()
        if not self._open:
            return {"policy_version": None, "profiles": []}
        assessment = next(iter(self._open.values()))
        profiles: dict[str, dict[str, Any]] = {}
        for item in assessment["rollout_catalog"]:
            profiles[item["name"]] = item
        return {
            "policy_version": assessment["policy"]["version"],
            "profiles": list(profiles.values()),
        }

    def resolve(
        self,
        number: int,
        payload: Any,
        *,
        approval_token: str | None,
    ) -> dict[str, Any]:
        if approval_token is None or not secrets.compare_digest(
            approval_token, self.approval_token
        ):
            raise PullRequestStudioError("invalid approval token")
        required = {
            "action", "actor", "selected_profile", "assessment_id",
            "assessment_result_sha256",
        }
        if not isinstance(payload, dict) or set(payload) != required:
            raise PullRequestStudioError("invalid review request")
        if payload["action"] not in {"approve", "reject", "decide_later"}:
            raise PullRequestStudioError("invalid review action")
        assessment = self.assessment(number)
        if payload["assessment_id"] != assessment["assessment_id"]:
            raise PullRequestStudioError("review page is stale")
        try:
            outcome = self.workflow.resolve(ResolutionCommand(
                handle=AssessmentHandle(
                    payload["assessment_id"], payload["assessment_result_sha256"]
                ),
                action=payload["action"],
                selected_profile=payload["selected_profile"],
                actor=payload["actor"],
            ))
        except (ChangeSafetyError, TypeError, ValueError) as exc:
            raise PullRequestStudioError(str(exc)) from exc
        self._open[number] = outcome.assessment
        return {**outcome.assessment, "decision": outcome.decision}

    def approve(
        self,
        number: int,
        payload: Any,
        *,
        approval_token: str | None,
    ) -> dict[str, Any]:
        translated = {
            "action": "approve",
            "actor": self.reviewer,
            "selected_profile": payload.get("selected_profile") if isinstance(payload, dict) else None,
            "assessment_id": payload.get("assessment_id") if isinstance(payload, dict) else None,
            "assessment_result_sha256": (
                payload.get("assessment_result_sha256") if isinstance(payload, dict) else None
            ),
        }
        return self.resolve(number, translated, approval_token=approval_token)

    def connect(
        self,
        payload: Any,
        *,
        approval_token: str | None,
    ) -> dict[str, Any]:
        if approval_token is None or not secrets.compare_digest(
            approval_token, self.approval_token
        ):
            raise PullRequestStudioError("invalid connection token")
        if (
            not isinstance(payload, dict)
            or set(payload) != {"repository"}
            or not isinstance(payload["repository"], str)
        ):
            raise PullRequestStudioError("invalid repository connection request")
        if self._workflow_factory is None:
            raise PullRequestStudioError("repository switching is not configured")
        source = payload["repository"].strip()
        if not source:
            raise PullRequestStudioError("repository is required")
        provider = self._provider_factory(source)
        provider.list_open_pull_requests()
        self.provider = provider
        self.workspace = self.state_root / provider.repository.replace("/", "--")
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.workflow = self._workflow_factory(provider, self.state_root)
        self._open = {}
        return self.dashboard()


def _row(assessment: dict[str, Any]) -> dict[str, Any]:
    change = assessment["change"]
    risk = assessment["risk"]
    return {
        "number": change["number"],
        "title": change["title"],
        "author": change["author"],
        "head_ref": change["head_ref"],
        "updated_at": change["updated_at"],
        "tier": risk["tier"],
        "profile": risk["minimum_profile"],
        "reason": risk["reason"],
        "review_status": assessment["review"]["status"],
        "head_sha": change["head_sha"],
    }
