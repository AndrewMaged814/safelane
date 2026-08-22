---
name: safelane
description: Coordinate a merged pull request through SafeLane. Use for SafeLane setup, release, deploy, rollout monitoring, emergency control, or release proof.
user-invocable: true
---

# SafeLane

SafeLane is the release coordinator. Use its commands as the complete production
interface. SafeLane assesses and authorizes progression; Argo executes analysis,
promotion, abort, and rollback.

## Setup

For a normal deterministic setup, run:

`safelane setup`

When the user asks for an agent-shaped setup:

1. Run `safelane setup inspect --json`.
2. Treat the returned file list as compact evidence, not source text. Read only
   repository files relevant to the reported checks, critical surfaces, Kubernetes
   resources, and uncertainties using this active session's normal file tools.
3. Build one `safelane.setup.proposal/v1` JSON proposal from that inspection.
   Preserve its `inspection_fingerprint`. Cite every critical surface and configure
   a concrete runtime assertion for each identified hazard.
4. Write the proposal to an absolute temporary path.
5. Explain the proposal once, then ask approval.
6. After approval, run `safelane setup apply --proposal <absolute-path> --yes --json`.
7. Run `safelane doctor`. Setup is complete only when doctor passes.

## Release

Require an exact merged pull-request number from the user. Ask for it when absent;
never select a recent or “latest” pull request.

1. Run `safelane release plan --pr <number> --json` once.
2. Explain the exact artifact, target, deterministic and semantic risk, cited
   hazards, assertion coverage, lane, weights, and authority ceiling. Treat model
   rationale as a hypothesis; it may raise risk but cannot lower deterministic risk.
3. Ask one approval for that frozen Safety Contract.
4. After approval, run
   `safelane release run <release-id> --yes --json` and remain attached.
5. On exit 0 or 1, run `safelane release proof <release-id> --json` and report the
   terminal outcome. Attribute AnalysisRun failure and normal rollback to Argo.
6. On exit 3, run `safelane release status <release-id> --json`, report that the
   mutation outcome is unknown, then reconnect with `release run`; it reconciles
   before acting. Never issue a direct promotion to resolve uncertainty.
7. On exit 4, report the exact uncovered hazard. Ask only whether the user accepts
   that hazard. When approved and policy permits it, run
   `safelane release accept-risk <release-id> --hazard <hazard-id> --reason <reason> --yes --json`,
   then reconnect with `release run`.

A paused release continues only after the user explicitly requests
`safelane release resume <release-id> --reason <reason>`. Emergency pause,
resume, and abort always carry a durable reason.

## Completion

Completion is a terminal proof, not a successful command invocation. Report the
release ID, artifact digest, lane, Argo analysis outcome, final state, and proof
command. When correlation is incomplete, state “cause unknown” and separate
observed facts from hypotheses.
