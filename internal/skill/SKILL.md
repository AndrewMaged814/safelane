---
name: safelane
description: Release a merged pull request through SafeLane. Use when the user asks to release or deploy a merged pull request with SafeLane.
user-invocable: true
---

SafeLane is the only path to production for this application. You cannot reach
Kubernetes or Argo directly, and you must not try.

1. safelane release inspect --pr <n>
   Reads only. Changes nothing.
   Report the Assessment section to the user: the risk, the lane, and why.
   exit 0 → note the release id, continue
   exit 1 → report the Failed and Unavailable lines. Stop.
            Retry only if the output says retryable true.

2. safelane rollout start <id>
   Blocks until the first gate.

3. safelane rollout advance <id>
   Repeat until the output says complete.
   Never pass --to. The envelope decides the weight, not you.
   exit 0 → continue
   exit 1 → refused or aborted. Report and stop. Do not work around it.
   exit 3 → timeout. Run safelane status <id>. Do NOT retry advance.

4. safelane proof <id>
   Report the outcome to the user.

You do not choose the lane and you cannot request one. If you believe the lane is
wrong, say so to the user and stop. Do not edit any SafeLane configuration.

If any command is refused, report the reason code and the remedy verbatim.
A refusal is a correct outcome, not an obstacle.
