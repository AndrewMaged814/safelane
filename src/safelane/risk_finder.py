from __future__ import annotations

from dataclasses import dataclass
from collections.abc import Sequence
from typing import Literal, Protocol

from .artifacts import sha256


@dataclass(frozen=True)
class AiAttempt:
    status: Literal["complete", "unavailable"]
    raw_response: bytes | None
    model_digest: str | None
    prompt_sha256: str | None
    response_schema_sha256: str | None
    latency_ms: int


class RiskFinder(Protocol):
    def find(self, canonical_diff: bytes) -> AiAttempt: ...


class FakeRiskFinder:
    """Deterministic adapter used at the public engine seam."""

    def __init__(self, raw_response: bytes | Sequence[bytes], *, status: Literal["complete", "unavailable"] = "complete"):
        self._responses = [raw_response] if isinstance(raw_response, bytes) else list(raw_response)
        if not self._responses:
            raise ValueError("FakeRiskFinder requires at least one response")
        self._status = status
        self.calls: list[bytes] = []

    def find(self, canonical_diff: bytes) -> AiAttempt:
        self.calls.append(canonical_diff)
        if self._status == "unavailable":
            return AiAttempt("unavailable", None, None, None, None, 0)
        response_index = len(self.calls) - 1
        if response_index >= len(self._responses):
            raise RuntimeError("FakeRiskFinder response sequence exhausted")
        return AiAttempt(
            status="complete",
            raw_response=self._responses[response_index],
            model_digest=sha256(b"safelane-fake-risk-finder"),
            prompt_sha256=sha256(b"safelane-fake-prompt-v2"),
            response_schema_sha256=sha256(b"ai-response-v2"),
            latency_ms=0,
        )
