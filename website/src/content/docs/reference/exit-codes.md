---
title: Exit Codes
description: Stable exit codes for humans and agents.
---

## An agent must distinguish refusal from uncertainty

SafeLane uses four exit codes. Branch on the code before parsing prose.

| Code | Name | Meaning |
| ---: | --- | --- |
| 0 | ExitOK | The command succeeded. |
| 1 | ExitFail | The command ran and reported a failure, such as rejected evidence or a refusal. |
| 2 | ExitUsage | The invocation is wrong: unknown command, missing release ID, or malformed flags. |
| 3 | ExitTimeout | A rollout promotion was sent, but its outcome is unknown. Read status. Do not retry. |

    0 → continue
    1 → report and stop
    2 → fix the invocation
    3 → run safelane status <id>; do not retry advance

## Why timeout is not failure

If SafeLane times out while waiting for Argo, the promotion may already have taken effect. Calling advance again can create a second promotion. Code 3 forces a status read first.

## Next

- [Installing the Agent Skill](../guides/agent-skill/)
- [Handling a Paused or Aborted Rollout](../guides/rollout-recovery/)
- [CLI Command Reference](./cli/)

