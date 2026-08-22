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

To create the isolated Kind environment and seed a healthy first-party baseline:

```bash
safelane demo up --yes
```

Docker must be running. SafeLane downloads checksum-verified pinned Kind, kubectl, and Argo Rollouts CLI binaries under `~/.safelane/demo/bin`, owns only the cluster named `safelane-demo`, keeps its kubeconfig under the SafeLane home directory, installs the pinned controller, resolves published fixtures to immutable digests, and binds the operator configuration when setup already exists. The private tool directory is visible only to SafeLane processes; the command never changes the ambient PATH or Kubernetes context.

Run `safelane doctor` after setup. Doctor reports external prerequisites and target readiness without changing them, including whether the stored Rollout and Service selectors and ports match the live target.
