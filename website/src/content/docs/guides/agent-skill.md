---
title: Using the Agent Skill
description: Run agent-shaped SafeLane setup and one-approval releases from Claude or Codex.
---

## An agent needs the release rules in the same place as the commands

Without a workflow, an agent may skip inspection, choose a lane, or retry an uncertain mutation. Every SafeLane release archive includes one canonical `/safelane` skill for Claude and Codex.

The normal installer places it under both `~/.claude/skills/safelane/` and `~/.agents/skills/safelane/`. Restart the agent session after installing or upgrading so it loads the current skill.

When `/safelane` is active, setup is agent-shaped by default:

1. `setup inspect --json` runs once and returns fingerprinted repository facts without an editable baseline.
2. The active agent reads only relevant files and submits evidence-backed application risk paths and semantic assertion intents to `setup plan --findings - --json`.
3. SafeLane adds product floors and mandatory assertions, then persists the exact compiled configuration under a setup ID.
4. The agent explains that plan once, requests one approval, applies the returned setup ID, and runs `doctor`.

The agent never authors policy YAML or Kubernetes manifests. The installed skill contains the small findings contract, so the setup path needs no schema search, CLI-help discovery, JSON processor, or generated script.

For a release, the skill requires one exact merged PR, explains the frozen Safety Contract once, runs `release run` to a terminal outcome, and reports `release proof`. Argo remains responsible for analysis failure, abort, and rollback.

## Why the skill does not replace Kubernetes permissions

Instructions shape agent behavior. Kubernetes permissions enforce the boundary when an instruction is ignored. SafeLane uses both, but the credential split is the final control.

## Next

- [Running a Release End to End](./release-end-to-end/)
- [The Boundary](../concepts/boundary/)
- [Exit Codes](../reference/exit-codes/)

