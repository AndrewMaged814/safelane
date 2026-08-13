from __future__ import annotations

import pytest

from safelane.image_provenance import (
    GitHubAttestationVerifier,
    ImageProvenanceError,
)


def test_github_attestation_verifier_binds_image_to_repository_and_source() -> None:
    calls: list[tuple[str, ...]] = []

    def runner(arguments, cwd):
        assert cwd is None
        calls.append(arguments)
        return b'[{"verificationResult":{"statement":{"subject":[{"name":"image"}]}}}]'

    result = GitHubAttestationVerifier(command_runner=runner).verify(
        repository="acme/payments",
        source_revision="b" * 40,
        image="ghcr.io/acme/payments@sha256:" + "c" * 64,
    )

    assert calls == [(
        "attestation", "verify",
        "oci://ghcr.io/acme/payments@sha256:" + "c" * 64,
        "--repo", "acme/payments",
        "--source-digest", "b" * 40,
        "--deny-self-hosted-runners",
        "--format", "json",
    )]
    assert result.provider == "github_artifact_attestation"
    assert result.source_revision == "b" * 40


def test_github_attestation_verifier_rejects_empty_verification() -> None:
    verifier = GitHubAttestationVerifier(
        command_runner=lambda arguments, cwd: b"[]"
    )

    with pytest.raises(ImageProvenanceError, match="no verified"):
        verifier.verify(
            repository="acme/payments",
            source_revision="b" * 40,
            image="ghcr.io/acme/payments@sha256:" + "c" * 64,
        )
