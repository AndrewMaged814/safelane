from __future__ import annotations

import subprocess
import sys


def test_validate_fixtures_command_checks_every_frozen_wire_contract() -> None:
    result = subprocess.run(
        [sys.executable, "-m", "safelane.cli", "validate-fixtures"],
        capture_output=True,
        text=True,
    )

    assert result.returncode == 0, result.stderr
    assert "13 schemas valid" in result.stdout
    assert "17 checked-in examples valid" in result.stdout
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
