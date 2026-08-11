from __future__ import annotations

import copy
import os
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator

from .artifacts import (
    ArtifactError,
    _profile,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from .diff_evidence import DiffSpan, parse_diff, parse_diff_metadata
from .risk_finder import AiAttempt, RiskFinder


class AssessmentError(RuntimeError):
    """The caller's request, policy, worktree, or Git range is invalid."""


class ResolutionError(RuntimeError):
    """An approval does not authorize the current assessment."""


@dataclass(frozen=True)
class AssessmentArtifacts:
    assessment: dict[str, Any]
    automatic_decision: dict[str, Any] | None


@dataclass(frozen=True)
class ResolvedArtifacts:
    assessment: dict[str, Any]
    decision: dict[str, Any]


@dataclass(frozen=True)
class _GitEvidence:
    raw: bytes
    spans: frozenset[DiffSpan]
    files: tuple[str, ...]
    lines_changed: int
    valid_utf8: bool
    binary_patch: bool


_TIER_RANK = {"safe": 0, "guarded": 1, "risky": 2}
_BASELINES = {
    "safe": ("scope.low", "The change affects at most 2 recognized files and 50 changed lines."),
    "guarded": ("scope.medium", "The change is outside the bounded Fast scope."),
    "risky": ("scope.high", "The change affects at least 10 files or 500 changed lines."),
}
_FLOORS = {
    "finding.breaking_api": ("risky", "An existing HTTP contract was removed or renamed."),
    "evidence.path_unrecognized": ("guarded", "At least one changed path is unrecognized or incompletely decoded."),
    "evidence.diff_invalid_utf8": ("guarded", "The complete Git diff could not be decoded as UTF-8."),
    "evidence.diff_over_budget": ("guarded", "The complete Git diff exceeds the AI evidence budget."),
    "evidence.ai_incomplete": ("guarded", "AI evidence was unavailable, invalid, unsupported, or unverifiable."),
    "evidence.safeguard_invalid": ("guarded", "The AI safeguard proposal was invalid or could not resolve to a trusted probe."),
}


class SafeLaneEngine:
    def __init__(
        self,
        *,
        policy_path: Path,
        trusted_probes_path: Path,
        risk_finder: RiskFinder,
    ) -> None:
        self._policy = load_yaml_bytes(policy_path.read_bytes())
        self._trusted_probes = load_yaml_bytes(trusted_probes_path.read_bytes())
        validate_artifact("policy-v2", self._policy)
        validate_artifact("trusted-probes-v1", self._trusted_probes)
        self._risk_finder = risk_finder

    def assess(self, worktree: Path, request_path: Path, assessed_at: str) -> AssessmentArtifacts:
        try:
            request = load_json_bytes(request_path.read_bytes())
            validate_artifact("assessment-request-v2", request)
        except (OSError, ArtifactError) as exc:
            raise AssessmentError(str(exc)) from exc
        evidence = self._read_git_evidence(worktree, request)
        if evidence.binary_patch:
            raise AssessmentError("binary patches are unsupported")
        recognized_prefixes = tuple(self._policy["release_service"]["path_prefixes"])
        recognized_files = [
            path for path in evidence.files if path.startswith(recognized_prefixes)
        ]
        if not recognized_files:
            raise AssessmentError("change must affect exactly one mapped release service")
        all_paths_recognized = len(recognized_files) == len(evidence.files)
        services = ["demo-api"]

        attempt, ai_status, findings, safeguard = self._assess_ai(evidence, all_paths_recognized)
        baseline_tier = self._baseline(len(evidence.files), evidence.lines_changed, all_paths_recognized)
        floors: list[str] = []
        if findings:
            floors.append("finding.breaking_api")
        if not all_paths_recognized:
            floors.append("evidence.path_unrecognized")
        if not evidence.valid_utf8:
            floors.append("evidence.diff_invalid_utf8")
        if len(evidence.raw) > self._policy["ai"]["max_diff_bytes"]:
            floors.append("evidence.diff_over_budget")
        if ai_status in {"partial", "unavailable"}:
            floors.append("evidence.ai_incomplete")
        if findings and safeguard is None:
            floors.append("evidence.safeguard_invalid")

        final_tier = baseline_tier
        for rule_id in floors:
            floor_tier = _FLOORS[rule_id][0]
            if _TIER_RANK[floor_tier] > _TIER_RANK[final_tier]:
                final_tier = floor_tier
        primary_reason = _BASELINES[baseline_tier][1]
        for rule_id in floors:
            tier, reason = _FLOORS[rule_id]
            if tier == final_tier:
                primary_reason = reason
                break
        confidence = "low" if any(rule.startswith("evidence.") for rule in floors) else "high"
        fast_eligible = final_tier == "safe" and ai_status == "complete" and not findings
        minimum_profile = {"safe": "Fast", "guarded": "Guarded", "risky": "Strict"}[final_tier]
        rollout_names = {
            "safe": ["Fast"],
            "guarded": ["Guarded", "Strict"],
            "risky": ["Strict"],
        }[final_tier]

        git_hash = sha256(evidence.raw)
        policy_hash = sha256(self._policy)
        catalog_hash = sha256(self._trusted_probes)
        input_envelope = {
            "schema_version": "1",
            "request": request,
            "policy_sha256": policy_hash,
            "git_diff_sha256": git_hash,
            "git_diff_byte_length": len(evidence.raw),
            "incident_history": "disabled_by_policy",
            "trusted_probe_catalog_sha256": catalog_hash,
            "ai_configuration": {
                "provider": "ollama",
                "model": self._policy["ai"]["model"],
                "ai_model_digest": attempt.model_digest if attempt else None,
                "prompt_sha256": attempt.prompt_sha256 if attempt else None,
                "response_schema_sha256": attempt.response_schema_sha256 if attempt else None,
                "max_diff_bytes": self._policy["ai"]["max_diff_bytes"],
                "timeout_seconds": self._policy["ai"]["timeout_seconds"],
                "attempts": self._policy["ai"]["attempts"],
                "temperature": self._policy["ai"]["temperature"],
                "seed": self._policy["ai"]["seed"],
                "num_ctx": self._policy["ai"]["num_ctx"],
                "num_predict": self._policy["ai"]["num_predict"],
            },
        }
        assessment_input_hash = sha256(input_envelope)
        assessment_id = (
            f'{request["repository"]}#{request["pull_request"]}@{request["head_sha"]}:'
            f'{self._policy["policy_version"]}'
        )
        assessment: dict[str, Any] = {
            "schema_version": "2",
            "assessment_id": assessment_id,
            "assessed_at": assessed_at,
            "policy_version": self._policy["policy_version"],
            "assessment_input_sha256": assessment_input_hash,
            "assessment_result_sha256": "sha256:" + "0" * 64,
            "change": {
                "repository": request["repository"],
                "pull_request": request["pull_request"],
                "base_sha": request["base_sha"],
                "head_sha": request["head_sha"],
                "files_changed": len(evidence.files),
                "lines_changed": evidence.lines_changed,
                "services": services,
                "all_paths_recognized": all_paths_recognized,
            },
            "evidence": {
                "git_diff_sha256": git_hash,
                "ai_status": ai_status,
                "ai_model_digest": attempt.model_digest if attempt else None,
                "prompt_sha256": attempt.prompt_sha256 if attempt else None,
                "response_schema_sha256": attempt.response_schema_sha256 if attempt else None,
                "incident_history": "disabled_by_policy",
            },
            "ai_findings": findings,
            "selected_safeguard": safeguard,
            "policy_trace": {
                "baseline": {
                    "rule_id": _BASELINES[baseline_tier][0],
                    "tier": baseline_tier,
                    "reason": _BASELINES[baseline_tier][1],
                },
                "safety_floors": [
                    {"rule_id": rule, "minimum_tier": _FLOORS[rule][0], "reason": _FLOORS[rule][1]}
                    for rule in floors
                ],
            },
            "policy_result": {
                "final_tier": final_tier,
                "minimum_profile": minimum_profile,
                "evidence_confidence": confidence,
                "fast_eligible": fast_eligible,
                "primary_reason": primary_reason,
            },
            "rollout_options": [_profile(name) for name in rollout_names],
            "review": {"status": "unresolved", "resolution": None},
        }
        assessment["assessment_result_sha256"] = _assessment_result_hash(assessment)

        decision = None
        if fast_eligible:
            event = self._resolution_event(assessment, "automatic", "Fast", assessed_at)
            assessment["review"] = {"status": "resolved", "resolution": event}
            decision = self._decision(assessment, event)
        validate_artifact("assessment-v2", assessment)
        if decision is not None:
            validate_artifact("decision-v3", decision)
        return AssessmentArtifacts(assessment, decision)

    def approve(self, current_assessment: dict[str, Any], human_event: dict[str, Any]) -> ResolvedArtifacts:
        validate_artifact("assessment-v2", current_assessment)
        if current_assessment["review"]["status"] != "unresolved":
            raise ResolutionError("assessment is already resolved")
        if _assessment_result_hash(current_assessment) != current_assessment["assessment_result_sha256"]:
            raise ResolutionError("reviewed result hash does not match assessment content")
        expected = {
            "assessment_id": current_assessment["assessment_id"],
            "head_sha": current_assessment["change"]["head_sha"],
            "assessment_input_sha256": current_assessment["assessment_input_sha256"],
            "assessment_result_sha256": current_assessment["assessment_result_sha256"],
        }
        if set(human_event) != {
            "type", "selected_profile", "resolved_at", *expected.keys()
        }:
            raise ResolutionError("invalid human resolution event shape")
        if human_event["type"] != "human":
            raise ResolutionError("non-Fast resolution must be human")
        for key, value in expected.items():
            if human_event[key] != value:
                raise ResolutionError(f"stale {key}")
        allowed = {profile["name"] for profile in current_assessment["rollout_options"]}
        if human_event["selected_profile"] not in allowed:
            raise ResolutionError("selected profile is not an allowed rollout option")
        resolved = copy.deepcopy(current_assessment)
        resolved["review"] = {"status": "resolved", "resolution": copy.deepcopy(human_event)}
        decision = self._decision(resolved, human_event)
        validate_artifact("assessment-v2", resolved)
        validate_artifact("decision-v3", decision)
        return ResolvedArtifacts(resolved, decision)

    def _read_git_evidence(self, worktree: Path, request: dict[str, Any]) -> _GitEvidence:
        def run(*args: str, text: bool = False) -> subprocess.CompletedProcess[Any]:
            environment = os.environ.copy()
            environment.update({"LC_ALL": "C", "LANG": "C", "GIT_PAGER": "cat"})
            completed = subprocess.run(
                ["git", "-C", str(worktree), *args],
                check=True,
                capture_output=True,
                text=text,
                env=environment,
            )
            if completed.stderr:
                raise AssessmentError("Git command wrote to stderr")
            return completed

        try:
            if run("status", "--porcelain", text=True).stdout:
                raise AssessmentError("worktree must be clean")
            for sha in (request["base_sha"], request["head_sha"]):
                run("cat-file", "-e", f"{sha}^{{commit}}")
            parent = run("rev-parse", f'{request["head_sha"]}^', text=True).stdout.strip()
            if parent != request["base_sha"]:
                raise AssessmentError("head_sha must be a direct child of base_sha")
            result = run(
                "-c", "core.quotePath=true", "diff", "--no-ext-diff", "--no-textconv", "--no-color",
                "--no-renames", "--unified=3", "--src-prefix=a/", "--dst-prefix=b/",
                request["base_sha"], request["head_sha"], "--",
            )
        except subprocess.CalledProcessError as exc:
            raise AssessmentError("invalid Git worktree or revision range") from exc
        raw = result.stdout
        files, binary_patch = parse_diff_metadata(raw)
        try:
            text_diff = raw.decode("utf-8")
            valid_utf8 = True
            parsed_spans, _, lines_changed = parse_diff(text_diff)
            spans = frozenset(parsed_spans)
        except UnicodeDecodeError:
            valid_utf8 = False
            spans, lines_changed = frozenset(), 0
        return _GitEvidence(raw, spans, files, lines_changed, valid_utf8, binary_patch)

    def _assess_ai(
        self, evidence: _GitEvidence, all_paths_recognized: bool
    ) -> tuple[AiAttempt | None, str, list[dict[str, Any]], dict[str, Any] | None]:
        if not evidence.valid_utf8:
            return None, "skipped_invalid_diff", [], None
        if len(evidence.raw) > self._policy["ai"]["max_diff_bytes"]:
            return None, "skipped_over_budget", [], None
        attempt = self._risk_finder.find(evidence.raw)
        if attempt.status != "complete" or attempt.raw_response is None:
            return attempt, "unavailable", [], None
        try:
            response = load_json_bytes(attempt.raw_response)
        except ArtifactError:
            return attempt, "partial", [], None
        if (
            not isinstance(response, dict)
            or set(response) != {"findings", "safeguard_proposal"}
            or not isinstance(response["findings"], list)
            or len(response["findings"]) > 1
        ):
            return attempt, "partial", [], None
        if not response["findings"]:
            if response["safeguard_proposal"] is not None:
                return attempt, "partial", [], None
            return attempt, "complete", [], None
        finding = response["findings"][0]
        schema = load_json_bytes(
            (Path(__file__).resolve().parents[2] / "schemas" / "ai-response-v2.schema.json").read_bytes()
        )
        finding_schema = {"$defs": schema["$defs"], **schema["$defs"]["finding"]}
        if list(Draft202012Validator(finding_schema).iter_errors(finding)):
            return attempt, "partial", [], None
        candidates = [
            DiffSpan(span["file"], span["side"], span["line"], span["text"])
            for span in finding["spans"]
        ]
        expected_roles = (
            candidates[0].side == "removed"
            and candidates[0].text == '@app.get("/v1/quote")'
            and candidates[1].side == "added"
            and candidates[1].text == '@app.get("/v2/quote")'
        )
        if not all_paths_recognized or not expected_roles or any(span not in evidence.spans for span in candidates):
            return attempt, "partial", [], None
        accepted = [{
            "id": "finding-001",
            "kind": "breaking_api",
            "spans": finding["spans"],
            "source_reference_verified": True,
        }]
        proposal = response["safeguard_proposal"]
        if proposal is None:
            return attempt, "partial", accepted, None
        proposal_schema = {"$defs": schema["$defs"], **schema["$defs"]["proposal"]}
        if list(Draft202012Validator(proposal_schema).iter_errors(proposal)):
            return attempt, "partial", accepted, None
        expected_proposal = {
            "finding_index": 0,
            "hypothesis_kind": "removed_http_route_unavailable",
            "verification_intent_kind": "preserve_removed_http_route",
            "approval_question_kind": "confirm_callers_migrated",
            "remediation_kind": "retain_removed_route_as_alias",
        }
        if proposal != expected_proposal:
            return attempt, "partial", accepted, None
        probe = self._trusted_probes["probes"][0]
        binding = probe["binding"]
        if binding != {
            "service": "demo-api",
            "finding_kind": "breaking_api",
            "hypothesis_kind": "removed_http_route_unavailable",
            "verification_intent_kind": "preserve_removed_http_route",
            "method": "GET",
            "path": "/v1/quote",
        }:
            return attempt, "partial", accepted, None
        job = self._policy["job_analysis"]
        safeguard = {
            "finding_ref": "finding-001",
            "selection_source": "ai_safeguard",
            "hypothesis_kind": proposal["hypothesis_kind"],
            "hypothesis": "Existing callers of GET /v1/quote may receive a non-success response.",
            "verification_intent_kind": proposal["verification_intent_kind"],
            "probe_id": probe["id"],
            "catalog_entry_sha256": sha256(probe),
            "probe_preview": {
                "method": binding["method"],
                "path": binding["path"],
                "expected_status": probe["assertion"]["expected_status"],
                "attempts": job["attempts"],
                "interval_seconds": job["interval_seconds"],
                "failure_allowance": job["failure_allowance"],
                "request_timeout_seconds": job["request_timeout_seconds"],
                "active_deadline_seconds": job["active_deadline_seconds"],
                "canary_only": True,
            },
            "approval_question_kind": proposal["approval_question_kind"],
            "approval_question": "Have all callers migrated away from GET /v1/quote?",
            "remediation_kind": proposal["remediation_kind"],
            "remediation": "Retain GET /v1/quote as an alias while introducing GET /v2/quote, then reassess.",
        }
        return attempt, "complete", accepted, safeguard

    def _baseline(self, files: int, lines: int, recognized: bool) -> str:
        if files >= self._policy["scope"]["large_min_files"] or lines >= self._policy["scope"]["large_min_lines"]:
            return "risky"
        if recognized and files <= self._policy["scope"]["small_max_files"] and lines <= self._policy["scope"]["small_max_lines"]:
            return "safe"
        return "guarded"

    def _resolution_event(
        self, assessment: dict[str, Any], event_type: str, profile: str, resolved_at: str
    ) -> dict[str, Any]:
        return {
            "type": event_type,
            "selected_profile": profile,
            "resolved_at": resolved_at,
            "assessment_id": assessment["assessment_id"],
            "head_sha": assessment["change"]["head_sha"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
        }

    def _decision(self, assessment: dict[str, Any], event: dict[str, Any]) -> dict[str, Any]:
        profile = next(
            profile for profile in assessment["rollout_options"]
            if profile["name"] == event["selected_profile"]
        )
        analysis = None
        if profile["name"] != "Fast":
            selected = assessment["selected_safeguard"]
            probe = self._trusted_probes["probes"][0]
            job = self._policy["job_analysis"]
            analysis = {
                "kind": "job_http_contract_probe",
                "probe_id": probe["id"],
                "catalog_entry_sha256": sha256(probe),
                "selection_source": "ai_safeguard" if selected is not None else "policy_fallback",
                "attempts": job["attempts"],
                "interval_seconds": job["interval_seconds"],
                "failure_allowance": job["failure_allowance"],
                "request_timeout_seconds": job["request_timeout_seconds"],
                "active_deadline_seconds": job["active_deadline_seconds"],
            }
        change = assessment["change"]
        return {
            "schema_version": "3",
            "assessment_id": assessment["assessment_id"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "repository": change["repository"],
            "pull_request": change["pull_request"],
            "base_sha": change["base_sha"],
            "head_sha": change["head_sha"],
            "service": "demo-api",
            "policy_version": assessment["policy_version"],
            "tier": assessment["policy_result"]["final_tier"],
            "primary_reason": assessment["policy_result"]["primary_reason"],
            "profile": copy.deepcopy(profile),
            "analysis": analysis,
            "resolution": {"type": event["type"], "resolved_at": event["resolved_at"]},
        }


def _assessment_result_hash(assessment: dict[str, Any]) -> str:
    immutable_result = {
        key: value for key, value in assessment.items()
        if key not in {"assessment_result_sha256", "review"}
    }
    return sha256(immutable_result)
