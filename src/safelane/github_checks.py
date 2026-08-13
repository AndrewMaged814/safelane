from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable


CommandRunner = Callable[[tuple[str, ...], Path | None], bytes]


class GitHubCheckError(RuntimeError):
    pass


@dataclass(frozen=True)
class CheckPublication:
    id: int
    url: str


class GitHubCheckPublisher:
    """Publish one exact-head SafeLane result through GitHub's Check Runs API."""

    def __init__(self, *, command_runner: CommandRunner) -> None:
        self._runner = command_runner

    def publish(
        self,
        assessment: dict[str, Any],
        *,
        check_run_id: int | None = None,
    ) -> CheckPublication:
        repository = assessment["change"]["repository"]
        head_sha = assessment["change"]["head_sha"]
        conclusion, title = _conclusion(assessment)
        risk = assessment["risk"]
        summary = (
            f"Tier: {risk['tier']}\n\n"
            f"Backend proposal: {risk['minimum_profile']}\n\n"
            f"{risk['reason']}\n\n"
            f"Policy: {assessment['policy']['version']}\n\n"
            f"Assessment: {assessment['assessment_result_sha256']}"
        )
        method = "PATCH" if check_run_id is not None else "POST"
        endpoint = (
            f"repos/{repository}/check-runs/{check_run_id}"
            if check_run_id is not None
            else f"repos/{repository}/check-runs"
        )
        arguments = [
            "api", "--method", method, endpoint,
            "-f", "name=SafeLane",
        ]
        if check_run_id is None:
            arguments.extend(("-f", f"head_sha={head_sha}"))
        arguments.extend((
            "-f", "status=completed",
            "-f", f"conclusion={conclusion}",
            "-f", f"external_id={assessment['assessment_id']}",
            "-f", f"output[title]={title}",
            "-f", f"output[summary]={summary}",
            "--header", "Accept: application/vnd.github+json",
        ))
        try:
            response = json.loads(self._runner(tuple(arguments), None))
            identifier = response["id"]
            url = response["html_url"]
        except (OSError, RuntimeError, UnicodeDecodeError, json.JSONDecodeError, KeyError, TypeError) as exc:
            raise GitHubCheckError("GitHub Check publication failed") from exc
        if not isinstance(identifier, int) or not isinstance(url, str):
            raise GitHubCheckError("GitHub returned an invalid Check Run")
        return CheckPublication(id=identifier, url=url)

    def invalidate(
        self, repository: str, check_run_id: int, *, superseded_by_head: str
    ) -> None:
        arguments = (
            "api", "--method", "PATCH", f"repos/{repository}/check-runs/{check_run_id}",
            "-f", "name=SafeLane",
            "-f", "status=completed",
            "-f", "conclusion=cancelled",
            "-f", "output[title]=Superseded by a newer PR head",
            "-f", f"output[summary]=This assessment was superseded by {superseded_by_head}.",
            "--header", "Accept: application/vnd.github+json",
        )
        try:
            self._runner(arguments, None)
        except (OSError, RuntimeError) as exc:
            raise GitHubCheckError("GitHub Check invalidation failed") from exc


def _conclusion(assessment: dict[str, Any]) -> tuple[str, str]:
    status = assessment["review"]["status"]
    tier = assessment["risk"]["tier"]
    if status == "rejected":
        return "failure", "Rollout rejected"
    if status == "approved":
        selected = assessment["review"]["resolution"]["selected_profile"]
        return "success", f"{selected} rollout authorized"
    return "action_required", f"{tier.title()} rollout review required"
