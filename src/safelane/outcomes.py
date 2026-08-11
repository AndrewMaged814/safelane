from __future__ import annotations

import os
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
        self, *, state_dir: Path, clock: Callable[[], str] | None = None
    ) -> None:
        self._state_dir = state_dir.resolve()
        self._clock = clock or _utc_now

    def record(self, observation: OutcomeObservation) -> dict[str, Any]:
        if not re.fullmatch(r"[A-Za-z0-9._-]{1,128}", observation.rollout_uid):
            raise OutcomeError("invalid rollout UID")
        directory = (
            self._state_dir
            / observation.repository.replace("/", "--")
            / f"pr-{observation.pull_request}"
        )
        try:
            assessment = load_json_bytes((directory / "assessment.json").read_bytes())
            decision = load_json_bytes((directory / "decision.json").read_bytes())
            manifest = load_yaml_bytes((directory / "release" / "rollout.yaml").read_bytes())
            validate_artifact("argo-rollout-v1", manifest)
        except (OSError, ArtifactError) as exc:
            raise OutcomeError("compiled release evidence is missing or invalid") from exc
        if (
            assessment["change"]["repository"] != observation.repository
            or assessment["change"]["number"] != observation.pull_request
        ):
            raise OutcomeError("outcome does not match the compiled pull request")
        annotations = manifest["metadata"]["annotations"]
        decision_hash = sha256(decision)
        if (
            annotations["safelane.dev/assessment-id"] != assessment["assessment_id"]
            or annotations["safelane.dev/assessment-result-sha256"]
            != assessment["assessment_result_sha256"]
            or annotations["safelane.dev/decision-sha256"] != decision_hash
            or annotations["safelane.dev/head-sha"] != assessment["change"]["head_sha"]
            or annotations["safelane.dev/policy-sha256"] != assessment["policy"]["sha256"]
        ):
            raise OutcomeError("compiled rollout provenance is inconsistent")
        expected_weights = [
            stage["set_weight"] for stage in decision["profile"]["stages"]
        ]
        if [stage.set_weight for stage in observation.stages] != expected_weights:
            raise OutcomeError("observed stages do not match the approved rollout profile")

        image = manifest["spec"]["template"]["spec"]["containers"][0]["image"]
        receipt = {
            "schema_version": "rollout-outcome-v1",
            "recorded_at": self._clock(),
            "repository": observation.repository,
            "pull_request": observation.pull_request,
            "assessment_id": assessment["assessment_id"],
            "assessment_result_sha256": assessment["assessment_result_sha256"],
            "decision_sha256": decision_hash,
            "rollout_manifest_sha256": sha256(manifest),
            "base_sha": assessment["change"]["base_sha"],
            "head_sha": assessment["change"]["head_sha"],
            "policy_sha256": assessment["policy"]["sha256"],
            "tier": assessment["risk"]["tier"],
            "profile": decision["profile"]["name"],
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
        outcomes = directory / "outcomes"
        outcomes.mkdir(exist_ok=True)
        _atomic_write(
            outcomes / f"{observation.rollout_uid}.json",
            canonical_json_bytes(receipt),
        )
        return receipt

    def summary(self) -> dict[str, Any]:
        receipts: list[dict[str, Any]] = []
        for path in self._state_dir.glob("*--*/pr-*/outcomes/*.json"):
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
        return {"total": len(receipts), "by_tier": by_tier}


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
