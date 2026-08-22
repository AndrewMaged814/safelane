<p align="center">
  <img src="assets/brand/safelane-logo.png" alt="SafeLane" width="680">
</p>

# SafeLane

<p align="center"><strong>Stop shipping every change the same way.</strong></p>

SafeLane is release coordination for coding agents. Give it an exact merged pull request once; it turns code and CI evidence into a frozen Safety Contract, asks for one approval, then coordinates Argo Rollouts until the release is promoted, rolled back, or needs one specific human decision.

## Why use it

Deployment agents are good at operating tools but inconsistent at release discipline. They rediscover the latest PR, choose ad-hoc rollout steps, check generic health endpoints, and return for approval at every gate.

SafeLane supplies the missing release loop:

```text
merged PR
   │
   ▼
Safety Contract ── artifact + hazards + concrete canary assertions + authority
   │ one approval
   ▼
SafeLane coordinates ──► Argo executes analysis and traffic mechanics
   │
   └──────────── durable Release Proof
```

- Deterministic setup never launches a hidden Claude or Codex process.
- Semantic assessment can identify cited hazards, but cannot choose weights or operations.
- Model outages select a recorded guarded fallback; they do not invent confidence.
- Runtime analysis exercises the canary-only `/api/demo` behavior and verifies its commit, not a hard-coded “healthy” URL.
- Argo owns analysis failure, abort, and rollback. SafeLane reconciles and proves what happened.
- `release run` stays attached and progresses within approved authority—no gate-by-gate babysitting.

## Workflow

```bash
safelane setup
safelane doctor
safelane release plan --pr 42
safelane release run rel_...
safelane release proof rel_...
```

For an isolated local environment:

```bash
safelane demo up --yes
safelane demo reset --yes
safelane demo down --yes
```

`demo` requires a running Docker engine. SafeLane downloads checksum-verified pinned Kind, kubectl, and Argo Rollouts CLI binaries into its private demo directory, uses the owned cluster `safelane-demo` and a private kubeconfig, and never changes the ambient PATH or Kubernetes context.

## Setup with an agent

Human setup is deterministic and conservative. An active Codex or Claude session can make it project-specific without SafeLane nesting another agent process:

```bash
safelane setup inspect --json
safelane setup plan --findings - --json
safelane setup apply setup_... --yes
safelane doctor
```

The agent submits only evidence-backed application risk paths and semantic assertion intents. SafeLane maps those intents to executable probe assertions, adds its product safety floors, compiles and persists the exact operator configuration, and returns an immutable setup ID. One approval applies that ID; the agent never edits a SafeLane baseline, policy YAML, or Kubernetes manifest.

## Install

macOS and Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/AndrewMaged814/SafeLane/main/docs/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/AndrewMaged814/SafeLane/main/docs/install.ps1 | iex
```

The release archive installs both the SafeLane binary and its canonical Claude/Codex
skill. Restart the agent session after installing or upgrading.

Build from source with Go 1.26.5 or later:

```bash
go build -o ./bin/safelane ./cmd/safelane
./bin/safelane --help
```

See the [documentation](https://andrewmaged814.github.io/safelane/) and [CLI reference](website/src/content/docs/reference/cli.md).

## License

[MIT](LICENSE)
