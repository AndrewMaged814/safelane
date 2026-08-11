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
    assert "additive-route manifest and hash valid" in result.stdout
    assert "demo revisions reproduce exactly" in result.stdout
