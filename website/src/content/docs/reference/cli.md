---
title: CLI Command Reference
description: One setup and release workflow, with explicit emergency controls.
---

## Primary workflow

```text
setup → doctor → release plan → release run → release proof
```

| Command | Purpose |
| --- | --- |
| `safelane setup` | Discover and write conservative operator configuration without invoking an agent. |
| `safelane setup inspect --json` | Return repository facts and a bounded risk/assertion proposal for the active Codex or Claude session. |
| `safelane setup apply --proposal <absolute-path>` | Validate bounded agent decisions and compile operator-owned configuration atomically. |
| `safelane doctor` | Check configuration, evidence providers, credentials, and stored-template compatibility with the live target. |
| `safelane release plan --pr <number>` | Freeze the exact PR, assessment, Safety Contract, and rendered bundle without production mutation. |
| `safelane release run <id>` | Ask once and coordinate Argo to a terminal or decision-required outcome. |
| `safelane release status [id]` | Reconcile one release or list active/decision-required attempts. |
| `safelane release proof <id>` | Read durable release proof. |
| `safelane release retry <id>` | Re-verify and create a new attempt from a terminal release. |
| `safelane release accept-risk <id> --hazard <id> --reason <text>` | Record one policy-permitted uncovered hazard decision. |
| `safelane release pause/resume/abort` | Use the separately audited emergency-control path. |
| `safelane demo up/reset/down` | Manage only SafeLane's isolated Kind demo. |

## Agent/CI form

```bash
safelane release plan --pr 42 --json
safelane release run rel_... --yes --json
safelane release proof rel_... --json
```

Progress is written to stderr. JSON results are written to stdout using `safelane.command.result/v1`; successful results include `state` and `next_command`.

`release run --step` performs at most one authorized progression. Default `release run` stays attached. `--yes` skips only the single confirmation—it does not widen the Safety Contract.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Operation succeeded. |
| 1 | Policy refusal or negative release outcome. |
| 2 | Usage error. |
| 3 | Mutation outcome unknown or timed out; reconnect with `release run`. |
| 4 | A specific human decision is required. |
