# SafeLane — Phase 1 assessment and decision contract

**Contract document:** v4, frozen 2026-08-08

**Wire schemas:** `assessment.json` v1 and `decision.json` v2

This document is the canonical Phase 1 artifact and lifecycle contract. It separates the evidence SafeLane Studio reviews from the small approved handoff Ahmed's rollout work consumes.

## Artifact boundary

SafeLane produces two different artifacts for one exact pull-request head SHA:

- **`assessment.json`** is Andrew-owned. It contains change evidence, bounded AI risk findings, incident connections, failure propensity, safety floors, explanations, confidence, the minimum rollout profile, and review state. SafeLane Studio reads it.
- **`decision.json`** is the approved projection of that assessment. It contains the exact change identity, final risk result, readable explanation, approval mode, and fully resolved rollout profile. Ahmed's release script reads it and nothing else.

The artifacts are deliberately separate. Deployment must not depend on Ollama response details, incident-corpus shape, Studio state, or future explanation fields. Studio still needs those details to make a guarded or risky assessment reviewable.

The architectural reason is recorded in [`docs/adr/0002-separate-assessment-from-rollout-decision.md`](docs/adr/0002-separate-assessment-from-rollout-decision.md).

## Lifecycle

1. SafeLane assesses one repository, pull request, and full head SHA under one policy version.
2. It writes `assessment.json` for that exact SHA.
3. A `safe` assessment resolves automatically with the policy-selected minimum profile and emits `decision.json`.
4. A `guarded` or `risky` assessment remains `needs_review`. A person may approve the suggested profile or choose a more careful valid profile. Only then does SafeLane emit `decision.json`.
5. Approval records a rollout decision; it does not deploy anything.
6. A new head SHA replaces the current assessment, invalidates the earlier approval, and makes the earlier decision stale.
7. Before rendering a Rollout, the consumer validates `decision.json` and confirms that its repository and full SHA match the release being attempted.

An absent or invalid decision is never approval to go fast. The release consumer uses its local fail-closed Strict fallback if it is invoked without a usable decision. That fallback is a safety behavior, not a substitute for the upstream human-approval gate.

## `assessment.json` v1

The assessment is the complete review record. This representative shape is normative; explanatory text is illustrative.

```json
{
  "schema_version": "1",
  "assessment_id": "AndrewMaged814/safelane-demo#42@a1b2c3d4e5f6789012345678901234567890abcd:2026.08.2",
  "policy_version": "2026.08.2",
  "change": {
    "repository": "AndrewMaged814/safelane-demo",
    "pr": 42,
    "base_sha": "0123456789abcdef0123456789abcdef01234567",
    "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
    "title": "Remove payout retry limit",
    "files_changed": 1,
    "lines_changed": 2,
    "services": ["payouts-api"],
    "shipping_at": "2026-08-20T14:00:00+03:00",
    "pushed_at": "2026-08-20T10:15:00Z"
  },
  "evidence_status": {
    "diff": "complete",
    "service_map": "complete",
    "incident_history": "checked_candidates",
    "ai_analysis": "complete"
  },
  "ai_analysis": {
    "model": "qwen2.5-coder:7b",
    "model_digest": "dae161e27b0e",
    "prompt_sha256": "<64 lowercase hexadecimal characters>",
    "response_schema_sha256": "<64 lowercase hexadecimal characters>",
    "chunk_count": 1
  },
  "failure_propensity": {
    "band": "low",
    "projection": 20
  },
  "ai_findings": [
    {
      "id": "finding-001",
      "kind": "retry_backoff",
      "category": "availability",
      "title": "Retry limit was removed",
      "explanation": "Workers can retry without a bound during an upstream failure.",
      "evidence": {
        "file": "src/workers/payouts.py",
        "line": 18,
        "added_line": "while True:"
      },
      "evidence_verified": true
    }
  ],
  "incident_history": {
    "candidate_ids": ["INC-003"],
    "connections": [
      {
        "incident_id": "INC-003",
        "classification": "repeated_trigger",
        "category": "availability",
        "title": "Unlimited retries repeated an earlier trigger",
        "explanation": "The change repeats the retry behavior recorded as the earlier incident trigger.",
        "change_evidence": {
          "file": "src/workers/payouts.py",
          "line": 18,
          "added_line": "while True:"
        },
        "incident_quote": "Workers retried indefinitely and exhausted the connection pool.",
        "evidence_verified": true
      }
    ]
  },
  "risk": {
    "confidence": "high",
    "tier": "risky",
    "main_risk": {
      "category": "availability",
      "title": "Retry limit was removed",
      "explanation": "Workers can retry without a bound and repeat an earlier connection-pool failure.",
      "source": "ai_finding",
      "source_ref": "finding-001",
      "evidence_verified": true
    },
    "reasons": [
      "AI finding: the payout worker now retries without a bound.",
      "Incident connection: INC-003 records the same unlimited-retry trigger."
    ],
    "minimum_profile": "strict"
  },
  "review": {
    "status": "needs_review",
    "resolution": null
  }
}
```

### Assessment identity

- `assessment_id` is deterministic: `<repository>#<pr>@<full-head-sha>:<policy-version>`.
- `base_sha` and `head_sha` are full 40-character lowercase Git SHAs. Short SHAs are display-only and never enter either artifact.
- `repository`, pull-request number, head SHA, and policy version identify the assessment. A different value in any of them means a different assessment.
- Generated wall-clock time is not part of assessment identity. `pushed_at` and `shipping_at` are canonical inputs, so identical canonical inputs and mocked AI output can still produce byte-stable artifacts.

Every field shown in the assessment shape is required. Arrays may be empty where the evidence status permits it. Counts are non-negative integers, `services` contains unique service names in lexical order, and every modeled object rejects unknown fields. `ai_findings` and incident candidates are each capped at five items.

### Evidence status and confidence

Allowed evidence-status values are:

| Field | Values |
|---|---|
| `diff` | `complete`, `chunked`, `over_budget`, `unavailable` |
| `service_map` | `complete`, `incomplete`, `unavailable` |
| `incident_history` | `checked_none`, `checked_candidates`, `unavailable` |
| `ai_analysis` | `complete`, `fallback_model`, `partial`, `invalid`, `timeout`, `unavailable` |

Assessment confidence is `high` only when the diff and service map are complete, incident history was checked, the primary-model analysis is complete, every changed path is recognized and mapped as required, and every accepted AI or incident reference was verified. Every other combination is `low` and applies at least the Guarded safety floor. A deliberately disabled optional source is represented in the versioned policy, not disguised as an unavailable source.

The 3B fallback always produces `fallback_model` and `low` confidence. Its verified findings may make the result more careful, but it can never permit `safe`.

### Failure propensity and the legacy projection

`failure_propensity.band` is exactly `low`, `medium`, or `high`.

`failure_propensity.projection` is a compatibility projection, not a probability or continuous score:

| Band | Projection |
|---|---:|
| `low` | `20` |
| `medium` | `50` |
| `high` | `80` |

No other projection value is valid. The final risk tier may be more careful than the propensity band because safety floors are applied afterward.

The normative band decision table lives in `docs/risk-signals.md`. Incident candidates, downstream impact, criticality, supported shipping windows, missing evidence, and AI findings do not inflate this propensity band; they apply independent safety floors or fast-lane eligibility rules.

### AI risk finding wire kinds

The four Phase 1 semantic findings have exactly these wire values:

| Wire value | Meaning | Minimum policy effect |
|---|---|---|
| `stored_data` | Database, schema, persisted-data, or encoding danger | `risky` |
| `access_control` | Login, permission, token, session, or real-secret danger | `risky` |
| `breaking_api` | Breaking request, response, field, endpoint, or permission contract | `risky` |
| `retry_backoff` | Retry-count, timeout, delay, or backoff behavior changed | `guarded`; escalates under the rules in `docs/risk-signals.md` |

The wire kind does not contain severity. Normal code maps verified facts to safety floors. AI never returns a risk tier, score, lane, profile, or approval.

`ai_findings` contains only findings whose file and exact added line were verified. A finding with unsupported evidence is rejected, sets `ai_analysis` to at least `partial`, and makes assessment confidence low. Other independently verified dangerous findings remain; one invalid item must not erase real danger found elsewhere.

Verified findings are sorted by wire kind, file, line, and added line, then assigned stable IDs `finding-001` through `finding-005`. Incident connections are sorted by incident ID and classification. This ordering makes source references and mocked-output artifacts deterministic.

### Incident connections

`incident_history.candidate_ids` records the bounded candidates normal code supplied. A candidate does not change risk by itself.

Only verified connections appear in `connections`. Their wire classifications are:

- `meaningful` — the change and incident share a verified component or behavior; minimum `guarded`;
- `repeated_trigger` — the change repeats the incident trigger or root cause; `risky`.

A shared service, shared words, or vague similarity is not a connection and is not emitted. Each connection must verify exact evidence from both the change and incident record.

### Main risk and reasons

`main_risk` is one display scenario, not the complete evidence store. Its source is `ai_finding`, `incident_connection`, or `rule`; `source_ref` must resolve inside the assessment or to a named policy rule.

Main risk selection is deterministic:

1. prefer a candidate that applies the strongest policy effect (`risky`, then `guarded`, then no floor);
2. break ties with the versioned `main_risk_priority` list in `policy.yaml`;
3. break any remaining tie by lexical `source_ref`.

The Phase 1 default priority is `stored_data`, `access_control`, `breaking_api`, `retry_backoff`, `incident_connection`, `impact_rule`, `propensity_rule`, `operations_rule`, then `confidence_rule`.

Normal code verifies all source references before display. Ollama may draft titles and explanations, but unsupported text is rejected. If no AI or incident scenario survives verification, SafeLane derives Main risk from the strongest rule reason.

`main_risk` is required for `guarded` and `risky`. It is `null` for `safe`, because fast-lane eligibility is positive proof and not a failure scenario. Studio displays the positive-proof reasons instead of inventing “no risk found.”

`reasons` contains one to four plain-English strings derived from the exact predicates that produced the result. They are ordered by policy effect, then the same policy priority, and must not invent evidence.

### Review state

`review.status` is exactly `needs_review` or `resolved`.

- `safe` resolves automatically. `resolution.mode` is `automatic`.
- `guarded` and `risky` require a human action. `resolution.mode` is `human`.
- A human may select the minimum profile or a more careful valid profile, never a faster one.
- An unresolved assessment has `resolution: null` and cannot produce a normal `decision.json`.
- Resolution stores the selected profile name, profile source, whether it is an override, and the approval timestamp. Phase 1 stores no approver identity because accounts and roles are out of scope.

A resolved assessment uses this exact object:

```json
{
  "mode": "human",
  "profile_name": "strict",
  "profile_source": "built_in",
  "profile_override": false,
  "resolved_at": "2026-08-20T10:20:00Z"
}
```

All five fields are required. `mode` is `automatic` or `human`; `profile_source` is `built_in`, `custom`, or `ai_assisted`; and `resolved_at` is an ISO 8601 timestamp. For deterministic tests, the approval event and its timestamp are canonical inputs.

## Bounded Ollama response contract

Ollama returns candidate evidence, not an assessment. The response contains only:

```json
{
  "findings": [
    {
      "kind": "retry_backoff",
      "category": "availability",
      "title": "Retry limit was removed",
      "explanation": "Workers can now retry without a bound.",
      "file": "src/workers/payouts.py",
      "added_line": "while True:"
    }
  ],
  "incident_connections": [
    {
      "incident_id": "INC-003",
      "classification": "repeated_trigger",
      "category": "availability",
      "title": "Earlier retry trigger was repeated",
      "explanation": "Both records describe unbounded worker retries.",
      "file": "src/workers/payouts.py",
      "added_line": "while True:",
      "incident_quote": "Workers retried indefinitely and exhausted the connection pool."
    }
  ]
}
```

The response schema rejects unknown fields, caps each array at five items, and allows empty arrays. It contains no confidence, severity, risk tier, score, lane, profile, or approval. SafeLane computes confidence from evidence completeness and adapter status; it does not trust a model's self-reported confidence.

A structurally invalid response is discarded. An individually unsupported item is rejected while other verified items remain. Either condition lowers assessment confidence and preserves every risk floor established by normal code or other verified findings.

This response contract supersedes the illustrative `confidence` field in `research/ollama-phase1.md`; the model and context-size decision in that research remains valid.

## `decision.json` v2

`decision.json` is the stable handoff. It intentionally excludes the full AI response, incident candidates, rejected evidence, and Studio review history.

```json
{
  "schema_version": "2",
  "policy_version": "2026.08.2",
  "assessment_id": "AndrewMaged814/safelane-demo#42@a1b2c3d4e5f6789012345678901234567890abcd:2026.08.2",
  "change": {
    "repository": "AndrewMaged814/safelane-demo",
    "sha": "a1b2c3d4e5f6789012345678901234567890abcd",
    "pr": 42,
    "services": ["payouts-api"],
    "shipping_at": "2026-08-20T14:00:00+03:00"
  },
  "risk": {
    "failure_propensity": "low",
    "score": 20,
    "tier": "risky",
    "confidence": "high",
    "main_risk": {
      "category": "availability",
      "title": "Retry limit was removed",
      "explanation": "Workers can retry without a bound and repeat an earlier connection-pool failure.",
      "source": "ai_finding",
      "source_ref": "finding-001",
      "evidence_verified": true
    },
    "reasons": [
      "AI finding: the payout worker now retries without a bound.",
      "Incident connection: INC-003 records the same unlimited-retry trigger."
    ]
  },
  "approval": {
    "mode": "human",
    "profile_override": false,
    "resolved_at": "2026-08-20T10:20:00Z"
  },
  "lane": {
    "name": "strict",
    "profile_source": "built_in",
    "traffic_router": "none",
    "replicas": 5,
    "steps": [
      { "set_weight": 20, "exposure_pods": 1, "checkpoint_seconds": 30 },
      { "set_weight": 40, "exposure_pods": 2, "checkpoint_seconds": 30 },
      { "set_weight": 60, "exposure_pods": 3, "checkpoint_seconds": 30 },
      { "set_weight": 100, "exposure_pods": 5, "checkpoint_seconds": 0 }
    ],
    "analysis": {
      "error_rate_threshold": 0.05,
      "interval_seconds": 10,
      "measurement_count": 3,
      "failure_limit": 1,
      "consecutive_error_limit": 2
    }
  }
}
```

### Decision rules

- Every field shown in the decision shape is required. `lane.analysis` is present but may be `null`; `risk.main_risk` is present but may be `null` only for `safe`.
- `assessment_id`, repository, SHA, policy version, risk result, explanation, and selected profile must equal the resolved assessment.
- `risk.failure_propensity` is `low`, `medium`, or `high`.
- `risk.score` is exactly `20`, `50`, or `80` and must match the propensity band. It remains only for schema-v2 compatibility and display; nothing branches on it.
- `risk.tier` is exactly `safe`, `guarded`, or `risky`.
- `risk.confidence` is exactly `high` or `low` and retains the assessment meaning: evidence completeness, never model probability.
- `risk.reasons` contains one to four strings. Each string is non-empty, evidence-backed, and inherited byte-for-byte from the resolved assessment.
- `approval.mode` is `automatic` only for an automatically resolved `safe` assessment; otherwise it is `human`.
- `profile_override` is true only when the selected profile is more careful than the policy-selected minimum.
- `profile_source` is `built_in`, `custom`, or `ai_assisted`. AI-assisted means a human approved a normal-code-validated draft.
- `traffic_router` allows `none` or `nginx`, but the Phase 1 demo value is locked to `none`. No nginx work is required for Gate 2.
- The final step always exposes all replicas, has weight `100`, and has `checkpoint_seconds: 0`.
- With `traffic_router: none`, `exposure_pods` is authoritative and `set_weight` must be the honest weight derived from the configured replica count.
- `analysis` is `null` for a Fast profile and required for every profile containing a health checkpoint.
- The selected profile must be at least as careful as the minimum profile required by the risk tier.

The exact built-in profile rules and custom-profile validation remain in [`docs/rollout-profiles.md`](docs/rollout-profiles.md).

## Validation and failure behavior

Both artifact schemas and the bounded Ollama response schema use `additionalProperties: false` at every modeled object. Unknown fields, missing required fields, wrong types, invalid enums, non-finite numbers, unsupported schema versions, or cross-field inconsistencies are hard validation errors. This supersedes the earlier rule that consumers ignore unknown fields.

Before rendering a Rollout, the consumer must verify:

1. `decision.json` passes schema and cross-field validation;
2. repository and full SHA match the requested release;
3. the selected profile is not faster than the risk tier permits;
4. pod stages, weights, checkpoints, and analysis settings obey `docs/rollout-profiles.md`; and
5. the complete Rollout manifest passes `kubectl argo rollouts lint`.

If any check fails, the consumer must not use any lane values from the invalid artifact. It synthesizes the locally bundled Strict fallback for the known demo service, marks the result `risky` with `low` confidence and a contract-error reason, and continues only through the release workflow's existing authorization boundary.

Missing Prometheus data is unhealthy. Provider or query errors use the configured consecutive-error limit. No contract error, missing evidence, or AI failure may produce a faster rollout.

## Handoff mechanics

- Andrew's side owns assessment, review, policy validation, and decision emission.
- Ahmed's side consumes only schema-valid `decision.json` or its own fixed Strict fallback.
- The two workstreams agree on the v2 decision schema before integration.
- Ahmed may hand-write schema-valid decision fixtures while Andrew's side is absent.
- Andrew validates against hand-written decisions while Ahmed's cluster is absent.
- The release script renders a complete Rollout, runs `kubectl argo rollouts lint`, then performs one `kubectl apply -f`.
- Never patch Rollout steps and never mix apply with `kubectl argo rollouts set image`.
- Burn one warm-up revision off-camera because Argo skips canary steps on a service's first deployment.

## Canonical source hierarchy

Use each artifact only for the concern it owns:

1. `CONTEXT.md` owns SafeLane's domain vocabulary.
2. Accepted ADRs own hard architectural boundaries.
3. `contract.md` owns artifact boundaries, lifecycle, field semantics, wire values, validation, and handoff behavior.
4. `docs/input-contracts.md` owns caller-supplied request, policy, incident, profile-draft, and evaluation-fixture shapes.
5. `docs/risk-signals.md` owns policy predicates and safety-floor effects.
6. `docs/rollout-profiles.md` owns profile behavior and profile-validation rules.
7. `docs/safelane-studio.md` owns the review and profile-management interaction.
8. `docs/golden-scenarios.md` owns acceptance and reporting rules.
9. Research files explain rationale and evidence but are non-normative when a later accepted decision differs.
10. README, brief, Q&A, schedule, and pitch files are narrative surfaces and never override the sources above.

When implemented, JSON Schema files are executable mirrors of this contract's wire shape. A discrepancy between a schema and this contract is a build-blocking defect to reconcile explicitly; neither side may silently choose one.

## Superseded descriptions

The following existing descriptions are not build instructions:

- `safelane-brief.html`: six deterministic signals, generic config-versus-code scoring, continuous-score language, and “no LLM” claims;
- `detailed-plan.md`: additive/composite scoring, copied DeployWhisper code, PyDriller in the hot path, the old dashboard, and the original day-by-day implementation sequence;
- `plan.md`: reversibility and timing described as additive scoring signals rather than safety floors, plus past-due traffic-router indecision;
- `safelane-qa.md`: config-versus-code scoring and any claim that the model can be turned off with identical assessment behavior;
- `research/risk-engine-options.md`: the earlier 90-day incident propensity match where it conflicts with the later verified-connection policy;
- `research/ollama-phase1.md`: the illustrative model-returned `confidence` field, superseded by normal-code evidence completeness in this contract.

Those files may be rewritten later as publishing and execution work. Implementers must follow the canonical sources above now; no implementation decision remains hidden in the stale narratives. The current `README.md` is a publishing surface aligned to this hierarchy, but remains non-normative.
