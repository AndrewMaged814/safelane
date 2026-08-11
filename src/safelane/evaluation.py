from __future__ import annotations

import re
from pathlib import Path
from typing import Any

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from .diff_evidence import parse_diff
from .risk_finder import AiAttempt, OllamaRiskFinder, RiskFinder


ROOT = Path(__file__).resolve().parents[2]
_CASE_IDS = ("fast-copy", "additive-route", "quote-contract-break")
_EXPECTED_PROBE_ID = "demo-api-public-quote-v1"


def run_ollama_evaluation(
    output_path: Path,
    *,
    risk_finder: RiskFinder | None = None,
    base_url: str = "http://127.0.0.1:11434",
) -> bool:
    """Run the frozen six-observation local-model gate and persist every attempt."""
    if risk_finder is None:
        policy = load_yaml_bytes((ROOT / "policy.yaml").read_bytes())
        ai = policy["ai"]
        risk_finder = OllamaRiskFinder(
            model=ai["model"],
            max_diff_bytes=ai["max_diff_bytes"],
            timeout_seconds=ai["timeout_seconds"],
            temperature=ai["temperature"],
            seed=ai["seed"],
            num_ctx=ai["num_ctx"],
            num_predict=ai["num_predict"],
            base_url=base_url,
        )
    else:
        policy = load_yaml_bytes((ROOT / "policy.yaml").read_bytes())

    ai = policy["ai"]
    expected_settings = {
        "provider": ai["provider"],
        "model": ai["model"],
        "max_diff_bytes": ai["max_diff_bytes"],
        "timeout_seconds": ai["timeout_seconds"],
        "attempts": ai["attempts"],
        "truncate": False,
        "temperature": ai["temperature"],
        "seed": ai["seed"],
        "num_ctx": ai["num_ctx"],
        "num_predict": ai["num_predict"],
    }

    observations: list[dict[str, Any]] = []
    for case_id in _CASE_IDS:
        manifest = _load_manifest(case_id)
        diff = (ROOT / manifest["diff_path"]).read_bytes()
        if sha256(diff) != manifest["git_diff_sha256"]:
            raise ArtifactError(f"{case_id} canonical diff hash mismatch")
        expected = manifest["expected_normalized_result"]
        validate_artifact("ai-response-v2", expected)
        for run in (1, 2):
            attempt = risk_finder.find(diff)
            observations.append(
                _observation(case_id, run, diff, manifest, attempt, expected_settings)
            )

    passed_count = sum(item["passed"] for item in observations)
    passed = passed_count == len(observations)
    report = {
        "schema_version": "1",
        "gate": "Gate B",
        "summary": {
            "passed": passed,
            "result": f"{passed_count}/{len(observations)} fixture observations",
        },
        "observations": observations,
    }
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_bytes(canonical_json_bytes(report))
    return passed


def _load_manifest(case_id: str) -> dict[str, Any]:
    manifest = load_json_bytes(
        (ROOT / "demo" / "evaluation" / f"{case_id}.manifest.json").read_bytes()
    )
    expected_keys = {
        "schema_version", "id", "diff_path", "git_diff_sha256",
        "expected_normalized_result", "accepted_spans", "forbidden_result",
    }
    if (
        not isinstance(manifest, dict)
        or set(manifest) != expected_keys
        or manifest["schema_version"] != "1"
        or manifest["id"] != case_id
    ):
        raise ArtifactError(f"invalid {case_id} evaluation manifest")
    return manifest


def _observation(
    case_id: str,
    run: int,
    diff: bytes,
    manifest: dict[str, Any],
    attempt: AiAttempt,
    expected_settings: dict[str, Any],
) -> dict[str, Any]:
    normalized: dict[str, Any] | None = None
    trusted_probe_id: str | None = None
    error = attempt.error
    if attempt.status == "complete" and attempt.raw_response is not None:
        try:
            candidate = load_json_bytes(attempt.raw_response)
            validate_artifact("ai-response-v2", candidate)
            normalized = candidate
            if candidate != manifest["expected_normalized_result"]:
                error = "unexpected_normalized_result"
            elif not _spans_match_diff(diff, manifest["accepted_spans"]):
                error = "unverified_source_span"
            elif candidate["findings"]:
                trusted_probe_id = _resolve_trusted_probe(candidate)
                if trusted_probe_id is None:
                    error = "untrusted_safeguard"
        except ArtifactError:
            error = "invalid_response"
    elif error is None:
        error = "ai_unavailable"

    if error is None:
        if (
            not _is_sha256(attempt.model_digest)
            or not _is_sha256(attempt.prompt_sha256)
            or not _is_sha256(attempt.response_schema_sha256)
            or attempt.settings != expected_settings
            or not attempt.raw_response
        ):
            error = "incomplete_audit_record"
        elif not 0 <= attempt.latency_ms <= expected_settings["timeout_seconds"] * 1000:
            error = "timeout_exceeded"

    passed = error is None
    return {
        "case_id": case_id,
        "run": run,
        "git_diff_sha256": sha256(diff),
        "status": attempt.status,
        "model_digest": attempt.model_digest,
        "prompt_sha256": attempt.prompt_sha256,
        "response_schema_sha256": attempt.response_schema_sha256,
        "settings": attempt.settings,
        "latency_ms": attempt.latency_ms,
        "raw_transport_response": _decode_optional(attempt.raw_transport_response),
        "raw_response": _decode_optional(attempt.raw_response),
        "normalized_result": normalized,
        "trusted_probe_id": trusted_probe_id,
        "passed": passed,
        "error": error,
    }


def _spans_match_diff(diff: bytes, accepted_spans: list[dict[str, Any]]) -> bool:
    try:
        parsed, _, _ = parse_diff(diff.decode("utf-8"))
    except UnicodeDecodeError:
        return False
    expected = {
        (span["file"], span["side"], span["line"], span["text"])
        for span in accepted_spans
    }
    actual = {(span.file, span.side, span.line, span.text) for span in parsed}
    return expected <= actual


def _resolve_trusted_probe(candidate: dict[str, Any]) -> str | None:
    proposal = candidate["safeguard_proposal"]
    finding = candidate["findings"][0]
    if proposal is None or finding["kind"] != "breaking_api":
        return None
    catalog = load_yaml_bytes((ROOT / "demo" / "trusted-probes.yaml").read_bytes())
    for probe in catalog["probes"]:
        binding = probe["binding"]
        if binding == {
            "service": "demo-api",
            "finding_kind": "breaking_api",
            "hypothesis_kind": proposal["hypothesis_kind"],
            "verification_intent_kind": proposal["verification_intent_kind"],
            "method": "GET",
            "path": "/v1/quote",
        } and probe["id"] == _EXPECTED_PROBE_ID:
            return probe["id"]
    return None


def _decode_optional(raw: bytes | None) -> str | None:
    if raw is None:
        return None
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        return None


def _is_sha256(value: str | None) -> bool:
    return isinstance(value, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", value) is not None
