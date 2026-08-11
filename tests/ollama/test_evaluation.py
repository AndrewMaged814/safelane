from __future__ import annotations

from pathlib import Path

from safelane.artifacts import canonical_json_bytes, load_json_bytes
from safelane.evaluation import run_ollama_evaluation
from safelane.risk_finder import AiAttempt, FakeRiskFinder


ROOT = Path(__file__).resolve().parents[2]


def test_six_observations_are_recorded_in_stable_case_and_run_order(tmp_path: Path) -> None:
    fast = (ROOT / "demo/expected/ai-fast.json").read_bytes()
    strict = (ROOT / "demo/expected/ai-strict.json").read_bytes()
    finder = FakeRiskFinder([fast, fast, fast, fast, strict, strict])
    output = tmp_path / "observations.json"

    passed = run_ollama_evaluation(output, risk_finder=finder)

    result = load_json_bytes(output.read_bytes())
    assert passed is True
    assert result["gate"] == "Gate B"
    assert result["summary"] == {"passed": True, "result": "6/6 fixture observations"}
    assert [(item["case_id"], item["run"]) for item in result["observations"]] == [
        ("fast-copy", 1),
        ("fast-copy", 2),
        ("additive-route", 1),
        ("additive-route", 2),
        ("quote-contract-break", 1),
        ("quote-contract-break", 2),
    ]
    assert all(item["passed"] for item in result["observations"])
    assert all(item["raw_response"] is not None for item in result["observations"])
    assert [item["trusted_probe_id"] for item in result["observations"]] == [
        None, None, None, None,
        "demo-api-public-quote-v1", "demo-api-public-quote-v1",
    ]
    assert output.read_bytes() == canonical_json_bytes(result)
    assert finder.calls == [
        (ROOT / "demo/evaluation/fast-copy.diff").read_bytes(),
        (ROOT / "demo/evaluation/fast-copy.diff").read_bytes(),
        (ROOT / "demo/evaluation/additive-route.diff").read_bytes(),
        (ROOT / "demo/evaluation/additive-route.diff").read_bytes(),
        (ROOT / "demo/evaluation/quote-contract-break.diff").read_bytes(),
        (ROOT / "demo/evaluation/quote-contract-break.diff").read_bytes(),
    ]


def test_mismatch_is_recorded_and_fails_the_gate_without_an_extra_attempt(tmp_path: Path) -> None:
    fast = (ROOT / "demo/expected/ai-fast.json").read_bytes()
    strict = (ROOT / "demo/expected/ai-strict.json").read_bytes()
    finder = FakeRiskFinder([fast, fast, strict, fast, strict, strict])
    output = tmp_path / "observations.json"

    passed = run_ollama_evaluation(output, risk_finder=finder)

    result = load_json_bytes(output.read_bytes())
    assert passed is False
    assert result["summary"] == {"passed": False, "result": "5/6 fixture observations"}
    assert len(finder.calls) == 6
    failed = [item for item in result["observations"] if not item["passed"]]
    assert [(item["case_id"], item["run"], item["error"]) for item in failed] == [
        ("additive-route", 1, "unexpected_normalized_result")
    ]


def test_semantic_match_without_complete_audit_evidence_cannot_pass(tmp_path: Path) -> None:
    class MissingAuditFinder:
        def find(self, canonical_diff: bytes) -> AiAttempt:
            del canonical_diff
            return AiAttempt("complete", (ROOT / "demo/expected/ai-fast.json").read_bytes(), None, None, None, 0)

    output = tmp_path / "observations.json"

    passed = run_ollama_evaluation(output, risk_finder=MissingAuditFinder())

    result = load_json_bytes(output.read_bytes())
    assert passed is False
    assert result["summary"]["result"] == "0/6 fixture observations"
    assert result["observations"][0]["error"] == "incomplete_audit_record"
