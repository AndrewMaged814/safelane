---
title: Release Record Schema
description: The persisted JSON shape behind Release Proof.
---

## A release ID without a record is only a label

SafeLane persists a record for every release attempt, including failed and indeterminate checks.

    {
      "schema_version": "...",
      "release_id": "rel_...",
      "created_at": "...",
      "request": {},
      "target": {},
      "caller": {},
      "evidence": {},
      "bundle": {},
      "eligibility": {},
      "assessment": {},
      "execution": [],
      "boundary": {},
      "envelope": {},
      "outcome": "..."
    }

| Section | Records |
| --- | --- |
| request | Release intent, pull request, environment, and caller metadata. |
| evidence | Verified repository, pull request, approval, required check, and artifact. |
| bundle | Template identity, target, pinned digest, rendered resources, hashes, and bundle digest. |
| eligibility | Status, policy version, reason code, retryability, and rollout envelope. |
| assessment | Facts, heuristic verdict, model verdict, combined risk, and lane. |
| execution | Timestamp, verb, requested weight, outcome, analysis, reason code, and detail. |
| boundary | Caller identity, controller identity, and caller capability. |
| envelope | Lane, weights, gates, source, and template digest. |

Read it through proof, proof --details, or proof --json. Proof reports what SafeLane recorded for that release.

## Why the record keeps failed attempts

An ineligible release is still an operational fact. Keeping the attempt makes the refusal inspectable and prevents a caller from manufacturing a clean history by retrying until a later command succeeds.

## Next

- [The Release Record & Proof](../concepts/record-and-proof/)
- [CLI Command Reference](./cli/)
- [Exit Codes](./exit-codes/)

