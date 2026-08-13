from __future__ import annotations

from dataclasses import replace
from pathlib import Path

import pytest

from safelane.change_safety import (
    ChangeMoved,
    ChangeSafety,
    ImageRegistration,
    PolicyInvalid,
    PullRequestRef,
    PullRequestSnapshot,
)


BASE_SHA = "a" * 40
HEAD_SHA = "b" * 40
TEST_IMAGE = "ghcr.io/acme/payments@sha256:" + "c" * 64


def register_test_image(safety: ChangeSafety, *, image: str = TEST_IMAGE) -> None:
    safety.register_image(ImageRegistration(
        repository="acme/payments",
        service="payments-api",
        source_revision=HEAD_SHA,
        image=image,
        oci_revision=HEAD_SHA,
    ))


def _policy(version: str, *, small_max_files: int) -> bytes:
    return f'''schema_version: "1"
policy_version: "{version}"
release_service:
  name: payments-api
  replicas: 5
  critical: true
  downstream_dependents: [checkout]
  path_prefixes: [src/payments/]
  deployment:
    namespace: payments
    rollout_name: payments-api
    container_name: api
    workload_label: payments-api
scope:
  small_max_files: {small_max_files}
  small_max_lines: 50
  large_min_files: 10
  large_min_lines: 500
ai:
  provider: ollama
  model: qwen2.5-coder:7b
  max_diff_bytes: 16384
  timeout_seconds: 60
  attempts: 1
  temperature: 0
  seed: 42
  num_ctx: 8192
  num_predict: 768
safety_case:
  accepted_categories: [availability, compatibility, data, security, operability]
  minimum_tier:
    availability: risky
    compatibility: guarded
    data: risky
    security: risky
    operability: guarded
trusted_probe_catalog:
  path: .safelane/trusted-probes.yaml
  non_fast_fallback_probe_id: payments-health
  category_bindings:
    availability: payments-api-contract
    compatibility: payments-api-contract
    data: payments-api-contract
    security: payments-api-contract
    operability: payments-health
rollout:
  traffic_router: none
  max_surge: 1
  max_unavailable: 0
profiles:
  Fast:
    stages:
      - {{set_weight: 100, exposure_pods: 5, analysis: false}}
  Guarded:
    stages:
      - {{set_weight: 40, exposure_pods: 2, analysis: true}}
      - {{set_weight: 100, exposure_pods: 5, analysis: false}}
  Strict:
    stages:
      - {{set_weight: 20, exposure_pods: 1, analysis: true}}
      - {{set_weight: 40, exposure_pods: 2, analysis: true}}
      - {{set_weight: 60, exposure_pods: 3, analysis: true}}
      - {{set_weight: 100, exposure_pods: 5, analysis: false}}
'''.encode()


class FakePullRequestHost:
    def __init__(self) -> None:
        self.snapshot = PullRequestSnapshot(
            repository="acme/payments",
            number=42,
            title="Bound retry policy",
            url="https://github.com/acme/payments/pull/42",
            author="andrew",
            base_ref="main",
            head_ref="fix/retries",
            base_sha=BASE_SHA,
            head_sha=HEAD_SHA,
            updated_at="2026-08-12T08:00:00Z",
            is_draft=False,
        )
        self.file_reads: list[tuple[str, str, str]] = []
        self.files = {
            (BASE_SHA, ".safelane/policy.yaml"): _policy(
                "payments-2026.08.1", small_max_files=2
            ),
            # The PR tries to weaken its own policy. This must never be read.
            (HEAD_SHA, ".safelane/policy.yaml"): _policy(
                "payments-evil-head", small_max_files=999
            ),
            (BASE_SHA, ".safelane/trusted-probes.yaml"): b'''schema_version: "1"
catalog_version: "payments-probes-1"
probes:
  - id: payments-health
    analysis_template: payments-health
    description: Verify service health.
  - id: payments-api-contract
    analysis_template: payments-api-contract
    description: Verify the approved API contract.
''',
            (HEAD_SHA, ".safelane/trusted-probes.yaml"): b'''schema_version: "1"
catalog_version: "malicious-head"
probes:
  - id: bypass
    analysis_template: bypass
    description: Bypass checks.
''',
        }

    def get_pull_request(self, change: PullRequestRef) -> PullRequestSnapshot:
        assert change == PullRequestRef("acme/payments", 42)
        return self.snapshot

    def read_file(self, repository: str, revision: str, path: str) -> bytes:
        self.file_reads.append((repository, revision, path))
        return self.files[(revision, path)]

    def diff(self, snapshot: PullRequestSnapshot) -> bytes:
        assert snapshot is self.snapshot
        return b'''diff --git a/src/payments/retries.py b/src/payments/retries.py
index 257cc56..5716ca5 100644
--- a/src/payments/retries.py
+++ b/src/payments/retries.py
@@ -1 +1 @@
-retry_limit = 5
+retry_limit = 6
'''


class NoFindingAnalyzer:
    def __init__(self) -> None:
        self.calls = 0

    def analyze(self, raw_diff: bytes, authorized_spans: list[dict]) -> dict:
        self.calls += 1
        assert b"retry_limit = 6" in raw_diff
        assert authorized_spans
        return {"status": "complete", "findings": []}


def test_assessment_uses_repository_policy_from_base_sha(tmp_path: Path) -> None:
    host = FakePullRequestHost()
    safety = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: NoFindingAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    )

    outcome = safety.assess(PullRequestRef("acme/payments", 42))

    assert outcome.assessment["schema_version"] == "change-assessment-v1"
    assert outcome.assessment["policy"] == {
        "version": "payments-2026.08.1",
        "source_revision": BASE_SHA,
        "sha256": outcome.assessment["policy"]["sha256"],
    }
    assert outcome.assessment["change"]["head_sha"] == HEAD_SHA
    assert outcome.assessment["trusted_probe_catalog"]["source_revision"] == BASE_SHA
    assert outcome.assessment["selected_trusted_probe"] is None
    assert outcome.assessment["risk"]["tier"] == "safe"
    assert outcome.automatic_decision is not None
    assert host.file_reads == [
        ("acme/payments", BASE_SHA, ".safelane/policy.yaml"),
        ("acme/payments", BASE_SHA, ".safelane/trusted-probes.yaml"),
    ]


def test_refresh_reuses_exact_head_assessment_without_rerunning_ai(tmp_path: Path) -> None:
    host = FakePullRequestHost()
    analyzer = NoFindingAnalyzer()
    safety = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: analyzer,
        clock=iter(["2026-08-12T09:00:00Z"]).__next__,
    )

    first = safety.assess(PullRequestRef("acme/payments", 42))
    second = safety.assess(PullRequestRef("acme/payments", 42))

    assert second.assessment == first.assessment
    assert second.automatic_decision == first.automatic_decision
    assert analyzer.calls == 1


def test_backend_policy_not_ai_maps_verified_category_to_tier(tmp_path: Path) -> None:
    host = FakePullRequestHost()

    class SafetyCaseAnalyzer:
        def analyze(self, raw_diff: bytes, authorized_spans: list[dict]) -> dict:
            return {
                "status": "complete",
                "findings": [{
                    "category": "availability",
                    "hypothesis_kind": "changed_behavior_may_violate_contract",
                    "verification_intent_kind": "verify_changed_contract_during_rollout",
                    "approval_question_kind": "confirm_contract_is_preserved",
                    "remediation_kind": "preserve_previous_contract_or_add_compatibility",
                    "spans": [authorized_spans[0]],
                }],
            }

    outcome = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: SafetyCaseAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    ).assess(PullRequestRef("acme/payments", 42))

    assert outcome.assessment["findings"][0]["category"] == "availability"
    assert "severity" not in outcome.assessment["findings"][0]
    assert outcome.assessment["risk"]["tier"] == "risky"
    assert outcome.assessment["risk"]["minimum_profile"] == "Strict"
    assert outcome.assessment["selected_trusted_probe"]["id"] == "payments-api-contract"
    assert outcome.assessment["selected_trusted_probe"]["selection_source"] == "ai_safety_case"


def test_assessment_does_not_publish_when_base_moves_during_analysis(
    tmp_path: Path,
) -> None:
    host = FakePullRequestHost()

    class MovingAnalyzer(NoFindingAnalyzer):
        def analyze(self, raw_diff: bytes, authorized_spans: list[dict]) -> dict:
            result = super().analyze(raw_diff, authorized_spans)
            host.snapshot = replace(host.snapshot, base_sha="d" * 40)
            return result

    safety = ChangeSafety(
        host=host,
        state_dir=tmp_path,
        analyzer_factory=lambda policy: MovingAnalyzer(),
        clock=lambda: "2026-08-12T09:00:00Z",
    )

    with pytest.raises(ChangeMoved, match="base or head moved"):
        safety.assess(PullRequestRef("acme/payments", 42))

    assert not (tmp_path / "acme--payments" / "pr-42" / "assessment.json").exists()


def test_non_fast_profiles_require_a_pre_terminal_analysis_checkpoint(
    tmp_path: Path,
) -> None:
    host = FakePullRequestHost()
    weakened = _policy("payments-weak", small_max_files=2).decode().replace(
        "      - {set_weight: 40, exposure_pods: 2, analysis: true}\n",
        "",
    ).replace(
        "      - {set_weight: 20, exposure_pods: 1, analysis: true}\n"
        "      - {set_weight: 40, exposure_pods: 2, analysis: true}\n"
        "      - {set_weight: 60, exposure_pods: 3, analysis: true}\n",
        "",
    )
    host.files[(BASE_SHA, ".safelane/policy.yaml")] = weakened.encode()

    with pytest.raises(PolicyInvalid, match="analysis checkpoint"):
        ChangeSafety(
            host=host,
            state_dir=tmp_path,
            analyzer_factory=lambda policy: NoFindingAnalyzer(),
        ).assess(PullRequestRef("acme/payments", 42))
