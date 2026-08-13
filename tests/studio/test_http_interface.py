from __future__ import annotations

import json
import threading
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

from safelane.artifacts import load_json_bytes
from safelane.pr_studio import PullRequestStudioService
from safelane.studio import create_studio_server


def empty_pr_service(tmp_path: Path, repository: str = "acme/one") -> PullRequestStudioService:
    class EmptyProvider:
        def __init__(self, repository: str) -> None:
            self.repository = repository

        def list_open_pull_requests(self) -> list[dict[str, object]]:
            return []

        def pull_request_diff(
            self, number: int, base_sha: str, head_sha: str
        ) -> bytes:
            raise AssertionError((number, base_sha, head_sha))

    class UnusedAssessor:
        def assess(self, repository, pull_request, diff):
            raise AssertionError((repository, pull_request, diff))

    return PullRequestStudioService(
        EmptyProvider(repository),
        tmp_path / "state" / repository.replace("/", "--"),
        UnusedAssessor(),
        state_root=tmp_path / "state",
        provider_factory=EmptyProvider,
    )


@contextmanager
def running(service: PullRequestStudioService) -> Iterator[str]:
    server = create_studio_server(service, port=0)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        host, port = server.server_address
        yield f"http://{host}:{port}"
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


def request_json(
    url: str,
    *,
    method: str = "GET",
    value: dict[str, object] | None = None,
    approval_token: str | None = None,
    origin: str | None = None,
    content_type: str = "application/json",
    host: str | None = None,
) -> tuple[int, dict[str, object]]:
    raw = None if value is None else json.dumps(value).encode()
    parsed = urlsplit(url)
    headers = {"Content-Type": content_type}
    if method == "POST":
        headers["Origin"] = origin or f"{parsed.scheme}://{parsed.netloc}"
        if approval_token is not None:
            headers["X-SafeLane-CSRF"] = approval_token
    if host is not None:
        headers["Host"] = host
    request = Request(
        url,
        data=raw,
        method=method,
        headers=headers,
    )
    try:
        with urlopen(request) as response:
            return response.status, load_json_bytes(response.read())
    except HTTPError as exc:
        return exc.code, load_json_bytes(exc.read())


def test_studio_shell_has_no_legacy_workspace_ui(tmp_path: Path) -> None:
    service = empty_pr_service(tmp_path)

    with running(service) as base_url:
        status, result = request_json(f"{base_url}/api/dashboard")
        with urlopen(f"{base_url}/") as response:
            html = response.read().decode()
        with urlopen(f"{base_url}/app.js") as response:
            script = response.read().decode().replace("\r\n", "\n")

    assert status == 200
    assert result["repository"] == "acme/one"
    assert "SafeLane Studio" in html
    assert "Approve Strict rollout" in script
    assert "rolloutPreview" in script
    assert "renderLegacy" not in script
    assert "Legacy workspace" not in script
    assert "/api/policy" not in script
    assert "/api/approve" not in script
    assert "api(`/api/assessment`" not in script


def test_no_policy_or_legacy_workspace_endpoint_exists(tmp_path: Path) -> None:
    service = empty_pr_service(tmp_path)

    with running(service) as base_url:
        policy_status, policy_result = request_json(
            f"{base_url}/api/policy", method="POST", value={"tier": "safe"},
            approval_token=service.approval_token,
        )
        assessment_status, assessment_result = request_json(f"{base_url}/api/assessment")
        approve_status, approve_result = request_json(
            f"{base_url}/api/approve", method="POST", value={"selected_profile": "Strict"},
            approval_token=service.approval_token,
        )

    assert (policy_status, policy_result) == (404, {"error": "not_found"})
    assert (assessment_status, assessment_result) == (404, {"error": "not_found"})
    assert (approve_status, approve_result) == (404, {"error": "not_found"})


def test_cross_origin_or_non_json_mutation_is_rejected(tmp_path: Path) -> None:
    service = empty_pr_service(tmp_path)
    payload = {"repository": "acme/two"}

    with running(service) as base_url:
        read_status, read_result = request_json(
            f"{base_url}/api/dashboard", host="evil.example"
        )
        origin_status, origin_result = request_json(
            f"{base_url}/api/connect", method="POST", value=payload,
            approval_token=service.approval_token, origin="https://evil.example",
        )
        type_status, type_result = request_json(
            f"{base_url}/api/connect", method="POST", value=payload,
            approval_token=service.approval_token, content_type="text/plain",
        )
        token_status, token_result = request_json(
            f"{base_url}/api/connect", method="POST", value=payload,
            approval_token="wrong-token",
        )
        host_status, host_result = request_json(
            f"{base_url}/api/connect", method="POST", value=payload,
            approval_token=service.approval_token,
            origin="http://evil.example", host="evil.example",
        )

    assert (read_status, read_result) == (403, {"error": "untrusted_request"})
    assert (origin_status, origin_result) == (403, {"error": "untrusted_request"})
    assert (type_status, type_result) == (400, {"error": "invalid_request"})
    assert (token_status, token_result) == (403, {"error": "untrusted_request"})
    assert (host_status, host_result) == (403, {"error": "untrusted_request"})
    assert service.provider.repository == "acme/one"


def test_repository_can_be_selected_through_the_studio_http_seam(
    tmp_path: Path,
) -> None:
    service = empty_pr_service(tmp_path, "acme/one")

    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/connect",
            method="POST",
            value={"repository": "acme/two"},
            approval_token=service.approval_token,
        )

    assert status == 200
    assert result["repository"] == "acme/two"
    assert result["counts"] == {"needs_review": 0, "resolved": 0}
