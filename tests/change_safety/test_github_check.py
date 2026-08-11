from __future__ import annotations

import json

from safelane.github_checks import GitHubCheckPublisher


def test_github_check_is_bound_to_assessed_head_and_review_state() -> None:
    calls: list[tuple[str, ...]] = []

    def run(arguments, cwd=None):
        assert cwd is None
        calls.append(arguments)
        return b'{"id":913,"html_url":"https://github.com/acme/payments/runs/913"}'

    assessment = {
        "assessment_id": "acme/payments#42@" + "b" * 40 + ":payments-1",
        "assessment_result_sha256": "sha256:" + "c" * 64,
        "change": {
            "repository": "acme/payments",
            "head_sha": "b" * 40,
        },
        "policy": {"version": "payments-1"},
        "risk": {
            "tier": "guarded",
            "minimum_profile": "Guarded",
            "reason": "Outside bounded Fast scope.",
        },
        "review": {"status": "unresolved", "resolution": None},
    }

    result = GitHubCheckPublisher(command_runner=run).publish(assessment)

    assert result.id == 913
    assert result.url.endswith("/913")
    arguments = calls[0]
    assert arguments[:4] == (
        "api", "--method", "POST", "repos/acme/payments/check-runs"
    )
    assert ("-f", "head_sha=" + "b" * 40) == arguments[6:8]
    assert "conclusion=action_required" in arguments
    assert "output[title]=Guarded rollout review required" in arguments

