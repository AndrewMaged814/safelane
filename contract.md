# SafeLane — pre-final artifact and lifecycle contract

**Contract document:** v5, frozen 2026-08-09

**Wire schemas:** assessment request v2, policy v2, AI response v2, assessment v2, decision v3,
release request v1, image catalog v1, trusted-probes v1, probe-result v1, and verification receipt v1

This document is the canonical pre-final artifact and lifecycle contract. Input policy details live in
`docs/input-contracts.md`, risk predicates in `docs/risk-signals.md`, rollout stages in
`docs/rollout-profiles.md`, and UI behavior in `docs/safelane-studio.md`.

## Product boundary

SafeLane has one evidence-to-enforcement path:

```text
exact Git change
    -> one bounded AI attempt
    -> verified finding and optional trusted safeguard
    -> fixed policy result
    -> automatic Fast resolution or explicit human resolution
    -> decision.json
    -> independently identified release
    -> trusted canary probe and Argo outcome
    -> verification receipt
```

AI supplies semantic candidates. It never supplies a probe ID, host, URL, request body, expected
status, image, command, credential, tier, profile, stage, approval, or Kubernetes field. Normal code
verifies source references, renders prose, resolves a trusted probe, and applies policy. The release
consumer reads no raw AI fields.

## Artifact boundary

- `assessment.json` v2 is Andrew-owned review state. It contains exact change identity, accepted AI
  findings, an optional validated safeguard, deterministic policy trace, evidence confidence, and
  resolution state.
- `decision.json` v3 is the only SafeLane-to-release runtime handoff. It contains the exact identity,
  authorization event, fully resolved built-in profile, and resolved trusted analysis profile.
- `release-request.json` v1 is independently produced by the release command. It identifies the
  expected base/head revisions and inspected application image.
- `image-catalog.json` v1 and `trusted-probes.yaml` are shared, read-only integration contracts.
- `verification-receipt.json` v1 is post-run evidence. It is not authorization and is never an input
  to compilation or deployment.

## Lifecycle and authorization

1. `assess` validates request v2 and policy v2, reads the exact Git range, and calls the pinned model
   at most once.
2. Assessment, approval, and release commands take the same exclusive local-workspace lock. After a
   new assessment has passed request/Git/policy validation, `assess` removes any prior decision
   before atomically publishing the new assessment; a Fast replacement decision is written last.
   A crash can therefore leave no decision, never an old decision beside a newer assessment.
3. The engine validates AI components independently, applies policy, writes assessment v2, and:
   - resolves Fast automatically only when every Fast precondition passes; or
   - leaves Guarded/Risky unresolved.
4. Studio may resolve the current Guarded/Risky assessment with a built-in profile at least as
   careful as its minimum. Approval records a plan; it does not deploy.
5. Resolution atomically replaces the resolved assessment first and creates/replaces decision v3
   last. The decision is the authorization commit point.
6. The release adapter independently constructs release request v1 and validates it with decision v3
   and the trusted catalogs before rendering.
7. Missing, malformed, unresolved, stale, identity-mismatched, or unapproved decisions reject release
   before manifest output. A Strict diagnostic preview is never substitute authorization.
8. After an analysis-bearing apply, the release adapter observes the exact
   Job/AnalysisRun/Rollout chain and writes receipt v1. Fast has no prediction or analysis Job, so it
   writes only the normal terminal/e2e log and no verification receipt.

## Assessment request v2

```json
{
  "schema_version": "2",
  "repository": "AndrewMaged814/safelane-demo",
  "pull_request": 42,
  "base_sha": "0123456789abcdef0123456789abcdef01234567",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd"
}
```

All fields are required. SHAs are full lowercase 40-character hexadecimal commit IDs and must resolve
inside the supplied clean worktree. The range must be linear and `head_sha` must be a direct child of
`base_sha` for the frozen demo. The request contains no caller-supplied changed paths, counts,
services, diff text, tier, profile, image, or shipping time.

## AI response v2

The model returns one JSON object with exactly two top-level fields:

```json
{
  "findings": [
    {
      "kind": "breaking_api",
      "spans": [
        {
          "file": "src/demo_api/app.py",
          "side": "removed",
          "line": 17,
          "text": "@app.get(\"/v1/quote\")"
        },
        {
          "file": "src/demo_api/app.py",
          "side": "added",
          "line": 18,
          "text": "@app.get(\"/v2/quote\")"
        }
      ]
    }
  ],
  "safeguard_proposal": {
    "finding_index": 0,
    "hypothesis_kind": "removed_http_route_unavailable",
    "verification_intent_kind": "preserve_removed_http_route",
    "approval_question_kind": "confirm_callers_migrated",
    "remediation_kind": "retain_removed_route_as_alias"
  }
}
```

Fast output is:

```json
{
  "findings": [],
  "safeguard_proposal": null
}
```

### Structural limits

- The envelope, finding, span, and proposal objects reject unknown fields.
- `findings` contains zero or one item. `breaking_api` is the only accepted pre-final kind.
- A finding contains exactly two spans for the executable quote-contract fixture.
- `side` is `removed` or `added`; `line` is a positive integer; all strings are bounded by the
  executable schema.
- The four proposal kinds are the exact enum values shown above. The model returns no narrative.
- `safeguard_proposal` must be `null` when `findings` is empty. When present, it refers to index `0`.

Validation is deliberately two-phase. The duplicate-key-rejecting JSON decoder first validates a
shallow envelope: the root is an object with exactly `findings` and `safeguard_proposal`, `findings`
is an array of at most one raw value, and the proposal key is present. The engine then validates the
raw finding and proposal independently against the `$defs` in `ai-response-v2.schema.json`, followed
by relationship validation. A malformed envelope invalidates the AI attempt. An invalid proposal is
rejected without erasing a structurally valid finding; an invalid finding discards its proposal and
applies the documented uncertainty floor. The full document is never treated as an all-or-nothing
schema result after the shallow envelope passes.

### Source and relationship validation

Normal code verifies every `(file, side, line, text)` tuple against the canonical Git diff. For the
accepted safety case it additionally requires:

- span 0 is the removed static `GET /v1/quote` decorator;
- span 1 is the added static `GET /v2/quote` decorator;
- both spans belong to the same mapped `demo-api` service;
- the hypothesis, intent, question, and remediation enums are mutually valid for `breaking_api`; and
- `(demo-api, breaking_api, removed_http_route_unavailable, preserve_removed_http_route, GET,
  /v1/quote)` resolves to the sole versioned trusted-probe entry.

Normal code renders these strings from verified values:

- hypothesis/impact: `Existing callers of GET /v1/quote may receive a non-success response.`
- approval question: `Have all callers migrated away from GET /v1/quote?`
- remediation: `Retain GET /v1/quote as an alias while introducing GET /v2/quote, then reassess.`

The UI says `source references verified`, never `AI reasoning verified`. A valid finding with an
invalid or absent proposal remains a Risky finding; the selected safeguard is omitted, evidence
confidence becomes low, and no rejected proposal is displayed or executed.

## Assessment v2

The normative shape is:

```json
{
  "schema_version": "2",
  "assessment_id": "AndrewMaged814/safelane-demo#42@a1b2c3d4e5f6789012345678901234567890abcd:2026.08.3",
  "assessed_at": "2026-08-09T12:00:00Z",
  "policy_version": "2026.08.3",
  "assessment_input_sha256": "sha256:...",
  "assessment_result_sha256": "sha256:...",
  "change": {
    "repository": "AndrewMaged814/safelane-demo",
    "pull_request": 42,
    "base_sha": "0123456789abcdef0123456789abcdef01234567",
    "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
    "files_changed": 1,
    "lines_changed": 2,
    "services": ["demo-api"],
    "all_paths_recognized": true
  },
  "evidence": {
    "git_diff_sha256": "sha256:...",
    "ai_status": "complete",
    "ai_model_digest": "sha256:...",
    "prompt_sha256": "sha256:...",
    "response_schema_sha256": "sha256:...",
    "incident_history": "disabled_by_policy"
  },
  "ai_findings": [
    {
      "id": "finding-001",
      "kind": "breaking_api",
      "spans": [
        {
          "file": "src/demo_api/app.py",
          "side": "removed",
          "line": 17,
          "text": "@app.get(\"/v1/quote\")"
        },
        {
          "file": "src/demo_api/app.py",
          "side": "added",
          "line": 18,
          "text": "@app.get(\"/v2/quote\")"
        }
      ],
      "source_reference_verified": true
    }
  ],
  "selected_safeguard": {
    "finding_ref": "finding-001",
    "selection_source": "ai_safeguard",
    "hypothesis_kind": "removed_http_route_unavailable",
    "hypothesis": "Existing callers of GET /v1/quote may receive a non-success response.",
    "verification_intent_kind": "preserve_removed_http_route",
    "probe_id": "demo-api-public-quote-v1",
    "catalog_entry_sha256": "sha256:...",
    "probe_preview": {
      "method": "GET",
      "path": "/v1/quote",
      "expected_status": 200,
      "attempts": 3,
      "interval_seconds": 10,
      "failure_allowance": 1,
      "request_timeout_seconds": 2,
      "active_deadline_seconds": 45,
      "canary_only": true
    },
    "approval_question_kind": "confirm_callers_migrated",
    "approval_question": "Have all callers migrated away from GET /v1/quote?",
    "remediation_kind": "retain_removed_route_as_alias",
    "remediation": "Retain GET /v1/quote as an alias while introducing GET /v2/quote, then reassess."
  },
  "policy_trace": {
    "baseline": {
      "rule_id": "scope.low",
      "tier": "safe",
      "reason": "The change affects at most 2 recognized files and 50 changed lines."
    },
    "safety_floors": [
      {
        "rule_id": "finding.breaking_api",
        "minimum_tier": "risky",
        "reason": "An existing HTTP contract was removed or renamed."
      }
    ]
  },
  "policy_result": {
    "final_tier": "risky",
    "minimum_profile": "Strict",
    "evidence_confidence": "high",
    "fast_eligible": false,
    "primary_reason": "An existing HTTP contract was removed or renamed."
  },
  "rollout_options": [
    {
      "name": "Strict",
      "source": "built_in",
      "traffic_router": "none",
      "replicas": 5,
      "max_surge": 1,
      "max_unavailable": 0,
      "stages": [
        {"set_weight": 20, "exposure_pods": 1, "analysis": true},
        {"set_weight": 40, "exposure_pods": 2, "analysis": true},
        {"set_weight": 60, "exposure_pods": 3, "analysis": true},
        {"set_weight": 100, "exposure_pods": 5, "analysis": false}
      ]
    }
  ],
  "review": {
    "status": "unresolved",
    "resolution": null
  }
}
```

Every shown field is required. `ai_findings` may be empty and `selected_safeguard` may be `null`.
`selected_safeguard.probe_preview` is a non-executable, normal-code projection of the hash-matched
trusted catalog plus policy-owned Job settings, allowing Studio to remain assessment-only. It never
contains the target, image, command, or credentials.
`rollout_options` is a normal-code copy of every built-in profile permitted by the final tier, ordered
from minimum to most careful: `[Fast]`, `[Guarded, Strict]`, or `[Strict]`. Studio reads these complete
previews from the assessment; it never loads policy. Resolution may select only an exact listed option,
and decision v3 copies that option byte-for-byte.
`evidence.ai_status` is `complete`, `partial`, `unavailable`, `skipped_invalid_diff`, or
`skipped_over_budget`. `complete` means the shallow envelope and every supplied component and
relationship passed; the valid empty-Fast response qualifies. `partial` means a model response
arrived but JSON/envelope/component/source/relationship validation rejected any part. `unavailable`
means the attempted transport timed out or failed. The two skipped states mean no call was made.
Model metadata fields are nullable unless status is `complete` or `partial`.

### Identity and hashes

`assessment_input_sha256` hashes this exact field-ordered canonical JSON envelope:

```json
{
  "schema_version": "1",
  "request": {
    "schema_version": "2",
    "repository": "AndrewMaged814/safelane-demo",
    "pull_request": 42,
    "base_sha": "0123456789abcdef0123456789abcdef01234567",
    "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd"
  },
  "policy_sha256": "sha256:...",
  "git_diff_sha256": "sha256:...",
  "git_diff_byte_length": 2048,
  "incident_history": "disabled_by_policy",
  "trusted_probe_catalog_sha256": "sha256:...",
  "ai_configuration": {
    "provider": "ollama",
    "model": "qwen2.5-coder:7b",
    "ai_model_digest": "sha256:...",
    "prompt_sha256": "sha256:...",
    "response_schema_sha256": "sha256:...",
    "max_diff_bytes": 16384,
    "timeout_seconds": 60,
    "attempts": 1,
    "temperature": 0,
    "seed": 42,
    "num_ctx": 8192,
    "num_predict": 768
  }
}
```

`policy_sha256` hashes the canonical JSON projection of validated policy v2. `git_diff_sha256`
hashes the raw canonical Git-diff bytes and `git_diff_byte_length` counts those bytes; the envelope
never embeds the diff, so even invalid UTF-8 has one reproducible representation. The incident value
is the exact shown string. Every SHA-256 value uses the lowercase `sha256:<64 hex>` form in real
artifacts.

`assessment_result_sha256` hashes the immutable canonical assessment result excluding itself and the
entire mutable `review` object. Accepted findings, selected safeguard, rendered text, probe binding,
policy trace, rollout options, and evidence status are therefore covered by approval.

`assessment_id` is `<repository>#<pull_request>@<head_sha>:<policy_version>`. It is an identifier, not
a substitute for either content hash.

### Resolution

Fast assessments resolve automatically. Guarded and Risky assessments begin unresolved and emit no
decision. A resolution event contains:

```json
{
  "type": "human",
  "selected_profile": "Strict",
  "resolved_at": "2026-08-09T12:05:00Z",
  "assessment_id": "...",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
  "assessment_input_sha256": "sha256:...",
  "assessment_result_sha256": "sha256:..."
}
```

`type` is `automatic` or `human`. Automatic resolution is valid only for Fast and uses the same
`assessed_at` timestamp. Human resolution selects only a built-in profile at least as careful as the
minimum. A stale ID, SHA, hash, policy, or faster profile rejects resolution. Phase 1 stores no
approver identity because accounts and roles are out of scope.

## Decision v3

```json
{
  "schema_version": "3",
  "assessment_id": "AndrewMaged814/safelane-demo#42@a1b2c3d4e5f6789012345678901234567890abcd:2026.08.3",
  "assessment_input_sha256": "sha256:...",
  "assessment_result_sha256": "sha256:...",
  "repository": "AndrewMaged814/safelane-demo",
  "pull_request": 42,
  "base_sha": "0123456789abcdef0123456789abcdef01234567",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
  "service": "demo-api",
  "policy_version": "2026.08.3",
  "tier": "risky",
  "primary_reason": "An existing HTTP contract was removed or renamed.",
  "profile": {
    "name": "Strict",
    "source": "built_in",
    "traffic_router": "none",
    "replicas": 5,
    "max_surge": 1,
    "max_unavailable": 0,
    "stages": [
      {"set_weight": 20, "exposure_pods": 1, "analysis": true},
      {"set_weight": 40, "exposure_pods": 2, "analysis": true},
      {"set_weight": 60, "exposure_pods": 3, "analysis": true},
      {"set_weight": 100, "exposure_pods": 5, "analysis": false}
    ]
  },
  "analysis": {
    "kind": "job_http_contract_probe",
    "probe_id": "demo-api-public-quote-v1",
    "catalog_entry_sha256": "sha256:...",
    "selection_source": "ai_safeguard",
    "attempts": 3,
    "interval_seconds": 10,
    "failure_allowance": 1,
    "request_timeout_seconds": 2,
    "active_deadline_seconds": 45
  },
  "resolution": {
    "type": "human",
    "resolved_at": "2026-08-09T12:05:00Z"
  }
}
```

Every field is required. `analysis` is `null` only for Fast. Guarded and Strict carry the policy-owned
trusted Job profile. `decision.json` contains no raw model output, source spans, hypothesis,
question, remediation, model metadata, or Studio state. Profile stages are already resolved; the
compiler never maps a tier to a profile or modifies their order.

`profile.stages[].analysis: true` compiles to exactly one inline Argo analysis step immediately after
that stage's `setWeight`; it is not a timed pause. `false` emits no analysis or pause. The Job's
attempt/interval/timeout/deadline fields own its actual duration.

`analysis.selection_source` is normal-code output: `ai_safeguard` when a validated proposal resolved
the probe or `policy_fallback` for every non-Fast decision whose assessment has
`selected_safeguard: null`, whether the non-Fast tier came from scope or uncertainty. It is not model
authority.

Decision v3 and the compiler enforce this complete cross-field authorization matrix:

| Tier | Allowed profile | Resolution | Analysis |
|---|---|---|---|
| `safe` | `Fast` only | `automatic` only | `null` |
| `guarded` | `Guarded` or `Strict` | `human` only | trusted Job with `policy_fallback` |
| `risky` | `Strict` only | `human` only | trusted Job with `ai_safeguard` or `policy_fallback` |

The selected profile must byte-match its built-in definition, including stages and analysis flags.
The JSON Schema uses cross-field conditions and the compiler repeats the matrix check; every other
tier/profile/resolution/analysis combination rejects before manifest output.

## Release request v1

```json
{
  "schema_version": "1",
  "repository": "AndrewMaged814/safelane-demo",
  "service": "demo-api",
  "base_sha": "0123456789abcdef0123456789abcdef01234567",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
  "policy_version": "2026.08.3",
  "image_catalog_version": "2026.08.1",
  "image_catalog_sha256": "sha256:...",
  "image_ref": "safelane-demo:a1b2c3d4e5f6789012345678901234567890abcd",
  "image_id": "sha256:...",
  "runtime_image_id": "sha256:..."
}
```

The release command creates this independently from explicit frozen arguments and trusted image
catalog lookup. Before compilation it verifies that Argo's current stable ReplicaSet source-revision
label equals `base_sha`.

The compiler rejects unless decision, request, image catalog, trusted-probe catalog, and stable-base
preflight agree on every applicable identity. It accepts only the one service, five replicas, three
built-in profiles, router `none`, and the frozen Job profile. It emits a complete manifest bundle;
patching an existing Rollout or using `kubectl argo rollouts set image` is outside the contract.
`decision_sha256` and `release_request_sha256` are SHA-256 hashes of the complete canonical bytes of
their respective validated input artifacts and are emitted as the exact Rollout annotations
`safelane.dev/decision-sha256` and `safelane.dev/release-request-sha256`.

## Image catalog v1

`image-catalog.json` has this exact closed shape; the real file contains all three frozen application
revisions and one probe image:

```json
{
  "schema_version": "1",
  "catalog_version": "2026.08.1",
  "application_images": [
    {
      "repository": "AndrewMaged814/safelane-demo",
      "service": "demo-api",
      "source_revision": "a1b2c3d4e5f6789012345678901234567890abcd",
      "image_ref": "safelane-demo:a1b2c3d4e5f6789012345678901234567890abcd",
      "image_id": "sha256:...",
      "runtime_image_id": "sha256:...",
      "oci_revision": "a1b2c3d4e5f6789012345678901234567890abcd"
    }
  ],
  "probe_images": [
    {
      "key": "demo-api-public-quote-probe@2026.08.1",
      "probe_id": "demo-api-public-quote-v1",
      "image_ref": "safelane-http-probe:2026.08.1",
      "image_id": "sha256:...",
      "runtime_image_id": "sha256:..."
    }
  ]
}
```

Every object rejects unknown fields and every field is required. Application entries are unique by
`(repository, service, source_revision)`; probe entries are unique by both `key` and `probe_id`.
SHAs are full lowercase Git SHAs. `image_id` is the Docker-inspected config digest;
`runtime_image_id` is the normalized digest inspected from kind's containerd after preload and is the
value later compared with Kubernetes container status. Both use lowercase `sha256:<64 hex>`.
Normalization requires exactly one terminal `sha256:<64 lowercase hex>` digest in the runtime value
and stores that digest; missing or ambiguous digests reject preparation/observation.
`image_ref` is a local immutable demo tag, and every application `oci_revision` must equal
`source_revision`. The trusted
probe entry owns only the probe image key; this catalog alone owns image references and IDs. The
compiler resolves that key here and rejects any probe ID mismatch. Catalog version and SHA-256 in
release request v1 cover the canonical JSON bytes of this entire validated model.

## Verification receipt v1

```json
{
  "schema_version": "1",
  "recorded_at": "2026-08-09T12:08:00Z",
  "assessment_result_sha256": "sha256:...",
  "decision_sha256": "sha256:...",
  "release_request_sha256": "sha256:...",
  "image_catalog_sha256": "sha256:...",
  "base_sha": "0123456789abcdef0123456789abcdef01234567",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
  "probe_id": "demo-api-public-quote-v1",
  "catalog_entry_sha256": "sha256:...",
  "selection_source": "ai_safeguard",
  "hypothesis_kind": "removed_http_route_unavailable",
  "rollout": {
    "name": "demo-api",
    "uid": "10000000-0000-0000-0000-000000000000",
    "decision_sha256_annotation": "sha256:...",
    "release_request_sha256_annotation": "sha256:...",
    "metadata_generation": 3,
    "observed_generation": 3,
    "phase": "Degraded",
    "abort": true,
    "abort_origin": "analysis_failure",
    "aborted_at": "2026-08-09T12:07:00Z",
    "progressing_condition": {
      "type": "Progressing",
      "status": "False",
      "reason": "RolloutAborted",
      "message": "Rollout aborted update to revision 3: Step-based analysis phase error/failed"
    },
    "stable_revision": "0123456789abcdef0123456789abcdef01234567",
    "current_revision": "a1b2c3d4e5f6789012345678901234567890abcd"
  },
  "analyses": [
    {
      "stage_index": 0,
      "analysis_run": {
        "name": "demo-api-...",
        "uid": "20000000-0000-0000-0000-000000000000",
        "owner_rollout_uid": "10000000-0000-0000-0000-000000000000",
        "phase": "Failed",
        "completed_at": "2026-08-09T12:06:55Z"
      },
      "canary_target": {
        "service_name": "demo-api-canary",
        "service_uid": "40000000-0000-0000-0000-000000000000",
        "service_selector_pod_template_hash": "abc123",
        "replica_set_uid": "50000000-0000-0000-0000-000000000000",
        "replica_set_pod_template_hash": "abc123",
        "source_revision": "a1b2c3d4e5f6789012345678901234567890abcd",
        "exposure_pods": 1,
        "endpoint_pod_uids": ["60000000-0000-0000-0000-000000000000"],
        "application_image_ref": "safelane-demo:a1b2c3d4e5f6789012345678901234567890abcd",
        "application_runtime_image_id": "sha256:..."
      },
      "job": {
        "name": "demo-api-...",
        "uid": "30000000-0000-0000-0000-000000000000",
        "owner_analysis_run_uid": "20000000-0000-0000-0000-000000000000",
        "phase": "Failed",
        "container_started": true,
        "probe_container_exit_code": 1,
        "probe_pod_uid": "70000000-0000-0000-0000-000000000000",
        "probe_pod_owner_job_uid": "30000000-0000-0000-0000-000000000000",
        "probe_image_ref": "safelane-http-probe:2026.08.1",
        "probe_runtime_image_id": "sha256:..."
      },
      "probe_result": {
        "schema_version": "1",
        "probe_id": "demo-api-public-quote-v1",
        "observations": [
          {"attempt": 1, "outcome": "http_response", "http_status": 404},
          {"attempt": 2, "outcome": "http_response", "http_status": 404},
          {"attempt": 3, "outcome": "http_response", "http_status": 404}
        ],
        "failures": 3,
        "failure_allowance": 1,
        "result": "failed"
      }
    }
  ],
  "release_adapter_abort_requested": false,
  "verdict": "prediction_observed_and_update_aborted",
  "inconclusive_reason": null
}
```

The compiler annotates the Rollout with the canonical decision and release-request hashes. The
observer starts from that exact annotated Rollout, requires equality between `metadata_generation`
and `observed_generation`, then follows controller references to each AnalysisRun, Job, and probe Pod; it
never joins by name prefix or newest timestamp. `analyses` is ordered by `stage_index`, so a
successful Strict rollout can represent all three Jobs. For each stage, the observer snapshots the
canary Service selector and EndpointSlice while the probe Pod is running, proves every endpoint Pod
is owned by the recorded head ReplicaSet, and records the actual application/probe runtime image IDs
from container status. Those normalized IDs must equal the hash-matched image catalog. A Job that
never starts has `container_started: false`, a null exit code, null Pod/image fields, and null
`probe_result`.

Rollout fields that have not been observed may be null only for the `inconclusive` variant.
`aborted_at`, `abort_origin: analysis_failure`, and the aborting `progressing_condition` are required
for `prediction_observed_and_update_aborted` and null for `prediction_not_observed`; both revision
fields and equal generations are required for either conclusive verdict. `hypothesis_kind` is
required for `ai_safeguard` and null for `policy_fallback`.
`job.phase` is the adapter's normalized `Complete` or `Failed` value from Kubernetes Job conditions;
`container_started` is true only when the named probe container has a terminated state with an exit
code, not merely when the Job object exists.

`abort_origin` is derived, never asserted by a caller: `release_adapter` when the adapter issued an
abort; `analysis_failure` only when the linked AnalysisRun completed before `aborted_at` and Argo's
condition has the exact step-analysis signature below; `external_or_unknown` when abort fields exist
without either predicate; and null when no abort exists. This deliberately does not claim it can
identify a human actor from Kubernetes state.

Only normal code derives the verdict using this exhaustive order:

1. `inconclusive` if any schema, hash, annotation, generation, revision, catalog/runtime-image,
   canary-target, UID/owner, timestamp, probe-log, or expected resource check mismatches; any Job did
   not start or finish; abort origin is not the one allowed by a conclusive rule; no AI prediction
   exists; or the HTTP evidence/terminal combination matches neither rule below.
   `inconclusive_reason` is then one closed enum naming the first failed check in the schema-defined
   order.
2. `prediction_observed_and_update_aborted` only for `ai_safeguard` when a started Job's validated log
   contains more HTTP responses with status other than expected 200 than the failure allowance; a
   timeout or connection error never counts as prediction evidence and any such transport outcome
   makes the receipt inconclusive. That Job and its owner AnalysisRun
   must be `Failed`, `analysis_run.completed_at < rollout.aborted_at`, `rollout.abort` true,
   `abort_origin: analysis_failure`, phase `Degraded`, and the `Progressing=False` condition reason
   `RolloutAborted` with a message containing `Step-based analysis phase error/failed`. The
   stable/current revision labels must equal base/head.
3. `prediction_not_observed` only for `ai_safeguard` when every configured analysis stage has one
   started Job whose every observation is HTTP 200—zero non-200, timeout, or connection-error
   observations—every Job and AnalysisRun is `Complete`/`Successful`, the Rollout is `Healthy` with
   `abort: false` and null abort origin, and both its stable and current revision equal `head_sha`.

`verification-receipt-v1.schema.json` is verdict-discriminated and enforces the corresponding
nullability and array cardinality. A policy-fallback run can produce only `inconclusive` with
`no_ai_prediction` after all earlier integrity/observability checks pass; it cannot claim that an AI
prediction was tested. Receipt collection timing out also produces `inconclusive`, never a guessed
terminal result.

For `inconclusive`, the non-null `inconclusive_reason` is the first failed check in this exact order
and closed enum: `artifact_binding_mismatch`, `catalog_binding_mismatch`,
`rollout_annotation_mismatch`, `generation_mismatch`, `revision_mismatch`,
`runtime_image_mismatch`, `canary_target_mismatch`, `resource_ownership_mismatch`,
`timestamp_order_invalid`, `probe_job_not_started`, `probe_job_incomplete`,
`probe_result_invalid`, `non_analysis_abort_observed`, `observer_timeout`, `no_ai_prediction`,
`prediction_evidence_mixed`, `terminal_state_inconsistent`. The other two verdicts require
`inconclusive_reason: null`.

## Canonical serialization

All JSON decoding rejects duplicate keys. All persisted JSON uses UTF-8 without BOM, LF line endings, declared model field order, two-space
indentation, separators `,` and `: `, RFC 3339 UTC timestamps ending in `Z`, no NaN/Infinity, and one
final newline. Hashes use those canonical bytes. Fixed fake-AI inputs and fixed clocks must produce
byte-identical goldens.

Validated YAML catalogs have two hashes. `trusted_probe_catalog_sha256` hashes the entire validated
catalog model after conversion to the same canonical JSON representation. `catalog_entry_sha256`
hashes the entire validated probe entry—`id`, `binding`, `assertion`, and `execution`—using the same
declared field order and representation. YAML comments, whitespace, and key
order therefore do not affect either hash. Catalog loading rejects duplicate keys, anchors, aliases,
merge keys, unknown fields, and non-string map keys.

## Validation and failure behavior

- Invalid request, policy, repository identity, Git range, or artifact schema: typed command error;
  no assessment or decision.
- Diff over 16,384 UTF-8 bytes, Ollama unavailable/timeout, malformed envelope, or unverifiable
  finding: valid low-confidence Guarded-or-higher assessment; never Fast.
- Valid finding plus invalid safeguard proposal: keep the finding and its Risky floor, omit the
  safeguard, and use only the policy-owned non-Fast fallback probe after human resolution.
- Invalid or stale resolution: no decision.
- Successful replacement assessment: invalidate the prior decision under the workspace lock before
  publishing; any interrupted replacement remains fail-closed with no decision.
- Invalid, absent, stale, identity-mismatched, or unapproved decision/release inputs: reject before
  rendering or kubectl.
- Invalid tier/profile/resolution/analysis matrix: reject in schema and again in the compiler.
- Probe Job never starts: receipt is `inconclusive`; never claim the prediction was observed or the
  product caused an application-level abort.

No uncertainty or accepted danger may lower the tier. No AI output may make a rollout faster.

## Canonical source hierarchy

1. `CONTEXT.md` owns domain language.
2. Accepted ADRs own hard architectural decisions.
3. This contract and executable schemas own wire artifacts, authorization, and handoff behavior.
4. `docs/input-contracts.md` and `docs/risk-signals.md` own supported inputs and policy predicates.
5. `docs/rollout-profiles.md` owns built-in stages and trusted Job behavior.
6. `docs/safelane-studio.md` owns the pre-final review UI.
7. `docs/golden-scenarios.md` owns acceptance fixtures and thresholds.
8. `plan.md` and `detailed-plan.md` own implementation order, gates, cuts, and schedule.
9. Prototypes, research, README, abstract, Q&A, brief, and earlier Git history are explanatory or
   publishing surfaces and never override the sources above.

## Superseded descriptions

The following are not build instructions:

- `safelane-brief.html` and publishing files that describe six additive signals, continuous scores,
  “no LLM,” self-learning, exact traffic percentages, or DORA predictions;
- pre-2026-08-09 plan versions, additive/composite scoring, copied DeployWhisper code, PyDriller in
  the hot path, the old dashboard/backtest, Prometheus, nginx, and the original sequence;
- the Phase-1-wide incident corpus, four-finding runtime, profile editor, and Generate-with-AI
  descriptions superseded by the bounded pre-final contract; and
- any claim that missing authorization may silently continue with a local Strict profile.
