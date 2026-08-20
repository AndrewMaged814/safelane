---
title: Handling a Paused or Aborted Rollout
description: Inspect status, pause a rollout, or abort it with a recorded reason.
---

## A paused rollout is not permission to guess

Argo can pause at a canary gate. A timeout can leave the outcome unknown. Retrying blindly can send a second promotion after the first one already took effect.

<pre class="mermaid">flowchart LR
  A["rollout advance"] --> B{"Outcome?"}
  B -->|paused / healthy| C["status ID"]
  B -->|timeout| C
  B -->|regression| D["rollout abort --reason ..."]
  C --> E["decide with current record"]</pre>

    safelane status rel_...
    safelane status --json rel_...
    safelane rollout pause rel_...
    safelane rollout abort --reason "error rate rose after gate" rel_...

Abort restores stable traffic. It does not invent a new release record.

## Why timeout returns exit code 3

A timeout means SafeLane does not know whether the promotion took effect. That is different from failure. Read status; do not retry rollout advance automatically.

## Next

- [Exit Codes](../reference/exit-codes/)
- [CLI Command Reference](../reference/cli/)
- [The Boundary](../concepts/boundary/)

