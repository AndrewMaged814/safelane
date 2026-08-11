from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest

from safelane.artifacts import ArtifactError
from safelane.demo_repository import create_demo_repository
from safelane.engine import AssessmentError, SafeLaneEngine
from safelane.risk_finder import FakeRiskFinder


ROOT = Path(__file__).parents[2]


def git(repo: Path, *args: str) -> str:
    return subprocess.run(
        ["git", "-C", str(repo), *args], check=True, capture_output=True, text=True
    ).stdout.strip()


def init_repo(path: Path) -> tuple[Path, str]:
    path.mkdir()
    git(path, "init", "--initial-branch=main")
    git(path, "config", "user.name", "SafeLane Test")
    git(path, "config", "user.email", "test@safelane.dev")
    git(path, "commit", "--allow-empty", "-m", "base")
    return path, git(path, "rev-parse", "HEAD")


def commit_files(repo: Path, files: dict[str, str], message: str = "change") -> str:
    for relative_path, content in files.items():
        target = repo / relative_path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content, encoding="utf-8", newline="\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-m", message)
    return git(repo, "rev-parse", "HEAD")


def request_file(path: Path, base: str, head: str, pull_request: int = 90) -> Path:
    path.write_text(json.dumps({
        "schema_version": "2",
        "repository": "AndrewMaged814/safelane-demo",
        "pull_request": pull_request,
        "base_sha": base,
        "head_sha": head,
    }), encoding="utf-8")
    return path


def engine(finder: FakeRiskFinder, trusted_probes: Path | None = None) -> SafeLaneEngine:
    return SafeLaneEngine(
        policy_path=ROOT / "policy.yaml",
        trusted_probes_path=trusted_probes or ROOT / "demo/trusted-probes.yaml",
        risk_finder=finder,
    )


@pytest.mark.parametrize(
    ("files", "expected_tier", "expected_rule"),
    [
        ({"src/demo_api/a.py": "x\n" * 25, "src/demo_api/b.py": "x\n" * 25}, "safe", "scope.low"),
        ({f"src/demo_api/{name}.py": "x\n" for name in "abc"}, "guarded", "scope.medium"),
        ({"src/demo_api/a.py": "x\n" * 51}, "guarded", "scope.medium"),
        ({f"src/demo_api/{index}.py": "x\n" for index in range(9)}, "guarded", "scope.medium"),
        ({"src/demo_api/a.py": "x\n" * 499}, "guarded", "scope.medium"),
        ({f"src/demo_api/{index}.py": "x\n" for index in range(10)}, "risky", "scope.high"),
        ({"src/demo_api/a.py": "x\n" * 500}, "risky", "scope.high"),
    ],
)
def test_every_scope_boundary_uses_the_frozen_baseline(
    tmp_path: Path, files: dict[str, str], expected_tier: str, expected_rule: str
) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, files)
    request = request_file(tmp_path / "request.json", base, head)
    finder = FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())

    assessment = engine(finder).assess(repo, request, "2026-08-09T12:00:00Z").assessment

    assert assessment["policy_trace"]["baseline"]["rule_id"] == expected_rule
    assert assessment["policy_result"]["final_tier"] == expected_tier
    assert assessment["policy_result"]["evidence_confidence"] == "high"
    assert assessment["policy_trace"]["safety_floors"] == []


def test_medium_scope_approves_with_policy_fallback_analysis(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {f"src/demo_api/{name}.py": "x\n" for name in "abc"})
    request = request_file(tmp_path / "request.json", base, head)
    lane = engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes()))
    assessment = lane.assess(repo, request, "2026-08-09T12:00:00Z").assessment
    event = {
        "type": "human", "selected_profile": "Guarded", "resolved_at": "2026-08-09T12:05:00Z",
        "assessment_id": assessment["assessment_id"], "head_sha": head,
        "assessment_input_sha256": assessment["assessment_input_sha256"],
        "assessment_result_sha256": assessment["assessment_result_sha256"],
    }

    resolved = lane.approve(assessment, event)

    assert resolved.decision["analysis"]["selection_source"] == "policy_fallback"
    assert resolved.decision["profile"]["name"] == "Guarded"


def build_sized_diff(tmp_path: Path, target_size: int) -> tuple[Path, Path, FakeRiskFinder]:
    repo, base = init_repo(tmp_path / f"repo-{target_size}")
    source = repo / "src/demo_api/budget.py"
    source.parent.mkdir(parents=True)
    source.write_text('VALUE = "base"\n', encoding="utf-8", newline="\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-m", "add budget fixture")
    base = git(repo, "rev-parse", "HEAD")
    filler = max(1, target_size - 300)
    for _ in range(4):
        source.write_text(f'VALUE = "{"x" * filler}"\n', encoding="utf-8", newline="\n")
        raw = subprocess.run(
            ["git", "-C", str(repo), "-c", "core.quotePath=true", "diff", "--no-ext-diff",
             "--no-textconv", "--no-color", "--no-renames", "--unified=3", "--src-prefix=a/",
             "--dst-prefix=b/", base, "--"],
            check=True, capture_output=True,
        ).stdout
        if len(raw) == target_size:
            break
        filler += target_size - len(raw)
    assert len(raw) == target_size
    git(repo, "add", "-A")
    git(repo, "commit", "-m", f"create {target_size}-byte diff")
    head = git(repo, "rev-parse", "HEAD")
    request = request_file(tmp_path / f"request-{target_size}.json", base, head, target_size)
    finder = FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())
    return repo, request, finder


def test_16384_bytes_calls_adapter_while_16385_skips_and_floors(tmp_path: Path) -> None:
    at_limit_repo, at_limit_request, at_limit_finder = build_sized_diff(tmp_path, 16_384)
    over_repo, over_request, over_finder = build_sized_diff(tmp_path, 16_385)

    at_limit = engine(at_limit_finder).assess(
        at_limit_repo, at_limit_request, "2026-08-09T12:00:00Z"
    ).assessment
    over = engine(over_finder).assess(over_repo, over_request, "2026-08-09T12:00:00Z").assessment

    assert len(at_limit_finder.calls) == 1
    assert len(at_limit_finder.calls[0]) == 16_384
    assert at_limit["evidence"]["ai_status"] == "complete"
    assert over_finder.calls == []
    assert over["evidence"]["ai_status"] == "skipped_over_budget"
    assert over["policy_result"]["final_tier"] == "guarded"
    assert "evidence.diff_over_budget" in {
        floor["rule_id"] for floor in over["policy_trace"]["safety_floors"]
    }


@pytest.fixture
def demo_repo(tmp_path: Path) -> Path:
    repo = tmp_path / "demo"
    create_demo_repository(repo)
    return repo


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("file", "src/demo_api/fabricated.py"),
        ("side", "added"),
        ("line", 999),
        ("text", '@app.get(dynamic_route)'),
    ],
)
def test_wrong_source_tuple_never_selects_executable_model_value(
    demo_repo: Path, field: str, value: object
) -> None:
    response = json.loads((ROOT / "demo/expected/ai-strict.json").read_text(encoding="utf-8"))
    response["findings"][0]["spans"][0][field] = value
    finder = FakeRiskFinder(json.dumps(response).encode())

    assessment = engine(finder).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment

    assert assessment["ai_findings"] == []
    assert assessment["selected_safeguard"] is None
    assert assessment["policy_result"]["evidence_confidence"] == "low"


def test_reversed_route_roles_are_rejected(demo_repo: Path) -> None:
    response = json.loads((ROOT / "demo/expected/ai-strict.json").read_text(encoding="utf-8"))
    response["findings"][0]["spans"].reverse()

    assessment = engine(FakeRiskFinder(json.dumps(response).encode())).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment

    assert assessment["ai_findings"] == []
    assert assessment["selected_safeguard"] is None


@pytest.mark.parametrize(
    ("field", "value"),
    [
        ("finding_index", 1),
        ("hypothesis_kind", "model_guess"),
        ("verification_intent_kind", "run_arbitrary_command"),
        ("approval_question_kind", "free_form_question"),
        ("remediation_kind", "apply_generated_patch"),
    ],
)
def test_each_invalid_proposal_component_preserves_risky_finding(
    demo_repo: Path, field: str, value: object
) -> None:
    response = json.loads((ROOT / "demo/expected/ai-strict.json").read_text(encoding="utf-8"))
    response["safeguard_proposal"][field] = value

    assessment = engine(FakeRiskFinder(json.dumps(response).encode())).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment

    assert [finding["kind"] for finding in assessment["ai_findings"]] == ["breaking_api"]
    assert assessment["selected_safeguard"] is None
    assert assessment["policy_result"]["final_tier"] == "risky"
    assert assessment["policy_result"]["primary_reason"] == (
        "An existing HTTP contract was removed or renamed."
    )
    assert [floor["rule_id"] for floor in assessment["policy_trace"]["safety_floors"]] == [
        "finding.breaking_api", "evidence.ai_incomplete", "evidence.safeguard_invalid"
    ]


def test_result_hash_changes_when_reviewed_ai_content_changes(demo_repo: Path) -> None:
    valid = (ROOT / "demo/expected/ai-strict.json").read_bytes()
    invalid = json.loads(valid)
    invalid["safeguard_proposal"]["finding_index"] = 1

    first = engine(FakeRiskFinder(valid)).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment
    second = engine(FakeRiskFinder(json.dumps(invalid).encode())).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment

    assert first["assessment_input_sha256"] == second["assessment_input_sha256"]
    assert first["assessment_result_sha256"] != second["assessment_result_sha256"]
    assert first["policy_result"]["final_tier"] == second["policy_result"]["final_tier"] == "risky"


def test_untrusted_probe_catalog_relationship_is_rejected_before_assessment(
    tmp_path: Path
) -> None:
    catalog = (ROOT / "demo/trusted-probes.yaml").read_text(encoding="utf-8")
    altered = tmp_path / "trusted-probes.yaml"
    altered.write_text(catalog.replace("path: /v1/quote", "path: /v9/quote"), encoding="utf-8")

    with pytest.raises(ArtifactError):
        engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes()), altered)


def test_change_with_no_mapped_release_service_is_rejected(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {"unmapped/readme.txt": "change\n"})
    request = request_file(tmp_path / "request.json", base, head)

    with pytest.raises(AssessmentError, match="one mapped release service"):
        engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
            repo, request, "2026-08-09T12:00:00Z"
        )


def test_binary_patch_is_rejected_before_model_call(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    blob = repo / "src/demo_api/blob.bin"
    blob.parent.mkdir(parents=True)
    blob.write_bytes(b"\x00\xff\x00\x01")
    git(repo, "add", "-A")
    git(repo, "commit", "-m", "binary change")
    head = git(repo, "rev-parse", "HEAD")
    request = request_file(tmp_path / "request.json", base, head)
    finder = FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())

    with pytest.raises(AssessmentError, match="binary patches are unsupported"):
        engine(finder).assess(repo, request, "2026-08-09T12:00:00Z")
    assert finder.calls == []


def test_invalid_utf8_keeps_mapped_service_and_applies_only_its_floor(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    target = repo / "src/demo_api/invalid.py"
    target.parent.mkdir(parents=True)
    target.write_bytes(b"VALUE = '\xff'\n")
    git(repo, "add", "-A")
    git(repo, "commit", "-m", "invalid utf8 text")
    head = git(repo, "rev-parse", "HEAD")
    request = request_file(tmp_path / "request.json", base, head)
    finder = FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())

    assessment = engine(finder).assess(repo, request, "2026-08-09T12:00:00Z").assessment

    assert assessment["change"]["services"] == ["demo-api"]
    assert assessment["evidence"]["ai_status"] == "skipped_invalid_diff"
    assert [floor["rule_id"] for floor in assessment["policy_trace"]["safety_floors"]] == [
        "evidence.diff_invalid_utf8"
    ]
    assert assessment["policy_result"]["final_tier"] == "guarded"
    assert finder.calls == []


def test_known_and_unknown_paths_apply_path_floor_without_losing_service(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {
        "src/demo_api/app.py": "known\n",
        "unmapped/readme.txt": "unknown\n",
    })
    request = request_file(tmp_path / "request.json", base, head)

    assessment = engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
        repo, request, "2026-08-09T12:00:00Z"
    ).assessment

    assert assessment["change"]["services"] == ["demo-api"]
    assert assessment["change"]["all_paths_recognized"] is False
    assert [floor["rule_id"] for floor in assessment["policy_trace"]["safety_floors"]] == [
        "evidence.path_unrecognized"
    ]


def test_quoted_non_ascii_path_is_conservatively_unrecognized(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {
        "src/demo_api/app.py": "known\n",
        "évidence.txt": "unknown\n",
    })
    request = request_file(tmp_path / "request.json", base, head)

    assessment = engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
        repo, request, "2026-08-09T12:00:00Z"
    ).assessment

    assert assessment["change"]["all_paths_recognized"] is False
    assert assessment["policy_result"]["final_tier"] == "guarded"


def test_binary_marker_words_inside_text_are_not_a_binary_patch(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {
        "src/demo_api/app.py": "Binary files are discussed here.\nGIT binary patch is a phrase.\n"
    })
    request = request_file(tmp_path / "request.json", base, head)

    artifacts = engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
        repo, request, "2026-08-09T12:00:00Z"
    )

    assert artifacts.assessment["policy_result"]["final_tier"] == "safe"
    assert artifacts.automatic_decision is not None


def test_successful_git_command_stderr_is_typed_input_error(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {"src/demo_api/app.py": "known\n"})
    request = request_file(tmp_path / "request.json", base, head)
    monkeypatch.setenv("GIT_TRACE", "1")

    with pytest.raises(AssessmentError, match="wrote to stderr"):
        engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
            repo, request, "2026-08-09T12:00:00Z"
        )


def test_unavailable_and_malformed_ai_apply_incomplete_floor(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    head = commit_files(repo, {"src/demo_api/app.py": "known\n"})
    request = request_file(tmp_path / "request.json", base, head)
    finders = [
        FakeRiskFinder(b"{}", status="unavailable"),
        FakeRiskFinder(b'{"findings": []}'),
    ]

    for finder in finders:
        assessment = engine(finder).assess(repo, request, "2026-08-09T12:00:00Z").assessment
        assert [floor["rule_id"] for floor in assessment["policy_trace"]["safety_floors"]] == [
            "evidence.ai_incomplete"
        ]
        assert assessment["policy_result"]["final_tier"] == "guarded"


def test_absent_proposal_keeps_finding_and_fixed_floor_order(demo_repo: Path) -> None:
    response = json.loads((ROOT / "demo/expected/ai-strict.json").read_text(encoding="utf-8"))
    response["safeguard_proposal"] = None

    assessment = engine(FakeRiskFinder(json.dumps(response).encode())).assess(
        demo_repo, ROOT / "demo/requests/strict.json", "2026-08-09T12:00:00Z"
    ).assessment

    assert [finding["kind"] for finding in assessment["ai_findings"]] == ["breaking_api"]
    assert [floor["rule_id"] for floor in assessment["policy_trace"]["safety_floors"]] == [
        "finding.breaking_api", "evidence.ai_incomplete", "evidence.safeguard_invalid"
    ]


def test_additive_route_temporary_history_remains_fast(tmp_path: Path) -> None:
    repo, base = init_repo(tmp_path / "repo")
    old_source = '@app.get("/v1/quote")\ndef quote_v1():\n    return {}\n'
    old = commit_files(repo, {"src/demo_api/app.py": old_source}, "old route")
    new_source = old_source + '\n@app.get("/v2/quote")\ndef quote_v2():\n    return {}\n'
    head = commit_files(repo, {"src/demo_api/app.py": new_source}, "add route")
    request = request_file(tmp_path / "request.json", old, head)

    artifacts = engine(FakeRiskFinder((ROOT / "demo/expected/ai-fast.json").read_bytes())).assess(
        repo, request, "2026-08-09T12:00:00Z"
    )

    assert artifacts.assessment["ai_findings"] == []
    assert artifacts.assessment["policy_result"]["final_tier"] == "safe"
    assert artifacts.automatic_decision is not None
