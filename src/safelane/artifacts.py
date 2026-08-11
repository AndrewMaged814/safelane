from __future__ import annotations

import hashlib
import json
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator
import yaml
from yaml.events import AliasEvent


class ArtifactError(ValueError):
    """A persisted artifact is malformed or violates SafeLane's contract."""


_ROOT = Path(__file__).resolve().parents[2]
_SCHEMAS = _ROOT / "schemas"


def _reject_constant(value: str) -> None:
    raise ArtifactError(f"non-finite JSON number: {value}")


def _object_without_duplicates(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ArtifactError(f"duplicate key: {key}")
        result[key] = value
    return result


def load_json_bytes(raw: bytes) -> Any:
    try:
        return json.loads(
            raw.decode("utf-8-sig"),
            object_pairs_hook=_object_without_duplicates,
            parse_constant=_reject_constant,
        )
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ArtifactError(f"invalid JSON: {exc}") from exc


def canonical_json_bytes(value: Any) -> bytes:
    try:
        text = json.dumps(
            _ordered(value),
            ensure_ascii=False,
            allow_nan=False,
            indent=2,
            separators=(",", ": "),
        )
    except (TypeError, ValueError) as exc:
        raise ArtifactError(f"cannot serialize canonical JSON: {exc}") from exc
    return (text + "\n").encode("utf-8")


def sha256(value: Any) -> str:
    raw = value if isinstance(value, bytes) else canonical_json_bytes(value)
    return "sha256:" + hashlib.sha256(raw).hexdigest()


_DECLARED_ORDERS = (
    ("schema_version", "repository", "pull_request", "base_sha", "head_sha"),
    ("schema_version", "request", "policy_sha256", "git_diff_sha256", "git_diff_byte_length", "incident_history", "trusted_probe_catalog_sha256", "ai_configuration"),
    ("provider", "model", "ai_model_digest", "prompt_sha256", "response_schema_sha256", "max_diff_bytes", "timeout_seconds", "attempts", "temperature", "seed", "num_ctx", "num_predict"),
    ("findings", "safeguard_proposal"),
    ("kind", "spans"),
    ("file", "side", "line", "text"),
    ("finding_index", "hypothesis_kind", "verification_intent_kind", "approval_question_kind", "remediation_kind"),
    ("schema_version", "policy_version", "release_service", "scope", "ai", "incident_history", "profile_for_tier", "rollout", "profiles", "trusted_probe_catalog", "job_analysis"),
    ("name", "replicas", "critical", "downstream_dependents", "path_prefixes"),
    ("small_max_files", "small_max_lines", "large_min_files", "large_min_lines"),
    ("provider", "model", "max_diff_bytes", "timeout_seconds", "attempts", "temperature", "seed", "num_ctx", "num_predict", "accepted_finding_kinds"),
    ("enabled",),
    ("safe", "guarded", "risky"),
    ("traffic_router", "max_surge", "max_unavailable"),
    ("Fast", "Guarded", "Strict"),
    ("source", "stages", "analysis"),
    ("source", "stages", "analysis_probe_id"),
    ("path", "non_fast_fallback_probe_id"),
    ("attempts", "interval_seconds", "failure_allowance", "request_timeout_seconds", "active_deadline_seconds"),
    ("schema_version", "assessment_id", "assessed_at", "policy_version", "assessment_input_sha256", "assessment_result_sha256", "change", "evidence", "ai_findings", "selected_safeguard", "policy_trace", "policy_result", "rollout_options", "review"),
    ("repository", "pull_request", "base_sha", "head_sha", "files_changed", "lines_changed", "services", "all_paths_recognized"),
    ("git_diff_sha256", "ai_status", "ai_model_digest", "prompt_sha256", "response_schema_sha256", "incident_history"),
    ("id", "kind", "spans", "source_reference_verified"),
    ("finding_ref", "selection_source", "hypothesis_kind", "hypothesis", "verification_intent_kind", "probe_id", "catalog_entry_sha256", "probe_preview", "approval_question_kind", "approval_question", "remediation_kind", "remediation"),
    ("method", "path", "expected_status", "attempts", "interval_seconds", "failure_allowance", "request_timeout_seconds", "active_deadline_seconds", "canary_only"),
    ("baseline", "safety_floors"),
    ("rule_id", "tier", "reason"),
    ("rule_id", "minimum_tier", "reason"),
    ("final_tier", "minimum_profile", "evidence_confidence", "fast_eligible", "primary_reason"),
    ("status", "resolution"),
    ("type", "selected_profile", "resolved_at", "assessment_id", "head_sha", "assessment_input_sha256", "assessment_result_sha256"),
    ("schema_version", "assessment_id", "assessment_input_sha256", "assessment_result_sha256", "repository", "pull_request", "base_sha", "head_sha", "service", "policy_version", "tier", "primary_reason", "profile", "analysis", "resolution"),
    ("name", "source", "traffic_router", "replicas", "max_surge", "max_unavailable", "stages"),
    ("set_weight", "exposure_pods", "analysis"),
    ("kind", "probe_id", "catalog_entry_sha256", "selection_source", "attempts", "interval_seconds", "failure_allowance", "request_timeout_seconds", "active_deadline_seconds"),
    ("type", "resolved_at"),
    ("schema_version", "repository", "service", "base_sha", "head_sha", "policy_version", "image_catalog_version", "image_catalog_sha256", "image_ref", "image_id", "runtime_image_id"),
    ("schema_version", "catalog_version", "probes"),
    ("id", "binding", "assertion", "execution"),
    ("service", "finding_kind", "hypothesis_kind", "verification_intent_kind", "method", "path"),
    ("expected_status",),
    ("target", "probe_image_key"),
    ("schema_version", "catalog_version", "application_images", "probe_images"),
    ("repository", "service", "source_revision", "image_ref", "image_id", "runtime_image_id", "oci_revision"),
    ("key", "probe_id", "image_ref", "image_id", "runtime_image_id"),
    ("schema_version", "probe_id", "observations", "failures", "failure_allowance", "result"),
    ("attempt", "outcome", "http_status"),
    ("schema_version", "recorded_at", "assessment_result_sha256", "decision_sha256", "release_request_sha256", "image_catalog_sha256", "base_sha", "head_sha", "probe_id", "catalog_entry_sha256", "selection_source", "hypothesis_kind", "rollout", "analyses", "release_adapter_abort_requested", "verdict", "inconclusive_reason"),
    ("name", "uid", "decision_sha256_annotation", "release_request_sha256_annotation", "metadata_generation", "observed_generation", "phase", "abort", "abort_origin", "aborted_at", "progressing_condition", "stable_revision", "current_revision"),
    ("type", "status", "reason", "message"),
    ("stage_index", "analysis_run", "canary_target", "job", "probe_result"),
    ("name", "uid", "owner_rollout_uid", "phase", "completed_at"),
    ("service_name", "service_uid", "service_selector_pod_template_hash", "replica_set_uid", "replica_set_pod_template_hash", "source_revision", "exposure_pods", "endpoint_pod_uids", "application_image_ref", "application_runtime_image_id"),
    ("name", "uid", "owner_analysis_run_uid", "phase", "container_started", "probe_container_exit_code", "probe_pod_uid", "probe_pod_owner_job_uid", "probe_image_ref", "probe_runtime_image_id"),
    ("schema_version", "id", "diff_path", "git_diff_sha256", "expected_normalized_result", "accepted_spans", "forbidden_result"),
)
_ORDERS_BY_KEYS = {frozenset(order): order for order in _DECLARED_ORDERS}


def _ordered(value: Any) -> Any:
    if isinstance(value, dict):
        order = _ORDERS_BY_KEYS.get(frozenset(value))
        keys = order if order is not None else tuple(value)
        return {key: _ordered(value[key]) for key in keys}
    if isinstance(value, list):
        return [_ordered(item) for item in value]
    return value


class _StrictLoader(yaml.SafeLoader):
    def compose_node(self, parent: Any, index: Any) -> Any:
        if self.check_event(AliasEvent):
            raise ArtifactError("YAML aliases and anchors are not allowed")
        return super().compose_node(parent, index)

    def construct_mapping(self, node: Any, deep: bool = False) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key_node, value_node in node.value:
            key = self.construct_object(key_node, deep=deep)
            if not isinstance(key, str):
                raise ArtifactError("YAML mapping keys must be strings")
            if key == "<<":
                raise ArtifactError("YAML merge keys are not allowed")
            if key in result:
                raise ArtifactError(f"duplicate key: {key}")
            result[key] = self.construct_object(value_node, deep=deep)
        return result


def load_yaml_bytes(raw: bytes) -> Any:
    try:
        value = yaml.load(raw.decode("utf-8-sig"), Loader=_StrictLoader)
    except ArtifactError:
        raise
    except (UnicodeDecodeError, yaml.YAMLError) as exc:
        raise ArtifactError(f"invalid YAML: {exc}") from exc
    return _ordered(value)


def validate_artifact(schema_name: str, value: Any) -> None:
    schema_path = _SCHEMAS / f"{schema_name}.schema.json"
    if not schema_path.is_file():
        raise ArtifactError(f"unknown artifact schema: {schema_name}")
    schema = load_json_bytes(schema_path.read_bytes())
    errors = sorted(Draft202012Validator(schema).iter_errors(value), key=lambda error: list(error.path))
    if errors:
        error = errors[0]
        location = ".".join(str(part) for part in error.absolute_path) or "$"
        raise ArtifactError(f"{schema_name} {location}: {error.message}")
    _validate_semantics(schema_name, value)


def _validate_semantics(schema_name: str, value: Any) -> None:
    if schema_name == "decision-v3":
        _validate_decision(value)
    elif schema_name == "image-catalog-v1":
        _validate_image_catalog(value)
    elif schema_name == "probe-result-v1":
        _validate_probe_result(value)


def _validate_decision(decision: dict[str, Any]) -> None:
    tier = decision["tier"]
    profile = decision["profile"]["name"]
    resolution = decision["resolution"]["type"]
    analysis = decision["analysis"]
    matrix = {
        "safe": ({"Fast"}, "automatic", {None}),
        "guarded": ({"Guarded", "Strict"}, "human", {"policy_fallback"}),
        "risky": ({"Strict"}, "human", {"ai_safeguard", "policy_fallback"}),
    }
    profiles, resolution_type, sources = matrix[tier]
    source = None if analysis is None else analysis["selection_source"]
    if profile not in profiles or resolution != resolution_type or source not in sources:
        raise ArtifactError("decision-v3 violates the tier/profile/resolution/analysis matrix")
    expected = _profile(profile)
    if decision["profile"] != expected:
        raise ArtifactError(f"decision-v3 profile does not byte-match built-in {profile}")


def _profile(name: str) -> dict[str, Any]:
    stages = {
        "Fast": [(100, 5, False)],
        "Guarded": [(40, 2, True), (100, 5, False)],
        "Strict": [(20, 1, True), (40, 2, True), (60, 3, True), (100, 5, False)],
    }[name]
    return {
        "name": name,
        "source": "built_in",
        "traffic_router": "none",
        "replicas": 5,
        "max_surge": 1,
        "max_unavailable": 0,
        "stages": [
            {"set_weight": weight, "exposure_pods": pods, "analysis": analysis}
            for weight, pods, analysis in stages
        ],
    }


def _validate_image_catalog(catalog: dict[str, Any]) -> None:
    identities: set[tuple[str, str, str]] = set()
    for image in catalog["application_images"]:
        identity = (image["repository"], image["service"], image["source_revision"])
        if identity in identities:
            raise ArtifactError("duplicate application image identity")
        identities.add(identity)
        if image["oci_revision"] != image["source_revision"]:
            raise ArtifactError("application OCI revision does not match source revision")
        if image["image_ref"] != f'safelane-demo:{image["source_revision"]}':
            raise ArtifactError("application image tag is not derived from the full source SHA")
    keys: set[str] = set()
    probe_ids: set[str] = set()
    for image in catalog["probe_images"]:
        if image["key"] in keys or image["probe_id"] in probe_ids:
            raise ArtifactError("duplicate probe image key or probe ID")
        keys.add(image["key"])
        probe_ids.add(image["probe_id"])


def _validate_probe_result(result: dict[str, Any]) -> None:
    observations = result["observations"]
    if [item["attempt"] for item in observations] != list(range(1, len(observations) + 1)):
        raise ArtifactError("probe attempts must be contiguous")
    failures = sum(
        item["outcome"] != "http_response" or item["http_status"] != 200
        for item in observations
    )
    expected_result = "failed" if failures > result["failure_allowance"] else "passed"
    if result["failures"] != failures or result["result"] != expected_result:
        raise ArtifactError("probe result is internally inconsistent")
