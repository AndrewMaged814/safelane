from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest

from safelane.artifacts import canonical_json_bytes, validate_artifact
from safelane.demo_repository import create_demo_repository
from safelane.engine import ResolutionError, SafeLaneEngine
from safelane.risk_finder import FakeRiskFinder


ROOT = Path(__file__).parents[2]


def engine_with_response(relative_path: str) -> SafeLaneEngine:
    return SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo" / "trusted-probes.yaml",
        risk_finder=FakeRiskFinder((ROOT / relative_path).read_bytes()),
    )


@pytest.fixture
def demo_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "repository"
    create_demo_repository(repo)
    return repo


def test_fast_assessment_resolves_automatically_through_engine_seam(demo_repo: Path) -> None:
    artifacts = engine_with_response("demo/expected/ai-fast.json").assess(
        demo_repo,
        ROOT / "demo" / "requests" / "fast.json",
        "2026-08-09T12:00:00Z",
    )

    assessment = artifacts.assessment
    decision = artifacts.automatic_decision
    assert assessment["policy_result"] == {
        "final_tier": "safe",
        "minimum_profile": "Fast",
        "evidence_confidence": "high",
        "fast_eligible": True,
        "primary_reason": "The change affects at most 2 recognized files and 50 changed lines.",
    }
    assert assessment["review"]["resolution"]["type"] == "automatic"
    assert decision is not None
    assert decision["profile"]["name"] == "Fast"
    assert decision["analysis"] is None
    validate_artifact("assessment-v2", assessment)
    validate_artifact("decision-v3", decision)


def test_risky_assessment_requires_explicit_strict_approval(demo_repo: Path) -> None:
    engine = engine_with_response("demo/expected/ai-strict.json")
    artifacts = engine.assess(
        demo_repo,
        ROOT / "demo" / "requests" / "strict.json",
        "2026-08-09T12:00:00Z",
    )

    assessment = artifacts.assessment
    assert artifacts.automatic_decision is None
    assert assessment["policy_result"]["final_tier"] == "risky"
    assert assessment["review"] == {"status": "unresolved", "resolution": None}
    assert assessment["selected_safeguard"]["probe_id"] == "demo-api-public-quote-v1"
    assert assessment["selected_safeguard"]["probe_preview"]["path"] == "/v1/quote"

    event = {
        "type": "human",
        "selected_profile": "Strict",
        "resolved_at": "2026-08-09T12:05:00Z",
        "assessment_id": assessment["assessment_id"],
        "head_sha": assessment["change"]["head_sha"],
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": assessment["assessment_result_sha256"],
    }
    resolved = engine.approve(assessment, event)

    assert resolved.assessment["review"]["status"] == "resolved"
    assert resolved.decision["resolution"]["type"] == "human"
    assert resolved.decision["profile"]["name"] == "Strict"
    assert resolved.decision["analysis"]["selection_source"] == "ai_safeguard"
    validate_artifact("assessment-v2", resolved.assessment)
    validate_artifact("decision-v3", resolved.decision)


def test_stale_approval_hash_emits_no_decision(demo_repo: Path) -> None:
    engine = engine_with_response("demo/expected/ai-strict.json")
    assessment = engine.assess(
        demo_repo,
        ROOT / "demo" / "requests" / "strict.json",
        "2026-08-09T12:00:00Z",
    ).assessment
    event = {
        "type": "human",
        "selected_profile": "Strict",
        "resolved_at": "2026-08-09T12:05:00Z",
        "assessment_id": assessment["assessment_id"],
        "head_sha": assessment["change"]["head_sha"],
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": "sha256:" + "0" * 64,
    }

    with pytest.raises(ResolutionError, match="stale assessment_result_sha256"):
        engine.approve(assessment, event)


def test_approval_recomputes_reviewed_result_hash_before_authorizing(demo_repo: Path) -> None:
    engine = engine_with_response("demo/expected/ai-strict.json")
    assessment = engine.assess(
        demo_repo, ROOT / "demo" / "requests" / "strict.json", "2026-08-09T12:00:00Z"
    ).assessment
    assessment["policy_result"]["primary_reason"] = "Schema-valid but unreviewed content."
    event = {
        "type": "human", "selected_profile": "Strict", "resolved_at": "2026-08-09T12:05:00Z",
        "assessment_id": assessment["assessment_id"], "head_sha": assessment["change"]["head_sha"],
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": assessment["assessment_result_sha256"],
    }

    with pytest.raises(ResolutionError, match="reviewed result hash"):
        engine.approve(assessment, event)


def test_fabricated_span_never_produces_trusted_safeguard_or_decision(demo_repo: Path) -> None:
    response = json.loads((ROOT / "demo" / "expected" / "ai-strict.json").read_text(encoding="utf-8"))
    response["findings"][0]["spans"][0]["line"] = 999
    engine = SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo" / "trusted-probes.yaml",
        risk_finder=FakeRiskFinder(json.dumps(response).encode()),
    )

    artifacts = engine.assess(
        demo_repo,
        ROOT / "demo" / "requests" / "strict.json",
        "2026-08-09T12:00:00Z",
    )

    assert artifacts.automatic_decision is None
    assert artifacts.assessment["selected_safeguard"] is None
    assert artifacts.assessment["policy_result"]["evidence_confidence"] == "low"
    assert "evidence.ai_incomplete" in {
        floor["rule_id"] for floor in artifacts.assessment["policy_trace"]["safety_floors"]
    }


def test_invalid_proposal_cannot_erase_verified_breaking_finding(demo_repo: Path) -> None:
    response = json.loads((ROOT / "demo" / "expected" / "ai-strict.json").read_text(encoding="utf-8"))
    response["safeguard_proposal"]["probe_id"] = "model-authored-probe"
    engine = SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo" / "trusted-probes.yaml",
        risk_finder=FakeRiskFinder(json.dumps(response).encode()),
    )

    artifacts = engine.assess(
        demo_repo, ROOT / "demo" / "requests" / "strict.json", "2026-08-09T12:00:00Z"
    )

    assert [finding["kind"] for finding in artifacts.assessment["ai_findings"]] == ["breaking_api"]
    assert artifacts.assessment["selected_safeguard"] is None
    assert artifacts.assessment["policy_result"]["final_tier"] == "risky"
    assert "evidence.safeguard_invalid" in {
        floor["rule_id"] for floor in artifacts.assessment["policy_trace"]["safety_floors"]
    }


def test_fixed_inputs_fake_ai_and_clock_produce_byte_identical_artifacts(demo_repo: Path) -> None:
    first = engine_with_response("demo/expected/ai-fast.json").assess(
        demo_repo, ROOT / "demo" / "requests" / "fast.json", "2026-08-09T12:00:00Z"
    )
    second = engine_with_response("demo/expected/ai-fast.json").assess(
        demo_repo, ROOT / "demo" / "requests" / "fast.json", "2026-08-09T12:00:00Z"
    )

    assert canonical_json_bytes(first.assessment) == canonical_json_bytes(second.assessment)
    assert canonical_json_bytes(first.automatic_decision) == canonical_json_bytes(second.automatic_decision)
    resolution = first.assessment["review"]["resolution"]
    assert resolution["assessment_input_sha256"] == first.assessment["assessment_input_sha256"]
    assert resolution["assessment_result_sha256"] == first.assessment["assessment_result_sha256"]


def test_one_engine_instance_assesses_fast_then_strict(demo_repo: Path) -> None:
    finder = FakeRiskFinder([
        (ROOT / "demo/expected/ai-fast.json").read_bytes(),
        (ROOT / "demo/expected/ai-strict.json").read_bytes(),
    ])
    engine = SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=ROOT / "demo/trusted-probes.yaml",
        risk_finder=finder,
    )

    fast = engine.assess(demo_repo, ROOT / "demo/requests/fast.json", "2026-08-09T12:00:00Z")
    strict = engine.assess(demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:01:00Z")

    assert fast.automatic_decision["profile"]["name"] == "Fast"
    assert strict.automatic_decision is None
    assert strict.assessment["policy_result"]["final_tier"] == "risky"
    assert len(finder.calls) == 2


def test_deleted_unknown_path_is_not_attributed_to_preceding_known_file(
    demo_repo: Path, tmp_path: Path
) -> None:
    unknown = demo_repo / "zzz-unknown.txt"
    unknown.write_text("unknown\n", encoding="utf-8")
    subprocess.run(["git", "-C", str(demo_repo), "add", "."], check=True)
    subprocess.run(["git", "-C", str(demo_repo), "commit", "-m", "test: add unknown path"], check=True)
    base = subprocess.run(
        ["git", "-C", str(demo_repo), "rev-parse", "HEAD"], check=True, capture_output=True, text=True
    ).stdout.strip()
    app = demo_repo / "src/demo_api/app.py"
    app.write_text(app.read_text(encoding="utf-8") + "\n# known change\n", encoding="utf-8")
    unknown.unlink()
    subprocess.run(["git", "-C", str(demo_repo), "add", "-A"], check=True)
    subprocess.run(["git", "-C", str(demo_repo), "commit", "-m", "test: delete unknown path"], check=True)
    head = subprocess.run(
        ["git", "-C", str(demo_repo), "rev-parse", "HEAD"], check=True, capture_output=True, text=True
    ).stdout.strip()
    request = tmp_path / "request.json"
    request.write_text(json.dumps({
        "schema_version": "2", "repository": "AndrewMaged814/safelane-demo", "pull_request": 99,
        "base_sha": base, "head_sha": head,
    }), encoding="utf-8")

    artifacts = engine_with_response("demo/expected/ai-fast.json").assess(
        demo_repo, request, "2026-08-09T12:00:00Z"
    )

    assert artifacts.assessment["change"]["all_paths_recognized"] is False
    assert artifacts.assessment["policy_result"]["final_tier"] == "guarded"
    assert artifacts.automatic_decision is None
