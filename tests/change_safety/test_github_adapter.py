from __future__ import annotations

from pathlib import Path

from safelane.engine import PullRequestRef
from safelane.pr_studio import GitHubPullRequestProvider


def test_github_adapter_reads_exact_pr_and_base_owned_policy() -> None:
    calls: list[tuple[str, ...]] = []

    def run(arguments: tuple[str, ...], cwd: Path | None = None) -> bytes:
        assert cwd is None
        calls.append(arguments)
        if arguments[:2] == ("pr", "view"):
            return b'''{"number":42,"title":"Bound retries","url":"https://github.com/acme/payments/pull/42","author":{"login":"andrew"},"headRefName":"fix/retries","baseRefName":"main","headRefOid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","baseRefOid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","updatedAt":"2026-08-12T09:00:00Z","isDraft":false,"state":"OPEN"}'''
        if "contents/.safelane/policy.yaml" in arguments[1]:
            return b'policy_version: payments-1\n'
        if "/compare/" in arguments[1]:
            return b"diff"
        raise AssertionError(arguments)

    host = GitHubPullRequestProvider("acme/payments", command_runner=run)

    snapshot = host.get_pull_request(PullRequestRef("acme/payments", 42))
    policy = host.read_file("acme/payments", "a" * 40, ".safelane/policy.yaml")
    diff = host.diff(snapshot)

    assert snapshot.head_sha == "b" * 40
    assert policy == b"policy_version: payments-1\n"
    assert diff == b"diff"
    assert calls[0] == (
        "pr", "view", "42", "--repo", "acme/payments", "--json", host._FIELDS
    )
    assert calls[1] == (
        "api",
        f"repos/acme/payments/contents/.safelane/policy.yaml?ref={'a' * 40}",
        "--header",
        "Accept: application/vnd.github.raw+json",
    )
