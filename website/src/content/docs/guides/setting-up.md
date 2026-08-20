---
title: Setting Up an Application
description: Create the operator-owned SafeLane application configuration.
---

## A repository cannot safely infer its production target

The caller's checkout does not own the cluster, namespace, rollout, policy, or controller credential. Letting it infer those values turns local context into production authority.

Run init from the application repository:

    safelane init --app podinfo --repo AndrewMaged814/podinfo

SafeLane creates operator-owned files under ~/.safelane/apps/podinfo/:

    project.yml
    policy.yml
    release-template/
    releases/

The project file names the GitHub repository, default branch, image repository and tag, mandatory checks, target cluster, namespace, Rollout, template path, and controller credentials. Edit the operator-owned files, then run doctor.

## Why configuration lives under SAFELANE_HOME

SafeLane keeps application configuration and records outside the application checkout. A caller can identify the repository, but it cannot rewrite the operator's policy by changing a working tree file.

For this prototype, `releases/rel_*.json` is the authoritative single-machine ledger. Each file is one attempt and updates land atomically. Shared-machine synchronization and concurrent SafeLane writers are unsupported.

## Next

- [Configuration File Schemas](../reference/configuration/)
- [Pre-flight Checks](./pre-flight/)
- [Running a Release End to End](./release-end-to-end/)

