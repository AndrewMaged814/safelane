---
name: safelane
description: Release a merged pull request through SafeLane. Use when the user asks to inspect, release, deploy, promote, monitor, or diagnose a SafeLane release.
user-invocable: true
---

# SafeLane release

SafeLane is the only production path for the protected application. Use its CLI
for every production read and transition; direct Kubernetes or Argo access is
outside your authority.

## 1. Establish the change

Identify one merged pull request and its repository. Read its checks with:

`gh pr checks <pr> --repo <owner/repo>`

Report every failed or cancelled check. These are safety signals even when the
operator's SafeLane policy does not make them mandatory. Keep the terms exact:

- **eligibility** means the configured mandatory evidence verified;
- **assessment** selects a bounded lane;
- neither means the change is correct or safe.

## 2. Inspect without mutation

Run `safelane release inspect --pr <pr> --repo <owner/repo>`.

Read `history`, `recorded_state`, `live_state`, `effective_state`, `state_source`,
and `next_command` from inspect's JSON. The ledger is authoritative for attempt
identity: repeated inspection reuses the latest attempt. Report actual check
outcomes before the model assessment. Treat its rationale as a hypothesis and
never supply a CI cause that the report did not contain.

Exit 1: report every Failed and Unavailable line and stop. Retry only when the
typed result says `retryable true`.

Follow only `next_command`. A terminal or `unknown` attempt has no start action.
When inspect offers start or advance, ask explicit approval by quoting the exact
command, release ID, application, and full production target. Completion means
the user approved that exact mutation.

## 3. Start and classify the result

Run the exact approved `safelane rollout start <release-id>` command.

- `exit 0`: first gate reached; continue with the next action printed.
- `rejected`: nothing was applied. Report the reason code and remedy verbatim.
- `failed after applying`: production mutation was attempted and the failed
  execution was recorded. Run the diagnostic reads below, then stop.
- `exit 3`: outcome is unknown. Run the diagnostic reads below and do not retry.

Diagnostic reads:

`safelane status <release-id> --json`

`safelane proof --details <release-id>`

`release_match: false` means annotation, execution binding, target, digest, and
generation did not all correlate. Describe live state as unrelated, never as
this attempt's outcome.

## 4. Advance one gate at a time

Ask approval with the exact advance command and target, then run it once. Repeat
inspection/status before asking for another gate.
Never pass `--to`; the recorded envelope chooses the next weight.

- `exit 0`: follow the printed next action.
- `exit 1`: report the refusal or recorded abort and stop.
- `exit 3`: read status and proof; do not retry the advance.

## 5. Report proof

Run `safelane proof --details <release-id>` and report the final outcome,
execution entries, analysis evidence, and boundary evidence.

## Causality gate

Name a runtime cause only when all three agree: `release_match: true`, the
persisted execution belongs to this release, and its AnalysisRun or Argo message
contains the supporting evidence. Otherwise say **cause unknown** and separate
facts from hypotheses. A temporal coincidence such as “aborted after this
change” is not causal evidence.

You do not choose a lane, request a weight, edit SafeLane configuration, or work
around a refusal. A refusal is a correct terminal result, distinct from a
post-apply failure.
