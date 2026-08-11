from __future__ import annotations

import argparse
import json
import tempfile
from pathlib import Path

from jsonschema import Draft202012Validator

from .artifacts import load_json_bytes, load_yaml_bytes, validate_artifact
from .demo_repository import create_demo_repository


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

    manifest = load_json_bytes((ROOT / "demo/evaluation/additive-route.manifest.json").read_bytes())
    expected_manifest_keys = ["schema_version", "id", "diff_path", "git_diff_sha256", "expected_normalized_result", "accepted_spans", "forbidden_result"]
    if list(manifest) != expected_manifest_keys or manifest["id"] != "additive-route":
        raise RuntimeError("invalid additive-route manifest shape")
    diff = (ROOT / manifest["diff_path"]).read_bytes()
    from .artifacts import sha256
    if sha256(diff) != manifest["git_diff_sha256"]:
        raise RuntimeError("additive-route diff hash mismatch")
    validate_artifact("ai-response-v2", manifest["expected_normalized_result"])
    if manifest["accepted_spans"] != [] or manifest["forbidden_result"] != "breaking_api":
        raise RuntimeError("invalid additive-route expected result")
    print("additive-route manifest and hash valid")

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
    args = parser.parse_args(argv)
    if args.command == "validate-fixtures":
        validate_fixtures()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
