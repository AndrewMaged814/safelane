from __future__ import annotations

import copy
import re
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Literal

from .artifacts import (
    ArtifactError,
    canonical_json_bytes,
    load_json_bytes,
    load_yaml_bytes,
    sha256,
    validate_artifact,
)
from .authorization import _process_authorization_key
from .authorization import signature_matches
from .state_io import atomic_write as _atomic_write, state_lock
from .engine import argo_rollout_for_decision, rollout_decision_for_assessment


class OutcomeError(RuntimeError):
    pass


@dataclass(frozen=True)
class StageObservation:
    set_weight: int
    outcome: Literal["succeeded", "failed", "skipped"]
    analysis_outcome: Literal["passed", "failed", "not_run"]


@dataclass(frozen=True)
class OutcomeObservation:
    repository: str
    pull_request: int
    rollout_uid: str
    result: Literal["succeeded", "failed", "aborted", "inconclusive"]
    stages: tuple[StageObservation, ...]
    incident_within_24h: bool | None


class OutcomeLedger:
    """Record post-rollout facts bound to a compiled SafeLane release."""

    def __init__(
        self,
        *,
        state_dir: Path,
        clock: Callable[[], str] | None = None,
        authorization_key: bytes | None = None,
    ) -> None:
        self._state_dir = state_dir.resolve()
        self._clock = clock or _utc_now
        self._authorization_key = authorization_key or _process_authorization_key()

    def record(self, observation: OutcomeObservation) -> dict[str, Any]:
        if not re.fullmatch(r"[A-Za-z0-9._-]{1,128}", observation.rollout_uid):
            raise OutcomeError("invalid rollout UID")
        directory = (
            self._state_dir
            / observation.repository.replace("/", "--")
            / f"pr-{observation.pull_request}"
        )
        with state_lock(directory):
            return self._record_unlocked(observation, directory)

    def _record_unlocked(
        self, observation: OutcomeObservation, directory: Path
    ) -> dict[str, Any]:
        try:
            assessment = load_json_bytes((directory / "assessment.json").read_bytes())
            decision = load_json_bytes((directory / "decision.json").read_bytes())
            manifest = load_yaml_bytes((directory / "release" / "rollout.yaml").read_bytes())
            image_catalog = load_json_bytes(
                (directory / "release" / "image-catalog.json").read_bytes()
            )
            validate_artifact("change-assessment-v1", assessment)
            validate_artifact("rollout-decision-v1", decision)
            validate_artifact("argo-rollout-v1", manifest)
            validate_artifact("repository-image-catalog-v1", image_catalog)
        except (OSError, ArtifactError) as exc:
            raise OutcomeError("compiled release evidence is missing or invalid") from exc
        if (
            assessment["change"]["repository"] != observation.repository
            or assessment["change"]["number"] != observation.pull_request
        ):
            raise OutcomeError("outcome does not match the compiled pull request")
        annotations = manifest["metadata"]["annotations"]
        unsigned_catalog = copy.deepcopy(image_catalog)
        catalog_signature = unsigned_catalog.pop("authorization_signature")
        if not signature_matches(
            unsigned_catalog, catalog_signature, self._authorization_key
        ):
            raise OutcomeError("compiled image catalog signature is invalid")
        decision_hash = sha256(decision)
        try:
            expected_decision = rollout_decision_for_assessment(
                assessment, self._authorization_key
            )
            expected_manifest = argo_rollout_for_decision(
                assessment,
                decision,
                manifest["spec"]["template"]["spec"]["containers"][0]["image"],
                self._authorization_key,
                image_catalog_sha256=sha256(image_catalog),
            )
        except (KeyError, TypeError, ValueError) as exc:
            raise OutcomeError("compiled rollout authorization is inconsistent") from exc
        if decision != expected_decision or manifest != expected_manifest:
            raise OutcomeError("compiled rollout authorization is inconsistent")
        if (
            annotations["safelane.dev/assessment-id"] != assessment["assessment_id"]
            or annotations["safelane.dev/assessment-result-sha256"]
            != assessment["assessment_result_sha256"]
            or annotations["safelane.dev/decision-sha256"] != decision_hash
            or annotations["safelane.dev/head-sha"] != assessment["change"]["head_sha"]
            or annotations["safelane.dev/policy-sha256"] != assessment["policy"]["sha256"]
            or annotations["safelane.dev/image-catalog-sha256"]
            != sha256(image_catalog)
        ):
            raise OutcomeError("compiled rollout provenance is inconsistent")
        expected_weights = [
            stage["set_weight"] for stage in decision["profile"]["stages"]
        ]
        if [stage.set_weight for stage in observation.stages] != expected_weights:
            raise OutcomeError("observed stages do not match the approved rollout profile")
        _validate_outcome_semantics(observation, decision["profile"]["stages"])

        image = manifest["spec"]["template"]["spec"]["containers"][0]["image"]
        outcomes = directory / "outcomes"
        receipt_path = outcomes / f"{observation.rollout_uid}.json"
        existing: dict[str, Any] | None = None
        if receipt_path.exists():
            try:
                candidate = load_json_bytes(receipt_path.read_bytes())
                validate_artifact("rollout-outcome-v1", candidate)
            except (OSError, ArtifactError) as exc:
                raise OutcomeError("existing rollout receipt is invalid") from exc
            if not isinstance(candidate, dict):
                raise OutcomeError("existing rollout receipt is invalid")
            existing = candidate
        receipt = {
            "schema_version": "rollout-outcome-v1",
            "recorded_at": (
                existing["recorded_at"] if existing is not None else self._clock()
            ),
            "repository": observation.repository,
            "pull_request": observation.pull_request,
            "assessment_id": assessment["assessment_id"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "decision_sha256": decision_hash,
            "rollout_manifest_sha256": sha256(manifest),
            "base_sha": assessment["change"]["base_sha"],
            "head_sha": assessment["change"]["head_sha"],
            "policy_sha256": assessment["policy"]["sha256"],
            "image_catalog_sha256": sha256(image_catalog),
            "tier": assessment["risk"]["tier"],
            "profile": decision["profile"]["name"],
            "trusted_probe_id": (
                decision["trusted_probe"]["id"]
                if decision["trusted_probe"] is not None else None
            ),
            "trusted_probe_entry_sha256": (
                decision["trusted_probe"]["catalog_entry_sha256"]
                if decision["trusted_probe"] is not None else None
            ),
            "policy_rule_ids": assessment["policy_rule_ids"],
            "finding_ids": [finding["id"] for finding in assessment["findings"]],
            "image": image,
            "rollout_uid": observation.rollout_uid,
            "result": observation.result,
            "stages": [
                {
                    "index": index,
                    "set_weight": stage.set_weight,
                    "outcome": stage.outcome,
                    "analysis_outcome": stage.analysis_outcome,
                }
                for index, stage in enumerate(observation.stages, start=1)
            ],
            "incident_within_24h": observation.incident_within_24h,
        }
        validate_artifact("rollout-outcome-v1", receipt)
        if existing is not None:
            if existing == receipt:
                return existing
            raise OutcomeError("rollout UID already has a different receipt")
        outcomes.mkdir(exist_ok=True)
        _atomic_write(
            receipt_path,
            canonical_json_bytes(receipt),
        )
        return receipt

    def summary(self, *, repository: str | None = None) -> dict[str, Any]:
        receipts: list[dict[str, Any]] = []
        repository_directory = (
            repository.replace("/", "--") if repository is not None else "*--*"
        )
        for path in self._state_dir.glob(
            f"{repository_directory}/pr-*/outcomes/*.json"
        ):
            try:
                receipt = load_json_bytes(path.read_bytes())
                validate_artifact("rollout-outcome-v1", receipt)
            except (OSError, ArtifactError) as exc:
                raise OutcomeError(f"invalid outcome receipt: {path}") from exc
            receipts.append(receipt)
        by_tier: dict[str, dict[str, int]] = {}
        for receipt in receipts:
            bucket = by_tier.setdefault(receipt["tier"], {
                "total": 0,
                "succeeded": 0,
                "failed_or_aborted": 0,
                "incidents_within_24h": 0,
            })
            bucket["total"] += 1
            if receipt["result"] == "succeeded":
                bucket["succeeded"] += 1
            if receipt["result"] in {"failed", "aborted"}:
                bucket["failed_or_aborted"] += 1
            if receipt["incident_within_24h"] is True:
                bucket["incidents_within_24h"] += 1
        by_rule = _calibration_buckets(receipts, "policy_rule_ids")
        by_finding = _calibration_buckets(receipts, "finding_ids")
        return {
            "total": len(receipts),
            "by_tier": by_tier,
            "by_rule": by_rule,
            "by_finding": by_finding,
        }


def _calibration_buckets(
    receipts: list[dict[str, Any]], field: str
) -> dict[str, dict[str, int]]:
    buckets: dict[str, dict[str, int]] = {}
    for receipt in receipts:
        for identifier in receipt[field]:
            bucket = buckets.setdefault(identifier, {
                "total": 0,
                "failed_or_aborted": 0,
                "incidents_within_24h": 0,
            })
            bucket["total"] += 1
            if receipt["result"] in {"failed", "aborted"}:
                bucket["failed_or_aborted"] += 1
            if receipt["incident_within_24h"] is True:
                bucket["incidents_within_24h"] += 1
    return buckets


def _validate_outcome_semantics(
    observation: OutcomeObservation,
    profile_stages: list[dict[str, Any]],
) -> None:
    terminal_interruption = False
    for observed, configured in zip(observation.stages, profile_stages):
        if terminal_interruption and observed.outcome != "skipped":
            raise OutcomeError("stages after failure or skip must remain skipped")
        if configured["analysis"]:
            if observed.analysis_outcome == "not_run" and observed.outcome != "skipped":
                raise OutcomeError("required analysis was not observed")
        elif observed.analysis_outcome != "not_run":
            raise OutcomeError("analysis was reported for a stage without a checkpoint")
        if observed.analysis_outcome == "failed" and observed.outcome != "failed":
            raise OutcomeError("failed analysis must fail its rollout stage")
        if observed.outcome in {"failed", "skipped"}:
            terminal_interruption = True
    if observation.result == "succeeded":
        if any(stage.outcome != "succeeded" for stage in observation.stages):
            raise OutcomeError("successful rollout requires every stage to succeed")
        if any(stage.analysis_outcome == "failed" for stage in observation.stages):
            raise OutcomeError("successful rollout cannot contain failed analysis")
    elif observation.result == "failed":
        if not any(
            stage.outcome == "failed" or stage.analysis_outcome == "failed"
            for stage in observation.stages
        ):
            raise OutcomeError("failed rollout requires failure evidence")
    elif observation.result == "aborted":
        if not any(
            stage.outcome in {"failed", "skipped"}
            or stage.analysis_outcome == "failed"
            for stage in observation.stages
        ):
            raise OutcomeError("aborted rollout requires interrupted-stage evidence")
    elif not any(
        stage.outcome != "succeeded" or stage.analysis_outcome == "failed"
        for stage in observation.stages
    ):
        raise OutcomeError("inconclusive rollout requires incomplete or failed evidence")


def _utc_now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")
