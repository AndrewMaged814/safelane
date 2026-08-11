from __future__ import annotations

import copy
import json
import threading
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen

import pytest

from safelane.artifacts import canonical_json_bytes, load_json_bytes, validate_artifact
from safelane.demo_repository import create_demo_repository
from safelane.engine import SafeLaneEngine
from safelane.risk_finder import FakeRiskFinder
from safelane.studio import StudioService, StudioWorkspace, create_studio_server


ROOT = Path(__file__).resolve().parents[2]


@pytest.fixture(scope="module")
def artifacts(tmp_path_factory: pytest.TempPathFactory) -> dict[str, object]:
    repo = tmp_path_factory.mktemp("studio-demo") / "repository"
    create_demo_repository(repo)
    rejected_proposal = json.loads(
        (ROOT / "demo/expected/ai-strict.json").read_text(encoding="utf-8")
    )
    rejected_proposal["safeguard_proposal"]["probe_id"] = "model-authored-probe"
    finder = FakeRiskFinder([
        (ROOT / "demo/expected/ai-fast.json").read_bytes(),
        (ROOT / "demo/expected/ai-strict.json").read_bytes(),
        json.dumps(rejected_proposal).encode(),
    ])
    engine = SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo/trusted-probes.yaml",
        risk_finder=finder,
    )
    fast = engine.assess(
        repo, ROOT / "demo/requests/fast.json", "2026-08-09T12:00:00Z"
    )
    strict = engine.assess(
        repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:01:00Z"
    )
    rejected = engine.assess(
        repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:02:00Z"
    )
    return {
        "fast_assessment": fast.assessment,
        "fast_decision": fast.automatic_decision,
        "strict_assessment": strict.assessment,
        "rejected_assessment": rejected.assessment,
    }


def approval_engine() -> SafeLaneEngine:
    return SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo/trusted-probes.yaml",
        risk_finder=FakeRiskFinder(b"{}", status="unavailable"),
    )


def write_workspace(
    path: Path,
    assessment: dict[str, object],
    decision: dict[str, object] | None = None,
) -> StudioWorkspace:
    path.mkdir()
    (path / "assessment.json").write_bytes(canonical_json_bytes(assessment))
    if decision is not None:
        (path / "decision.json").write_bytes(canonical_json_bytes(decision))
    return StudioWorkspace(path)


@contextmanager
def running(service: StudioService) -> Iterator[str]:
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


def approval_payload(assessment: dict[str, object]) -> dict[str, object]:
    change = assessment["change"]
    assert isinstance(change, dict)
    return {
        "selected_profile": "Strict",
        "assessment_id": assessment["assessment_id"],
        "head_sha": change["head_sha"],
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": assessment["assessment_result_sha256"],
    }


def test_fast_read_is_resolved_automatically_and_static_shell_has_no_mutation_ui(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    workspace = write_workspace(
        tmp_path / "workspace",
        copy.deepcopy(artifacts["fast_assessment"]),  # type: ignore[arg-type]
        copy.deepcopy(artifacts["fast_decision"]),  # type: ignore[arg-type]
    )
    service = StudioService(workspace, approval_engine())

    with running(service) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")
        with urlopen(f"{base_url}/") as response:
            html = response.read().decode()
        with urlopen(f"{base_url}/app.js") as response:
            script = response.read().decode()

    assessment = result["assessment"]
    assert status == 200
    assert isinstance(assessment, dict)
    assert assessment["review"]["resolution"]["type"] == "automatic"  # type: ignore[index]
    assert result["decision_path"].endswith("decision.json")  # type: ignore[union-attr]
    assert "SafeLane Studio" in html
    assert "Resolved automatically" in script
    assert "Approve Strict rollout" in script
    assert "First exposure:" in script
    assert (
        'if (finding) {\n    chain.append(chainStep(index++, "Normal code", '
        '"2/2 source references verified"' in script
    )
    assert "/api/policy" not in script
    assert "deploy" not in script.lower()


def test_risky_read_exposes_only_the_frozen_causal_chain(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    current = result["assessment"]
    assert status == 200
    assert isinstance(current, dict)
    assert current["review"] == {"status": "unresolved", "resolution": None}
    assert [span["side"] for span in current["ai_findings"][0]["spans"]] == [  # type: ignore[index]
        "removed", "added",
    ]
    assert current["selected_safeguard"]["probe_id"] == "demo-api-public-quote-v1"  # type: ignore[index]
    assert current["selected_safeguard"]["probe_preview"]["canary_only"] is True  # type: ignore[index]
    assert current["rollout_options"][0]["name"] == "Strict"  # type: ignore[index]
    assert result["decision_path"] is None


def test_rejected_proposal_keeps_verified_finding_and_uses_policy_fallback(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["rejected_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    current = result["assessment"]
    assert status == 200
    assert current["selected_safeguard"] is None  # type: ignore[index]
    assert current["ai_findings"][0]["source_reference_verified"] is True  # type: ignore[index]
    assert current["policy_result"]["final_tier"] == "risky"  # type: ignore[index]


def test_approval_compare_and_swaps_then_writes_exact_artifacts_in_order(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]
    service = StudioService(
        workspace,
        approval_engine(),
        clock=lambda: "2026-08-09T12:05:00Z",
    )
    payload = approval_payload(assessment)  # type: ignore[arg-type]

    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token,
        )
        read_status, current = request_json(f"{base_url}/api/assessment")
        replay_status, replay = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token,
        )

    resolved = load_json_bytes((workspace.path / "assessment.json").read_bytes())
    decision = load_json_bytes((workspace.path / "decision.json").read_bytes())
    assert status == read_status == 200
    assert resolved["review"]["status"] == "resolved"
    assert resolved["review"]["resolution"]["type"] == "human"
    assert decision["assessment_id"] == resolved["assessment_id"]
    assert decision["assessment_input_sha256"] == resolved["assessment_input_sha256"]
    assert decision["assessment_result_sha256"] == resolved["assessment_result_sha256"]
    assert decision["profile"]["name"] == "Strict"
    assert decision["analysis"]["probe_id"] == "demo-api-public-quote-v1"
    assert result["decision_path"].endswith("decision.json")  # type: ignore[union-attr]
    assert current["assessment"]["review"]["status"] == "resolved"  # type: ignore[index]
    assert replay_status == 409
    assert replay == {"error": "approval_conflict"}
    validate_artifact("assessment-v2", resolved)
    validate_artifact("decision-v3", decision)
    assert (workspace.path / "assessment.json").read_bytes() == canonical_json_bytes(resolved)
    assert (workspace.path / "decision.json").read_bytes() == canonical_json_bytes(decision)


def test_stale_page_is_rejected_without_writing(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]
    before = (workspace.path / "assessment.json").read_bytes()
    payload = approval_payload(assessment)  # type: ignore[arg-type]
    payload["assessment_result_sha256"] = "sha256:" + "0" * 64

    service = StudioService(workspace, approval_engine())
    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token,
        )

    assert status == 409
    assert result == {"error": "stale_approval"}
    assert (workspace.path / "assessment.json").read_bytes() == before
    assert not (workspace.path / "decision.json").exists()


def test_second_atomic_write_failure_leaves_no_authorizing_decision(
    tmp_path: Path,
    artifacts: dict[str, object],
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]
    payload = approval_payload(assessment)  # type: ignore[arg-type]
    from safelane import studio

    real_write = studio._atomic_write
    calls = 0

    def fail_decision(path: Path, raw: bytes) -> None:
        nonlocal calls
        calls += 1
        if calls == 2:
            raise OSError("injected decision write failure")
        real_write(path, raw)

    monkeypatch.setattr(studio, "_atomic_write", fail_decision)

    service = StudioService(workspace, approval_engine())
    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token,
        )

        read_status, read_result = request_json(f"{base_url}/api/assessment")

    resolved = load_json_bytes((workspace.path / "assessment.json").read_bytes())
    assert status == 500
    assert result == {"error": "artifact_write_failed"}
    assert resolved["review"]["status"] == "resolved"
    assert not (workspace.path / "decision.json").exists()
    assert read_status == 500
    assert read_result == {"error": "workspace_invalid"}


def test_no_policy_or_profile_mutation_endpoint_exists(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]

    service = StudioService(workspace, approval_engine())
    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/policy", method="POST", value={"tier": "safe"},
            approval_token=service.approval_token,
        )

    assert status == 404
    assert result == {"error": "not_found"}


def test_checked_in_risky_demo_workspace_is_approvable(tmp_path: Path) -> None:
    assessment = load_json_bytes((ROOT / "demo/studio-risky/assessment.json").read_bytes())
    workspace = write_workspace(tmp_path / "workspace", assessment)
    service = StudioService(
        workspace,
        approval_engine(),
        clock=lambda: "2026-08-09T12:05:00Z",
    )

    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/approve",
            method="POST",
            value=approval_payload(assessment),
            approval_token=service.approval_token,
        )

    assert status == 200
    assert result["assessment"]["review"]["status"] == "resolved"  # type: ignore[index]
    assert (workspace.path / "decision.json").is_file()


def test_assessment_inconsistent_decision_is_rejected_fail_closed(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    decision = copy.deepcopy(artifacts["fast_decision"])
    decision["base_sha"] = "f" * 40  # type: ignore[index]
    workspace = write_workspace(
        tmp_path / "workspace",
        copy.deepcopy(artifacts["fast_assessment"]),  # type: ignore[arg-type]
        decision,  # type: ignore[arg-type]
    )

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    assert status == 500
    assert result == {"error": "workspace_invalid"}


def test_automatic_resolution_must_use_the_assessment_timestamp(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["fast_assessment"])
    decision = copy.deepcopy(artifacts["fast_decision"])
    altered_time = "2099-01-01T00:00:00Z"
    assessment["review"]["resolution"]["resolved_at"] = altered_time  # type: ignore[index]
    decision["resolution"]["resolved_at"] = altered_time  # type: ignore[index]
    workspace = write_workspace(
        tmp_path / "workspace",
        assessment,  # type: ignore[arg-type]
        decision,  # type: ignore[arg-type]
    )

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    assert status == 500
    assert result == {"error": "workspace_invalid"}


def test_unresolved_assessment_beside_a_decision_is_rejected_fail_closed(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    workspace = write_workspace(
        tmp_path / "workspace",
        copy.deepcopy(artifacts["strict_assessment"]),  # type: ignore[arg-type]
        copy.deepcopy(artifacts["fast_decision"]),  # type: ignore[arg-type]
    )

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    assert status == 500
    assert result == {"error": "workspace_invalid"}


def test_assessment_content_must_match_its_reviewed_result_hash(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    assessment["policy_result"]["primary_reason"] = "Schema-valid unreviewed content."  # type: ignore[index]
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]

    with running(StudioService(workspace, approval_engine())) as base_url:
        status, result = request_json(f"{base_url}/api/assessment")

    assert status == 500
    assert result == {"error": "workspace_invalid"}


def test_stale_loaded_policy_rejects_approval_without_a_decision(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]
    engine = approval_engine()
    engine._policy["policy_version"] = "2099.1"
    service = StudioService(workspace, engine)

    with running(service) as base_url:
        status, result = request_json(
            f"{base_url}/api/approve", method="POST",
            value=approval_payload(assessment),  # type: ignore[arg-type]
            approval_token=service.approval_token,
        )

    assert status == 500
    assert result == {"error": "workspace_invalid"}
    assert not (workspace.path / "decision.json").exists()


def test_cross_origin_or_non_json_approval_cannot_mutate_workspace(
    tmp_path: Path, artifacts: dict[str, object]
) -> None:
    assessment = copy.deepcopy(artifacts["strict_assessment"])
    workspace = write_workspace(tmp_path / "workspace", assessment)  # type: ignore[arg-type]
    service = StudioService(workspace, approval_engine())
    payload = approval_payload(assessment)  # type: ignore[arg-type]

    with running(service) as base_url:
        read_status, read_result = request_json(
            f"{base_url}/api/assessment", host="evil.example"
        )
        origin_status, origin_result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token, origin="https://evil.example",
        )
        type_status, type_result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token, content_type="text/plain",
        )
        token_status, token_result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token="wrong-token",
        )
        host_status, host_result = request_json(
            f"{base_url}/api/approve", method="POST", value=payload,
            approval_token=service.approval_token,
            origin="http://evil.example", host="evil.example",
        )

    assert (read_status, read_result) == (403, {"error": "untrusted_request"})
    assert (origin_status, origin_result) == (403, {"error": "untrusted_request"})
    assert (type_status, type_result) == (400, {"error": "invalid_request"})
    assert (token_status, token_result) == (403, {"error": "untrusted_request"})
    assert (host_status, host_result) == (403, {"error": "untrusted_request"})
    assert not (workspace.path / "decision.json").exists()
