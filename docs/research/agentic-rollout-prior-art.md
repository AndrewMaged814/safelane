# SafeLane product boundary against agentic-delivery prior art

**Decision ticket:** [Decide whether SafeLane has a distinct product boundary](https://github.com/AndrewMaged814/safelane/issues/32)  
**Research date:** 2026-08-15  
**Positioning under test:** “SafeLane ships reviewed changes on autopilot, while keeping every release inside your rules.”

## Decision

**Keep SafeLane only around a narrow, testable boundary: artifact-bound progressive authority. Kill or reframe it if the prototype becomes only an agent that writes an Argo Rollout, watches metrics, or follows a deployment playbook.**

The settled positioning is clear and marketable, but it is not a distinct capability by itself. Stakpak already markets an autonomous DevOps agent that deploys software, follows team procedures, runs continuously, fixes safe problems, and puts deterministic guardrails around agent actions. Argo Rollouts already executes bounded canaries and promotes or aborts from runtime analysis. The Argo AI metric plugin and its reference AIOps agent already add LLM diagnosis, promotion judgment, and remediation. Sigstore and Kyverno already bind admission policy to image digests, signatures, and attestations. Spinnaker already models stateful, artifact-oriented delivery constraints including canary evaluation.

What remains uncovered **as one product contract** in the reviewed systems is:

> An autonomous deployment agent may plan and repair a rollout, but a separate cluster-side authority grants one exact artifact only the next bounded exposure step, rejects every caller that exceeds it, and records whether actual exposure stayed within that grant.

This is a differentiation by control seam and end-to-end experience, not by a novel underlying primitive. Its value is still a hypothesis: primary-source research can show the seam is not present in the reviewed products, but cannot show that teams will adopt another release control component.

## The exact boundary

SafeLane owns only this loop:

1. **Resolve the release subject.** Identify one reviewed source revision and one immutable deployable artifact.
2. **Evaluate release rules.** Consume existing review, CI, provenance, and runtime evidence; do not recreate their producers.
3. **Grant the next exposure envelope.** Produce an internal, machine-verifiable authorization for that artifact, environment, rollout step, and expiry.
4. **Enforce outside the agent.** Check attempted Argo Rollout mutations at a shared Kubernetes boundary, irrespective of whether Codex, Claude Code, CI, or a human submitted them.
5. **Return repairable denials.** Explain the violated rule and permitted boundary in structured form so the deployment agent can correct and resubmit without a human.
6. **Advance or stop.** Let Argo and existing probes execute and measure the canary; use their results as evidence for the next grant, promotion, or abort.
7. **Prove the boundary held.** Record granted versus observed exposure for the exact artifact.

The public product can simply say “your release rules.” The implementation must not collapse guidance and authority into a prompt. Following the useful Stakpak pattern, agent-facing guidance can help the planner, while a deterministic policy representation and a cluster-side enforcement point remain authoritative. A release permit/grant is an internal protocol object, not the product metaphor.

### Boundary test

SafeLane is inside its boundary only when all four statements are true:

- **Exact subject:** the authorization identifies an immutable artifact and its reviewed change, not a mutable tag, workload name, or label selector alone.
- **Progressive authority:** authorization is limited to the next exposure step rather than a binary “may deploy to production.”
- **Independent enforcement:** the submitting agent cannot change the grant, enforcement policy, or bypass path with the same credentials it uses to release.
- **Closed-loop recovery:** a rejection is actionable input to the agent, and the enforced outcome is recorded.

If any statement is removed, SafeLane collapses into an existing category: supply-chain admission, progressive delivery, or agent-runtime guardrails.

## Stakpak: the experience to follow, not the boundary to copy

Stakpak demonstrates a strong three-layer product experience:

| Layer | Stakpak behavior | Lesson for SafeLane |
| --- | --- | --- |
| Guidance | Rulebooks are Markdown SOPs with tags that the agent uses to decide when to load them; they encode a team's procedures and standards. | Make release configuration legible and easy to author, but do not treat prose as enforcement. |
| Autonomy | Autopilot runs continuously, detects changes, fixes what it considers safe, and alerts only when intervention matters. Profiles select allowed tools and auto-approval behavior. | Default to autonomous release planning, repair, and progression; human approval is a policy outcome, not a mandatory stage. |
| Enforcement | Warden is documented as a deterministic enforcer that intercepts agent actions before they reach the environment. The open CLI wraps a container with a separately downloaded Warden executable. Customizable Cedar guardrails are an Enterprise capability. | Put hard limits outside model judgment and make denial recoverable. Keep the SafeLane enforcement target release-specific. |

Primary sources: [Stakpak Rulebooks](https://stakpak.gitbook.io/docs/how-it-works/rulebooks), [writing a Rulebook](https://stakpak.gitbook.io/docs/how-it-works/rulebooks/how-to-write-a-rulebook), [Autopilot](https://stakpak.gitbook.io/docs/how-it-works/autopilot), [Warden Guardrails](https://stakpak.gitbook.io/docs/how-it-works/warden-guardrails), [edition comparison](https://stakpak.gitbook.io/docs/get-started/oss-vs-cloud-vs-enterprise), and the pinned open-source [Warden wrapper](https://github.com/stakpak/agent/blob/760cd2b5984d29c2d513bb15ca33e995fae45f17/cli/src/commands/warden.rs).

### Direct overlap

Stakpak's README promises to generate infrastructure, debug Kubernetes, configure CI/CD, and automate deployments while using Warden to block destructive operations. Its Autopilot docs explicitly include investigating failed deployments and suggesting or applying fixes. A SafeLane implementation made primarily of deployment instructions, tools, schedules, and generic action blocking would therefore be a narrower Stakpak profile or Rulebook, not a separate product ([Stakpak README](https://github.com/stakpak/agent/blob/760cd2b5984d29c2d513bb15ca33e995fae45f17/README.md), [Autopilot](https://stakpak.gitbook.io/docs/how-it-works/autopilot)).

### Remaining separation

The public Stakpak documentation describes Warden at the **agent-action/network boundary**. Its open repository delegates enforcement to a downloaded executable and exposes wrapping, volume, and logging integration; the policy engine itself is not inspectable there. The reviewed public sources do not document a release object bound to a source revision, image digest, environment, and maximum traffic exposure, nor enforcement against changes submitted by callers outside the wrapped agent runtime.

That is the meaningful SafeLane separation: **Stakpak constrains what its agent can do; SafeLane's candidate boundary constrains what any caller may expose for this exact release.** This is a conclusion about documented product contracts, not a claim that Stakpak could not add such a feature.

## Capability ownership matrix

| Capability | Existing owner | Implication for SafeLane |
| --- | --- | --- |
| Canary/blue-green execution, traffic weights, pauses, analysis, promotion, abort | Argo Rollouts | Integrate; never rebuild. |
| AI canary diagnosis and promote/abort result | `rollouts-plugin-metric-ai` + `kubernetes-aiops-agent` | Treat agent judgment as evidence, not final authority. |
| General Kubernetes agent runtime, MCP/A2A tools, optional human tool approval | kagent | Do not build an agent framework. SafeLane should be callable from existing agents. |
| Autonomous DevOps operation, procedural knowledge, action guardrails | Stakpak | Copy the simple experience and separation of guidance from enforcement; specialize the enforcement contract. |
| Image digest resolution, signature/attestation verification, admission policy | Sigstore policy-controller; Kyverno | Reuse for identity/evidence verification. Artifact binding alone is not novel. |
| Artifact promotion under stateless and stateful environment constraints, including canary analysis | Spinnaker Managed Delivery | “Policy-gated artifact promotion” is not novel. SafeLane must prove its agent-native repair plus cluster-enforced exposure envelope. |
| Per-release, per-step exposure grant enforced against every submitting identity, with repair feedback and observed-exposure proof | No reviewed project exposes the complete contract | Candidate SafeLane boundary. |

## Closest technical prior art

### Argo Rollouts

Argo Rollouts is the execution engine. It supplies canary and blue-green strategies, fine-grained traffic shifting, external analysis, promotion, abort, and rollback. Canary steps are declared in the submitted Rollout spec; failed analysis can abort and return traffic to stable ([project README](https://github.com/argoproj/argo-rollouts/blob/f2c5c2b51ff5ef0b071fcf9883614907aa055c52/README.md), [analysis documentation](https://github.com/argoproj/argo-rollouts/blob/f2c5c2b51ff5ef0b071fcf9883614907aa055c52/docs/features/analysis.md)).

Argo enforces the rollout configuration it receives. The reviewed sources do not establish that the submitter was entitled to choose that image, step weight, pause, or analysis template. SafeLane is distinct only if its authority sits outside the caller-controlled Rollout spec and restricts changes to that spec.

### Argo AI metric plugin and Kubernetes AIOps agent

The metric plugin sends rollout identity and stable/canary selectors to an A2A agent, receives a structured `promote` decision, and maps it to an Argo measurement success or failure. The reference Kubernetes AIOps agent collects logs, events, state, and metrics; an LLM workflow returns the promotion judgment and can open a GitHub issue or pull request for remediation ([plugin A2A client](https://github.com/argoproj-labs/rollouts-plugin-metric-ai/blob/2ed36703f382a714bc111c02e037c3cb0cb93bb7/internal/plugin/a2a.go), [plugin result mapping](https://github.com/argoproj-labs/rollouts-plugin-metric-ai/blob/2ed36703f382a714bc111c02e037c3cb0cb93bb7/internal/plugin/plugin.go), [AIOps analysis agent](https://github.com/kdubois/kubernetes-aiops-agent/blob/148b446d0900349032f428df4400f48addb06e2f/src/main/java/dev/kevindubois/rollout/agent/agents/AnalysisAgent.java), [workflow](https://github.com/kdubois/kubernetes-aiops-agent/blob/148b446d0900349032f428df4400f48addb06e2f/src/main/java/dev/kevindubois/rollout/agent/workflow/KubernetesWorkflow.java)).

This already invalidates “AI evaluates a canary and decides promotion” as SafeLane's product. The request identifies a rollout and label selectors, not a reviewed revision and immutable artifact; the plugin trusts the remote agent's Boolean as the metric result. SafeLane's agent may interpret the same evidence, but only a deterministic policy decision can expand authority.

### kagent

kagent provides Kubernetes-native agents, A2A, MCP tool servers, operational integrations including Argo and Prometheus, persistent sessions, and optional approval for individual tool calls. Authority comes from configured tools and Kubernetes RBAC; approval is a generic agent-tool protocol ([kagent README](https://github.com/kagent-dev/kagent/blob/7beafb75b4ec9f9a69cc67a9c6ed2cc57904a2cd/README.md), [architecture](https://github.com/kagent-dev/kagent/blob/7beafb75b4ec9f9a69cc67a9c6ed2cc57904a2cd/docs/architecture/README.md), [human-in-the-loop protocol](https://github.com/kagent-dev/kagent/blob/7beafb75b4ec9f9a69cc67a9c6ed2cc57904a2cd/docs/architecture/human-in-the-loop.md)).

SafeLane should not ship its own generic planner, memory, A2A runtime, or Kubernetes tool suite. Its contract should work when invoked by kagent, Stakpak, Codex, Claude Code, Cursor, or CI.

### Supply-chain and admission policy

Sigstore policy-controller already resolves image tags to digests, validates signatures and attestations, evaluates CUE or Rego policy over attestations, and can include Kubernetes object metadata and spec in policy evaluation. Kyverno image rules likewise verify attestations and mutate tags to digests by default ([Sigstore policy-controller](https://docs.sigstore.dev/policy-controller/overview/), [Kyverno verifyImages](https://kyverno.io/docs/policy-types/cluster-policy/verify-images/overview/)). Kubernetes validating admission policies and webhooks can reject objects before persistence ([Kubernetes admission control](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)).

Therefore SafeLane cannot claim “policy verifies evidence for an exact artifact” or “Kubernetes rejects an unsafe manifest” as a unique primitive. Its candidate contribution is generating and enforcing a **short-lived, stateful exposure envelope** whose allowed next state changes as trusted rollout evidence arrives, plus the agent recovery protocol and exposure proof around that envelope.

### Spinnaker Managed Delivery

Spinnaker's environment constraints directly weaken a broad SafeLane claim: it models artifact promotion through stateless and stateful constraints, can deploy an artifact for canary analysis, and gates further promotion on the result. Multiple constraints must pass before promotion ([environment constraints](https://spinnaker.io/docs/guides/user/managed-delivery/environment-constraints/), [automated canary analysis](https://spinnaker.io/docs/guides/user/canary/)).

SafeLane is not distinct merely because policy, artifacts, and canaries are connected. The remaining differentiation is the autonomous engineering-agent interface, machine-actionable correction of rejected rollout intents, and enforcement at the target cluster against every caller rather than solely inside a delivery orchestrator's desired-state loop.

## Architecture consequences

This decision substantially narrows the prototype:

- **Agent is planner, not sovereign.** It gathers evidence, proposes rollout steps, interprets denials, and repairs manifests.
- **Release rules have an enforceable core.** Friendly prose may guide agent behavior, but hard limits must compile to or be expressed as deterministic data/policy.
- **The grant is generated and internal.** Avoid making humans manage “permits”; the autonomous flow creates them from trusted evidence.
- **Kubernetes credentials preserve separation.** The releasing identity may create/update the demo Rollout but may not change SafeLane policy, grants, admission configuration, traffic-routing resources directly, or the enforcer itself.
- **Existing tools retain their jobs.** Argo executes; existing CI/review/provenance systems produce evidence; an existing probe evaluates runtime health.
- **Proof must use observed state.** Repeating the requested or granted weight is not proof. The demo must record the exposure state observed from the rollout/traffic boundary.

## Hackathon proof and kill criteria

The smallest convincing proof of the distinct boundary is:

1. SafeLane receives an immutable artifact plus pre-existing review/test evidence.
2. It autonomously proposes an Argo canary exceeding the allowed initial exposure.
3. A cluster-side SafeLane enforcement point rejects it with a structured allowed maximum.
4. The agent corrects and resubmits without human intervention.
5. Argo runs the bounded canary and an existing probe returns runtime evidence.
6. SafeLane advances or aborts according to deterministic release rules.
7. The record compares the artifact-bound grants with observed exposure over time.

Kill or reframe the product if the prototype can be honestly described as any of the following:

- “a Stakpak Rulebook/profile for deploying with Argo”;
- “the Argo AI metric plugin with a nicer agent prompt”;
- “Kyverno/Sigstore policy that checks an image and Rollout spec”;
- “Spinnaker-style delivery constraints exposed through chat”;
- “an agent whose own credentials or prompt are the safety boundary.”

Also reframe if a bypassing caller can achieve the unsafe exposure, if the grant is not bound to the deployed digest, or if the final proof reports intended rather than observed exposure.

## What this research does not prove

- It does not prove demand, willingness to install a cluster component, or preference over existing delivery platforms.
- It does not choose Rego, Cedar, Kyverno, a validating webhook, or another implementation.
- It does not establish the evidence-chain format or prove that traffic exposure can be observed accurately for every Argo integration.
- Stakpak Warden's engine is not present in the reviewed open repository, so conclusions about its boundary are limited to its public documentation and wrapper integration.

Those are separate validation and architecture decisions. This ticket establishes only that SafeLane has a coherent candidate boundary after duplicated capabilities are removed—and that the boundary disappears immediately if deterministic per-release cluster authority is treated as optional.
