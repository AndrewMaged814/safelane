# SafeLane

SafeLane coordinates one approved, evidence-bound software release to a terminal outcome while keeping production exposure inside operator-declared rules.

## Language

**Release Request**:
The identity of one exact merged change and target environment: repository, pull request, and environment.
_Avoid_: Latest pull request, deployment request

**Change Dossier**:
The bounded evidence SafeLane constructs for assessment: exact diff, artifact, CI evidence, critical surfaces, approved runtime assertions, deterministic findings, and truncation state.
_Avoid_: Prompt context, caller-supplied evidence

**Safety Contract**:
The frozen, hashed agreement for one release attempt: artifact identity, hazards, runtime assertions, lane, progression authority, rendered bundle, and target.
_Avoid_: Rollout plan, model recommendation

**Hazard**:
A cited failure mode connected to an affected surface and a required runtime assertion.
_Avoid_: Vague concern, risk score

**Runtime Assertion**:
A concrete, executable claim about the canary-only application surface, such as response semantics, success rate, latency, or artifact identity.
_Avoid_: Health check, generic healthy metric

**Change Assessment**:
The combined deterministic and semantic account of hazards and reversibility. A semantic assessor may raise deterministic risk but never lower it or choose operations.
_Avoid_: Model decision, confidence score

**Release Lane**:
An operator-declared sequence of bounded traffic exposures. The canonical lanes are Fast (`50 → 100`), Standard (`25 → 50 → 100`), and Guarded (`25 → 50 → 75 → 100`).
_Avoid_: Model-chosen weights, caller-selected rollout

**Progression Authority**:
The maximum exposure already approved by the Safety Contract. It may stop before the end of a lane when a specific hazard is not covered.
_Avoid_: Kubernetes permission, blanket autonomy

**Uncovered Hazard**:
A Hazard with no approved Runtime Assertion that exercises its affected surface. It may require a hazard-specific risk acceptance or stop the release before exposure.

**Risk Acceptance**:
A durable, hazard-specific human decision to continue despite an Uncovered Hazard when policy allows it. It never records the hazard as covered.
_Avoid_: Override, ignore warning

**Release Attempt**:
One immutable Safety Contract and its reconciled execution history. Retrying creates a new attempt rather than mutating a terminal one.

**Release Run**:
The attached coordination loop that reconciles a Release Attempt, requests only authorized Argo progression, and remains attached until a terminal or decision-required outcome.
_Avoid_: Gate command, autonomous deployment engine

**Argo Abort**:
Argo Rollouts' response to failed runtime analysis. Argo owns analysis evaluation, traffic restoration, and normal-path rollback; SafeLane observes and records the outcome.
_Avoid_: SafeLane rollback

**Emergency Control**:
The explicitly separate pause, resume, or abort path with caller, timestamp, and reason. Normal Release Run never uses it to react to analysis failure.
_Avoid_: Normal progression, silent resume

**Release Proof**:
The durable Artifact, Assessment, Decision, Execution, Boundary, and Outcome record for one Release Attempt.
_Avoid_: Console log, deployment status

**Demo Application**:
The first-party SafeLane Demo API whose `/healthz` remains green while `/api/demo` can fail semantically, by availability, or by latency.
_Avoid_: Generic sample workload

**External Probe**:
The digest-pinned, permissionless Analysis Job that verifies canary identity and sends bounded requests only to the canary Service.
_Avoid_: `/api/analysis`, stable-traffic smoke test
