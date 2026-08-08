# Phase 1 golden scenarios and acceptance thresholds

**Decision date:** 2026-08-08

**Decision owner:** Andrew

**Scope:** Gate 2 validation of SafeLane's fixed policy, bounded Ollama findings, incident connections, evidence verification, and fail-safer behavior.

## Decision

Gate 2 has three separate checks:

1. **Deterministic conformance gate** — normal code must implement every policy branch, boundary, invariant, and safe fallback exactly.
2. **Locked Ollama challenge** — twelve authored cases run twice must produce the intended semantic findings, evidence decisions, incident connections, and final risk tiers.
3. **Real-history smoke gate** — the same assessment entry point must process one pinned historical `roots/trellis` diff and preserve its real provenance and limitations.

The deterministic gate requires byte-stable output. The Ollama gate requires **semantic stability**, not byte-identical prose: finding order, reason wording, and latency may vary, but accepted finding kinds, accepted evidence, incident-connection class, confidence, risk tier, and rollout profile may not.

This is a conformance and demo-readiness evaluation. It is not an accuracy estimate, calibration study, backtest, or claim about production incident reduction.

## Shared challenge baseline

Unless a case says otherwise, each synthetic challenge uses:

- no more than two files and 50 changed lines;
- exactly one mapped, non-critical service with no downstream dependents;
- a supported shipping window;
- successfully loaded policy, service map, and incident store;
- no incident candidates;
- the pinned `qwen2.5-coder:7b` setup from `research/ollama-phase1.md`; and
- no danger outside the behavior named by the case.

This baseline makes the change eligible for `safe` when no verified danger or uncertainty is present. It prevents an unrelated normal-code rule from hiding an AI or incident-classification error.

Each checked-in case manifest must include the fields required by `research/risk-engine-evaluation.md`: stable ID, provenance, parent and diff, service facts, incident candidates, expected and forbidden findings, exact supporting spans, expected confidence and tier, rationale, author, review status, and creation date.

The finding names below are semantic names. The contract-freeze decision must assign each one exactly one wire enum; changing the wire spelling must not change these labels or their policy meaning.

## Twelve locked challenge cases

### True hazards

| ID | Changed behavior and required evidence | Expected accepted finding | Expected result |
|---|---|---|---|
| `stored-data-drop` | `db/migrations/042_payouts.sql` adds `ALTER TABLE payouts DROP COLUMN bank_reference;` | Stored-data danger, citing that exact added line | `high` confidence, `risky`, Strict profile |
| `access-check-weakened` | `src/auth/authorize.py` replaces an administrator-only result with `return user.is_authenticated` | Access-control danger, citing that exact added line and the removed administrator check as context | `high` confidence, `risky`, Strict profile |
| `required-api-field` | `src/api/models.py` replaces `currency: str | None = None` with `currency: str` in an existing request model | Breaking-API danger, citing the new required field and using the removed optional declaration as context | `high` confidence, `risky`, Strict profile |
| `unbounded-retry` | `src/workers/payouts.py` replaces a bounded attempt loop with `while True:` around the existing retry body | Retry/backoff danger with the unbounded-retry escalation, citing `while True:` | `high` confidence, `risky`, Strict profile |

### Contrast cases

| ID | Changed behavior and required evidence | Expected accepted or rejected finding | Expected result |
|---|---|---|---|
| `read-only-query` | `src/reports/payouts.sql` adds `SELECT id, status FROM payouts ORDER BY created_at DESC;` | No stored-data danger; reading and ordering data does not alter persisted state | `high` confidence, `safe`, Fast profile |
| `marked-test-secret` | `tests/fixtures/auth.py` adds `TEST_API_TOKEN = "fixture-token-not-a-secret"` in a repository-recognized test fixture | No access/secret danger; the fixture is clearly synthetic and cannot be loaded by production code | `high` confidence, `safe`, Fast profile |
| `new-optional-field` | `src/api/models.py` adds `note: str | None = None` to an existing request model | No breaking-API danger; an optional field is additive | `high` confidence, `safe`, Fast profile |
| `bounded-retry-change` | `src/workers/payouts.py` changes `MAX_RETRIES = 3` to `MAX_RETRIES = 4` | Accept the retry/backoff finding, but forbid the unbounded/repeated-trigger escalation | `high` confidence, `guarded`, Guarded profile |

`bounded-retry-change` is a **tier contrast**, not a no-finding case. The policy already says any retry-count change is at least guarded. Calling it a negative finding would contradict `docs/risk-signals.md`.

### Incident pairs

The incident cases use a small harmless diff under the shared baseline. Normal code supplies only the named incident candidate to Ollama and verifies exact quotes from both inputs.

| ID | Change and incident candidate | Expected connection | Expected result |
|---|---|---|---|
| `incident-same-component` | `BATCH_FLUSH_SECONDS = 5` in `settlement-batcher`; candidate says, “Large settlement batches exhausted worker memory,” in the same component | Meaningful same-component/behavior-family connection, but not the same trigger or root cause | `high` confidence, `guarded`, Guarded profile |
| `incident-repeated-trigger` | `BATCH_SIZE = 200` in `settlement-batcher`; candidate trigger says, “Increasing payout batches to 200 exhausted worker memory during settlement.” | Repeated trigger, citing the exact changed value and incident quote | `high` confidence, `risky`, Strict profile |
| `incident-same-service-distractor` | `EMAIL_SUBJECT = "Your payout is ready"`; same-service candidate concerns settlement-batch memory exhaustion | No connection; sharing a service is candidate selection, not evidence | `high` confidence, `safe`, Fast profile |
| `incident-shared-words-distractor` | Frontend text adds `retry_badge_text = "Retry payment"`; candidate concerns payout workers exhausting the connection pool through endless retries | No connection; shared words do not establish the same component or behavior | `high` confidence, `safe`, Fast profile |

The final locked fixtures must contain fuller surrounding code and incident records, but they may not change the behavior, expected evidence, or classifications above. Development examples must use different text and components.

## Acceptance thresholds

### Gate A — deterministic conformance

Gate A passes only when all of the following are true:

- every decision-table row and numeric boundary in `research/risk-engine-evaluation.md` passes;
- every monotonicity, positive-proof, determinism, and minimum-profile property passes;
- every injected failure reaches its documented safe state;
- every expected `decision.json` is schema-valid and byte-stable; and
- there are **zero failures**.

No percentage or flaky retry allowance applies to deterministic code.

### Gate B — locked Ollama challenge

Run all twelve cases twice with a warmed model, producing 24 observations. Gate B passes only with:

- `24/24` schema-valid responses;
- `24/24` expected confidence values, final risk tiers, and minimum rollout profiles;
- every expected finding present in both runs of its case;
- every finding backed by an exact accepted source reference;
- zero forbidden findings or dangerous escalations in the contrast cases;
- zero under-triage and zero false-fast results;
- zero fabricated code or incident references accepted;
- zero false incident connections; and
- both genuine incident connections assigned the correct connection class.

Over-triage is safer than under-triage in production, but it does not pass this tiny authored challenge: a system that marks every case risky has not demonstrated useful discrimination. An unexpected extra finding or higher tier is therefore a failed case and must be reported as over-triage.

Exact reason prose, finding order, and latency are observations, not pass/fail criteria, provided every reason remains evidence-backed and readable. Record all timings and both raw runs.

If the challenge causes a prompt, schema, parser, model, or policy change, the failed case moves into the development set. Replace it with a new unseen case covering the same boundary, repin every evaluated artifact, and perform one fresh reported run. Do not repeatedly tune against the locked set.

### Gate C — real `roots/trellis` history

Use commit [`5e884c1a9508173935096dc7e2fa6a7aab16743d`](https://github.com/roots/trellis/commit/5e884c1a9508173935096dc7e2fa6a7aab16743d) with parent `5890fc20b45821e20378a66c2a522a9ad35acf43`.

Gate C passes only when SafeLane:

- ingests the authentic two-file, three-line diff through the normal assessment entry point;
- preserves the upstream repository, commit, parent, and license provenance;
- identifies the permission-changing evidence `mode: 0775` and the added templated `mode` application;
- accepts no fabricated evidence;
- selects the permission/access danger and its `risky` safety floor under the declared demo service map; and
- emits a schema-valid Strict-profile decision without inventing an incident connection.

The result may be described only as an authentic historical diff linked by a published IaC defect dataset to a later fix. It must not be called a production incident, prediction, accuracy benchmark, or proof of causality.

Do not select the documentation-only Trellis comparator until the prompt is frozen. It is a human-reviewed narrative comparator, not part of the pass/fail gate, because the source dataset does not label unlisted commits as clean.

## Grading report

The final report must show:

- Gate A rows/properties/failures passed and failed as raw counts;
- the full expected-tier × actual-tier table for all 24 Gate B observations;
- under-triage, false-fast, and over-triage counts;
- findings found, missed, wrong-kind, and forbidden;
- invalid references emitted, rejected, and accepted;
- true and false incident connections;
- fallback results;
- per-run timing and pin information; and
- the Gate C result with the approved limitation wording.

The only overall outcomes are `PASS` and `FAIL`. A failure may still be demonstrated honestly, but SafeLane must not call Gate 2 complete until every hard threshold above passes.
