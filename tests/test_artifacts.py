from __future__ import annotations

import json

import pytest

from safelane.artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from jsonschema import Draft202012Validator


def test_canonical_json_uses_declared_order_lf_and_final_newline() -> None:
    artifact = {"schema_version": "2", "repository": "example/repo", "pull_request": 42}

    assert canonical_json_bytes(artifact) == (
        b'{\n'
        b'  "schema_version": "2",\n'
        b'  "repository": "example/repo",\n'
        b'  "pull_request": 42\n'
        b'}\n'
    )


def test_canonical_json_preserves_declared_model_order_for_hash_envelopes() -> None:
    envelope = {
        "schema_version": "1",
        "request": {"schema_version": "2"},
        "policy_sha256": "sha256:" + "a" * 64,
        "git_diff_sha256": "sha256:" + "b" * 64,
        "git_diff_byte_length": 12,
        "incident_history": "disabled_by_policy",
    }

    keys = list(json.loads(canonical_json_bytes(envelope)).keys())
    assert keys == list(envelope)


def test_canonical_json_reorders_known_models_to_declared_order() -> None:
    declared = {
        "schema_version": "2",
        "repository": "AndrewMaged814/safelane-demo",
        "pull_request": 42,
        "base_sha": "0" * 40,
        "head_sha": "1" * 40,
    }
    scrambled = {key: declared[key] for key in reversed(declared)}

    assert canonical_json_bytes(scrambled) == canonical_json_bytes(declared)


def test_json_decoder_rejects_duplicate_keys() -> None:
    with pytest.raises(ArtifactError, match="duplicate key: schema_version"):
        load_json_bytes(b'{"schema_version":"2","schema_version":"3"}')


def test_request_schema_accepts_full_identity_and_rejects_unknown_fields() -> None:
    request = {
        "schema_version": "2",
        "repository": "AndrewMaged814/safelane-demo",
        "pull_request": 42,
        "base_sha": "0" * 40,
        "head_sha": "1" * 40,
    }
    validate_artifact("assessment-request-v2", request)

    with pytest.raises(ArtifactError, match="unexpected"):
        validate_artifact("assessment-request-v2", request | {"tier": "safe"})


@pytest.mark.parametrize(
    ("tier", "profile", "resolution", "analysis"),
    [
        ("safe", "Strict", "automatic", None),
        ("safe", "Fast", "human", None),
        ("guarded", "Fast", "human", "policy_fallback"),
        ("guarded", "Guarded", "automatic", "policy_fallback"),
        ("risky", "Strict", "automatic", "ai_safeguard"),
        ("risky", "Strict", "human", None),
    ],
)
def test_decision_matrix_rejects_unauthorized_combinations(
    tier: str, profile: str, resolution: str, analysis: str | None
) -> None:
    decision = json.loads((FIXTURES / "decision-strict.example.json").read_text(encoding="utf-8"))
    decision["tier"] = tier
    decision["profile"]["name"] = profile
    decision["resolution"]["type"] = resolution
    if analysis is None:
        decision["analysis"] = None
    else:
        decision["analysis"]["selection_source"] = analysis

    with pytest.raises(ArtifactError):
        validate_artifact("decision-v3", decision)


from pathlib import Path

FIXTURES = Path(__file__).parents[1] / "demo" / "expected"


@pytest.mark.parametrize(
    ("schema_name", "relative_path"),
    [
        ("assessment-request-v2", "demo/requests/fast.json"),
        ("assessment-request-v2", "demo/requests/strict.json"),
        ("ai-response-v2", "demo/expected/ai-fast.json"),
        ("ai-response-v2", "demo/expected/ai-strict.json"),
        ("decision-v3", "demo/expected/decision-fast.example.json"),
        ("decision-v3", "demo/expected/decision-strict.example.json"),
        ("release-request-v1", "demo/expected/release-request-fast.example.json"),
        ("release-request-v1", "demo/expected/release-request-strict.example.json"),
        ("image-catalog-v1", "demo/image-catalog.example.json"),
        ("probe-result-v1", "demo/expected/probe-result-failed.example.json"),
    ],
)
def test_checked_in_json_contract_examples_validate(schema_name: str, relative_path: str) -> None:
    value = load_json_bytes(Path(relative_path).read_bytes())
    validate_artifact(schema_name, value)


def test_trusted_probe_yaml_hash_ignores_comments_and_key_order() -> None:
    first = load_yaml_bytes(Path("demo/trusted-probes.yaml").read_bytes())
    reordered = load_yaml_bytes(Path("tests/fixtures/trusted-probes-reordered.yaml").read_bytes())

    validate_artifact("trusted-probes-v1", first)
    validate_artifact("trusted-probes-v1", reordered)
    assert sha256(first) == sha256(reordered)


def test_policy_schema_rejects_nested_unknown_fields() -> None:
    policy = load_yaml_bytes(Path("policy.yaml").read_bytes())
    policy["ai"]["best_of"] = 3

    with pytest.raises(ArtifactError):
        validate_artifact("policy-v2", policy)


def test_decision_schema_itself_enforces_authorization_matrix() -> None:
    decision = load_json_bytes((FIXTURES / "decision-strict.example.json").read_bytes())
    decision["resolution"]["type"] = "automatic"
    schema = load_json_bytes(Path("schemas/decision-v3.schema.json").read_bytes())

    assert list(Draft202012Validator(schema).iter_errors(decision))


def test_assessment_schema_rejects_arbitrary_rollout_and_resolution_payloads() -> None:
    schema = load_json_bytes(Path("schemas/assessment-v2.schema.json").read_bytes())
    rollout_validator = Draft202012Validator({"$defs": schema["$defs"], **schema["$defs"]["profile"]})
    resolution_validator = Draft202012Validator({"$defs": schema["$defs"], **schema["$defs"]["resolution"]})

    assert list(rollout_validator.iter_errors({"name": "Whatever"}))
    assert list(resolution_validator.iter_errors({"approved": True}))


def test_receipt_schema_discriminates_verdict_and_closed_reason_enum() -> None:
    receipt = load_json_bytes((FIXTURES / "receipt-strict.example.json").read_bytes())
    validate_artifact("verification-receipt-v1", receipt)
    receipt["verdict"] = "inconclusive"
    receipt["inconclusive_reason"] = "made_up_reason"

    with pytest.raises(ArtifactError):
        validate_artifact("verification-receipt-v1", receipt)


def test_receipt_observed_verdict_rejects_successful_all_200_evidence() -> None:
    receipt = load_json_bytes((FIXTURES / "receipt-strict.example.json").read_bytes())
    analysis = receipt["analyses"][0]
    analysis["analysis_run"]["phase"] = "Successful"
    analysis["job"]["phase"] = "Complete"
    analysis["job"]["probe_container_exit_code"] = 0
    for observation in analysis["probe_result"]["observations"]:
        observation["http_status"] = 200
    analysis["probe_result"]["failures"] = 0
    analysis["probe_result"]["result"] = "passed"

    with pytest.raises(ArtifactError):
        validate_artifact("verification-receipt-v1", receipt)


def test_receipt_observed_verdict_rejects_mixed_transport_evidence() -> None:
    receipt = load_json_bytes((FIXTURES / "receipt-strict.example.json").read_bytes())
    observation = receipt["analyses"][0]["probe_result"]["observations"][2]
    observation["outcome"] = "timeout"
    observation["http_status"] = None

    with pytest.raises(ArtifactError):
        validate_artifact("verification-receipt-v1", receipt)


def test_assessment_schema_ties_risky_tier_to_strict_only() -> None:
    assessment = load_json_bytes((FIXTURES / "assessment-strict.example.json").read_bytes())
    fast = load_json_bytes((FIXTURES / "decision-fast.example.json").read_bytes())["profile"]
    assessment["rollout_options"] = [fast]

    with pytest.raises(ArtifactError):
        validate_artifact("assessment-v2", assessment)


def test_resolved_risky_assessment_cannot_select_faster_unlisted_profile() -> None:
    assessment = load_json_bytes((FIXTURES / "assessment-strict.example.json").read_bytes())
    assessment["review"] = {
        "status": "resolved",
        "resolution": {
            "type": "human",
            "selected_profile": "Fast",
            "resolved_at": "2026-08-09T12:05:00Z",
            "assessment_id": assessment["assessment_id"],
            "head_sha": assessment["change"]["head_sha"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
        },
    }

    with pytest.raises(ArtifactError):
        validate_artifact("assessment-v2", assessment)


def test_strict_decision_and_release_request_share_frozen_identity() -> None:
    decision = load_json_bytes((FIXTURES / "decision-strict.example.json").read_bytes())
    release = load_json_bytes((FIXTURES / "release-request-strict.example.json").read_bytes())

    assert (decision["base_sha"], decision["head_sha"]) == (release["base_sha"], release["head_sha"])
