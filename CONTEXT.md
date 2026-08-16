# SafeLane

SafeLane coordinates autonomous software releases while keeping production exposure within declared release rules.

## Language

**Autonomous deployer**:
The agent identity that initiates and adapts a release. It is treated as fallible and restricted, not as an owner of the Kubernetes control plane.
_Avoid_: Cluster administrator, trusted release authority

**Release Request**:
A caller's submission of one change identity and one environment selector: repository, pull request, and optionally environment. It does not carry evidence, cluster or namespace, reviewers, CI results, digests, or bookkeeping. SafeLane collects and verifies those itself.
_Avoid_: Evidence dossier, release-evidence.json, deployment request

**Reviewed Change**:
A change merged into the demo fork after approval by the other hackathon teammate and a passing required CI check. The approval and CI result are evidence inputs, not SafeLane-generated claims.

**Release Evidence**:
The linked evidence for one Release Request: the merge commit SHA on `main`, the required publish-workflow result for that exact commit, the immutable OCI image digest, and an independent pull-request approval when the Release Policy requires one. Trusted build provenance is a planned future addition to this set, not a current requirement.

**Rendered Manifest Bundle**:
The exact final Kubernetes object bytes SafeLane renders and intends to apply for one release: the Argo Rollout with its immutable-digest pod template, stable/canary Services, any active traffic-routing object, the AnalysisTemplate, and materially relevant non-secret referenced resources. SafeLane renders it from the operator-owned Release Template pinned to the verified digest; the caller never supplies it. Each rendered object is content-hashed and bound to the Release, and the same bundle is later applied. Runtime status and observed traffic are separate evidence.
_Avoid_: Caller-submitted manifests, agent-authored bundle

**Infrastructure Workstream**:
Ahmed's owned execution environment for the prototype: cluster selection, Argo Rollouts, routing, namespace-scoped RBAC, image pull access, reachable endpoint, and runtime execution. SafeLane depends on its handoff but does not own cluster provisioning or broad administration.

**SafeLane Workstream**:
The release-facing behavior owned by SafeLane: GitHub and artifact evidence checks, trusted bundle rendering, Release Eligibility, Release Policy, artifact-bound release authority, neutral agent interaction, independent enforcement contract, and Release Proof.

**Guidance Layer**:
Agent-specific instructions and discovery material that teach when and how to call SafeLane, including managed sections in AGENTS.md/CLAUDE.md, `.safelane/agent-guidance.md`, skills, Paks, or MCP descriptions. Guidance changes behavior but is never a security boundary.

**Tool-Shaping Layer**:
The small typed SafeLane CLI/API/MCP surface offered to callers, with competing direct production-deployment tools withheld from the protected release agent where possible. Tool shaping improves the path agents discover; it does not authorize them.

**Enforcement Layer**:
The deterministic SafeLane controller, constrained cluster identity, RBAC, and independent admission policy that decide and enforce the allowed release path. This layer remains authoritative when guidance is ignored.

**Managed Integration Section**:
A clearly delimited block in an existing AGENTS.md or CLAUDE.md file that SafeLane may add or update without overwriting unrelated user content. Ambiguous files are left unchanged and receive a separate integration file instead.

**Restricted Caller Identity**:
The caller's namespace-scoped Kubernetes identity for observability only: it may read the protected Rollout and required status, but cannot create, patch, promote, replace, or delete protected workloads, Services, or traffic-routing resources. SafeLane's constrained execution identity is distinct.

**Live Release Summary**:
The 10–15 second human-readable Release Proof view: release ID, application/environment, caller, review/merge/digest evidence, Release Eligibility, static rollout envelope, stage health, direct-bypass result, and final promotion or abort.

**Complete Release Record**:
The machine-readable proof retrieved with `safelane proof <release-id> --details` or `--json`, containing the four proof sections—artifact, decision, execution, and boundary—with provenance, all CI checks, manifest hashes, AnalysisRun details, traffic samples, denial metadata, caller identity, and timestamps.

**Progress Vertical Slice**:
The smallest coherent 20-minute meeting demonstration, due 26 August 2026: a reviewed Podinfo PR is merged, CI publishes an immutable digest, an agent calls the neutral SafeLane interface, GitHub/artifact evidence is validated, SafeLane renders the trusted bundle from the operator-owned template, SafeLane records Release Eligibility and attaches the operator's static rollout envelope only when eligible, and SafeLane creates a Release with Artifact and Decision proof. A restricted caller is shown to be denied direct production mutation. If the cluster is ready, SafeLane also patches the pre-created Rollout to the verified digest and Argo begins canary stage one against the baseline version. SafeLane does not scan the rendered YAML.

**Final Prototype**:
The complete demonstration that adds healthy autonomous progression to 100%, policy-defined runtime analysis, direct-bypass resistance, unsafe-transition rejection and recovery, and complete Artifact/Decision/Execution/Boundary proof.

**Agentic Planner**:
The unprivileged SafeLane component that gathers evidence and proposes artifact-specific release transitions. Its output is advice, never production authority.
_Avoid_: Release authority, deployment executor

**Release Controller**:
The small deterministic SafeLane authority that validates proposed release transitions and applies only those allowed by the active Release Policy through a constrained production identity.
_Avoid_: Agent, general Kubernetes controller

**Release Eligibility**:
Whether one exact Release Request may enter SafeLane. The outcomes are eligible, ineligible, and indeterminate. Evidence completeness is not release risk and does not choose a rollout envelope.
_Avoid_: Risk tier, severity, unknown status, policy decision from evidence

**Release Policy**:
The operator-owned rules that name required evidence, optional checks, and the static rollout envelope an eligible release may follow. Later extensions may add risk-based envelopes; evidence completeness does not imply them.
_Avoid_: Rulebook, prompt, risk engine

**Release Transition**:
A typed proposal to move one release between allowed lifecycle states, such as Start, Advance, Pause, Resume, or Abort. It carries intent rather than Kubernetes configuration.
_Avoid_: Command, manifest, patch

**Release Template**:
The immutable, operator-owned definition from which the Release Controller renders an application's Rollout. A Release pins its exact template content by digest.
_Avoid_: Agent-generated manifest, mutable template

**Emergency Control**:
A privileged path outside normal release execution that may pause, stop, or tighten an active Release but may never widen its authority or weaken its pinned constraints.
_Avoid_: Override, policy bypass

**Open-source Composition**:
The hackathon baseline assembled from an existing agent, SafeLane's release-facing interface, Argo Rollouts, Kubernetes admission, and existing runtime analysis. It is the reference system SafeLane must improve or honestly package.

**Release Proof**:
The concise record showing which artifact was released, which eligibility and static envelope were recorded, what exposure outcome Argo observed, and which parts came from integrated systems versus SafeLane behavior.

**Vertical Slice**:
One end-to-end demonstration in which a real reviewed change earns its way from a SafeLane release request to 100% production: SafeLane starts a bounded canary, one unsafe transition is rejected and corrected, existing canary analysis proves health, and the result is recorded as Release Proof. A faulty artifact may extend the demo with abort and diagnosis but is not required for the core path.

**Demo Application**:
The existing public `stefanprodan/podinfo` application, used as the substrate for the first vertical slice because it supplies tests, deployment packaging, health/readiness endpoints, metrics, and a small visible-change surface.
