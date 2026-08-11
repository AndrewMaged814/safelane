from __future__ import annotations

import json
import os
import re
import secrets
import subprocess
import tempfile
import threading
import copy
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Protocol
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from jsonschema import Draft202012Validator

from .artifacts import (
    ArtifactError,
    _profile,
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
)
from .diff_evidence import DiffSpan, parse_diff, parse_diff_metadata


class PullRequestStudioError(RuntimeError):
    pass


CommandRunner = Callable[[tuple[str, ...], Path | None], bytes]


class GitHubPullRequestProvider:
    """Read open pull requests from one GitHub repository through authenticated `gh`."""

    _FIELDS = (
        "number,title,url,author,headRefName,baseRefName,headRefOid,baseRefOid,"
        "updatedAt,isDraft"
    )

    def __init__(
        self,
        source: str | Path,
        *,
        command_runner: CommandRunner | None = None,
    ) -> None:
        self._runner = command_runner or _run_gh
        self.local_path: Path | None = None
        source_text = str(source)
        candidate = Path(source_text)
        if candidate.is_dir():
            self.local_path = candidate.resolve()
            origin = _run_git_origin(self.local_path)
            self.repository = _github_repository(origin)
        else:
            self.repository = _github_repository(source_text)

    def list_open_pull_requests(self) -> list[dict[str, Any]]:
        raw = self._runner(
            (
                "pr", "list", "--repo", self.repository, "--state", "open",
                "--limit", "100", "--json", self._FIELDS,
            ),
            None,
        )
        try:
            values = json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise PullRequestStudioError("GitHub returned invalid pull-request data") from exc
        if not isinstance(values, list):
            raise PullRequestStudioError("GitHub returned invalid pull-request data")
        pull_requests: list[dict[str, Any]] = []
        for value in values:
            try:
                author = value["author"]["login"]
                item = {
                    "number": value["number"],
                    "title": value["title"],
                    "url": value["url"],
                    "author": author,
                    "head_ref": value["headRefName"],
                    "base_ref": value["baseRefName"],
                    "head_sha": value["headRefOid"],
                    "base_sha": value["baseRefOid"],
                    "updated_at": value["updatedAt"],
                    "is_draft": value["isDraft"],
                }
            except (KeyError, TypeError) as exc:
                raise PullRequestStudioError("GitHub returned an incomplete pull request") from exc
            pull_requests.append(item)
        return pull_requests

    def pull_request_diff(self, number: int, base_sha: str, head_sha: str) -> bytes:
        del number
        return self._runner(
            (
                "api",
                f"repos/{self.repository}/compare/{base_sha}...{head_sha}",
                "--header", "Accept: application/vnd.github.v3.diff",
            ),
            None,
        )


class PullRequestProvider(Protocol):
    repository: str

    def list_open_pull_requests(self) -> list[dict[str, Any]]: ...

    def pull_request_diff(self, number: int, base_sha: str, head_sha: str) -> bytes: ...


class PullRequestAssessor(Protocol):
    def assess(
        self, repository: str, pull_request: dict[str, Any], diff: bytes
    ) -> dict[str, Any]: ...


class PullRequestAnalyzer(Protocol):
    def analyze(
        self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]
    ) -> dict[str, Any]: ...


_ANALYSIS_SCHEMA: dict[str, Any] = {
    "type": "object",
    "additionalProperties": False,
    "required": ["findings"],
    "properties": {
        "findings": {
            "type": "array",
            "maxItems": 1,
            "items": {
                "type": "object",
                "additionalProperties": False,
                "required": [
                    "category",
                    "severity",
                    "hypothesis_kind",
                    "verification_intent_kind",
                    "approval_question_kind",
                    "remediation_kind",
                    "spans",
                ],
                "properties": {
                    "category": {
                        "enum": [
                            "availability", "compatibility", "data", "security",
                            "operability",
                        ]
                    },
                    "severity": {"enum": ["low", "medium", "high"]},
                    "hypothesis_kind": {
                        "const": "changed_behavior_may_violate_contract"
                    },
                    "verification_intent_kind": {
                        "const": "verify_changed_contract_during_rollout"
                    },
                    "approval_question_kind": {
                        "const": "confirm_contract_is_preserved"
                    },
                    "remediation_kind": {
                        "const": "preserve_previous_contract_or_add_compatibility"
                    },
                    "spans": {
                        "type": "array",
                        "minItems": 1,
                        "maxItems": 4,
                        "items": {
                            "type": "object",
                            "additionalProperties": False,
                            "required": ["file", "side", "line", "text"],
                            "properties": {
                                "file": {"type": "string", "minLength": 1},
                                "side": {"enum": ["removed", "added"]},
                                "line": {"type": "integer", "minimum": 1},
                                "text": {"type": "string"},
                            },
                        },
                    },
                },
            },
        }
    },
}

_FINDING_COPY = {
    "availability": (
        "Availability-sensitive change",
        "The local model identified a possible availability impact tied to the "
        "source-verified changed lines below.",
    ),
    "compatibility": (
        "Compatibility-sensitive change",
        "The local model identified a possible compatibility impact tied to the "
        "source-verified changed lines below.",
    ),
    "data": (
        "Data-sensitive change",
        "The local model identified a possible data-integrity impact tied to the "
        "source-verified changed lines below.",
    ),
    "security": (
        "Security-sensitive change",
        "The local model identified a possible security impact tied to the "
        "source-verified changed lines below.",
    ),
    "operability": (
        "Operability-sensitive change",
        "The local model identified a possible operability impact tied to the "
        "source-verified changed lines below.",
    ),
}

_DOCUMENTATION_PREFIXES = ("docs/",)


class OllamaPullRequestAnalyzer:
    """One bounded local-model call whose source citations are verified by normal code."""

    def __init__(
        self,
        *,
        model: str,
        base_url: str = "http://127.0.0.1:11434",
        timeout_seconds: int = 60,
        temperature: int = 0,
        seed: int = 42,
        num_ctx: int = 8192,
        num_predict: int = 768,
    ) -> None:
        self.model = model
        self.base_url = base_url.rstrip("/")
        self.timeout_seconds = timeout_seconds
        self.options = {
            "temperature": temperature,
            "seed": seed,
            "num_ctx": num_ctx,
            "num_predict": num_predict,
        }

    def analyze(
        self, raw_diff: bytes, authorized_spans: list[dict[str, Any]]
    ) -> dict[str, Any]:
        prompt = (
            "Review this exact pull-request diff for concrete release risks. Return only JSON "
            "matching the schema. Every finding must cite one or more tuples copied exactly from "
            "AUTHORIZED SPANS. Do not invent incidents, runtime facts, files, or line numbers. "
            "Choose only a category and severity; SafeLane renders all explanatory prose. "
            "Use an empty findings array when the diff does not support a concrete claim.\n\n"
            f"AUTHORIZED SPANS\n{canonical_json_bytes(authorized_spans).decode().rstrip()}\n\n"
            f"DIFF\n{raw_diff.decode('utf-8')}"
        )
        body = canonical_json_bytes({
            "model": self.model,
            "prompt": prompt,
            "format": _ANALYSIS_SCHEMA,
            "stream": False,
            "options": self.options,
        })
        request = Request(
            f"{self.base_url}/api/generate",
            data=body,
            method="POST",
            headers={"Content-Type": "application/json"},
        )
        try:
            with urlopen(request, timeout=self.timeout_seconds) as response:
                envelope = load_json_bytes(response.read())
            if (
                not isinstance(envelope, dict)
                or envelope.get("model") != self.model
                or envelope.get("done") is not True
                or not isinstance(envelope.get("response"), str)
            ):
                return {"status": "invalid", "findings": []}
            result = load_json_bytes(envelope["response"].encode("utf-8"))
            if list(Draft202012Validator(_ANALYSIS_SCHEMA).iter_errors(result)):
                return {"status": "invalid", "findings": []}
            return {"status": "complete", "findings": result["findings"]}
        except (ArtifactError, HTTPError, URLError, OSError, ValueError):
            return {"status": "unavailable", "findings": []}


class PullRequestAssessmentEngine:
    """Apply deterministic scope floors around optional source-bound AI findings."""

    _TIER_RANK = {"safe": 0, "guarded": 1, "risky": 2}

    def __init__(self, *, policy_path: Path, analyzer: PullRequestAnalyzer) -> None:
        self.policy = load_yaml_bytes(policy_path.read_bytes())
        self.analyzer = analyzer

    def assess(
        self, repository: str, pull_request: dict[str, Any], diff: bytes
    ) -> dict[str, Any]:
        del repository, pull_request
        metadata_files, binary_patch = parse_diff_metadata(diff)
        valid_utf8 = True
        try:
            spans, parsed_files, lines_changed = parse_diff(diff.decode("utf-8"))
        except UnicodeDecodeError:
            valid_utf8 = False
            spans, parsed_files, lines_changed = (), metadata_files, 0
        files = parsed_files or metadata_files
        service_prefixes = tuple(self.policy["release_service"]["path_prefixes"])
        all_paths_recognized = (
            valid_utf8
            and not binary_patch
            and bool(metadata_files)
            and parsed_files == metadata_files
            and not any(path.startswith("<unrecognized-path-") for path in metadata_files)
            and all(_recognized_path(path, service_prefixes) for path in metadata_files)
        )
        authorized_spans = [_span_dict(span) for span in spans]

        ai_status = "complete"
        raw_findings: list[Any] = []
        if not valid_utf8:
            ai_status = "skipped_invalid_diff"
        elif binary_patch:
            ai_status = "skipped_binary_diff"
        elif len(diff) > self.policy["ai"]["max_diff_bytes"]:
            ai_status = "skipped_over_budget"
        else:
            try:
                analysis = self.analyzer.analyze(diff, authorized_spans)
                ai_status = analysis.get("status", "invalid")
                candidate_findings = analysis.get("findings", [])
                if isinstance(candidate_findings, list):
                    raw_findings = candidate_findings
                else:
                    ai_status = "invalid"
            except (OSError, RuntimeError, ValueError):
                ai_status = "unavailable"

        allowed = set(spans)
        findings: list[dict[str, Any]] = []
        invalid_finding = False
        for index, candidate in enumerate(raw_findings[:1], start=1):
            finding = self._verified_finding(candidate, allowed, index)
            if finding is None:
                invalid_finding = True
            else:
                findings.append(finding)
        if invalid_finding and ai_status == "complete":
            ai_status = "partial"

        scope = self.policy["scope"]
        if len(files) >= scope["large_min_files"] or lines_changed >= scope["large_min_lines"]:
            tier = "risky"
            reason = "This PR crosses the large-change safety threshold."
        elif (
            all_paths_recognized
            and len(files) <= scope["small_max_files"]
            and lines_changed <= scope["small_max_lines"]
        ):
            tier = "safe"
            reason = "This PR is inside the bounded Fast scope."
        else:
            tier = "guarded"
            reason = "This PR is outside the bounded Fast scope."

        high = next((item for item in findings if item["severity"] == "high"), None)
        medium = next((item for item in findings if item["severity"] == "medium"), None)
        if high is not None:
            tier, reason = "risky", high["title"]
        elif medium is not None and self._TIER_RANK[tier] < self._TIER_RANK["guarded"]:
            tier, reason = "guarded", medium["title"]
        if ai_status != "complete" and tier == "safe":
            tier = "guarded"
            reason = "AI evidence is incomplete, so SafeLane will not infer Fast eligibility."
        if binary_patch and tier == "safe":
            tier = "guarded"
            reason = "Binary changes require a guarded rollout."

        profile = {"safe": "Fast", "guarded": "Guarded", "risky": "Strict"}[tier]
        option_names = {
            "safe": ["Fast"],
            "guarded": ["Guarded", "Strict"],
            "risky": ["Strict"],
        }[tier]
        return {
            "tier": tier,
            "profile": profile,
            "reason": reason,
            "confidence": "high" if ai_status == "complete" else "low",
            "findings": findings,
            "rollout_options": [_profile(name) for name in option_names],
            "evidence": {
                "ai_status": ai_status,
                "files_changed": len(files),
                "lines_changed": lines_changed,
                "valid_utf8": valid_utf8,
                "binary_patch": binary_patch,
                "all_paths_recognized": all_paths_recognized,
            },
        }

    @staticmethod
    def _verified_finding(
        candidate: Any, allowed: set[DiffSpan], index: int
    ) -> dict[str, Any] | None:
        if not isinstance(candidate, dict):
            return None
        required = {
            "category",
            "severity",
            "hypothesis_kind",
            "verification_intent_kind",
            "approval_question_kind",
            "remediation_kind",
            "spans",
        }
        if set(candidate) != required:
            return None
        if candidate["severity"] not in {"low", "medium", "high"}:
            return None
        if candidate["category"] not in {
            "availability", "compatibility", "data", "security", "operability"
        }:
            return None
        expected_kinds = {
            "hypothesis_kind": "changed_behavior_may_violate_contract",
            "verification_intent_kind": "verify_changed_contract_during_rollout",
            "approval_question_kind": "confirm_contract_is_preserved",
            "remediation_kind": "preserve_previous_contract_or_add_compatibility",
        }
        if any(candidate.get(key) != value for key, value in expected_kinds.items()):
            return None
        cited: list[DiffSpan] = []
        if not isinstance(candidate["spans"], list) or not candidate["spans"]:
            return None
        try:
            for span in candidate["spans"]:
                if not isinstance(span, dict) or set(span) != {"file", "side", "line", "text"}:
                    return None
                cited.append(DiffSpan(span["file"], span["side"], span["line"], span["text"]))
        except (KeyError, TypeError):
            return None
        if any(span not in allowed for span in cited):
            return None
        title, rationale = _FINDING_COPY[candidate["category"]]
        return {
            "id": f"finding-{index:03d}",
            "title": title,
            "category": candidate["category"],
            "severity": candidate["severity"],
            "rationale": rationale,
            "spans": candidate["spans"],
            "safety_case": expected_kinds,
            "source_references_verified": True,
        }


class PullRequestStudioService:
    """Application seam for the repository's open-PR review inbox."""

    def __init__(
        self,
        provider: PullRequestProvider,
        workspace: Path,
        assessor: PullRequestAssessor,
        *,
        state_root: Path | None = None,
        provider_factory: Callable[[str], PullRequestProvider] | None = None,
    ) -> None:
        self.provider = provider
        self.assessor = assessor
        self._lock = threading.RLock()
        self._open: dict[int, dict[str, Any]] = {}
        self.approval_token = secrets.token_urlsafe(32)
        self.state_root = (state_root or workspace.parent).resolve()
        self.state_root.mkdir(parents=True, exist_ok=True)
        self._provider_factory = provider_factory or GitHubPullRequestProvider
        self._set_workspace(workspace)

    def _set_workspace(self, workspace: Path) -> None:
        self.workspace = workspace.resolve()
        self.workspace.mkdir(parents=True, exist_ok=True)
        self.assessments_path = self.workspace / "assessments"
        self.assessments_path.mkdir(exist_ok=True)
        self.decisions_path = self.workspace / "decisions"
        self.decisions_path.mkdir(exist_ok=True)

    def dashboard(self) -> dict[str, Any]:
        with self._lock:
            rows: list[dict[str, Any]] = []
            current: dict[int, dict[str, Any]] = {}
            for pull_request in self.provider.list_open_pull_requests():
                assessment = self._load_or_assess(pull_request)
                current[pull_request["number"]] = assessment
                rows.append(self._row(assessment))
            self._open = current
            rows.sort(
                key=lambda row: (
                    row["review_status"] == "resolved",
                    {"risky": 0, "guarded": 1, "safe": 2}[row["tier"]],
                    row["updated_at"],
                )
            )
            return {
                "repository": self.provider.repository,
                "counts": {
                    "needs_review": sum(
                        row["review_status"] == "unresolved" for row in rows
                    ),
                    "resolved": sum(
                        row["review_status"] == "resolved" for row in rows
                    ),
                },
                "changes": rows,
            }

    def assessment(self, number: int) -> dict[str, Any]:
        with self._lock:
            self.dashboard()
            try:
                return self._open[number]
            except KeyError as exc:
                raise PullRequestStudioError("open pull request was not found") from exc

    def profiles(self) -> dict[str, Any]:
        return {
            "policy_version": getattr(self.assessor, "policy", {}).get(
                "policy_version", "2026.08.3"
            ),
            "profiles": [_profile(name) for name in ("Fast", "Guarded", "Strict")],
        }

    def connect(
        self,
        payload: Any,
        *,
        approval_token: str | None,
    ) -> dict[str, Any]:
        if approval_token is None or not secrets.compare_digest(
            approval_token, self.approval_token
        ):
            raise PullRequestStudioError("invalid connection token")
        if (
            not isinstance(payload, dict)
            or set(payload) != {"repository"}
            or not isinstance(payload["repository"], str)
        ):
            raise PullRequestStudioError("invalid repository connection request")
        source = payload["repository"].strip()
        if not source or len(source) > 2_048:
            raise PullRequestStudioError("repository is required")

        candidate = self._provider_factory(source)
        candidate.list_open_pull_requests()
        with self._lock:
            self.provider = candidate
            self._set_workspace(
                self.state_root / candidate.repository.replace("/", "--")
            )
            self._open = {}
            return self.dashboard()

    def approve(
        self,
        number: int,
        payload: Any,
        *,
        approval_token: str | None,
    ) -> dict[str, Any]:
        if approval_token is None or not secrets.compare_digest(
            approval_token, self.approval_token
        ):
            raise PullRequestStudioError("invalid approval token")
        expected_keys = {
            "selected_profile", "assessment_id", "head_sha",
            "policy_version", "assessment_input_sha256", "assessment_result_sha256",
        }
        if not isinstance(payload, dict) or set(payload) != expected_keys:
            raise PullRequestStudioError("invalid approval request")
        if not all(isinstance(value, str) for value in payload.values()):
            raise PullRequestStudioError("invalid approval request")

        with self._lock:
            current = self.assessment(number)
            if current["review"]["status"] != "unresolved":
                raise PullRequestStudioError("pull request is already resolved")
            expected = {
                "assessment_id": current["assessment_id"],
                "head_sha": current["change"]["head_sha"],
                "policy_version": current["policy_version"],
                "assessment_input_sha256": current["assessment_input_sha256"],
                "assessment_result_sha256": current["assessment_result_sha256"],
            }
            if any(payload[key] != value for key, value in expected.items()):
                raise PullRequestStudioError("approval page is stale")
            if _assessment_hash(current) != current["assessment_result_sha256"]:
                raise PullRequestStudioError("assessment content is inconsistent")
            allowed = {profile["name"] for profile in current["rollout_options"]}
            if payload["selected_profile"] not in allowed:
                raise PullRequestStudioError("rollout profile is not allowed")

            resolved = copy.deepcopy(current)
            resolved_at = _utc_now()
            resolved["review"] = {
                "status": "resolved",
                "resolution": {
                    "type": "human",
                    "selected_profile": payload["selected_profile"],
                    "resolved_at": resolved_at,
                },
            }
            decision = self._decision_document(resolved, payload["selected_profile"])
            assessment_path = self.assessments_path / f"pr-{number}.json"
            decision_path = self.decisions_path / f"pr-{number}.json"
            _atomic_write(assessment_path, canonical_json_bytes(resolved))
            _atomic_write(decision_path, canonical_json_bytes(decision))
            self._open[number] = resolved
            return {**resolved, "decision_path": str(decision_path)}

    def _decision_document(
        self, assessment: dict[str, Any], selected_profile: str
    ) -> dict[str, Any]:
        selected = next(
            profile for profile in assessment["rollout_options"]
            if profile["name"] == selected_profile
        )
        return {
            "schema_version": "studio-pr-review-v1",
            "authorization_scope": "studio_local_review_only",
            "assessment_id": assessment["assessment_id"],
            "policy_version": assessment["policy_version"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "repository": self.provider.repository,
            "pull_request": assessment["change"]["number"],
            "base_sha": assessment["change"]["base_sha"],
            "head_sha": assessment["change"]["head_sha"],
            "tier": assessment["risk"]["tier"],
            "profile": selected,
            "resolution": assessment["review"]["resolution"],
            "note": (
                "This local Studio review record is not release authorization and "
                "must not be consumed by deployment tooling."
            ),
        }

    def _validate_decision_state(
        self, assessment: dict[str, Any], decision_path: Path
    ) -> None:
        if assessment["review"]["status"] == "unresolved":
            if decision_path.exists():
                raise PullRequestStudioError(
                    "unresolved pull request has an authorizing decision"
                )
            return
        if not decision_path.exists():
            raise PullRequestStudioError("resolved pull request is missing its decision")
        try:
            decision = load_json_bytes(decision_path.read_bytes())
        except (OSError, ArtifactError) as exc:
            raise PullRequestStudioError("pull request decision is invalid") from exc
        profile = assessment["review"]["resolution"]["selected_profile"]
        if decision != self._decision_document(assessment, profile):
            raise PullRequestStudioError(
                "pull request decision does not match its assessment"
            )

    def _load_or_assess(self, pull_request: dict[str, Any]) -> dict[str, Any]:
        path = self.assessments_path / f"pr-{pull_request['number']}.json"
        decision_path = self.decisions_path / f"pr-{pull_request['number']}.json"
        if path.exists():
            try:
                cached = load_json_bytes(path.read_bytes())
            except (OSError, ArtifactError):
                cached = None
            if self._matches(cached, pull_request):
                self._validate_decision_state(cached, decision_path)
                return cached

        if decision_path.exists():
            decision_path.unlink()
        diff = self.provider.pull_request_diff(
            pull_request["number"],
            pull_request["base_sha"],
            pull_request["head_sha"],
        )
        result = self.assessor.assess(self.provider.repository, pull_request, diff)
        assessment = self._assessment_document(pull_request, diff, result)
        _atomic_write(path, canonical_json_bytes(assessment))
        if assessment["review"]["status"] == "resolved":
            profile = assessment["review"]["resolution"]["selected_profile"]
            decision = self._decision_document(assessment, profile)
            _atomic_write(decision_path, canonical_json_bytes(decision))
        return assessment

    def _assessment_document(
        self,
        pull_request: dict[str, Any],
        diff: bytes,
        result: dict[str, Any],
    ) -> dict[str, Any]:
        assessed_at = _utc_now()
        assessment: dict[str, Any] = {
            "schema_version": "studio-pr-assessment-v2",
            "artifact_scope": "studio_local_review_only",
            "assessment_id": (
                f"{self.provider.repository}#{pull_request['number']}@"
                f"{pull_request['head_sha']}"
            ),
            "assessed_at": assessed_at,
            "policy_version": self._policy_version(),
            "assessment_input_sha256": "sha256:" + "0" * 64,
            "assessment_result_sha256": "sha256:" + "0" * 64,
            "change": {"repository": self.provider.repository, **pull_request},
            "evidence": {
                "git_diff_sha256": sha256(diff),
                **result["evidence"],
            },
            "findings": result["findings"],
            "risk": {
                key: result[key]
                for key in ("tier", "profile", "reason", "confidence")
            },
            "rollout_options": result["rollout_options"],
            "review": {"status": "unresolved", "resolution": None},
        }
        assessment["assessment_input_sha256"] = _assessment_input_hash(assessment)
        if result["tier"] == "safe":
            assessment["review"] = {
                "status": "resolved",
                "resolution": {
                    "type": "automatic",
                    "selected_profile": "Fast",
                    "resolved_at": assessed_at,
                },
            }
        assessment["assessment_result_sha256"] = _assessment_hash(assessment)
        return assessment

    def _matches(self, value: Any, pull_request: dict[str, Any]) -> bool:
        if (
            not isinstance(value, dict)
            or value.get("schema_version") != "studio-pr-assessment-v2"
            or value.get("artifact_scope") != "studio_local_review_only"
        ):
            return False
        change = value.get("change")
        return (
            isinstance(change, dict)
            and change.get("number") == pull_request["number"]
            and change.get("base_sha") == pull_request["base_sha"]
            and change.get("head_sha") == pull_request["head_sha"]
            and value.get("policy_version") == self._policy_version()
            and value.get("assessment_input_sha256") == _assessment_input_hash(value)
            and value.get("assessment_result_sha256") == _assessment_hash(value)
        )

    def _policy_version(self) -> str:
        policy = getattr(self.assessor, "policy", None)
        version = policy.get("policy_version") if isinstance(policy, dict) else None
        if not isinstance(version, str) or not version:
            raise PullRequestStudioError("assessment policy version is unavailable")
        return version

    @staticmethod
    def _row(assessment: dict[str, Any]) -> dict[str, Any]:
        change = assessment["change"]
        risk = assessment["risk"]
        return {
            "number": change["number"],
            "title": change["title"],
            "author": change["author"],
            "head_ref": change["head_ref"],
            "updated_at": change["updated_at"],
            "tier": risk["tier"],
            "profile": risk["profile"],
            "reason": risk["reason"],
            "review_status": assessment["review"]["status"],
            "head_sha": change["head_sha"],
        }


def _github_repository(source: str) -> str:
    normalized = source.strip().rstrip("/")
    if normalized.endswith(".git"):
        normalized = normalized[:-4]
    ssh = re.fullmatch(r"(?:ssh://)?git@github\.com[:/](?P<repo>[^/]+/[^/]+)", normalized)
    if ssh:
        return ssh.group("repo")
    url = re.fullmatch(r"https?://github\.com/(?P<repo>[^/]+/[^/]+)", normalized)
    if url:
        return url.group("repo")
    slug = re.fullmatch(r"[^/\s]+/[^/\s]+", normalized)
    if slug:
        return normalized
    raise PullRequestStudioError(
        "repository must be a local Git checkout or a github.com owner/repository URL"
    )


def _run_git_origin(repository: Path) -> str:
    environment = os.environ.copy()
    environment.update({"GIT_PAGER": "cat", "LC_ALL": "C", "LANG": "C"})
    try:
        result = subprocess.run(
            ["git", "-C", str(repository), "remote", "get-url", "origin"],
            capture_output=True,
            check=False,
            env=environment,
        )
    except OSError as exc:
        raise PullRequestStudioError("Git is unavailable") from exc
    if result.returncode != 0:
        raise PullRequestStudioError("local repository has no GitHub origin")
    try:
        return result.stdout.decode("utf-8").strip()
    except UnicodeDecodeError as exc:
        raise PullRequestStudioError("Git origin is not valid UTF-8") from exc


def _run_gh(arguments: tuple[str, ...], cwd: Path | None = None) -> bytes:
    environment = os.environ.copy()
    environment.update({"GH_PAGER": "cat", "NO_COLOR": "1"})
    try:
        result = subprocess.run(
            ["gh", *arguments],
            cwd=cwd,
            capture_output=True,
            check=False,
            env=environment,
        )
    except OSError as exc:
        raise PullRequestStudioError("GitHub CLI is unavailable") from exc
    if result.returncode != 0:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise PullRequestStudioError(detail or "GitHub request failed")
    return result.stdout


def _assessment_hash(assessment: dict[str, Any]) -> str:
    immutable = {
        key: value
        for key, value in assessment.items()
        if key not in {"assessment_result_sha256", "review"}
    }
    return sha256(immutable)


def _assessment_input_hash(assessment: dict[str, Any]) -> str:
    change = assessment.get("change")
    evidence = assessment.get("evidence")
    if not isinstance(change, dict) or not isinstance(evidence, dict):
        return ""
    return sha256({
        "schema_version": assessment.get("schema_version"),
        "repository": change.get("repository"),
        "pull_request": change.get("number"),
        "base_sha": change.get("base_sha"),
        "head_sha": change.get("head_sha"),
        "policy_version": assessment.get("policy_version"),
        "git_diff_sha256": evidence.get("git_diff_sha256"),
    })


def _recognized_path(path: str, service_prefixes: tuple[str, ...]) -> bool:
    if path.startswith(service_prefixes):
        return True
    return path == "README.md" or (
        path.endswith(".md") and path.startswith(_DOCUMENTATION_PREFIXES)
    )


def _span_dict(span: DiffSpan) -> dict[str, Any]:
    return {
        "file": span.file,
        "side": span.side,
        "line": span.line,
        "text": span.text,
    }


def _atomic_write(path: Path, raw: bytes) -> None:
    temporary_name: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="wb",
            prefix=f".{path.name}.",
            suffix=".tmp",
            dir=path.parent,
            delete=False,
        ) as temporary:
            temporary_name = temporary.name
            temporary.write(raw)
            temporary.flush()
            os.fsync(temporary.fileno())
        os.replace(temporary_name, path)
        temporary_name = None
    finally:
        if temporary_name is not None:
            try:
                Path(temporary_name).unlink()
            except FileNotFoundError:
                pass


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
