---
name: safelane
description: Coordinate a merged pull request through SafeLane. Use for SafeLane setup, release, deploy, rollout monitoring, emergency control, or release proof.
---

# SafeLane

SafeLane is the release coordinator. Use its commands as the complete production
interface. SafeLane assesses and authorizes progression; Argo executes analysis,
promotion, abort, and rollback.

## Setup

When this skill is active inside Claude or Codex, setup is agent-shaped by
default. Use deterministic setup only when the user explicitly asks for a
manual, conservative, or non-agent setup:

`safelane setup`

For agent-shaped setup:

1. Run `safelane setup inspect --json` exactly once and keep that result.
2. Use its returned `proposal` as the complete bounded decision contract.
   SafeLane owns required checks, mandatory evidence, lanes, model configuration,
   policy YAML, and Release Templates. The proposal contains only `summary`,
   cited `risk_paths`, and concrete `runtime_assertions` plus its schema and
   inspection fingerprint.
3. Treat the returned file list as compact evidence, not source text. Read only
   repository files relevant to the reported checks, critical surfaces, Kubernetes
   resources, and uncertainties using this active session's normal file tools.
4. Tailor only evidence-backed risk-path floors and runtime assertions. Keep each
   risk-path reason specific enough for the user to verify.
5. Write the small proposal object to an absolute temporary path with the active
   session's native file-writing tool. The workflow requires no CLI-help lookup,
   schema discovery, shell JSON processor, or generated script. Explain the
   project-specific decisions once, then ask approval.
6. After approval, run `safelane setup apply --proposal <absolute-path> --yes --json`.
7. For the first-party demo, run `safelane demo up --yes --json` so the probe
   digest and private credentials are bound. Then run `safelane doctor`.
   Setup is complete only when doctor passes.

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
