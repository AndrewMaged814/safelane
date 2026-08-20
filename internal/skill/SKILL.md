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

Exit 0 returns a release ID. Report eligibility, risk, lane, failed checks from
step 1, and the model rationale. Treat model rationale as a hypothesis: verify
material claims against the diff, existing tests, and check output. Attribute no
runtime event to the change during this read-only step.

Exit 1: report every Failed and Unavailable line and stop. Retry only when the
typed result says `retryable true`.

Before starting a high-risk release or one with a failed check, ask the user for
explicit approval. State that start applies the Rendered Manifest Bundle.

## 3. Start and classify the result

Run `safelane rollout start <release-id>`.

- `exit 0`: first gate reached; continue with the next action printed.
- `rejected`: nothing was applied. Report the reason code and remedy verbatim.
- `failed after applying`: production mutation was attempted and the failed
  execution was recorded. Run the diagnostic reads below, then stop.
- `exit 3`: outcome is unknown. Run the diagnostic reads below and do not retry.

Diagnostic reads:

`safelane status <release-id> --json`

`safelane proof --details <release-id>`

`release_match: false` means the shared live Rollout state is not correlated to
this release's observed generation and digest. Describe it as prior, stale, or
unrelated live state—not as this release's outcome.

## 4. Advance one gate at a time

Run `safelane rollout advance <release-id>` until the output says complete.
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
