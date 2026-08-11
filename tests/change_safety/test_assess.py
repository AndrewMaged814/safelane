from __future__ import annotations

from dataclasses import replace
from pathlib import Path

from safelane.change_safety import (
    ChangeSafety,
    PullRequestRef,
    PullRequestSnapshot,
)


BASE_SHA = "a" * 40
HEAD_SHA = "b" * 40


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
    analysis_template: payments-api-contract
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
    assert outcome.assessment["risk"]["tier"] == "safe"
    assert outcome.automatic_decision is not None
    assert host.file_reads == [
        ("acme/payments", BASE_SHA, ".safelane/policy.yaml")
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
