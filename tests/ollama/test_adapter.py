from __future__ import annotations

import json
from io import BytesIO
from pathlib import Path
from urllib.error import HTTPError, URLError

import pytest

from safelane.artifacts import load_json_bytes, sha256
from safelane.risk_finder import OllamaRiskFinder


ROOT = Path(__file__).resolve().parents[2]
DIGEST = "sha256:" + "d" * 64


class _Response:
    def __init__(self, body: bytes):
        self._body = body

    def __enter__(self) -> _Response:
        return self

    def __exit__(self, *_: object) -> None:
        return None

    def read(self) -> bytes:
        return self._body


def _adapter() -> OllamaRiskFinder:
    return OllamaRiskFinder(
        model="qwen2.5-coder:7b",
        max_diff_bytes=16_384,
        timeout_seconds=60,
        temperature=0,
        seed=42,
        num_ctx=8_192,
        num_predict=768,
    )


def test_one_bounded_generate_call_records_auditable_attempt(monkeypatch: pytest.MonkeyPatch) -> None:
    response_value = {"findings": [], "safeguard_proposal": None}
    generate_envelope = json.dumps(
        {
            "model": "qwen2.5-coder:7b",
            "created_at": "2026-08-11T00:00:00Z",
            "response": json.dumps(response_value, separators=(",", ":")),
            "done": True,
            "done_reason": "stop",
        }
    ).encode()
    calls: list[tuple[str, str, int | None, bytes | None]] = []

    def fake_urlopen(request: object, timeout: int | None = None) -> _Response:
        method = request.get_method()  # type: ignore[attr-defined]
        body = request.data  # type: ignore[attr-defined]
        url = request.full_url  # type: ignore[attr-defined]
        calls.append((method, url, timeout, body))
        if url.endswith("/api/tags"):
            return _Response(json.dumps({"models": [{"name": "qwen2.5-coder:7b", "digest": DIGEST.removeprefix("sha256:")}]}).encode())
        return _Response(generate_envelope)

    monkeypatch.setattr("safelane.risk_finder.urlopen", fake_urlopen)
    diff = (ROOT / "demo/evaluation/quote-contract-break.diff").read_bytes()

    attempt = _adapter().find(diff)

    assert [(method, url.rsplit("/", 2)[-2:]) for method, url, _, _ in calls] == [
        ("GET", ["api", "tags"]),
        ("POST", ["api", "generate"]),
    ]
    request = load_json_bytes(calls[1][3] or b"")
    assert calls[1][2] == 60
    assert request["model"] == "qwen2.5-coder:7b"
    assert request["stream"] is False
    assert request["truncate"] is False
    assert request["options"] == {
        "temperature": 0,
        "seed": 42,
        "num_ctx": 8_192,
        "num_predict": 768,
    }
    assert request["prompt"].count(diff.decode()) == 1
    route_changes_text = (
        request["prompt"]
        .split("ROUTE DECORATOR CHANGES\n", 1)[1]
        .split("\n\nCANONICAL DIFF", 1)[0]
    )
    assert load_json_bytes(route_changes_text.encode()) == {
        "removed": [{
            "file": "src/demo_api/app.py", "side": "removed", "line": 17,
            "text": '@app.get("/v1/quote")',
        }],
        "added": [{
            "file": "src/demo_api/app.py", "side": "added", "line": 18,
            "text": '@app.get("/v2/quote")',
        }],
    }
    assert request["format"] == load_json_bytes((ROOT / "schemas" / "ai-response-v2.schema.json").read_bytes())
    assert attempt.status == "complete"
    assert attempt.raw_response == json.dumps(response_value, separators=(",", ":")).encode()
    assert attempt.raw_transport_response == generate_envelope
    assert attempt.model_digest == DIGEST
    assert attempt.prompt_sha256 == sha256(request["prompt"].encode())
    assert attempt.response_schema_sha256 == sha256(request["format"])
    assert attempt.settings == {
        "provider": "ollama",
        "model": "qwen2.5-coder:7b",
        "max_diff_bytes": 16_384,
        "timeout_seconds": 60,
        "attempts": 1,
        "truncate": False,
        **request["options"],
    }
    assert attempt.latency_ms >= 0
    assert attempt.error is None


def test_manifest_digest_is_cached_but_each_find_has_exactly_one_generate_call(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[str] = []

    def fake_urlopen(request: object, timeout: int | None = None) -> _Response:
        del timeout
        calls.append(request.full_url)  # type: ignore[attr-defined]
        if request.full_url.endswith("/api/tags"):  # type: ignore[attr-defined]
            return _Response(json.dumps({"models": [{"name": "qwen2.5-coder:7b", "digest": DIGEST}]}).encode())
        return _Response(json.dumps({"model": "qwen2.5-coder:7b", "response": '{"findings":[],"safeguard_proposal":null}', "done": True}).encode())

    monkeypatch.setattr("safelane.risk_finder.urlopen", fake_urlopen)
    adapter = _adapter()

    adapter.find(b"first")
    adapter.find(b"second")

    assert sum(url.endswith("/api/tags") for url in calls) == 1
    assert sum(url.endswith("/api/generate") for url in calls) == 2


def test_generate_failure_is_unavailable_without_retry(monkeypatch: pytest.MonkeyPatch) -> None:
    calls: list[str] = []

    def fake_urlopen(request: object, timeout: int | None = None) -> _Response:
        del timeout
        calls.append(request.full_url)  # type: ignore[attr-defined]
        if request.full_url.endswith("/api/tags"):  # type: ignore[attr-defined]
            return _Response(json.dumps({"models": [{"name": "qwen2.5-coder:7b", "digest": DIGEST}]}).encode())
        raise URLError("timed out")

    monkeypatch.setattr("safelane.risk_finder.urlopen", fake_urlopen)

    attempt = _adapter().find(b"diff")

    assert attempt.status == "unavailable"
    assert attempt.raw_response is None
    assert attempt.model_digest == DIGEST
    assert attempt.error == "transport_error"
    assert sum(url.endswith("/api/generate") for url in calls) == 1


def test_http_error_body_is_preserved_for_diagnosis(monkeypatch: pytest.MonkeyPatch) -> None:
    error_body = b'{"error":"schema grammar rejected"}'

    def fake_urlopen(request: object, timeout: int | None = None) -> _Response:
        del timeout
        if request.full_url.endswith("/api/tags"):  # type: ignore[attr-defined]
            return _Response(json.dumps({"models": [{"name": "qwen2.5-coder:7b", "digest": DIGEST}]}).encode())
        raise HTTPError(request.full_url, 400, "Bad Request", {}, BytesIO(error_body))  # type: ignore[attr-defined]

    monkeypatch.setattr("safelane.risk_finder.urlopen", fake_urlopen)

    attempt = _adapter().find(b"diff")

    assert attempt.status == "unavailable"
    assert attempt.error == "http_error"
    assert attempt.raw_transport_response == error_body


def test_over_budget_diff_is_rejected_before_any_network_call(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "safelane.risk_finder.urlopen",
        lambda *_args, **_kwargs: pytest.fail("network must not be called"),
    )

    with pytest.raises(ValueError, match="exceeds 16384-byte AI budget"):
        _adapter().find(b"x" * 16_385)


def test_at_budget_diff_is_sent_whole_with_server_truncation_disabled(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[object] = []

    def fake_urlopen(request: object, timeout: int | None = None) -> _Response:
        del timeout
        calls.append(request)
        if request.full_url.endswith("/api/tags"):  # type: ignore[attr-defined]
            return _Response(json.dumps({"models": [{"name": "qwen2.5-coder:7b", "digest": DIGEST}]}).encode())
        return _Response(json.dumps({
            "model": "qwen2.5-coder:7b",
            "response": '{"findings":[],"safeguard_proposal":null}',
            "done": True,
        }).encode())

    monkeypatch.setattr("safelane.risk_finder.urlopen", fake_urlopen)
    diff = b"x" * 16_384

    attempt = _adapter().find(diff)

    generate_calls = [call for call in calls if call.full_url.endswith("/api/generate")]  # type: ignore[attr-defined]
    assert len(generate_calls) == 1
    request = load_json_bytes(generate_calls[0].data)  # type: ignore[attr-defined]
    assert request["truncate"] is False
    assert request["prompt"].endswith(diff.decode())
    assert request["prompt"].count(diff.decode()) == 1
    assert attempt.status == "complete"


def test_non_utf8_diff_is_rejected_before_any_network_call(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        "safelane.risk_finder.urlopen",
        lambda *_args, **_kwargs: pytest.fail("network must not be called"),
    )

    with pytest.raises(ValueError, match="valid UTF-8"):
        _adapter().find(b"\xff")
