# Phase 1 input contracts

**Contract version:** 1
**Decision date:** 2026-08-08
**Decision owner:** Andrew

This document freezes every caller-supplied input needed to build and evaluate the Phase 1 risk engine. `contract.md` owns the emitted artifacts; this document owns the request, policy, incident, and challenge-fixture shapes that produce them.

All JSON objects and YAML mappings reject unknown fields. All paths use forward slashes, are relative to their declared root, and must remain inside that root after normalization. UTF-8 without a byte-order mark is the only supported text encoding.

## Assessment invocation

The CLI receives a repository worktree path and one `assessment-request.json` path. The worktree is trusted only as a source of Git objects; caller-supplied counts, changed paths, or patch text are never accepted.

```json
{
  "schema_version": "1",
  "repository": "AndrewMaged814/safelane-demo",
  "pr": 42,
  "base_sha": "0123456789abcdef0123456789abcdef01234567",
  "head_sha": "a1b2c3d4e5f6789012345678901234567890abcd",
  "title": "Remove payout retry limit",
  "shipping_at": "2026-08-20T14:00:00+03:00",
  "pushed_at": "2026-08-20T10:15:00Z"
}
```

Every field is required. `schema_version` is exactly `"1"`; `repository` is `<owner>/<name>`; `pr` is a positive integer; both SHAs are full 40-character lowercase hexadecimal object IDs; `title` is non-empty; and both timestamps are RFC 3339 date-times with an explicit offset.

Before assessment, SafeLane verifies that both commits exist, `base_sha` is an ancestor of `head_sha`, and the worktree's canonical `origin` repository matches `repository`. It reads the change with Git's rename-aware diff for `base_sha..head_sha`. `files_changed` is the number of diff entries; `lines_changed` is added lines plus deleted lines from Git numstat. A rename is one entry and maps by its destination path, while a deletion maps by its source path. Paths, added-line numbers, and evidence text are derived from the unified diff. A missing object, repository mismatch, or non-ancestor range is an invocation error and emits no assessment. A binary or undecodable changed file remains in the file count but makes the diff evidence incomplete and confidence low.

## `policy.yaml`

One policy file owns thresholds, shipping support, service topology, risk-to-profile mappings, Main risk priority, and rollout profiles:

```yaml
schema_version: "1"
policy_version: 2026.08.2
timezone: Africa/Cairo
release_service: payouts-api

limits:
  small_max_files: 2
  small_max_lines: 50
  large_min_files: 10
  large_min_lines: 500
  incident_lookback_days: 180
  incident_candidate_limit: 5
  ai_max_chunks: 3

supported_shipping_windows:
  - days: [sun, mon, tue, wed, thu]
    start: "09:00"
    end: "18:00"

services:
  payouts-api:
    path_globs: ["src/payouts/**", "db/payouts/**"]
    critical: true
    downstream: [ledger-api]
    replicas: 5
    max_error_rate: 0.05
  ledger-api:
    path_globs: ["src/ledger/**"]
    critical: false
    downstream: []
    replicas: 5
    max_error_rate: 0.05

tier_profiles:
  safe: fast
  guarded: guarded
  risky: strict

main_risk_priority:
  - stored_data
  - access_control
  - breaking_api
  - retry_backoff
  - incident_connection
  - impact_rule
  - propensity_rule
  - operations_rule
  - confidence_rule

profiles:
  fast:
    base: fast
    source: built_in
    description: Expose all pods immediately.
    steps: [all]
    checkpoint: null
  guarded:
    base: guarded
    source: built_in
    description: Check health after exposing two pods.
    steps: [2, all]
    checkpoint:
      seconds: 30
      interval_seconds: 10
      measurement_count: 3
      max_error_rate: 0.05
      failure_limit: 1
      consecutive_error_limit: 2
  strict:
    base: strict
    source: built_in
    description: Increase exposure one stage at a time.
    steps: [1, 2, 3, all]
    checkpoint:
      seconds: 30
      interval_seconds: 10
      measurement_count: 3
      max_error_rate: 0.05
      failure_limit: 1
      consecutive_error_limit: 2
```

### Policy validation

- `schema_version` is exactly `"1"`. `policy_version` is a non-empty immutable identifier and changes on every approved policy edit. `timezone` is an IANA time-zone name. `release_service` resolves to exactly one configured service; its replicas and health limit are used to resolve the rollout lane.
- Limit values are positive integers. Small thresholds must be below their corresponding large thresholds. `incident_candidate_limit` is at most 5 and `ai_max_chunks` is at most 3 in Phase 1.
- Window days use `sun`, `mon`, `tue`, `wed`, `thu`, `fri`, or `sat`; times are zero-padded 24-hour `HH:MM`; `start` is before `end`. Cross-midnight and overlapping windows are invalid in Phase 1. A shipping time is supported when its instant, converted to `timezone`, falls inside a matching half-open interval `[start, end)`.
- Service names and path globs are unique. Every downstream name resolves to another service and cannot reference itself. Graph cycles are valid; traversal uses a visited set. Every changed path must match exactly one service. Zero or multiple matches make the service map incomplete and confidence low.
- `critical` is Boolean; `replicas` is a positive integer; and `max_error_rate` is greater than 0 and at most 1.
- `tier_profiles` has exactly the three shown risk keys and each value resolves to a profile. The mapped profile must be at least as careful as its tier's built-in minimum.
- `main_risk_priority` contains each supported finding or rule key exactly once.
- Every profile has exactly `base`, `source`, `description`, `steps`, and `checkpoint`. `base` is `fast`, `guarded`, or `strict`; `source` is `built_in`, `custom`, or `ai_assisted`; `description` is non-empty; `steps` ends with `all`; preceding values are positive, strictly increasing integers below the release service's replica count.
- `checkpoint` is `null` exactly when `steps` is `[all]`; otherwise it is required. Duration, interval, and measurement fields are positive integers; `failure_limit` is a non-negative integer; `consecutive_error_limit` is a positive integer; and `max_error_rate` is greater than 0 and at most the release service's value. `measurement_count` is exactly `floor(seconds / interval_seconds)` and must be at least 1.
- The named built-ins have `source: built_in`, the matching `base`, and exactly the definitions shown above. No other profile may use `source: built_in`.
- A custom or AI-assisted profile is at least as careful as its base only when it meets every comparison rule in `docs/rollout-profiles.md`. Its `source` is persisted into `decision.json.profile_source`; normal code, not the model, assigns `ai_assisted` after an approved generated draft.

The decision rules and default threshold meanings remain canonical in `docs/risk-signals.md`. This shape makes them parseable; it does not redefine them.

## `incidents.json`

Phase 1 uses one explicit local incident store rather than free-form Markdown:

```json
{
  "schema_version": "1",
  "sample_data": true,
  "incidents": [
    {
      "id": "INC-003",
      "service": "payouts-api",
      "component": "retry-worker",
      "occurred_at": "2026-07-18T09:15:00Z",
      "affected_paths": ["src/payouts/workers/retry.py"],
      "summary": "Payout workers exhausted the connection pool.",
      "trigger": "Workers retried indefinitely after an upstream timeout.",
      "root_cause": "The retry loop had no attempt limit or backoff."
    }
  ]
}
```

Every incident field is required. IDs are non-empty and unique. `service` must resolve in `policy.yaml`; `component`, `summary`, `trigger`, and `root_cause` are non-empty; `occurred_at` is an RFC 3339 date-time; and `affected_paths` contains unique repository-relative paths. `sample_data` is required so demos cannot silently present synthetic records as real history.

Normal code filters incidents to directly affected services and the configured lookback from `shipping_at`, orders candidates by `occurred_at` descending then `id`, and supplies at most `incident_candidate_limit` records to Ollama. The exact stored strings are the only valid sources for `incident_quote` verification.

## AI-generated profile draft

The one-shot Studio generator accepts a non-empty user description plus the already validated policy context described in `docs/rollout-profiles.md`. Ollama may return only this candidate object:

```json
{
  "schema_version": "1",
  "name": "extra-careful",
  "description": "Expose one pod before a longer health check.",
  "base": "strict",
  "steps": [1, 2, 3, "all"],
  "checkpoint": {
    "seconds": 60,
    "interval_seconds": 10,
    "measurement_count": 6,
    "max_error_rate": 0.03,
    "failure_limit": 1,
    "consecutive_error_limit": 2
  }
}
```

The fields use the same types and rules as a policy profile except that model output has no `source` field. The name must be a lowercase ASCII slug and must not replace a built-in. Normal code validates the draft against the release service and base profile; invalid output changes nothing. A valid draft is still not policy until Studio shows the YAML diff and a person approves the save, at which point normal code persists `source: ai_assisted`.

## Challenge case manifest

Each evaluation case is one directory containing this `case.json` plus the named immutable inputs:

```json
{
  "schema_version": "1",
  "id": "bounded-retry-change",
  "provenance": {
    "kind": "synthetic",
    "source_url": null,
    "license": null
  },
  "request": "assessment-request.json",
  "repository_bundle": "repository.bundle",
  "policy": "policy.yaml",
  "incidents": "incidents.json",
  "patch": "change.patch",
  "expected": {
    "confidence": "high",
    "tier": "guarded",
    "minimum_profile": "guarded",
    "service_facts": {
      "directly_changed": ["payouts-api"],
      "downstream": [],
      "critical": false
    },
    "incident_candidate_ids": [],
    "findings": [
      {"kind": "retry_backoff", "file": "src/workers/payouts.py", "line": 7, "added_line": "MAX_RETRIES = 4"}
    ],
    "forbidden_findings": [],
    "incident_connections": [],
    "forbidden_incident_connections": []
  },
  "label": {
    "rationale": "A bounded retry-count change is guarded, not unbounded.",
    "author": "Andrew",
    "review_status": "reviewed",
    "created_at": "2026-08-08T00:00:00+03:00"
  }
}
```

All fields are required; expectation arrays may be empty. `kind` is `synthetic` or `real`; real cases require non-null HTTPS `source_url` and SPDX-compatible `license`, while synthetic cases require both to be null. Referenced files are relative to the case directory and cannot escape it. `repository.bundle` is a Git bundle containing the request's base and head commits. The evaluator verifies the bundle, clones it to an isolated temporary worktree, configures its canonical origin identity from the request, and invokes the normal assessment entry point. `change.patch` must equal the Git-generated diff for those bundled commits byte-for-byte; it is retained as inspectable fixture evidence, never used as an alternate assessment input. `service_facts` and `incident_candidate_ids` are expected derivations, not alternate engine inputs. Finding kinds, incident classifications, confidence, tiers, and profiles use the canonical enums from `contract.md`.

The evaluator hashes the manifest and every referenced file before a run and records those SHA-256 values in its report. Fixture expectations are owned by `docs/golden-scenarios.md`; this manifest only makes those labels executable.
