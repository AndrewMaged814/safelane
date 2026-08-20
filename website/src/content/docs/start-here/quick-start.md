---
title: Quick Start
description: Run the real SafeLane release sequence.
---

## A release fails before rollout when its setup is wrong

If project.yml, policy.yml, the template, GitHub access, or the target Rollout is wrong, starting the canary is too late. Run the checks first.

    safelane init --app podinfo --repo AndrewMaged814/podinfo
    safelane doctor
    safelane release inspect --pr 42
    safelane rollout start rel_...
    safelane rollout advance rel_...
    safelane proof rel_...

The inspect report includes evidence rows, Assessment, Eligibility, Release ID, and the next command:

    Release: rel_...
    Eligibility: eligible (...)
    Assessment: risk medium, lane standard
    Nothing was changed.
    Next: safelane rollout start rel_...

Repeat rollout advance until the output says complete. Do not pass --to; the envelope decides the weight.

Use proof --details for the complete human-readable record or proof --json for the machine-readable contract.

## Why release inspect is read-only

Inspection answers “may this release start?” without changing the cluster. SafeLane persists the decision, but the rollout begins only after rollout start.

## Next

- [Running a Release End to End](../guides/release-end-to-end/)
- [Pre-flight Checks](../guides/pre-flight/)
- [Exit Codes](../reference/exit-codes/)

