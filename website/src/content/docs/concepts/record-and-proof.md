---
title: The Release Record & Proof
description: The persisted object that binds evidence, decision, execution, and boundary.
---

## A green terminal line is not a release history

Logs show what a command printed. They do not bind the exact pull request, image digest, rendered objects, lane, rollout actions, and Kubernetes identities into one object.

<pre class="mermaid">flowchart LR
  A["Artifact evidence"] --> E["Release Record"]
  B["Eligibility + assessment"] --> E
  C["Rendered bundle hash"] --> E
  D["Execution + boundary"] --> E
  E --> F["safelane proof <id>"]</pre>

The record includes the release ID, request, target, caller, evidence result, rendered bundle, eligibility, assessment, execution entries, boundary, envelope, and outcome. It is stored under the operator-owned releases directory.

proof reads that persisted record. It does not call GitHub, GHCR, or the policy evaluator again.

| Before | After |
| --- | --- |
| “The rollout succeeded” in a log. | A proof record names the immutable artifact and the enforced outcome. |
| YAML changed between review and execution. | SafeLane hashes the Rendered Manifest Bundle before execution. |
| Caller and controller are hard to distinguish. | The boundary section records both identities and capability. |

## Why proof is a read of the record

Re-evaluating a release during proof could produce a different answer after policy or external services change. Proof reports what SafeLane recorded for that release.

## Next

- [Release Record Schema](../reference/release-record/)
- [Running a Release End to End](../guides/release-end-to-end/)
- [Exit Codes](../reference/exit-codes/)

