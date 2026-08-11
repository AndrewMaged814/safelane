from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from safelane.change_safety import ChangeSafety, PullRequestRef, ResolutionCommand

from .test_resolve import GuardedHost, NoFindingAnalyzer


@dataclass(frozen=True)
class Publication:
    id: int
    url: str


class RecordingPublisher:
    def __init__(self) -> None:
        self.calls: list[tuple[dict, int | None]] = []

    def publish(self, assessment, *, check_run_id=None):
        self.calls.append((assessment, check_run_id))
        return Publication(913, "https://github.com/acme/payments/runs/913")


def test_assessment_creates_and_resolution_updates_same_check_run(tmp_path: Path) -> None:
    publisher = RecordingPublisher()
    safety = ChangeSafety(
        host=GuardedHost(),
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        check_publisher=publisher,
        clock=iter([
            "2026-08-12T09:00:00Z",
            "2026-08-12T09:05:00Z",
        ]).__next__,
    )
    assessed = safety.assess(PullRequestRef("acme/payments", 42))
    safety.resolve(ResolutionCommand(
        handle=assessed.handle,
        action="approve",
        selected_profile="Guarded",
        actor="andrew",
    ))

    assert publisher.calls[0][1] is None
    assert publisher.calls[0][0]["review"]["status"] == "unresolved"
    assert publisher.calls[1][1] == 913
    assert publisher.calls[1][0]["review"]["status"] == "approved"

