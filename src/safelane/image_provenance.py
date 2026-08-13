from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

from .artifacts import canonical_json_bytes, sha256


CommandRunner = Callable[[tuple[str, ...], Path | None], bytes]


class ImageProvenanceError(RuntimeError):
    pass


@dataclass(frozen=True)
class VerifiedImageProvenance:
    provider: str
    source_revision: str
    verification_sha256: str


class GitHubAttestationVerifier:
    """Verify OCI provenance against GitHub's signed artifact attestations."""

    def __init__(self, *, command_runner: CommandRunner) -> None:
        self._runner = command_runner

    def verify(
        self, *, repository: str, source_revision: str, image: str
    ) -> VerifiedImageProvenance:
        arguments = (
            "attestation",
            "verify",
            f"oci://{image}",
            "--repo",
            repository,
            "--source-digest",
            source_revision,
            "--deny-self-hosted-runners",
            "--format",
            "json",
        )
        try:
            raw = self._runner(arguments, None)
            result = json.loads(raw)
        except (OSError, RuntimeError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise ImageProvenanceError(
                "GitHub artifact-attestation verification failed"
            ) from exc
        if not isinstance(result, list) or not result:
            raise ImageProvenanceError(
                "GitHub returned no verified artifact attestation"
            )
        return VerifiedImageProvenance(
            provider="github_artifact_attestation",
            source_revision=source_revision,
            verification_sha256=sha256(canonical_json_bytes(result)),
        )
