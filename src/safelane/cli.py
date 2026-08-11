from __future__ import annotations

import argparse
import json
import tempfile
from pathlib import Path

from jsonschema import Draft202012Validator

from .artifacts import load_json_bytes, load_yaml_bytes, validate_artifact
from .demo_repository import create_demo_repository
from .evaluation import run_ollama_evaluation
from .pr_studio import (
    GitHubPullRequestProvider,
    OllamaPullRequestAnalyzer,
    PullRequestAssessmentEngine,
    PullRequestStudioError,
    PullRequestStudioService,
)
from .studio import serve_studio


ROOT = Path(__file__).resolve().parents[2]
SCHEMAS = [
    "assessment-request-v2", "policy-v2", "ai-response-v2", "assessment-v2", "decision-v3",
    "release-request-v1", "image-catalog-v1", "trusted-probes-v1", "probe-result-v1",
    "verification-receipt-v1",
]
EXAMPLES = [
    ("assessment-request-v2", "demo/requests/fast.json", "json"),
    ("assessment-request-v2", "demo/requests/strict.json", "json"),
    ("ai-response-v2", "demo/expected/ai-fast.json", "json"),
    ("ai-response-v2", "demo/expected/ai-strict.json", "json"),
    ("assessment-v2", "demo/expected/assessment-fast.example.json", "json"),
    ("assessment-v2", "demo/expected/assessment-strict.example.json", "json"),
    ("decision-v3", "demo/expected/decision-fast.example.json", "json"),
    ("decision-v3", "demo/expected/decision-strict.example.json", "json"),
    ("release-request-v1", "demo/expected/release-request-fast.example.json", "json"),
    ("release-request-v1", "demo/expected/release-request-strict.example.json", "json"),
    ("image-catalog-v1", "demo/image-catalog.example.json", "json"),
    ("probe-result-v1", "demo/expected/probe-result-failed.example.json", "json"),
    ("verification-receipt-v1", "demo/expected/receipt-strict.example.json", "json"),
    ("assessment-v2", "demo/studio-risky/assessment.json", "json"),
    ("policy-v2", "policy.yaml", "yaml"),
    ("trusted-probes-v1", "demo/trusted-probes.yaml", "yaml"),
]


def validate_fixtures() -> None:
    for name in SCHEMAS:
        schema = load_json_bytes((ROOT / "schemas" / f"{name}.schema.json").read_bytes())
        Draft202012Validator.check_schema(schema)
    print(f"{len(SCHEMAS)} schemas valid")

    for schema_name, relative_path, encoding in EXAMPLES:
        raw = (ROOT / relative_path).read_bytes()
        value = load_json_bytes(raw) if encoding == "json" else load_yaml_bytes(raw)
        validate_artifact(schema_name, value)
    print(f"{len(EXAMPLES)} checked-in examples valid")

    manifest_names = ["fast-copy", "additive-route", "quote-contract-break"]
    expected_manifest_keys = ["schema_version", "id", "diff_path", "git_diff_sha256", "expected_normalized_result", "accepted_spans", "forbidden_result"]
    from .artifacts import sha256
    for manifest_name in manifest_names:
        manifest = load_json_bytes(
            (ROOT / f"demo/evaluation/{manifest_name}.manifest.json").read_bytes()
        )
        if list(manifest) != expected_manifest_keys or manifest["id"] != manifest_name:
            raise RuntimeError(f"invalid {manifest_name} manifest shape")
        diff = (ROOT / manifest["diff_path"]).read_bytes()
        if sha256(diff) != manifest["git_diff_sha256"]:
            raise RuntimeError(f"{manifest_name} diff hash mismatch")
        validate_artifact("ai-response-v2", manifest["expected_normalized_result"])
        findings = manifest["expected_normalized_result"]["findings"]
        expected_spans = [] if not findings else findings[0]["spans"]
        if manifest["accepted_spans"] != expected_spans:
            raise RuntimeError(f"invalid {manifest_name} accepted spans")
    print(f"{len(manifest_names)} evaluation manifests and hashes valid")

    expected = json.loads((ROOT / "demo" / "revisions.json").read_text(encoding="utf-8"))
    with tempfile.TemporaryDirectory(prefix="safelane-demo-") as directory:
        actual = create_demo_repository(Path(directory) / "repository")
    if actual != expected:
        raise RuntimeError(f"demo revision drift: expected {expected}, got {actual}")
    print("demo revisions reproduce exactly")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="safelane")
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("validate-fixtures", help="validate schemas and frozen demo fixtures")
    evaluation = subparsers.add_parser(
        "evaluate-ollama", help="run the six-observation one-shot local-model gate"
    )
    evaluation.add_argument(
        "--output",
        type=Path,
        default=ROOT / "demo/evaluation/ollama-observations.json",
    )
    evaluation.add_argument("--base-url", default="http://127.0.0.1:11434")
    studio = subparsers.add_parser(
        "studio", help="review the open pull requests of a local or remote GitHub repository"
    )
    studio.add_argument(
        "--repository",
        help="local Git checkout, GitHub URL, or owner/repository (defaults to .)",
    )
    studio.add_argument(
        "--state-dir",
        type=Path,
        help="where PR assessments and decisions are stored",
    )
    studio.add_argument("--base-url", default="http://127.0.0.1:11434")
    studio.add_argument("--port", type=int, default=4173)
    args = parser.parse_args(argv)
    if args.command == "validate-fixtures":
        validate_fixtures()
    elif args.command == "evaluate-ollama":
        passed = run_ollama_evaluation(args.output, base_url=args.base_url)
        result = load_json_bytes(args.output.read_bytes())
        print(result["summary"]["result"])
        return 0 if passed else 1
    elif args.command == "studio":
        if not 1 <= args.port <= 65_535:
            parser.error("--port must be between 1 and 65535")
        try:
            provider = GitHubPullRequestProvider(args.repository or ".")
            provider.list_open_pull_requests()
            policy = load_yaml_bytes((ROOT / "policy.yaml").read_bytes())
            ai = policy["ai"]
            analyzer = OllamaPullRequestAnalyzer(
                model=ai["model"],
                base_url=args.base_url,
                timeout_seconds=ai["timeout_seconds"],
                temperature=ai["temperature"],
                seed=ai["seed"],
                num_ctx=ai["num_ctx"],
                num_predict=ai["num_predict"],
            )
            assessor = PullRequestAssessmentEngine(
                policy_path=ROOT / "policy.yaml", analyzer=analyzer
            )
            if args.state_dir is not None:
                state_root = args.state_dir
            elif provider.local_path is not None:
                state_root = provider.local_path / ".safelane" / "studio"
            else:
                state_root = Path.cwd() / ".safelane" / "studio"
            workspace = state_root / provider.repository.replace("/", "--")
            service = PullRequestStudioService(
                provider,
                workspace,
                assessor,
                state_root=state_root,
            )
        except (PullRequestStudioError, OSError) as exc:
            parser.error(str(exc))
        serve_studio(service, port=args.port)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
