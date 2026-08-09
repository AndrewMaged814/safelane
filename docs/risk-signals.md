# Pre-final risk policy

**Version:** 2 · **decision date:** 2026-08-09

SafeLane uses deterministic scope facts plus one bounded AI finding. Normal code chooses the final
tier. A rule may keep or raise rollout care; no reassuring signal may cancel danger or uncertainty.

## Supported assessment

The pre-final assesses exactly one directly changed, known `demo-api` service. Zero or more than one
directly changed deployable service is unsupported and produces no decision. Policy validation has
already fixed the service as non-critical with no downstream dependents; there are no dormant
criticality, topology, incident, or shipping-window branches.

## Change-scope band

- **Low / Safe baseline:** at most 2 files and at most 50 changed lines, with every path recognized.
- **High / Risky baseline:** at least 10 files or at least 500 changed lines.
- **Medium / Guarded baseline:** every other supported one-service change.
- An unknown or incompletely decoded path is at least Guarded with low evidence confidence.

These labels are policy categories, not failure probabilities or numeric scores.

## Stable rule IDs and trace ordering

Exactly one baseline is emitted:

| Rule ID | Predicate | Tier | Exact reason |
|---|---|---|---|
| `scope.low` | at most 2 files and 50 lines; all paths recognized | Safe | `The change affects at most 2 recognized files and 50 changed lines.` |
| `scope.medium` | supported one-service change between the low and high bounds | Guarded | `The change is outside the bounded Fast scope.` |
| `scope.high` | at least 10 files or at least 500 lines | Risky | `The change affects at least 10 files or 500 changed lines.` |

Every applicable safety floor is emitted in this fixed order:

| Order | Rule ID | Predicate | Minimum tier | Exact reason |
|---:|---|---|---|---|
| 1 | `finding.breaking_api` | accepted verified breaking finding | Risky | `An existing HTTP contract was removed or renamed.` |
| 2 | `evidence.path_unrecognized` | any path is unknown or incompletely decoded | Guarded | `At least one changed path is unrecognized or incompletely decoded.` |
| 3 | `evidence.diff_invalid_utf8` | canonical diff is not valid UTF-8 | Guarded | `The complete Git diff could not be decoded as UTF-8.` |
| 4 | `evidence.diff_over_budget` | canonical diff exceeds 16,384 UTF-8 bytes | Guarded | `The complete Git diff exceeds the AI evidence budget.` |
| 5 | `evidence.ai_incomplete` | an attempted AI call is unavailable, times out, has a malformed envelope, returns an unsupported/unverifiable finding, or supplies a proposal without an accepted finding | Guarded | `AI evidence was unavailable, invalid, unsupported, or unverifiable.` |
| 6 | `evidence.safeguard_invalid` | an otherwise accepted finding has an absent, invalid, or unresolvable proposal | Guarded | `The AI safeguard proposal was invalid or could not resolve to a trusted probe.` |

`policy_trace.baseline` contains the baseline rule ID, tier, and exact reason. Its
`safety_floors` contains every applicable row above in table order; the engine never records only the
winning rule. `final_tier` is the maximum of the baseline and all floors. `primary_reason` is the
first safety-floor reason in the table whose tier equals `final_tier`; when no such floor exists it is
the baseline reason. This fixes output order and tie-breaking for byte-identical goldens.

Evidence confidence is `high` only when paths and the complete diff are valid, one schema-valid AI
envelope completes, every accepted finding is source-verifiable, and any present proposal is valid.
Any `evidence.*` floor makes it `low`; a verified finding itself does not.

## AI evidence and uncertainty

- The canonical diff is sent to the model once only when it is valid UTF-8 and at most 16,384 bytes.
- Over-budget input, timeout, unavailable model, malformed envelope, unsupported finding, or
  unverifiable finding span applies the corresponding Guarded evidence floor with low confidence.
- Normal-code floors remain effective when AI fails. Uncertainty can never create Fast.
- A structurally valid finding and safeguard proposal are verified separately. Rejecting the proposal
  cannot erase a verified finding.

## Breaking API finding

`breaking_api` applies when verified changed lines remove or rename an existing request, response,
field, endpoint, permission, or meaning that callers may rely on. Adding a new endpoint while
retaining the old one is not breaking by itself.

The executable pre-final case is narrower: exactly one removed static `GET /v1/quote` decorator and
one added static `GET /v2/quote` decorator. A verified finding sets the Risky floor and minimum Strict
profile.

The AI safeguard proposal does not affect the tier. When its source relationships are valid, normal
code may bind its `preserve_removed_http_route` intent to the trusted compatibility probe. The
hypothesis, approval question, and remediation are review-only deterministic projections. They never
authorize, deploy, or lower care.

## Fast-lane eligibility

Fast requires all of the following positive proof:

- the validated single release service is changed;
- no more than 2 files and 50 lines changed;
- every path is recognized and the complete diff is decodable and within budget;
- one schema-valid AI response returned with zero accepted findings and a null safeguard proposal;
- every deterministic check completed without uncertainty; and
- the Safe baseline survived every floor.

An empty or missing AI warning alone is insufficient.

## Final tier and resolution

- Safe maps to Fast and resolves automatically.
- Guarded maps to Guarded and requires explicit human resolution.
- Risky maps to Strict and requires explicit human resolution.
- A human may select a more careful built-in profile but never a faster one. In the bounded policy,
  the Risky fixture has no more-careful alternative than Strict.
- Every resolved non-Fast assessment with no accepted `selected_safeguard` uses the policy-owned
  fallback analysis, including high-confidence medium/high scope baselines with complete empty AI
  evidence. Fallback does not manufacture an AI prediction.

## Explicitly excluded

- stored-data, access-control, retry/backoff, incident-connection, and catch-all AI findings;
- service criticality/downstream branches, shipping support, and decision-reuse clocks;
- continuous scores, trained probabilities, author scoring, or self-learning;
- tests, vulnerabilities, or DORA measurements as risk signals; and
- AI-generated tier, rollout profile, probe, test, command, or remediation execution.
