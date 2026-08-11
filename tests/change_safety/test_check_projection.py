from __future__ import annotations

from dataclasses import dataclass
from dataclasses import replace
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
        self.invalidations: list[tuple[str, int, str]] = []

    def publish(self, assessment, *, check_run_id=None):
        self.calls.append((assessment, check_run_id))
        return Publication(913, "https://github.com/acme/payments/runs/913")

    def invalidate(self, repository, check_run_id, *, superseded_by_head):
        self.invalidations.append((repository, check_run_id, superseded_by_head))


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


def test_new_head_invalidates_previous_check_before_creating_next(tmp_path: Path) -> None:
    publisher = RecordingPublisher()
    host = GuardedHost()
    safety = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        check_publisher=publisher,
        clock=iter([
            "2026-08-12T09:00:00Z",
            "2026-08-12T09:10:00Z",
        ]).__next__,
    )
    safety.assess(PullRequestRef("acme/payments", 42))

    host.snapshot = replace(host.snapshot, head_sha="d" * 40)
    safety.assess(PullRequestRef("acme/payments", 42))

    assert publisher.invalidations == [("acme/payments", 913, "d" * 40)]
    assert publisher.calls[-1][1] is None


def test_failed_check_publication_retries_on_refresh(tmp_path: Path) -> None:
    class FlakyPublisher(RecordingPublisher):
        def publish(self, assessment, *, check_run_id=None):
            self.calls.append((assessment, check_run_id))
            if len(self.calls) == 1:
                raise RuntimeError("temporary GitHub failure")
            return Publication(914, "https://github.com/acme/payments/runs/914")

    publisher = FlakyPublisher()
    safety = ChangeSafety(
        host=GuardedHost(),
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        check_publisher=publisher,
        clock=lambda: "2026-08-12T09:00:00Z",
    )

    safety.assess(PullRequestRef("acme/payments", 42))
    safety.assess(PullRequestRef("acme/payments", 42))

    assert len(publisher.calls) == 2
