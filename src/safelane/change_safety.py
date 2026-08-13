"""Implementation of the canonical SafeLaneEngine. Import from safelane.engine."""

from __future__ import annotations

import copy
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Literal, Protocol

import yaml

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    change_assessment_result_sha256,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from .authorization import (
    _process_authorization_key,
    authorization_signature,
    signature_matches,
)
from .image_provenance import ImageProvenanceError, VerifiedImageProvenance
from .pr_studio import PullRequestAnalyzer, PullRequestAssessmentEngine
from .state_io import atomic_write as _atomic_write, state_lock


class SafeLaneEngineError(RuntimeError):
    """A pull request could not be safely assessed or resolved."""


class RepositoryNotConfigured(SafeLaneEngineError):
    pass


class PolicyInvalid(SafeLaneEngineError):
    pass


class ChangeMoved(SafeLaneEngineError):
    pass


class AssessmentNotFound(SafeLaneEngineError):
    pass


class AssessmentStale(SafeLaneEngineError):
    pass


class AlreadyResolved(SafeLaneEngineError):
    pass


class ProfileNotAllowed(SafeLaneEngineError):
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
class ImageRegistration:
    repository: str
    pull_request: int
    service: str
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

    def invalidate(
        self, repository: str, check_run_id: int, *, superseded_by_head: str
    ) -> None: ...


class ImageProvenanceVerifier(Protocol):
    def verify(
        self,
        *,
        repository: str,
        source_revision: str,
        image: str,
        signer_workflow: str,
    ) -> VerifiedImageProvenance: ...


AnalyzerFactory = Callable[[dict[str, Any]], PullRequestAnalyzer]
Clock = Callable[[], str]


class SafeLaneEngine:
    """Canonical SafeLane engine: assess, resolve, and compile one GitHub pull request."""

    POLICY_PATH = ".safelane/policy.yaml"

    def __init__(
        self,
        *,
        host: PullRequestHost,
        state_dir: Path,
        analyzer_factory: AnalyzerFactory,
        check_publisher: CheckPublisher | None = None,
        clock: Clock | None = None,
        authorization_key: bytes | None = None,
        image_provenance_verifier: ImageProvenanceVerifier | None = None,
    ) -> None:
        self._host = host
        self._state_dir = state_dir.resolve()
        self._analyzer_factory = analyzer_factory
        self._check_publisher = check_publisher
        self._clock = clock or _utc_now
        self._authorization_key = authorization_key or _process_authorization_key()
        self._image_provenance_verifier = image_provenance_verifier

    def outcome_ledger(self):
        from .outcomes import OutcomeLedger

        return OutcomeLedger(
            state_dir=self._state_dir,
            authorization_key=self._authorization_key,
        )

    def check_projection(self, change: PullRequestRef) -> dict[str, Any]:
        if self._check_publisher is None:
            return {"status": "not_configured"}
        path = self._directory(change.repository, change.number) / "github-check.json"
        try:
            value = load_json_bytes(path.read_bytes())
        except (OSError, ArtifactError):
            return {"status": "pending"}
        if not isinstance(value, dict) or value.get("status") not in {
            "published", "unavailable"
        }:
            return {"status": "unavailable", "error": "invalid projection record"}
        return copy.deepcopy(value)

    def assess(self, change: PullRequestRef) -> AssessmentOutcome:
        with state_lock(self._directory(change.repository, change.number)):
            return self._assess_unlocked(change)

    def _assess_unlocked(self, change: PullRequestRef) -> AssessmentOutcome:
        snapshot = self._host.get_pull_request(change)
        self._validate_snapshot(change, snapshot)
        policy_bytes = self._read_base_policy(snapshot)
        policy = self._load_policy(policy_bytes, snapshot.base_sha)
        catalog_bytes = self._read_base_catalog(snapshot, policy)
        catalog = self._load_catalog(catalog_bytes, snapshot.base_sha)
        _validate_catalog_bindings(policy, catalog)
        diff = self._host.diff(snapshot)
        cached = self._cached(snapshot, policy_bytes, catalog_bytes, diff)
        if cached is not None:
            current = self._host.get_pull_request(change)
            if (
                current.head_sha != snapshot.head_sha
                or current.base_sha != snapshot.base_sha
            ):
                raise ChangeMoved(
                    "pull request base or head moved while assessment was running"
                )
            self._publish_check(cached.assessment)
            return cached
        analyzer = self._analyzer_factory(copy.deepcopy(policy))
        evaluator = PullRequestAssessmentEngine(
            policy=policy,
            analyzer=analyzer,
        )
        result = evaluator.assess(snapshot.repository, _snapshot_dict(snapshot), diff)
        selected_probe = _select_trusted_probe(policy, catalog, result)
        rollout_catalog = evaluator.profile_catalog()

        current = self._host.get_pull_request(change)
        if (
            current.head_sha != snapshot.head_sha
            or current.base_sha != snapshot.base_sha
        ):
            raise ChangeMoved(
                "pull request base or head moved while assessment was running"
            )

        assessment = self._assessment(
            snapshot, policy, policy_bytes, catalog, catalog_bytes, diff, result,
            rollout_catalog, selected_probe,
        )
        decision = None
        if result["tier"] == "safe":
            resolution = {
                "type": "automatic",
                "selected_profile": "Fast",
                "resolved_at": assessment["assessed_at"],
            }
            assessment["review"] = {"status": "approved", "resolution": resolution}
            decision = rollout_decision_for_assessment(
                assessment, self._authorization_key
            )
        validate_artifact("change-assessment-v1", assessment)
        if decision is not None:
            validate_artifact("rollout-decision-v1", decision)
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
        catalog_bytes: bytes,
        diff: bytes,
    ) -> AssessmentOutcome | None:
        directory = self._directory(snapshot.repository, snapshot.number)
        try:
            assessment = load_json_bytes((directory / "assessment.json").read_bytes())
            validate_artifact("change-assessment-v1", assessment)
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
        if (
            assessment.get("trusted_probe_catalog", {}).get("sha256")
            != sha256(catalog_bytes)
        ):
            return None
        if assessment.get("evidence", {}).get("git_diff_sha256") != sha256(diff):
            return None
        result_hash = assessment.get("assessment_result_sha256")
        if result_hash != change_assessment_result_sha256(assessment):
            return None
        decision = None
        resolution = assessment.get("review", {}).get("resolution")
        if isinstance(resolution, dict) and resolution.get("type") == "automatic":
            try:
                candidate = load_json_bytes((directory / "decision.json").read_bytes())
                validate_artifact("rollout-decision-v1", candidate)
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
        with state_lock(assessment_path.parent):
            return self._resolve_unlocked(command, assessment_path)

    def _resolve_unlocked(
        self, command: ResolutionCommand, assessment_path: Path
    ) -> ResolutionOutcome:
        try:
            assessment = load_json_bytes(assessment_path.read_bytes())
            validate_artifact("change-assessment-v1", assessment)
        except (OSError, ArtifactError) as exc:
            raise AssessmentNotFound(command.handle.assessment_id) from exc
        if assessment.get("assessment_id") != command.handle.assessment_id:
            raise AssessmentStale("assessment identity does not match the handle")
        if (
            assessment.get("assessment_result_sha256")
            != command.handle.assessment_result_sha256
            or change_assessment_result_sha256(assessment)
            != command.handle.assessment_result_sha256
        ):
            raise AssessmentStale("assessment content does not match the reviewed handle")
        if assessment["review"]["status"] != "unresolved":
            raise AlreadyResolved(command.handle.assessment_id)
        if not command.actor.strip():
            raise SafeLaneEngineError("resolution actor is required")
        if command.action == "decide_later":
            return ResolutionOutcome(assessment=assessment, decision=None)

        change = PullRequestRef(
            assessment["change"]["repository"], assessment["change"]["number"]
        )
        current = self._host.get_pull_request(change)
        if current.head_sha != assessment["change"]["head_sha"]:
            raise AssessmentStale("pull request has a newer head revision")
        if current.base_sha != assessment["change"]["base_sha"]:
            raise AssessmentStale("pull request has a newer base revision")
        policy_bytes = self._read_base_policy(current)
        if sha256(policy_bytes) != assessment["policy"]["sha256"]:
            raise AssessmentStale("base policy no longer matches the assessment")
        policy = self._load_policy(policy_bytes, current.base_sha)
        catalog_bytes = self._read_base_catalog(current, policy)
        if sha256(catalog_bytes) != assessment["trusted_probe_catalog"]["sha256"]:
            raise AssessmentStale("trusted probe catalog no longer matches the assessment")
        catalog = self._load_catalog(catalog_bytes, current.base_sha)
        _validate_catalog_bindings(policy, catalog)
        expected_probe = _select_trusted_probe(
            policy,
            catalog,
            {"tier": assessment["risk"]["tier"], "findings": assessment["findings"]},
        )
        if assessment["selected_trusted_probe"] != expected_probe:
            raise AssessmentStale("trusted probe selection does not match base policy")

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
            rollout_decision_for_assessment(resolved, self._authorization_key)
            if selected_profile is not None
            else None
        )
        validate_artifact("change-assessment-v1", resolved)
        if decision is not None:
            validate_artifact("rollout-decision-v1", decision)
        decision_path = assessment_path.with_name("decision.json")
        if decision is None:
            _remove_if_exists(decision_path)
        if decision is not None:
            _atomic_write(
                decision_path,
                canonical_json_bytes(decision),
            )
        _atomic_write(assessment_path, canonical_json_bytes(resolved))
        self._publish_check(resolved)
        return ResolutionOutcome(assessment=resolved, decision=decision)

    def compile(self, binding: ReleaseBinding) -> RolloutBundle:
        assessment_path = self._assessment_path(binding.handle)
        with state_lock(assessment_path.parent):
            return self._compile_unlocked(binding, assessment_path)

    def _compile_unlocked(
        self, binding: ReleaseBinding, assessment_path: Path
    ) -> RolloutBundle:
        try:
            assessment = load_json_bytes(assessment_path.read_bytes())
            validate_artifact("change-assessment-v1", assessment)
            decision = load_json_bytes(
                assessment_path.with_name("decision.json").read_bytes()
            )
            validate_artifact("rollout-decision-v1", decision)
        except (OSError, ArtifactError) as exc:
            raise AssessmentNotFound(binding.handle.assessment_id) from exc
        if (
            assessment.get("assessment_id") != binding.handle.assessment_id
            or assessment.get("assessment_result_sha256")
            != binding.handle.assessment_result_sha256
            or change_assessment_result_sha256(assessment)
            != binding.handle.assessment_result_sha256
        ):
            raise AssessmentStale("release binding does not match the assessment")
        review = assessment.get("review", {})
        resolution = review.get("resolution")
        if review.get("status") != "approved" or not isinstance(resolution, dict):
            raise AssessmentStale("release requires an approved assessment")
        expected_decision = rollout_decision_for_assessment(
            assessment, self._authorization_key
        )
        if decision != expected_decision:
            raise AssessmentStale("rollout decision does not match the approved assessment")
        if not re.fullmatch(r".+@sha256:[0-9a-f]{64}", binding.image):
            raise SafeLaneEngineError("release image must use an immutable sha256 digest")

        change = PullRequestRef(
            assessment["change"]["repository"], assessment["change"]["number"]
        )
        current = self._host.get_pull_request(change)
        if current.head_sha != assessment["change"]["head_sha"]:
            raise AssessmentStale("pull request has a newer head revision")
        if current.base_sha != assessment["change"]["base_sha"]:
            raise AssessmentStale("pull request has a newer base revision")
        policy_bytes = self._read_base_policy(current)
        if sha256(policy_bytes) != assessment["policy"]["sha256"]:
            raise AssessmentStale("base policy no longer matches the assessment")
        policy = self._load_policy(policy_bytes, current.base_sha)
        catalog_bytes = self._read_base_catalog(current, policy)
        if sha256(catalog_bytes) != assessment["trusted_probe_catalog"]["sha256"]:
            raise AssessmentStale("trusted probe catalog no longer matches the assessment")
        catalog = self._load_catalog(catalog_bytes, current.base_sha)
        _validate_catalog_bindings(policy, catalog)
        selected_probe = assessment["selected_trusted_probe"]
        expected_probe = _select_trusted_probe(
            policy,
            catalog,
            {"tier": assessment["risk"]["tier"], "findings": assessment["findings"]},
        )
        if selected_probe != expected_probe:
            raise AssessmentStale("trusted probe selection does not match base policy")
        image_catalog = self._validate_release_image(assessment, binding.image)
        image_catalog_hash = sha256(image_catalog)

        decision_hash = sha256(decision)
        try:
            manifest = argo_rollout_for_decision(
                assessment,
                decision,
                binding.image,
                self._authorization_key,
                image_catalog_sha256=image_catalog_hash,
            )
        except ValueError as exc:
            raise AssessmentStale(str(exc)) from exc
        validate_artifact("argo-rollout-v1", manifest)
        release_directory = assessment_path.parent / "release"
        release_directory.mkdir(exist_ok=True)
        path = release_directory / "rollout.yaml"
        _remove_if_exists(path)
        _atomic_write(
            release_directory / "image-catalog.json",
            canonical_json_bytes(image_catalog),
        )
        raw = yaml.safe_dump(
            manifest, sort_keys=False, allow_unicode=True
        ).encode("utf-8")
        _atomic_write(path, raw)
        return RolloutBundle(manifest=manifest, decision_sha256=decision_hash, path=path)

    def register_image(self, registration: ImageRegistration) -> Path:
        if not re.fullmatch(r"[^/]+/[^/]+", registration.repository):
            raise SafeLaneEngineError("registered image repository is invalid")
        if registration.pull_request < 1:
            raise SafeLaneEngineError("registered image pull request is invalid")
        if not re.fullmatch(r".+@sha256:[0-9a-f]{64}", registration.image):
            raise SafeLaneEngineError("registered image must use an immutable sha256 digest")
        if self._image_provenance_verifier is None:
            raise SafeLaneEngineError(
                "image registration requires a configured provenance verifier"
            )
        change = PullRequestRef(registration.repository, registration.pull_request)
        snapshot = self._host.get_pull_request(change)
        self._validate_snapshot(change, snapshot)
        policy_bytes = self._read_base_policy(snapshot)
        policy = self._load_policy(policy_bytes, snapshot.base_sha)
        if registration.service != policy["release_service"]["name"]:
            raise SafeLaneEngineError("image service does not match base-owned policy")
        signer_workflow = policy["image_provenance"]["signer_workflow"]
        try:
            provenance = self._image_provenance_verifier.verify(
                repository=registration.repository,
                source_revision=snapshot.head_sha,
                image=registration.image,
                signer_workflow=signer_workflow,
            )
        except ImageProvenanceError as exc:
            raise SafeLaneEngineError(str(exc)) from exc
        if provenance.source_revision != snapshot.head_sha:
            raise SafeLaneEngineError("verified provenance revision does not match")
        current = self._host.get_pull_request(change)
        if current.base_sha != snapshot.base_sha or current.head_sha != snapshot.head_sha:
            raise ChangeMoved("pull request base or head moved during image verification")
        path = self._image_catalog_path(registration.repository)
        with state_lock(path.parent):
            catalog = {
                "schema_version": "repository-image-catalog-v1",
                "catalog_version": 1,
                "application_images": [],
            }
            if path.exists():
                existing = load_json_bytes(path.read_bytes())
                self._validate_image_catalog(existing)
                catalog = {
                    key: copy.deepcopy(value)
                    for key, value in existing.items()
                    if key != "authorization_signature"
                }
                catalog["catalog_version"] += 1
            identity = (
                registration.repository,
                registration.service,
                snapshot.head_sha,
            )
            catalog["application_images"] = [
                item for item in catalog["application_images"]
                if (item["repository"], item["service"], item["source_revision"])
                != identity
            ]
            catalog["application_images"].append({
                "repository": registration.repository,
                "service": registration.service,
                "source_revision": snapshot.head_sha,
                "image": registration.image,
                "oci_revision": snapshot.head_sha,
                "provenance": {
                    "provider": provenance.provider,
                    "source_revision": provenance.source_revision,
                    "verification_sha256": provenance.verification_sha256,
                    "signer_workflow": signer_workflow,
                },
            })
            catalog["application_images"].sort(key=lambda item: (
                item["repository"], item["service"], item["source_revision"]
            ))
            catalog["authorization_signature"] = authorization_signature(
                catalog, self._authorization_key
            )
            validate_artifact("repository-image-catalog-v1", catalog)
            path.parent.mkdir(parents=True, exist_ok=True)
            _atomic_write(path, canonical_json_bytes(catalog))
            return path

    def _image_catalog_path(self, repository: str) -> Path:
        return self._state_dir / repository.replace("/", "--") / "image-catalog.json"

    def _validate_image_catalog(self, catalog: Any) -> None:
        validate_artifact("repository-image-catalog-v1", catalog)
        unsigned = copy.deepcopy(catalog)
        signature = unsigned.pop("authorization_signature")
        if not signature_matches(unsigned, signature, self._authorization_key):
            raise SafeLaneEngineError("image catalog authorization signature is invalid")
        identities: set[tuple[str, str, str]] = set()
        for item in catalog["application_images"]:
            identity = (item["repository"], item["service"], item["source_revision"])
            if (
                identity in identities
                or item["source_revision"] != item["oci_revision"]
                or item["source_revision"] != item["provenance"]["source_revision"]
            ):
                raise SafeLaneEngineError("image catalog provenance is invalid")
            identities.add(identity)

    def _validate_release_image(
        self, assessment: dict[str, Any], image: str
    ) -> dict[str, Any]:
        path = self._image_catalog_path(assessment["change"]["repository"])
        try:
            catalog = load_json_bytes(path.read_bytes())
            self._validate_image_catalog(catalog)
        except (OSError, ArtifactError, SafeLaneEngineError) as exc:
            raise SafeLaneEngineError(
                "a signed repository image catalog is required before compilation"
            ) from exc
        expected = {
            "repository": assessment["change"]["repository"],
            "service": assessment["service"]["name"],
            "source_revision": assessment["change"]["head_sha"],
            "image": image,
            "oci_revision": assessment["change"]["head_sha"],
        }
        matching = [
            item
            for item in catalog["application_images"]
            if all(item.get(key) == value for key, value in expected.items())
            and item["provenance"]["provider"]
            == assessment["image_provenance"]["provider"]
            and item["provenance"]["signer_workflow"]
            == assessment["image_provenance"]["signer_workflow"]
        ]
        if len(matching) != 1:
            raise SafeLaneEngineError(
                "release image is not catalog-bound to this service and PR head"
            )
        return catalog

    def _read_base_policy(self, snapshot: PullRequestSnapshot) -> bytes:
        try:
            return self._host.read_file(
                snapshot.repository, snapshot.base_sha, self.POLICY_PATH
            )
        except (KeyError, OSError, RuntimeError) as exc:
            raise RepositoryNotConfigured(
                f"{snapshot.repository}@{snapshot.base_sha} has no {self.POLICY_PATH}"
            ) from exc

    def _read_base_catalog(
        self, snapshot: PullRequestSnapshot, policy: dict[str, Any]
    ) -> bytes:
        path = policy["trusted_probe_catalog"]["path"]
        try:
            return self._host.read_file(snapshot.repository, snapshot.base_sha, path)
        except (KeyError, OSError, RuntimeError) as exc:
            raise RepositoryNotConfigured(
                f"{snapshot.repository}@{snapshot.base_sha} has no {path}"
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

    @staticmethod
    def _load_catalog(raw: bytes, base_sha: str) -> dict[str, Any]:
        try:
            catalog = load_yaml_bytes(raw)
            validate_artifact("repository-trusted-probes-v1", catalog)
        except (ArtifactError, TypeError, ValueError) as exc:
            raise PolicyInvalid(
                f"trusted probe catalog at {base_sha} is invalid: {exc}"
            ) from exc
        return catalog

    def _assessment(
        self,
        snapshot: PullRequestSnapshot,
        policy: dict[str, Any],
        policy_bytes: bytes,
        catalog: dict[str, Any],
        catalog_bytes: bytes,
        diff: bytes,
        result: dict[str, Any],
        rollout_catalog: list[dict[str, Any]],
        selected_probe: dict[str, Any] | None,
    ) -> dict[str, Any]:
        assessed_at = self._clock()
        policy_hash = sha256(policy_bytes)
        input_hash = sha256({
            "repository": snapshot.repository,
            "pull_request": snapshot.number,
            "base_sha": snapshot.base_sha,
            "head_sha": snapshot.head_sha,
            "policy_sha256": policy_hash,
            "trusted_probe_catalog_sha256": sha256(catalog_bytes),
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
            "trusted_probe_catalog": {
                "version": catalog["catalog_version"],
                "source_revision": snapshot.base_sha,
                "sha256": sha256(catalog_bytes),
            },
            "change": _snapshot_dict(snapshot),
            "service": copy.deepcopy(policy["release_service"]),
            "image_provenance": copy.deepcopy(policy["image_provenance"]),
            "risk": {
                "tier": result["tier"],
                "minimum_profile": result["profile"],
                "reason": result["reason"],
                "evidence_confidence": result["confidence"],
            },
            "evidence": {
                "git_diff_sha256": sha256(diff),
                **copy.deepcopy(result["evidence"]),
            },
            "findings": copy.deepcopy(result["findings"]),
            "policy_rule_ids": copy.deepcopy(result["policy_rules"]),
            "selected_trusted_probe": copy.deepcopy(selected_probe),
            "rollout_options": copy.deepcopy(result["rollout_options"]),
            "rollout_catalog": copy.deepcopy(rollout_catalog),
            "review": {"status": "unresolved", "resolution": None},
        }
        assessment["assessment_result_sha256"] = change_assessment_result_sha256(
            assessment
        )
        return assessment

    def _write(
        self,
        snapshot: PullRequestSnapshot,
        assessment: dict[str, Any],
        decision: dict[str, Any] | None,
    ) -> None:
        directory = self._directory(snapshot.repository, snapshot.number)
        directory.mkdir(parents=True, exist_ok=True)
        _remove_if_exists(directory / "decision.json")
        _remove_if_exists(directory / "release" / "rollout.yaml")
        _remove_if_exists(directory / "release" / "image-catalog.json")
        if decision is not None:
            _atomic_write(directory / "decision.json", canonical_json_bytes(decision))
        _atomic_write(directory / "assessment.json", canonical_json_bytes(assessment))

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
        pending_invalidations = _pending_check_invalidations(existing)
        if (
            existing is not None
            and existing.get("status") == "published"
            and existing.get("key", {}).get("head_sha") != key["head_sha"]
            and isinstance(existing.get("id"), int)
        ):
            invalidation = {
                "repository": assessment["change"]["repository"],
                "id": existing["id"],
                "superseded_by_head": key["head_sha"],
            }
            if invalidation not in pending_invalidations:
                pending_invalidations.append(invalidation)
        remaining_invalidations: list[dict[str, Any]] = []
        for invalidation in pending_invalidations:
            try:
                self._check_publisher.invalidate(
                    invalidation["repository"],
                    invalidation["id"],
                    superseded_by_head=invalidation["superseded_by_head"],
                )
            except (OSError, RuntimeError, ValueError):
                remaining_invalidations.append(invalidation)
        if (
            existing is not None
            and existing.get("key") == key
            and existing.get("status") == "published"
        ):
            record = copy.deepcopy(existing)
            if remaining_invalidations:
                record["pending_invalidations"] = remaining_invalidations
            else:
                record.pop("pending_invalidations", None)
            if record != existing:
                _atomic_write(projection_path, canonical_json_bytes(record))
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
            if check_run_id is not None:
                record["id"] = check_run_id
        if remaining_invalidations:
            record["pending_invalidations"] = remaining_invalidations
        _atomic_write(projection_path, canonical_json_bytes(record))

    @staticmethod
    def _validate_snapshot(
        change: PullRequestRef, snapshot: PullRequestSnapshot
    ) -> None:
        if snapshot.repository != change.repository or snapshot.number != change.number:
            raise SafeLaneEngineError("pull-request host returned the wrong change")
        if len(snapshot.base_sha) != 40 or len(snapshot.head_sha) != 40:
            raise SafeLaneEngineError("pull-request host returned an invalid revision")


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


def rollout_decision_for_assessment(
    assessment: dict[str, Any], authorization_key: bytes,
) -> dict[str, Any]:
    """Derive the only rollout decision authorized by a resolved assessment."""
    resolution = assessment["review"]["resolution"]
    if assessment["review"]["status"] != "approved" or not isinstance(
        resolution, dict
    ):
        raise ValueError("assessment is not approved")
    selected_profile = resolution["selected_profile"]
    try:
        profile = next(
            item for item in assessment["rollout_options"]
            if item["name"] == selected_profile
        )
    except StopIteration as exc:
        raise ValueError("approved profile is not an assessment option") from exc
    decision = {
        "schema_version": "rollout-decision-v1",
        "assessment_id": assessment["assessment_id"],
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": assessment["assessment_result_sha256"],
        "repository": assessment["change"]["repository"],
        "pull_request": assessment["change"]["number"],
        "base_sha": assessment["change"]["base_sha"],
        "head_sha": assessment["change"]["head_sha"],
        "policy": copy.deepcopy(assessment["policy"]),
        "trusted_probe_catalog": copy.deepcopy(
            assessment["trusted_probe_catalog"]
        ),
        "service": assessment["service"]["name"],
        "tier": assessment["risk"]["tier"],
        "profile": copy.deepcopy(profile),
        "trusted_probe": copy.deepcopy(assessment["selected_trusted_probe"]),
        "resolution": copy.deepcopy(resolution),
    }
    decision["authorization_signature"] = authorization_signature(
        decision, authorization_key
    )
    return decision


def argo_rollout_for_decision(
    assessment: dict[str, Any],
    decision: dict[str, Any],
    image: str,
    authorization_key: bytes,
    *,
    image_catalog_sha256: str,
) -> dict[str, Any]:
    """Materialize the deterministic Argo Rollout bound by an exact decision."""
    if decision != rollout_decision_for_assessment(assessment, authorization_key):
        raise ValueError("rollout decision does not match the approved assessment")
    deployment = assessment["service"]["deployment"]
    labels = {"app.kubernetes.io/name": deployment["workload_label"]}
    selected_probe = decision["trusted_probe"]
    steps: list[dict[str, Any]] = []
    for stage in decision["profile"]["stages"]:
        steps.append({"setWeight": stage["set_weight"]})
        if stage["analysis"]:
            if selected_probe is None:
                raise ValueError("analysis stage has no assessment-bound trusted probe")
            steps.append({
                "analysis": {
                    "templates": [{
                        "templateName": selected_probe["analysis_template"]
                    }]
                }
            })
    return {
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
                "safelane.dev/decision-sha256": sha256(decision),
                "safelane.dev/head-sha": assessment["change"]["head_sha"],
                "safelane.dev/policy-sha256": assessment["policy"]["sha256"],
                "safelane.dev/image-catalog-sha256": image_catalog_sha256,
            },
        },
        "spec": {
            "replicas": decision["profile"]["replicas"],
            "selector": {"matchLabels": dict(labels)},
            "template": {
                "metadata": {"labels": dict(labels)},
                "spec": {
                    "containers": [{
                        "name": deployment["container_name"],
                        "image": image,
                    }]
                },
            },
            "strategy": {
                "canary": {
                    "maxSurge": decision["profile"]["max_surge"],
                    "maxUnavailable": decision["profile"]["max_unavailable"],
                    "steps": steps,
                }
            },
        },
    }


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
        if any(
            right["set_weight"] <= left["set_weight"]
            or right["exposure_pods"] < left["exposure_pods"]
            for left, right in zip(stages, stages[1:])
        ):
            raise ValueError(f"{name} stages must increase exposure")
        if name == "Fast" and any(stage["analysis"] for stage in stages):
            raise ValueError("Fast cannot add a trusted analysis checkpoint")
        if name in {"Guarded", "Strict"} and not any(
            stage["analysis"] for stage in stages[:-1]
        ):
            raise ValueError(f"{name} requires a pre-terminal analysis checkpoint")


def _validate_catalog_bindings(
    policy: dict[str, Any], catalog: dict[str, Any]
) -> None:
    probes = catalog["probes"]
    identifiers = [probe["id"] for probe in probes]
    if len(set(identifiers)) != len(identifiers):
        raise PolicyInvalid("trusted probe IDs must be unique")
    configured = policy["trusted_probe_catalog"]
    referenced = {
        configured["non_fast_fallback_probe_id"],
        *configured["category_bindings"].values(),
    }
    missing = referenced - set(identifiers)
    if missing:
        raise PolicyInvalid(
            f"trusted probe bindings reference unknown IDs: {sorted(missing)}"
        )


def _select_trusted_probe(
    policy: dict[str, Any],
    catalog: dict[str, Any],
    result: dict[str, Any],
) -> dict[str, Any] | None:
    if result["tier"] == "safe":
        return None
    configured = policy["trusted_probe_catalog"]
    findings = result["findings"]
    if findings:
        probe_id = configured["category_bindings"][findings[0]["category"]]
        source = "ai_safety_case"
    else:
        probe_id = configured["non_fast_fallback_probe_id"]
        source = "policy_fallback"
    probe = next(item for item in catalog["probes"] if item["id"] == probe_id)
    return {
        "id": probe_id,
        "catalog_entry_sha256": sha256(probe),
        "analysis_template": probe["analysis_template"],
        "selection_source": source,
    }


def _remove_if_exists(path: Path) -> None:
    try:
        path.unlink()
    except FileNotFoundError:
        pass


def _pending_check_invalidations(
    record: dict[str, Any] | None,
) -> list[dict[str, Any]]:
    if record is None or not isinstance(record.get("pending_invalidations"), list):
        return []
    valid: list[dict[str, Any]] = []
    for item in record["pending_invalidations"]:
        if (
            isinstance(item, dict)
            and set(item) == {"repository", "id", "superseded_by_head"}
            and isinstance(item["repository"], str)
            and isinstance(item["id"], int)
            and isinstance(item["superseded_by_head"], str)
        ):
            valid.append(copy.deepcopy(item))
    return valid


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
