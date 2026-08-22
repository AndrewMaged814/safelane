---
title: Installing the Agent Skill
description: Install SafeLane's Claude Code workflow for agent-driven releases.
---

## An agent needs the release rules in the same place as the commands

Without a workflow, an agent may skip inspection, choose a lane, retry a timeout, or edit policy. SafeLane includes a Claude Code skill named /safelane.

The committed skill lives at internal/skill/SKILL.md. Install or link it using your Claude Code skill setup, then start a new session so the skill is loaded.

The workflow is narrow:

1. Run safelane release plan --pr <n>.
2. Continue only on exit code 0.
3. Run safelane release run <id>.
4. Repeat safelane release run <id> until complete.
5. Run safelane release proof <id> and report the outcome.

The skill tells the agent not to choose a lane, not to edit SafeLane configuration, not to retry a timeout, and to report refusal codes verbatim.

## Why the skill does not replace Kubernetes permissions

Instructions shape agent behavior. Kubernetes permissions enforce the boundary when an instruction is ignored. SafeLane uses both, but the credential split is the final control.

## Next

- [Running a Release End to End](./release-end-to-end/)
- [The Boundary](../concepts/boundary/)
- [Exit Codes](../reference/exit-codes/)

