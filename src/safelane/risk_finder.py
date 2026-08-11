from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from time import perf_counter
from typing import Any, Literal, Protocol
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

from .artifacts import ArtifactError, canonical_json_bytes, load_json_bytes, sha256
from .diff_evidence import parse_diff


@dataclass(frozen=True)
class AiAttempt:
    status: Literal["complete", "unavailable"]
    raw_response: bytes | None
    model_digest: str | None
    prompt_sha256: str | None
    response_schema_sha256: str | None
    latency_ms: int
    settings: dict[str, Any] | None = None
    raw_transport_response: bytes | None = None
    error: str | None = None


class RiskFinder(Protocol):
    def find(self, canonical_diff: bytes) -> AiAttempt: ...


class _HttpFailure(RuntimeError):
    def __init__(self, body: bytes):
        super().__init__("Ollama returned an HTTP error")
        self.body = body


class OllamaRiskFinder:
    """One-shot, bounded Ollama adapter for the frozen SafeLane AI contract."""

    def __init__(
        self,
        *,
        model: str,
        max_diff_bytes: int,
        timeout_seconds: int,
        temperature: int,
        seed: int,
        num_ctx: int,
        num_predict: int,
        base_url: str = "http://127.0.0.1:11434",
    ) -> None:
        self._model = model
        self._max_diff_bytes = max_diff_bytes
        self._timeout_seconds = timeout_seconds
        self._options = {
            "temperature": temperature,
            "seed": seed,
            "num_ctx": num_ctx,
            "num_predict": num_predict,
        }
        self._settings = {
            "provider": "ollama",
            "model": model,
            "max_diff_bytes": max_diff_bytes,
            "timeout_seconds": timeout_seconds,
            "attempts": 1,
            "truncate": False,
            **self._options,
        }
        self._base_url = base_url.rstrip("/")
        root = Path(__file__).resolve().parents[2]
        self._response_schema = load_json_bytes(
            (root / "schemas" / "ai-response-v2.schema.json").read_bytes()
        )
        self._response_schema_sha256 = sha256(self._response_schema)
        self._model_digest: str | None = None
        self.attempts: list[AiAttempt] = []

    def find(self, canonical_diff: bytes) -> AiAttempt:
        if len(canonical_diff) > self._max_diff_bytes:
            raise ValueError(
                f"canonical diff exceeds {self._max_diff_bytes}-byte AI budget"
            )
        try:
            decoded_diff = canonical_diff.decode("utf-8")
        except UnicodeDecodeError as exc:
            raise ValueError("canonical diff must be valid UTF-8") from exc

        prompt = self._prompt(decoded_diff)
        prompt_sha256 = sha256(prompt.encode("utf-8"))
        started = perf_counter()
        raw_transport: bytes | None = None
        try:
            digest = self._model_digest or self._fetch_model_digest()
            self._model_digest = digest
            request_value = {
                "model": self._model,
                "prompt": prompt,
                "format": self._response_schema,
                "stream": False,
                "truncate": False,
                "options": self._options,
            }
            raw_transport = self._request(
                "/api/generate", method="POST", body=canonical_json_bytes(request_value)
            )
            envelope = load_json_bytes(raw_transport)
            if (
                not isinstance(envelope, dict)
                or envelope.get("model") != self._model
                or envelope.get("done") is not True
                or not isinstance(envelope.get("response"), str)
            ):
                raise ArtifactError("invalid Ollama generate envelope")
            attempt = AiAttempt(
                status="complete",
                raw_response=envelope["response"].encode("utf-8"),
                model_digest=digest,
                prompt_sha256=prompt_sha256,
                response_schema_sha256=self._response_schema_sha256,
                latency_ms=_elapsed_ms(started),
                settings=dict(self._settings),
                raw_transport_response=raw_transport,
            )
        except _HttpFailure as exc:
            raw_transport = exc.body
            attempt = AiAttempt(
                status="unavailable",
                raw_response=None,
                model_digest=self._model_digest,
                prompt_sha256=prompt_sha256,
                response_schema_sha256=self._response_schema_sha256,
                latency_ms=_elapsed_ms(started),
                settings=dict(self._settings),
                raw_transport_response=raw_transport,
                error="http_error",
            )
        except (ArtifactError, URLError, OSError):
            attempt = AiAttempt(
                status="unavailable",
                raw_response=None,
                model_digest=self._model_digest,
                prompt_sha256=prompt_sha256,
                response_schema_sha256=self._response_schema_sha256,
                latency_ms=_elapsed_ms(started),
                settings=dict(self._settings),
                raw_transport_response=raw_transport,
                error="invalid_envelope" if raw_transport is not None else "transport_error",
            )
        self.attempts.append(attempt)
        return attempt

    def _fetch_model_digest(self) -> str:
        response = load_json_bytes(self._request("/api/tags", method="GET"))
        if not isinstance(response, dict) or not isinstance(response.get("models"), list):
            raise ArtifactError("invalid Ollama model manifest")
        for model in response["models"]:
            if not isinstance(model, dict):
                continue
            if model.get("name") == self._model and isinstance(model.get("digest"), str):
                digest = model["digest"]
                if len(digest) == 64:
                    digest = f"sha256:{digest}"
                if digest.startswith("sha256:") and len(digest) == 71:
                    return digest
        raise ArtifactError(f"pinned Ollama model is unavailable: {self._model}")

    def _request(self, path: str, *, method: str, body: bytes | None = None) -> bytes:
        request = Request(
            f"{self._base_url}{path}",
            data=body,
            method=method,
            headers={"Content-Type": "application/json"},
        )
        try:
            with urlopen(request, timeout=self._timeout_seconds) as response:
                return response.read()
        except HTTPError as exc:
            raise _HttpFailure(exc.read()) from exc

    def _prompt(self, canonical_diff: str) -> str:
        schema_text = canonical_json_bytes(self._response_schema).decode("utf-8").rstrip()
        parsed_spans, _, _ = parse_diff(canonical_diff)
        changed_spans = [
            {"file": span.file, "side": span.side, "line": span.line, "text": span.text}
            for span in parsed_spans
        ]
        route_spans = [span for span in changed_spans if span["text"].startswith("@app.")]
        allowed_text = canonical_json_bytes(route_spans).decode("utf-8").rstrip()
        route_changes = {
            "removed": [
                span for span in route_spans if span["side"] == "removed"
            ],
            "added": [
                span for span in route_spans if span["side"] == "added"
            ],
        }
        route_changes_text = canonical_json_bytes(route_changes).decode("utf-8").rstrip()
        return (
            "You are SafeLane's bounded breaking-API risk finder. Return only JSON matching "
            "the supplied schema. Classify the authoritative ROUTE DECORATOR CHANGES: one removed "
            "route plus one different added route is a breaking rename and requires exactly one "
            "breaking_api finding and the enum-only proposal; an empty removed array is an additive "
            "change and requires zero findings with a null proposal. A context route is preserved. "
            "For a breaking rename, copy the removed tuple first and the added tuple second. "
            "Every cited span must be copied byte-for-byte from AUTHORIZED CHANGED-LINE TUPLES; "
            "those tuples contain the normal-code-computed Git line numbers. "
            "Never invent a path, line, or executable safeguard.\n\n"
            f"RESPONSE SCHEMA\n{schema_text}\n\n"
            f"AUTHORIZED CHANGED-LINE TUPLES\n{allowed_text}\n\n"
            f"ROUTE DECORATOR CHANGES\n{route_changes_text}\n\n"
            f"CANONICAL DIFF\n{canonical_diff}"
        )


def _elapsed_ms(started: float) -> int:
    return max(0, round((perf_counter() - started) * 1000))


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
            settings={
                "provider": "ollama",
                "model": "qwen2.5-coder:7b",
                "max_diff_bytes": 16_384,
                "timeout_seconds": 60,
                "attempts": 1,
                "truncate": False,
                "temperature": 0,
                "seed": 42,
                "num_ctx": 8_192,
                "num_predict": 768,
            },
        )
