# SafeLane setup

Run setup from the application repository:

```bash
safelane setup
```

SafeLane discovers the GitHub remote, default branch, workflow checks, image workflow, Kubernetes resources, critical application surfaces, and concrete runtime assertions. It previews a conservative configuration, asks once, writes operator-owned files under `~/.safelane/apps/<application>/`, and installs the SafeLane skill. It never invokes Claude or Codex and never edits the application repository.

## Project-specific setup with Codex or Claude

The active agent session can propose a smarter policy without a nested process:

```bash
safelane setup inspect --json
safelane setup plan --findings - --json
safelane setup apply setup_... --yes
safelane doctor
```

`setup inspect` returns fingerprinted facts, uncertainties, mandatory product
assertions, and compact file evidence. It persists that inspection under the
SafeLane home directory but does not edit the application repository or return
an editable baseline.
The active agent contributes only application-specific risk paths and semantic
assertion intents, each with file/line evidence. SafeLane accepts only intents
that its configured probe can compile into executable Runtime Assertions.

`setup plan` validates those findings, adds SafeLane-owned safety floors, compiles
the complete configuration, and persists it under a content-addressed setup ID.
`setup apply <setup-id>` rechecks the repository fingerprint and applies that
exact plan atomically. Interactive use requires typing `APPLY`; noninteractive
use requires `--yes`.

## Demo environment

SafeLane no longer provisions clusters. Provide a reachable cluster with Argo
Rollouts, a traffic router and a metrics provider, then point the operator
configuration at it.

Run `safelane doctor` after setup. Doctor reports external prerequisites and target readiness without changing them, including whether the stored Rollout and Service selectors and ports match the live target.
