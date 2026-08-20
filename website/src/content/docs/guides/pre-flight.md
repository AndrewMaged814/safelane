---
title: Pre-flight Checks
description: Use doctor to verify the release path before inspection or rollout.
---

## A broken credential or template should stop before the canary

doctor checks the operator path without creating a release. Run it after init and whenever the cluster, credentials, policy, or template changes.

    safelane doctor
    safelane doctor --project ~/.safelane/apps/podinfo/project.yml --policy ~/.safelane/apps/podinfo/policy.yml --template-dir ~/.safelane/apps/podinfo/release-template

Doctor checks project configuration, policy, Release Template, GitHub access, GHCR access, the caller kubeconfig, the controller kubeconfig, and the target Rollout.

Use --github-token for an explicit GitHub API token. Otherwise SafeLane uses GITHUB_TOKEN when present and can make unauthenticated requests against public repositories.

## Why doctor runs before release inspect

Inspection should report release evidence, not hide a broken local setup among remote checks. Doctor makes the boundary visible before a release ID exists.

## Next

- [Quick Start](../start-here/quick-start/)
- [Setting Up an Application](./setting-up/)
- [Configuration File Schemas](../reference/configuration/)

