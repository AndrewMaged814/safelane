from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from safelane.cli import _repository_state_root


def test_validate_fixtures_command_checks_every_frozen_wire_contract() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "validate-fixtures"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "17 schemas valid" in result.stdout
    assert "18 checked-in examples valid" in result.stdout
    assert "3 evaluation manifests and hashes valid" in result.stdout
    assert "demo revisions reproduce exactly" in result.stdout


def test_evaluate_ollama_command_documents_output_and_base_url_flags() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "evaluate-ollama", "--help"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "--output" in result.stdout
    assert "--base-url" in result.stdout


def test_studio_command_accepts_only_a_pull_request_repository_source() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "studio", "--help"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "--repository" in result.stdout
    assert "--state-dir" in result.stdout
    assert "--workspace" not in result.stdout
    assert "--port" in result.stdout
    assert "--host" not in result.stdout


def test_assess_pr_command_exposes_only_pull_request_identity_and_runtime_options() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "assess-pr", "--help"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "--repository" in result.stdout
    assert "--number" in result.stdout
    assert "--state-dir" in result.stdout
    assert "--base-url" in result.stdout
    assert "--diff" not in result.stdout
    assert "--policy" not in result.stdout


def test_cli_commands_share_the_same_repository_state_root(tmp_path: Path) -> None:
    class Provider:
        local_path = tmp_path

    assert _repository_state_root(Provider(), None) == (
        tmp_path / ".safelane" / "studio"
    )


def test_register_image_command_requires_revision_and_immutable_image() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "register-image", "--help"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0
    assert "--repository" in result.stdout
    assert "--service" in result.stdout
    assert "--number" in result.stdout
    assert "--image" in result.stdout
    assert "--source-revision" not in result.stdout
