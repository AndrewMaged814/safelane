---
title: Running a Release End to End
description: The complete inspect, start, advance, and proof workflow.
---

## A rollout command is too late to discover a bad release

Start with the release identity. SafeLane does the checking before it touches Argo.

<pre class="mermaid">flowchart LR
  A["doctor"] --> B["release inspect --pr N"]
  B --> C["rollout start ID"]
  C --> D["rollout advance ID"]
  D -->|repeat until complete| D
  D --> E["proof ID"]</pre>

    safelane doctor
    safelane release inspect --pr 42
    safelane rollout start rel_...
    safelane rollout advance rel_...
    safelane proof --details rel_...

Inspect verifies the merged commit, required publish check, immutable GHCR digest, and optional independent approval. Start applies the Rendered Manifest Bundle through the controller identity and stops at gate one. Advance reads Argo status and the lane envelope. Proof joins artifact, decision, execution, and boundary data.

## Why you never pass --to

The flag exists as a parser-level compatibility surface, but the agent workflow must not use it. The envelope owns the next weight. A caller-selected target would bypass the decision SafeLane just recorded.

## Next

- [Handling a Paused or Aborted Rollout](./rollout-recovery/)
- [The Release Record & Proof](../concepts/record-and-proof/)
- [Installing the Agent Skill](./agent-skill/)

