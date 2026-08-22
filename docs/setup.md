# SafeLane setup

Run setup from the application repository:

```bash
safelane setup
```

SafeLane discovers the GitHub remote, default branch, workflow checks, image workflow, Kubernetes resources, critical application surfaces, and concrete runtime assertions. It previews a conservative configuration, asks once, writes operator-owned files under `~/.safelane/apps/<application>/`, and installs the SafeLane skill. It never invokes Claude or Codex and never edits the application repository.

## Project-specific setup with Codex or Claude

The active agent session can propose a smarter policy without a nested process:

```bash
safelane setup inspect --json > inspection.json
safelane setup apply --proposal /absolute/path/proposal.json --yes
safelane doctor
```

`setup inspect` is read-only. Its JSON includes an inspection fingerprint,
uncertainties, and a small validator-ready `proposal`. The active agent changes
only evidence-backed risk-path floors and runtime assertions. Required checks,
mandatory evidence, lanes, model configuration, policy YAML, and Release
Templates remain deterministic SafeLane output.

`setup apply` validates the bounded proposal and compiles the complete operator configuration atomically. It rejects stale fingerprints, unknown fields, unsafe risk paths, or missing assertions. Interactive use requires typing `APPLY`; noninteractive use requires `--yes`.

## Demo environment

To create the isolated Kind environment and seed a healthy first-party baseline:

```bash
safelane demo up --yes
```

Docker must be running. SafeLane downloads checksum-verified pinned Kind, kubectl, and Argo Rollouts CLI binaries under `~/.safelane/demo/bin`, owns only the cluster named `safelane-demo`, keeps its kubeconfig under the SafeLane home directory, installs the pinned controller, resolves published fixtures to immutable digests, and binds the operator configuration when setup already exists. The private tool directory is visible only to SafeLane processes; the command never changes the ambient PATH or Kubernetes context.

Run `safelane doctor` after setup. Doctor reports external prerequisites and target readiness without changing them, including whether the stored Rollout and Service selectors and ports match the live target.
