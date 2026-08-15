# SafeLane 20-minute progress slice

**Due 26 August 2026.**

## Required coherent slice

The progress meeting succeeds when the team can show this chain:

1. A real Podinfo PR in the public fork was approved by Ahmed and merged to `main`.
2. A minimal GitHub Actions workflow ran for that merge commit and published an image to a public GHCR package, exposing an immutable OCI digest.
3. Codex, Claude Code, CI, or another caller invokes the neutral SafeLane CLI, submitting release identity and evidence only.
4. SafeLane verifies the merged commit on `main`, the publish workflow on that commit, the immutable digest, and independent PR approval when configured.
5. SafeLane renders the deployment bundle from the operator-owned Release Template, pinned to the verified digest, and hashes every rendered resource. It does not scan that YAML.
6. SafeLane records Release Eligibility from that evidence. It does not treat evidence completeness as risk and does not choose an envelope from it.
7. SafeLane returns eligibility, and, when eligible, the operator's static rollout envelope, then persists the Release record.
8. `safelane proof <release-id>` shows populated Artifact and Decision proof, with Execution and Boundary explicitly pending.
9. SafeLane patches the pre-created `podinfo` Rollout to the verified digest through its constrained, patch-only controller identity.
10. Argo executes a genuine first canary stage against the pre-existing baseline version.
11. A restricted caller identity attempts a direct mutation of the protected Rollout, receives `Forbidden`, and the protected state is shown unchanged.

Ahmed pre-creates the Rollout at a baseline image as a provisioning step. This is what makes step 10 possible at all: Argo does not execute canary steps on a Rollout's initial creation, so a release that *created* the Rollout would jump to 100% and show no canary. It also lets the controller identity drop `create` entirely and hold only patch on the named resource.

Step 11 needs no SafeLane code and runs from Ahmed's environment. It is the demonstration's strongest minute — the half of the claim that survives an agent ignoring guidance. If the cluster is not ready, steps 1–8 still stand on their own; do not block the meeting on full cluster execution.

## Deliberately deferred from this slice

Present these as roadmap, not as gaps:

- scanning the rendered YAML — SafeLane renders the bundle and proceeds from GitHub and GHCR evidence;
- the HTTP API and MCP adapter — the CLI is the surface; the intake boundary stays transport-neutral;
- build-provenance and attestation verification — required evidence is merged commit, passing publish workflow, and immutable digest; independent approval only when configured;
- risk-based or dynamically chosen rollout envelopes;
- Execution and Boundary proof ingestion, which waits for real Argo and runtime evidence.

## Stretch demonstrations

Treat these as strong progress or roadmap items, not meeting blockers:

- full autonomous promotion;
- precise traffic observation;
- runtime AI-assisted analysis;
- unsafe-transition rejection and recovery; and
- complete Execution and Boundary proof.

## Presentation note

Twenty minutes is more airtime than this slice needs, and the failure mode is over-narration rather than thin content. Budget for one live release command, one live denial, and an explicit account of what is integrated, what is SafeLane's, and what is next.

## Final prototype bar

The final prototype succeeds only when it additionally demonstrates:

- healthy autonomous progression to 100%;
- runtime analysis at policy-defined stages;
- direct-bypass resistance with a restricted caller identity;
- rejection and automatic recovery from an unsafe transition; and
- complete Artifact, Decision, Execution, and Boundary proof.

Any unfinished portion is reported explicitly as a roadmap item rather than treated as a failure of the entire effort.
