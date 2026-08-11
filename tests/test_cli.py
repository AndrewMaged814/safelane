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
    assert "10 schemas valid" in result.stdout
    assert "15 checked-in examples valid" in result.stdout
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
