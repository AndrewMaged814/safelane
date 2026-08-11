from __future__ import annotations

import copy
import os
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Literal, Protocol

import yaml

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from .pr_studio import PullRequestAnalyzer, PullRequestAssessmentEngine


class ChangeSafetyError(RuntimeError):
    """A pull request could not be safely assessed or resolved."""


class RepositoryNotConfigured(ChangeSafetyError):
    pass


class PolicyInvalid(ChangeSafetyError):
    pass


class ChangeMoved(ChangeSafetyError):
    pass


class AssessmentNotFound(ChangeSafetyError):
    pass


class AssessmentStale(ChangeSafetyError):
    pass


class AlreadyResolved(ChangeSafetyError):
    pass


class ProfileNotAllowed(ChangeSafetyError):
    pass


@dataclass(frozen=True)
class PullRequestRef:
    repository: str
    number: int


@dataclass(frozen=True)
class PullRequestSnapshot:
    repository: str
    number: int
    title: str
    url: str
    author: str
    base_ref: str
    head_ref: str
    base_sha: str
    head_sha: str
    updated_at: str
    is_draft: bool


@dataclass(frozen=True)
class AssessmentHandle:
    assessment_id: str
    assessment_result_sha256: str


@dataclass(frozen=True)
class AssessmentOutcome:
    assessment: dict[str, Any]
    automatic_decision: dict[str, Any] | None
    handle: AssessmentHandle


@dataclass(frozen=True)
class ResolutionCommand:
    handle: AssessmentHandle
    action: Literal["approve", "reject", "decide_later"]
    selected_profile: str | None
    actor: str


@dataclass(frozen=True)
class ResolutionOutcome:
    assessment: dict[str, Any]
    decision: dict[str, Any] | None


@dataclass(frozen=True)
class ReleaseBinding:
    handle: AssessmentHandle
    image: str


@dataclass(frozen=True)
class RolloutBundle:
    manifest: dict[str, Any]
    decision_sha256: str
    path: Path


class PullRequestHost(Protocol):
    def get_pull_request(self, change: PullRequestRef) -> PullRequestSnapshot: ...

    def read_file(self, repository: str, revision: str, path: str) -> bytes: ...

    def diff(self, snapshot: PullRequestSnapshot) -> bytes: ...


class CheckPublisher(Protocol):
    def publish(
        self, assessment: dict[str, Any], *, check_run_id: int | None = None
    ) -> Any: ...


AnalyzerFactory = Callable[[dict[str, Any]], PullRequestAnalyzer]
Clock = Callable[[], str]


class ChangeSafety:
    """Deep module for exact-revision pull-request safety decisions."""

    POLICY_PATH = ".safelane/policy.yaml"

    def __init__(
        self,
        *,
        host: PullRequestHost,
        state_dir: Path,
        analyzer_factory: AnalyzerFactory,
        check_publisher: CheckPublisher | None = None,
        clock: Clock | None = None,
    ) -> None:
        self._host = host
        self._state_dir = state_dir.resolve()
        self._analyzer_factory = analyzer_factory
        self._check_publisher = check_publisher
        self._clock = clock or _utc_now

    def assess(self, change: PullRequestRef) -> AssessmentOutcome:
        snapshot = self._host.get_pull_request(change)
        self._validate_snapshot(change, snapshot)
        policy_bytes = self._read_base_policy(snapshot)
        policy = self._load_policy(policy_bytes, snapshot.base_sha)
        diff = self._host.diff(snapshot)
        cached = self._cached(snapshot, policy_bytes, diff)
        if cached is not None:
            self._publish_check(cached.assessment)
            return cached
        analyzer = self._analyzer_factory(copy.deepcopy(policy))
        evaluator = PullRequestAssessmentEngine(
            policy=policy,
            analyzer=analyzer,
        )
        result = evaluator.assess(snapshot.repository, _snapshot_dict(snapshot), diff)
        rollout_catalog = [
            evaluator._profile(name) for name in ("Fast", "Guarded", "Strict")
        ]

        current = self._host.get_pull_request(change)
        if current.head_sha != snapshot.head_sha:
            raise ChangeMoved(
                f"pull request moved from {snapshot.head_sha} to {current.head_sha}"
            )

        assessment = self._assessment(
            snapshot, policy, policy_bytes, diff, result, rollout_catalog
        )
        decision = None
        if result["tier"] == "safe":
            resolution = {
                "type": "automatic",
                "selected_profile": "Fast",
                "resolved_at": assessment["assessed_at"],
            }
            assessment["review"] = {"status": "approved", "resolution": resolution}
            decision = self._decision(assessment, "Fast", resolution)
        self._write(snapshot, assessment, decision)
        self._publish_check(assessment)
        return AssessmentOutcome(
            assessment=assessment,
            automatic_decision=decision,
            handle=AssessmentHandle(
                assessment["assessment_id"], assessment["assessment_result_sha256"]
            ),
        )

    def _cached(
        self,
        snapshot: PullRequestSnapshot,
        policy_bytes: bytes,
        diff: bytes,
    ) -> AssessmentOutcome | None:
        directory = self._directory(snapshot.repository, snapshot.number)
        try:
            assessment = load_json_bytes((directory / "assessment.json").read_bytes())
        except (OSError, ArtifactError):
            return None
        expected = {
            "repository": snapshot.repository,
            "number": snapshot.number,
            "base_sha": snapshot.base_sha,
            "head_sha": snapshot.head_sha,
        }
        if not isinstance(assessment, dict) or assessment.get("schema_version") != "change-assessment-v1":
            return None
        if any(assessment.get("change", {}).get(key) != value for key, value in expected.items()):
            return None
        if assessment.get("policy", {}).get("sha256") != sha256(policy_bytes):
            return None
        if assessment.get("evidence", {}).get("git_diff_sha256") != sha256(diff):
            return None
        result_hash = assessment.get("assessment_result_sha256")
        if result_hash != _assessment_result_hash(assessment):
            return None
        decision = None
        resolution = assessment.get("review", {}).get("resolution")
        if isinstance(resolution, dict) and resolution.get("type") == "automatic":
            try:
                candidate = load_json_bytes((directory / "decision.json").read_bytes())
            except (OSError, ArtifactError):
                return None
            if (
                candidate.get("assessment_id") != assessment["assessment_id"]
                or candidate.get("assessment_result_sha256") != result_hash
            ):
                return None
            decision = candidate
        return AssessmentOutcome(
            assessment=assessment,
            automatic_decision=decision,
            handle=AssessmentHandle(assessment["assessment_id"], result_hash),
        )

    def resolve(self, command: ResolutionCommand) -> ResolutionOutcome:
        assessment_path = self._assessment_path(command.handle)
        try:
            assessment = load_json_bytes(assessment_path.read_bytes())
        except (OSError, ArtifactError) as exc:
            raise AssessmentNotFound(command.handle.assessment_id) from exc
        if assessment.get("assessment_id") != command.handle.assessment_id:
            raise AssessmentStale("assessment identity does not match the handle")
        if (
            assessment.get("assessment_result_sha256")
            != command.handle.assessment_result_sha256
            or _assessment_result_hash(assessment)
            != command.handle.assessment_result_sha256
        ):
            raise AssessmentStale("assessment content does not match the reviewed handle")
        if assessment["review"]["status"] != "unresolved":
            raise AlreadyResolved(command.handle.assessment_id)
        if not command.actor.strip():
            raise ChangeSafetyError("resolution actor is required")
        if command.action == "decide_later":
            return ResolutionOutcome(assessment=assessment, decision=None)

        change = PullRequestRef(
            assessment["change"]["repository"], assessment["change"]["number"]
        )
        current = self._host.get_pull_request(change)
        if current.head_sha != assessment["change"]["head_sha"]:
            raise AssessmentStale("pull request has a newer head revision")
        policy_bytes = self._read_base_policy(current)
        if sha256(policy_bytes) != assessment["policy"]["sha256"]:
            raise AssessmentStale("base policy no longer matches the assessment")

        selected_profile = command.selected_profile
        if command.action == "reject":
            if selected_profile is not None:
                raise ProfileNotAllowed("rejection cannot select a rollout profile")
        else:
            allowed = {item["name"] for item in assessment["rollout_options"]}
            if selected_profile not in allowed:
                raise ProfileNotAllowed("selected profile is not allowed")

        resolved_at = self._clock()
        resolution = {
            "type": "human",
            "action": command.action,
            "actor": command.actor,
            "selected_profile": selected_profile,
            "resolved_at": resolved_at,
        }
        resolved = copy.deepcopy(assessment)
        resolved["review"] = {
            "status": "approved" if command.action == "approve" else "rejected",
            "resolution": resolution,
        }
        decision = (
            self._decision(resolved, selected_profile, resolution)
            if selected_profile is not None
            else None
        )
        _atomic_write(assessment_path, canonical_json_bytes(resolved))
        if decision is not None:
            _atomic_write(
                assessment_path.with_name("decision.json"),
                canonical_json_bytes(decision),
            )
        self._publish_check(resolved)
        return ResolutionOutcome(assessment=resolved, decision=decision)

    def compile(self, binding: ReleaseBinding) -> RolloutBundle:
        assessment_path = self._assessment_path(binding.handle)
        try:
            assessment = load_json_bytes(assessment_path.read_bytes())
            decision = load_json_bytes(
                assessment_path.with_name("decision.json").read_bytes()
            )
        except (OSError, ArtifactError) as exc:
            raise AssessmentNotFound(binding.handle.assessment_id) from exc
        if (
            assessment.get("assessment_id") != binding.handle.assessment_id
            or assessment.get("assessment_result_sha256")
            != binding.handle.assessment_result_sha256
            or _assessment_result_hash(assessment)
            != binding.handle.assessment_result_sha256
        ):
            raise AssessmentStale("release binding does not match the assessment")
        review = assessment.get("review", {})
        resolution = review.get("resolution")
        if review.get("status") != "approved" or not isinstance(resolution, dict):
            raise AssessmentStale("release requires an approved assessment")
        selected_profile = resolution.get("selected_profile")
        expected_decision = self._decision(
            assessment, selected_profile, resolution
        )
        if decision != expected_decision:
            raise AssessmentStale("rollout decision does not match the approved assessment")
        if not re.fullmatch(r".+@sha256:[0-9a-f]{64}", binding.image):
            raise ChangeSafetyError("release image must use an immutable sha256 digest")

        change = PullRequestRef(
            assessment["change"]["repository"], assessment["change"]["number"]
        )
        current = self._host.get_pull_request(change)
        if current.head_sha != assessment["change"]["head_sha"]:
            raise AssessmentStale("pull request has a newer head revision")
        policy_bytes = self._read_base_policy(current)
        if sha256(policy_bytes) != assessment["policy"]["sha256"]:
            raise AssessmentStale("base policy no longer matches the assessment")
        policy = self._load_policy(policy_bytes, current.base_sha)

        decision_hash = sha256(decision)
        deployment = policy["release_service"]["deployment"]
        labels = {"app.kubernetes.io/name": deployment["workload_label"]}
        steps: list[dict[str, Any]] = []
        for stage in decision["profile"]["stages"]:
            steps.append({"setWeight": stage["set_weight"]})
            if stage["analysis"]:
                steps.append({
                    "analysis": {
                        "templates": [{
                            "templateName": deployment["analysis_template"]
                        }]
                    }
                })
        manifest = {
            "apiVersion": "argoproj.io/v1alpha1",
            "kind": "Rollout",
            "metadata": {
                "name": deployment["rollout_name"],
                "namespace": deployment["namespace"],
                "annotations": {
                    "safelane.dev/assessment-id": assessment["assessment_id"],
                    "safelane.dev/assessment-result-sha256": assessment[
                        "assessment_result_sha256"
                    ],
                    "safelane.dev/decision-sha256": decision_hash,
                    "safelane.dev/head-sha": assessment["change"]["head_sha"],
                    "safelane.dev/policy-sha256": assessment["policy"]["sha256"],
                },
            },
            "spec": {
                "replicas": policy["release_service"]["replicas"],
                "selector": {"matchLabels": dict(labels)},
                "template": {
                    "metadata": {"labels": dict(labels)},
                    "spec": {
                        "containers": [{
                            "name": deployment["container_name"],
                            "image": binding.image,
                        }]
                    },
                },
                "strategy": {
                    "canary": {
                        "maxSurge": policy["rollout"]["max_surge"],
                        "maxUnavailable": policy["rollout"]["max_unavailable"],
                        "steps": steps,
                    }
                },
            },
        }
        validate_artifact("argo-rollout-v1", manifest)
        release_directory = assessment_path.parent / "release"
        release_directory.mkdir(exist_ok=True)
        path = release_directory / "rollout.yaml"
        raw = yaml.safe_dump(
            manifest, sort_keys=False, allow_unicode=True
        ).encode("utf-8")
        _atomic_write(path, raw)
        return RolloutBundle(manifest=manifest, decision_sha256=decision_hash, path=path)

    def _read_base_policy(self, snapshot: PullRequestSnapshot) -> bytes:
        try:
            return self._host.read_file(
                snapshot.repository, snapshot.base_sha, self.POLICY_PATH
            )
        except (KeyError, OSError, RuntimeError) as exc:
            raise RepositoryNotConfigured(
                f"{snapshot.repository}@{snapshot.base_sha} has no {self.POLICY_PATH}"
            ) from exc

    @staticmethod
    def _load_policy(raw: bytes, base_sha: str) -> dict[str, Any]:
        try:
            policy = load_yaml_bytes(raw)
            validate_artifact("repository-policy-v1", policy)
            _validate_policy_semantics(policy)
        except (ArtifactError, TypeError, ValueError) as exc:
            raise PolicyInvalid(f"policy at {base_sha} is invalid: {exc}") from exc
        return policy

    def _assessment(
        self,
        snapshot: PullRequestSnapshot,
        policy: dict[str, Any],
        policy_bytes: bytes,
        diff: bytes,
        result: dict[str, Any],
        rollout_catalog: list[dict[str, Any]],
    ) -> dict[str, Any]:
        assessed_at = self._clock()
        policy_hash = sha256(policy_bytes)
        input_hash = sha256({
            "repository": snapshot.repository,
            "pull_request": snapshot.number,
            "base_sha": snapshot.base_sha,
            "head_sha": snapshot.head_sha,
            "policy_sha256": policy_hash,
            "git_diff_sha256": sha256(diff),
        })
        assessment: dict[str, Any] = {
            "schema_version": "change-assessment-v1",
            "assessment_id": (
                f"{snapshot.repository}#{snapshot.number}@{snapshot.head_sha}:"
                f"{policy['policy_version']}"
            ),
            "assessed_at": assessed_at,
            "assessment_input_sha256": input_hash,
            "assessment_result_sha256": "sha256:" + "0" * 64,
            "policy": {
                "version": policy["policy_version"],
                "source_revision": snapshot.base_sha,
                "sha256": policy_hash,
            },
            "change": _snapshot_dict(snapshot),
            "service": copy.deepcopy(policy["release_service"]),
            "risk": {
                "tier": result["tier"],
                "minimum_profile": result["profile"],
                "reason": result["reason"],
                "confidence": result["confidence"],
            },
            "evidence": {
                "git_diff_sha256": sha256(diff),
                **copy.deepcopy(result["evidence"]),
            },
            "findings": copy.deepcopy(result["findings"]),
            "rollout_options": copy.deepcopy(result["rollout_options"]),
            "rollout_catalog": copy.deepcopy(rollout_catalog),
            "review": {"status": "unresolved", "resolution": None},
        }
        assessment["assessment_result_sha256"] = _assessment_result_hash(assessment)
        return assessment

    @staticmethod
    def _decision(
        assessment: dict[str, Any], selected_profile: str, resolution: dict[str, Any]
    ) -> dict[str, Any]:
        profile = next(
            item for item in assessment["rollout_options"]
            if item["name"] == selected_profile
        )
        return {
            "schema_version": "rollout-decision-v1",
            "assessment_id": assessment["assessment_id"],
            "assessment_input_sha256": assessment["assessment_input_sha256"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "repository": assessment["change"]["repository"],
            "pull_request": assessment["change"]["number"],
            "base_sha": assessment["change"]["base_sha"],
            "head_sha": assessment["change"]["head_sha"],
            "policy": copy.deepcopy(assessment["policy"]),
            "service": assessment["service"]["name"],
            "tier": assessment["risk"]["tier"],
            "profile": copy.deepcopy(profile),
            "resolution": copy.deepcopy(resolution),
        }

    def _write(
        self,
        snapshot: PullRequestSnapshot,
        assessment: dict[str, Any],
        decision: dict[str, Any] | None,
    ) -> None:
        directory = self._directory(snapshot.repository, snapshot.number)
        directory.mkdir(parents=True, exist_ok=True)
        _atomic_write(directory / "assessment.json", canonical_json_bytes(assessment))
        if decision is not None:
            _atomic_write(directory / "decision.json", canonical_json_bytes(decision))

    def _assessment_path(self, handle: AssessmentHandle) -> Path:
        try:
            repository, rest = handle.assessment_id.split("#", 1)
            number_text, _ = rest.split("@", 1)
            number = int(number_text)
        except (ValueError, TypeError) as exc:
            raise AssessmentNotFound(handle.assessment_id) from exc
        return (
            self._directory(repository, number)
            / "assessment.json"
        )

    def _directory(self, repository: str, number: int) -> Path:
        return self._state_dir / repository.replace("/", "--") / f"pr-{number}"

    def _publish_check(self, assessment: dict[str, Any]) -> None:
        if self._check_publisher is None:
            return
        directory = self._directory(
            assessment["change"]["repository"], assessment["change"]["number"]
        )
        projection_path = directory / "github-check.json"
        key = {
            "assessment_id": assessment["assessment_id"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "review_status": assessment["review"]["status"],
            "head_sha": assessment["change"]["head_sha"],
        }
        existing: dict[str, Any] | None = None
        try:
            candidate = load_json_bytes(projection_path.read_bytes())
            if isinstance(candidate, dict):
                existing = candidate
        except (OSError, ArtifactError):
            pass
        if existing is not None and existing.get("key") == key:
            return
        check_run_id = None
        if (
            existing is not None
            and existing.get("key", {}).get("head_sha") == key["head_sha"]
            and isinstance(existing.get("id"), int)
        ):
            check_run_id = existing["id"]
        try:
            publication = self._check_publisher.publish(
                assessment, check_run_id=check_run_id
            )
            record = {
                "status": "published",
                "id": publication.id,
                "url": publication.url,
                "key": key,
            }
        except (OSError, RuntimeError, ValueError) as exc:
            record = {
                "status": "unavailable",
                "error": str(exc),
                "key": key,
            }
        _atomic_write(projection_path, canonical_json_bytes(record))

    @staticmethod
    def _validate_snapshot(
        change: PullRequestRef, snapshot: PullRequestSnapshot
    ) -> None:
        if snapshot.repository != change.repository or snapshot.number != change.number:
            raise ChangeSafetyError("pull-request host returned the wrong change")
        if len(snapshot.base_sha) != 40 or len(snapshot.head_sha) != 40:
            raise ChangeSafetyError("pull-request host returned an invalid revision")


def _snapshot_dict(snapshot: PullRequestSnapshot) -> dict[str, Any]:
    return {
        "repository": snapshot.repository,
        "number": snapshot.number,
        "title": snapshot.title,
        "url": snapshot.url,
        "author": snapshot.author,
        "base_ref": snapshot.base_ref,
        "head_ref": snapshot.head_ref,
        "base_sha": snapshot.base_sha,
        "head_sha": snapshot.head_sha,
        "updated_at": snapshot.updated_at,
        "is_draft": snapshot.is_draft,
    }


def _assessment_result_hash(assessment: dict[str, Any]) -> str:
    content = copy.deepcopy(assessment)
    content.pop("assessment_result_sha256", None)
    content.pop("review", None)
    return sha256(content)


def _validate_policy_semantics(policy: dict[str, Any]) -> None:
    scope = policy["scope"]
    if scope["small_max_files"] >= scope["large_min_files"]:
        raise ValueError("small_max_files must be less than large_min_files")
    if scope["small_max_lines"] >= scope["large_min_lines"]:
        raise ValueError("small_max_lines must be less than large_min_lines")
    replicas = policy["release_service"]["replicas"]
    for name, profile in policy["profiles"].items():
        stages = profile["stages"]
        if stages[-1]["set_weight"] != 100 or stages[-1]["exposure_pods"] != replicas:
            raise ValueError(f"{name} must finish at 100% and all replicas")
        if any(stage["exposure_pods"] > replicas for stage in stages):
            raise ValueError(f"{name} exposure exceeds configured replicas")


def _atomic_write(path: Path, data: bytes) -> None:
    temporary = path.with_name(f".{path.name}.{os.getpid()}.tmp")
    try:
        temporary.write_bytes(data)
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
