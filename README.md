<p align="center">
  <img src="assets/brand/safelane-logo.png" alt="SafeLane" width="680">
</p>

# SafeLane

<p align="center">
  <strong>Ship on autopilot. Stay in control.</strong>
</p>

<p align="center">
  SafeLane ships reviewed changes on autopilot, while keeping every release inside your rules.
</p>

<p align="center">
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/badge/license-MIT-13c55b.svg"></a>
  <img alt="Project status: prototype" src="https://img.shields.io/badge/status-prototype-f59e0b.svg">
  <img alt="Built for a DevOps hackathon" src="https://img.shields.io/badge/DevOps-hackathon%202026-30363d.svg">
</p>

## The missing safety layer for deployment agents

Deployment agents can plan a release, watch it, and repair it. They should not set their own limits
or hold the keys to production.

SafeLane gives Codex, Claude Code, CI, and humans one guarded path to production. A caller asks to
release one reviewed change and one immutable image. SafeLane checks the evidence, applies the
Release Policy, and allows only the rollout step that the policy permits.

The caller does not send Kubernetes configuration. It does not choose the policy. It does not get
production credentials. If it tries to change the protected application directly, Kubernetes denies
the change.

**The agent can drive the release. SafeLane remains the release authority.**

## One request. One decision. One proof.

```mermaid
flowchart LR
    A["Codex, Claude Code,<br/>CI, or a human"] -->|"Release Request"| B["SafeLane"]
    B --> C["Verify<br/>GitHub + GHCR"]
    C --> D["Assess risk<br/>DeployWhisper"]
    D --> E["Decide<br/>Release Policy"]
    E -->|"Allowed rollout step"| F["Execute<br/>Argo Rollouts"]
    F --> G["Enforce<br/>Kubernetes"]
    C --> H["Release Proof"]
    E --> H
    F --> H
    G --> H
```

1. **Request.** The caller sends release identity and evidence: the application, target, reviewed
   change, CI result, and immutable image digest.
2. **Verify.** SafeLane checks GitHub and GHCR. It then renders the Kubernetes objects from an
   operator-owned Release Template. The caller cannot supply or change these objects.
3. **Decide.** DeployWhisper reports advisory risk on the exact Rendered Manifest Bundle. The
   Release Policy decides whether to deny, wait for a human, or allow a bounded rollout path.
4. **Execute and prove.** SafeLane gives the approved image and limits to Argo Rollouts. Argo runs
   the canary. Kubernetes enforces the boundary. SafeLane joins the evidence and outcome into one
   Release Proof.

SafeLane fails closed. Failed, missing, or unknown evidence never becomes permission to release.

## The part SafeLane owns

GitHub already stores reviews and CI results. GHCR already stores images. DeployWhisper already finds
deployment risk. Argo Rollouts already runs canaries. Kubernetes already enforces access.

SafeLane does not rebuild those tools. It makes them answer one release question:

> May this exact reviewed image take the next production step under this Release Policy?

SafeLane owns the small layer needed to answer that question:

- one caller-neutral Release Request for agents, CI, and humans;
- independent checks that bind the reviewed change to an immutable image;
- one trusted Rendered Manifest Bundle for risk checks and execution;
- a typed policy decision that limits the next rollout step;
- a constrained Release Controller that cannot act outside those limits; and
- one Release Proof that shows the artifact, decision, execution, and enforced boundary.

Risk providers advise. The Release Policy decides. Argo executes. Kubernetes enforces. SafeLane
binds the full release together.

## Try the working slice

SafeLane requires Go 1.26.5 or later.

```powershell
git clone https://github.com/AndrewMaged814/safelane.git
cd safelane
go test ./...
go run ./cmd/safelane release --file testdata/release-evidence.json
```

The command uses the real GitHub and GHCR verification paths. The checked-in request contains
placeholder demo evidence, so SafeLane records the attempt and returns `unknown`. This is the
expected fail-closed result. [`testdata/README.md`](testdata/README.md) lists the values that the live
demo must provide.

## Prototype status

SafeLane is an active DevOpsDays Cairo 2026 hackathon prototype.

**Works now**

- `safelane release` accepts release identity and evidence only.
- SafeLane verifies the merged pull request, independent approval, required CI check, and GHCR
  digest against the real services.
- SafeLane renders the bundle once from the Release Template and hashes every object.
- Every attempt gets a stable Release ID and a stored Release record, including failed or unknown
  checks.

**Next**

- run DeployWhisper on the rendered bundle;
- map its result through the versioned Release Policy; and
- add `safelane proof <release-id>` with Artifact and Decision proof.

**Final demonstration**

- patch a pre-created Argo Rollout through the constrained Release Controller;
- progress a healthy canary to 100% using trusted runtime results;
- deny a direct change from the Restricted Caller Identity; and
- finish the Execution and Boundary sections of Release Proof.

See the [progress demonstration](docs/prototype/progress-meeting.md) for the exact build order and the
[open issue map](https://github.com/AndrewMaged814/safelane/issues/31) for current work.

## What SafeLane is not

- **Not a deployment agent.** Any agent, CI job, or human can use the same release interface.
- **Not a new risk engine.** DeployWhisper is the first external risk provider. Its result is advice,
  not production authority.
- **Not a rollout engine.** Argo Rollouts owns workload, traffic, and analysis execution.
- **Not a cluster administrator.** The Release Controller gets only the narrow access needed to
  update the protected Rollout.
- **Not a prompt-only guardrail.** Guidance helps agents find SafeLane. Kubernetes access rules still
  stop a caller that ignores the guidance.

## Repository guide

| Path | Start here for |
| --- | --- |
| [`CONTEXT.md`](CONTEXT.md) | The exact SafeLane vocabulary and product scope |
| [`docs/prototype/release-interface.md`](docs/prototype/release-interface.md) | The Release Request, result, and Release Proof contracts |
| [`docs/prototype/progress-meeting.md`](docs/prototype/progress-meeting.md) | The current demonstration and its finish line |
| [`docs/policy/safelane-policy.yml`](docs/policy/safelane-policy.yml) | The example risk tiers and allowed rollout paths |
| [`docs/research/`](docs/research) | Prior-art, evidence, and integration decisions |
| [`internal/release`](internal/release) | Release Request, Release Evidence, bundle, and Release types |
| [`internal/orchestrate`](internal/orchestrate) | The shared release path used by every caller |
| [`internal/verify`](internal/verify) | Live GitHub and GHCR evidence checks |
| [`internal/render`](internal/render) | Trusted bundle rendering and content hashes |
| [`cmd/safelane`](cmd/safelane) | The CLI entry point |

## Attribution

SafeLane uses [DeployWhisper](https://github.com/deploywhisper/deploywhisper) as a replaceable External
Risk Provider. SafeLane calls it through a pinned adapter and does not copy its code. Read the
[integration research](docs/research/deploywhisper-spike.md) for the full decision.

## License

SafeLane is available under the [MIT License](LICENSE).
