---
title: CLI Command Reference
description: Current SafeLane commands and their important flags.
---

## A command reference must match the binary

SafeLane's current commands are:

| Command | Purpose |
| --- | --- |
| safelane init | Create operator-owned application configuration. |
| safelane doctor | Check whether SafeLane can release now. |
| safelane release inspect | Verify a change and record eligibility. |
| safelane release retry | Re-collect evidence and create a linked attempt after a retryable outcome. |
| safelane rollout start | Apply the trusted bundle and begin the canary. |
| safelane rollout advance | Move to the next policy weight. |
| safelane rollout pause | Pause the current rollout. |
| safelane rollout abort | Abort and record a reason. |
| safelane status | Show one rollout or list open releases. |
| safelane proof | Retrieve a persisted Release Proof. |
| safelane version | Print the build version. |

    safelane release inspect --pr <number> [--repo owner/name] [--environment production] [--image ghcr.io/owner/app@sha256:...] [--json]
    safelane release retry <release-id>
    safelane rollout start <release-id>
    safelane rollout advance <release-id>
    safelane rollout pause <release-id>
    safelane rollout abort --reason "..." <release-id>
    safelane status [<release-id>] [--json]
    safelane proof <release-id> [--details | --json]

Release inspection also accepts file, template-dir, project, policy, store-dir, and github-token. Rollout commands accept project, store-dir, controller-kubeconfig, and controller-context.

Inspection is idempotent for repository + PR + application + environment + cluster + namespace + Rollout. It returns the latest attempt and complete history. Only `release retry` creates a later attempt, and only after a retryable terminal outcome.

## Next

- [Quick Start](../start-here/quick-start/)
- [Exit Codes](./exit-codes/)
- [Configuration File Schemas](./configuration/)

