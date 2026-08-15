# SafeLane build-versus-integrate investigation

**Question:** Can Stakpak or another existing agent, combined with Argo Rollouts, narrow RBAC, admission policy, and existing AI canary analysis, provide the same user experience and safety guarantees without a separate SafeLane core?

**Research date:** 2026-08-15

## Verdict

The broad commercial-product hypothesis does **not** survive as a claim of wholly new deployment capability. “An agentic way of deploying rollouts” is already composable from existing tools:

1. Stakpak supplies an autonomous DevOps agent, operational Rulebooks, and Warden guardrails.
2. kagent supplies a Kubernetes-native agent runtime and MCP tools for Kubernetes, Argo, and observability.
3. Argo Rollouts supplies canary traffic progression, analysis, promotion, abort, and rollback.
4. `rollouts-plugin-metric-ai` plus `kubernetes-aiops-agent` supplies AI-driven canary analysis, promote/rollback decisions, root-cause analysis, and optional GitHub remediation.
5. Kubernetes RBAC plus ValidatingAdmissionPolicy, Kyverno, or Gatekeeper supplies resource admission and field constraints.

That stack can reproduce the **demo shape and much of the user experience** without SafeLane. A demo that only shows an agent submitting a canary, receiving an admission denial, and then promoting after an AI health check is therefore an integration exercise, not a distinct product.

For the hackathon, that is not a reason to stop. It is the starting constraint: build the composition, be explicit about what is reused, and use the demo to discover whether a small SafeLane-specific authority or Release Proof layer improves the workflow enough to keep. The prototype does not need to establish a complete production-grade product boundary.

The narrower hypothesis survives, conditionally:

> SafeLane is a release-authority layer that joins exact artifact identity, trusted engineering evidence, policy, and progressive exposure into one independently enforced, observable release boundary.

None of the named tools, in their documented role, provides that complete join. Argo owns rollout mechanics, the AI plugin owns canary judgment, admission tools validate individual requests, and agent runtimes execute tools. SafeLane would need to own only the missing release-specific authority and proof seam. If SafeLane does not own that seam, reframe it as a composition or kill the product boundary.

## What existing tools already solve

### Stakpak

Stakpak describes Rulebooks as Markdown SOPs that teach the agent an organization's way of working, including production deployment procedures. Its Autopilot runs continuously, fixes what is safe, and escalates when human input is needed ([Rulebooks](https://stakpak.gitbook.io/docs/how-it-works/rulebooks), [Autopilot](https://stakpak.gitbook.io/docs/how-it-works/autopilot)). Warden is described as a deterministic policy enforcer that inspects and blocks unsafe or unauthorized operations before they reach the environment ([Warden](https://stakpak.gitbook.io/docs/how-it-works/warden-guardrails)).

Stakpak therefore solves the agent experience and generic agent-runtime safety story. Its public documentation does not show an artifact-specific, per-step Kubernetes exposure grant or an independently recorded proof that actual traffic stayed within that grant. Its open agent repository invokes a separately downloaded Warden plugin rather than exposing the enforcement implementation ([Warden wrapper](https://github.com/stakpak/agent/blob/main/cli/src/commands/warden.rs)).

### kagent

kagent is a Kubernetes-native framework for defining agents, model configuration, MCP tools, and controllers as Kubernetes resources. Its built-in tools cover Kubernetes, Argo, Prometheus, Grafana, Istio, Helm, and related operations ([kagent README](https://github.com/kagent-dev/kagent/blob/main/README.md)). Its tools repository exposes Argo actions such as promote, pause, and set rollout image ([kagent tools](https://github.com/kagent-dev/tools)).

kagent solves agent hosting and tool integration. It does not define release evidence semantics, artifact-bound authority, exposure grants, or observed-compliance proof. A SafeLane implementation that is mostly an agent prompt plus kagent tools is not a separate product.

### `kubernetes-aiops-agent`

The project describes an autonomous Kubernetes agent that analyzes logs, events, and metrics, identifies root causes, creates GitHub pull requests, and integrates with Argo Rollouts for canary analysis ([repository](https://github.com/kdubois/kubernetes-aiops-agent)). It solves runtime diagnosis and remediation suggestions.

It does not independently authorize the deployment, bind its decision to an immutable artifact and upstream evidence, or enforce a maximum exposure. Its promote/rollback result is agent judgment consumed by the caller.

### `rollouts-plugin-metric-ai`

The plugin is an Argo metric provider that collects stable/canary context and delegates analysis to an A2A agent. The documented flow is: collect diagnostics, ask the agent to analyze, receive a structured promote/rollback response, let Argo promote or abort, then optionally create a GitHub issue or PR ([README and flow](https://github.com/argoproj-labs/rollouts-plugin-metric-ai)).

It directly covers the most obvious “agentic rollout” story. It does not provide an independent release authority. The agent's response becomes the metric result that Argo consumes; the plugin's documented inputs are rollout names and label selectors rather than a signed source-to-artifact evidence chain.

### Argo Rollouts

Argo Rollouts is already the correct execution engine: canary and blue-green strategies, weighted traffic shaping, metric analysis, automated promotion, abort, and rollback ([project README](https://github.com/argoproj/argo-rollouts)). SafeLane should not rebuild any of these mechanics.

Argo executes the Rollout and its configured AnalysisTemplates. It does not decide whether the caller was entitled to request a particular exposure envelope, nor does it independently join review evidence to an immutable artifact.

### Kubernetes admission, Kyverno, and Gatekeeper

Kubernetes ValidatingAdmissionPolicy evaluates CEL expressions over admission requests, supports parameter resources, can fail closed, and can emit a custom rejection message ([Kubernetes documentation](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/)). That is sufficient for static checks such as allowed namespaces, image digests, actor identity, field allowlists, and bounded rollout values.

Kyverno explicitly complements RBAC: RBAC controls who can access resources and Kyverno enforces additional policy compliance ([Kyverno README](https://github.com/kyverno/kyverno)). Gatekeeper provides validating and mutating admission webhooks, parameterized constraints, audit, and policy reporting ([Gatekeeper introduction](https://open-policy-agent.github.io/gatekeeper/website/docs/)).

These tools solve admission and policy enforcement. They do not, by themselves, provide a release lifecycle, evidence trust graph, per-artifact step authority, or an observed traffic-exposure ledger. They also cannot protect against a cluster owner who can change the policies or bypass the API server; that remains outside SafeLane's stated threat model.

## What can be directly reused

- **Agent experience:** Stakpak's autonomous interaction and Rulebook-like guidance; alternatively kagent's agent/MCP/A2A runtime.
- **Runtime analysis:** `rollouts-plugin-metric-ai` and `kubernetes-aiops-agent`, or an existing trusted probe already used by the application.
- **Rollout execution:** Argo Rollouts, including traffic routing, pauses, AnalysisRuns, promotion, abort, and rollback.
- **Access control:** Kubernetes RBAC and namespace isolation.
- **Admission:** Kubernetes ValidatingAdmissionPolicy for simple CEL checks; Kyverno or Gatekeeper when richer policy templates, audit, or external data are required.
- **Evidence:** existing GitHub review/check identifiers, SLSA/Sigstore provenance, and OCI manifest digests. SafeLane should not invent a competing evidence format or store.
- **Deployment risk:** evaluate DeployWhisper as the external provider for infrastructure/deployment-artifact analysis before designing any SafeLane risk engine. SafeLane should consume and conservatively normalize its report, not copy its orchestration layer.
- **Agent protocol:** MCP or A2A only as an adapter surface; neither protocol is itself a root of trust for release evidence.

## What SafeLane would still uniquely provide

SafeLane is justified only if it owns all four parts of this seam:

1. **Evidence-to-artifact join:** prove that the reviewed source revision and required checks apply to the exact OCI artifact being released.
2. **Release-specific authority:** issue or maintain a short-lived, target-bound authority for one artifact to receive only its permitted next exposure step.
3. **Independent enforcement:** make Kubernetes reject a rollout mutation that exceeds the authority, regardless of the planner's intent or the caller's proposed YAML.
4. **Observed-exposure proof:** record what actually happened, not merely what the Rollout spec requested, and tie that outcome to the artifact, policy, and runtime evidence.

The novelty is not “an agent can deploy,” “AI can analyze a canary,” or “Kubernetes can reject a resource.” The novelty is the **release authority state that connects those existing systems** and remains meaningful across progressive steps.

## What should be removed from SafeLane's scope

- A general-purpose DevOps or coding agent.
- A custom LLM canary-analysis engine.
- A new rollout controller, traffic router, probe system, or remediation engine.
- A proprietary evidence schema, provenance store, or signing system.
- A new deployment/infrastructure risk engine before evaluating DeployWhisper or other existing providers.
- A generic policy language competing with CEL, Kyverno, Gatekeeper, OPA, or Cedar.
- Warden-like generic command/network sandboxing as the primary product; use existing agent-runtime guardrails where useful.
- A dashboard, multi-agent/provider matrix, broad cluster support, and production platform hardening in the initial prototype.
- Detailed rollback/replay/state-machine features until the authority seam is shown to be necessary and distinct.

## The falsifying comparison

Build a composition with one existing agent, Argo Rollouts, the AI metric plugin, namespace RBAC, and fail-closed admission policy. If that composition can:

- accept a reviewed artifact and verify its evidence;
- allow only its permitted initial exposure;
- reject an excessive next step with an actionable response;
- let the agent correct and retry;
- allow Argo and an existing probe to promote or abort; and
- produce trustworthy proof of actual exposure,

then SafeLane's separate core is not justified. The work should be reframed as a reference integration or demo.

If the composition fails specifically at the evidence-to-artifact join, per-step authority, independent enforcement across the rollout lifecycle, or observed-exposure proof, SafeLane survives as that narrow release-authority component.

## Hackathon decision

**Proceed with a composition-first prototype.** SafeLane is currently a release-facing interface and experiment harness around existing systems, not a claim that it replaces them. Keep only the smallest SafeLane behavior that the demo proves useful: potentially an artifact/evidence-bound release handoff, a policy-aware next-step decision, or a Release Proof record. Treat each as a hypothesis to test, not a pre-committed controller architecture.

The next experiment should build the smallest composition and label every step as either **integrated** (agent, Argo, admission, analysis, provenance) or **SafeLane behavior** (the release-facing handoff and any authority/proof seam that earns its place). If the composition already provides the desired experience, keep SafeLane as a thin reference integration. If it fails at a narrow seam, add only that seam. Defer advanced replay protection, rollback compatibility, multi-cluster identity, and complete state-machine security until this vertical slice demonstrates a need for them.
