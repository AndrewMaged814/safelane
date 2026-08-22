---
title: Quick Start
description: Approve one exact Safety Contract and let SafeLane coordinate the release.
---

## Install and set up

From the application repository:

```bash
safelane setup
safelane doctor
```

For the isolated local demonstration, start Docker and run `safelane demo up --yes` before doctor. SafeLane downloads its pinned, checksum-verified Kind, kubectl, and Argo Rollouts CLI tools privately, owns that Kind cluster, and never changes your ambient PATH or Kubernetes context.

## Release one exact merged PR

```bash
safelane release plan --pr 42
safelane release run rel_...
safelane release proof rel_...
```

Planning performs no production mutation. It verifies the merge commit, CI, and immutable image; combines deterministic and semantic assessment; connects cited hazards to concrete canary assertions; freezes the lane, authority, and rendered bundle; and returns the exact run command.

Running shows that frozen contract and asks once. It then stays attached while Argo runs canary-only Analysis Jobs and traffic progression. Argo aborts and restores stable traffic on analysis failure; SafeLane reconciles and records that outcome.

Another decision is requested only when the frozen contract identifies a specific uncovered hazard and policy permits explicit acceptance.

## Agent/CI form

```bash
safelane release plan --pr 42 --json
safelane release run rel_... --yes --json
safelane release proof rel_... --json
```

The agent never searches for the “latest PR,” never chooses arbitrary weights, and never loops over gate commands. Re-running `release run` safely reconnects and reconciles before acting.
