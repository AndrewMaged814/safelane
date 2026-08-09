# Pre-final acceptance scenarios

**Version:** 2 · **decision date:** 2026-08-09

Acceptance proves one bounded AI-to-safeguard path and one real Argo abort. It is not an accuracy
benchmark or production-safety claim.

## Gate A — deterministic conformance

Real temporary Git repositories plus a fake AI adapter must prove:

- Fast and quote-contract assessment/decision goldens are byte-identical with fixed inputs/clocks;
- every scope boundary and Fast precondition in `docs/risk-signals.md`;
- every baseline/floor rule ID, exact reason, trace order, final-tier tie, and primary-reason tie-break;
- wrong file, side, line, text, missing span, reversed route roles, dynamic route, or fabricated
  reference is rejected;
- duplicate JSON keys at any artifact or AI-response level are rejected;
- missing/extra proposal fields, bad finding index, and unsupported hypothesis, intent, question, or
  remediation enum are rejected;
- a valid `breaking_api` finding retains its Risky floor when the proposal is rejected;
- shallow-envelope failure rejects the AI attempt, while component validation preserves a valid
  finding when only its proposal is invalid;
- rejected model content never appears as a trusted safeguard or executable value;
- the verified quote contract resolves only to `demo-api-public-quote-v1` and its exact catalog hash;
- adding uncertainty or danger never lowers tier and no invalid AI path produces Fast;
- medium/high baseline cases with a complete empty AI response remain high-confidence non-Fast,
  approve through Guarded/Strict as allowed, and receive `policy_fallback` analysis;
- assessment `rollout_options` contains exactly `[Fast]`, `[Guarded, Strict]`, or `[Strict]` for its
  tier; approval/decision rejects any altered or unlisted preview;
- stale assessment identity/hash/policy resolution emits no decision;
- approved head A → successful unresolved assessment of head B removes A's decision, after which an
  A release request rejects; a simulated failure between invalidation and publish also leaves no
  decision;
- every invalid tier/profile/resolution/analysis matrix combination—including Risky + Fast,
  non-Fast + automatic, Safe + human, Guarded + `ai_safeguard`, and non-Fast + null analysis—fails
  both schema and compiler;
- the exact assessment-input envelope reproduces for valid and invalid-UTF-8 diffs from raw-byte hash
  and length;
- both owners produce identical canonical whole-catalog and full-entry probe hashes from semantically
  identical YAML with different comments/key order;
- image catalog v1 accepts exactly the three frozen application identities and one probe image and
  rejects wrong OCI revision, tag, build/runtime image ID, duplicate identity/key, or probe-key binding;
- absent, invalid, unapproved, wrong-SHA/service/image/catalog decision inputs reject before manifest
  output.

Receipt schema/golden tests cover all three verdict variants: linked observed failure, all-stage probe
success with all HTTP 200 and promotion, and inconclusive startup failure. Additional inconclusive
goldens cover one tolerated 404, transport-only/mixed failures, a fully observed policy-fallback run
with `no_ai_prediction`, metadata/observed generation mismatch, invalid AnalysisRun-completion versus
abort ordering, wrong runtime image ID, and a canary Service/EndpointSlice not bound to the head
ReplicaSet. Negative cases also include missing/incomplete statuses, malformed probe-result logs,
wrong annotations, catalog/hash/revision mismatch, wrong Rollout/AnalysisRun/Job/Pod owner UID, and an
external/manual abort represented as `abort_origin: external_or_unknown` with reason
`non_analysis_abort_observed`. Each must either validate as the exact expected variant or fail schema
validation; none may be silently coerced to a conclusive verdict.

## Gate B — six one-shot Ollama observations

With the pinned, warmed 7B model, run each case twice without retry or best-of selection:

| Case | Change | Expected normalized result |
|---|---|---|
| `fast-copy` | bounded response-copy change | zero findings; null proposal |
| `additive-route` | add `/v2/quote`, retain `/v1/quote` | zero findings; null proposal |
| `quote-contract-break` | remove `/v1/quote`, add `/v2/quote` | one verified `breaking_api` finding, exact two spans, expected proposal enums, trusted probe binding |

Gate B passes only when all 6 one-shot responses are envelope-valid, normalize to the expected result,
accept zero fabricated references, and finish inside the configured timeout. Record model manifest
digest, prompt/schema hashes, model settings, raw response, normalized result, and latency for every
observation. Report `6/6 fixture observations`; never convert this tiny authored set into an accuracy
percentage.

## Gate C — Studio and authorization

- Fast shows positive proof and automatic resolution without inventing a hypothesis.
- Risky shows both spans, the deterministic safety-case text, validation ledger, trusted probe,
  Strict preview, approval question, remediation, and unresolved state.
- Medium Guarded/high Risky baseline-only views preserve high confidence and show no invented
  uncertainty floor; low-confidence Guarded and Risky-with-rejected-proposal views show their actual
  floors/evidence. All four show the policy-fallback notice and allowed approval action without
  displaying rejected model content as a trusted safeguard.
- Approval compare-and-swaps assessment ID, head SHA, input hash, and result hash.
- A stale page or faster profile is rejected.
- The exact resolved assessment is written before its decision, and only that decision authorizes the
  release path.

## Gate D — real integration twice

Each counted run begins with the defined namespace reset and proves:

1. Warm-up reaches five Ready stable pods.
2. Fast promotes and becomes the stable base; `GET /v1/quote` returns 200.
3. Human approval produces the exact SHA- and trusted-probe-bound Strict decision.
4. The first Strict stage creates one Ready canary pod; `canaryService` selects only the head SHA.
5. The trusted Job records 404 from `GET /v1/quote` and exits nonzero.
6. Job and AnalysisRun become `Failed`; Argo automatically aborts and the Rollout is `Degraded`.
7. The failed ReplicaSet scales down and the stable base serves `/v1/quote` successfully again.
8. Receipt v1 binds the assessment, decision/release annotations, revisions, catalogs, prediction,
   structured probe result, equal generations, actual runtime image IDs, the canary
   Service/EndpointSlice → head ReplicaSet/pod snapshot, and the Rollout → AnalysisRun → Job → Pod
   UID/owner-UID chain with verdict
   `prediction_observed_and_update_aborted`.
9. The Rollout has non-null `abortedAt` and its `Progressing=False` condition has reason
   `RolloutAborted` with the step-analysis failure message; the linked AnalysisRun completed first,
   `abort_origin` is `analysis_failure`, and the release adapter issued no abort.

An image/startup failure, wrong identity, missing status, or catalog mismatch fails the gate and
produces `inconclusive`. The overall pre-final gate is `PASS` only after two clean runs.

## Deferred evaluation

The old 12-case × two-run challenge, incident pairs, Trellis history, other finding kinds, payout
idempotency, and accuracy/confusion-matrix reporting are final-round or post-hackathon work.
