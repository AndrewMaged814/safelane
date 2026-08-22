---
title: Using the Agent Skill
description: Run agent-shaped SafeLane setup and one-approval releases from Claude or Codex.
---

## An agent needs the release rules in the same place as the commands

Without a workflow, an agent may skip inspection, choose a lane, or retry an uncertain mutation. Every SafeLane release archive includes one canonical `/safelane` skill for Claude and Codex.

The normal installer places it under both `~/.claude/skills/safelane/` and `~/.agents/skills/safelane/`. Restart the agent session after installing or upgrading so it loads the current skill.

When `/safelane` is active, setup is agent-shaped by default:

1. `setup inspect --json` returns compact repository evidence and a complete validator-ready `proposal`.
2. The active agent reads only relevant repository files and tailors project-specific checks, risk policy, and runtime assertions.
3. SafeLane validates and applies the proposal after one approval, then `doctor` verifies readiness.

For a release, the skill requires one exact merged PR, explains the frozen Safety Contract once, runs `release run` to a terminal outcome, and reports `release proof`. Argo remains responsible for analysis failure, abort, and rollback.

## Why the skill does not replace Kubernetes permissions

Instructions shape agent behavior. Kubernetes permissions enforce the boundary when an instruction is ignored. SafeLane uses both, but the credential split is the final control.

## Next

- [Running a Release End to End](./release-end-to-end/)
- [The Boundary](../concepts/boundary/)
- [Exit Codes](../reference/exit-codes/)

