# Kubernetes enforcement feasibility for SafeLane

## Decision

**Proceed with the hackathon demonstration, but narrow the enforcement claim.** Kubernetes can reject an Argo Rollout whose exact image digest, rollout strategy, or Kubernetes-mediated traffic configuration exceeds an artifact-bound release boundary. It can do this on create, update, status-subresource operations, rollback, and controller-created resources, with a useful rejection message. It cannot, by admission policy alone, prove that the literal fraction of real production requests never exceeded that boundary.

The defensible prototype claim is:

> For a least-privilege release agent and a selected Kubernetes-native traffic router, every admitted configuration change that could expose the release remains bound to the permitted image digest and configured exposure limit. SafeLane separately records observed rollout and traffic evidence.

Do **not** claim protection from cluster administrators, node or etcd access, direct changes to an out-of-cluster traffic plane, or a compromised Argo controller. Do not call configured traffic weight proof of actual request-level exposure.

This result does not require SafeLane to become an approval gate. SafeLane can still autonomously plan, correct, promote, or abort releases; deterministic enforcement sits underneath that control loop.

## Why enforcement is possible

Kubernetes admission runs after authentication and authorization but before an API mutation is persisted. A rejecting validator rejects the entire request and returns the error to the caller ([Kubernetes admission-controller lifecycle](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#what-are-they)). Admission requests include the operation, user identity, old and new objects, and the requested subresource, so a policy can validate both the desired object and the transition that produced it ([AdmissionReview request](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#request)).

For the single-app demo, an artifact-bound boundary can be represented by a protected, namespaced parameter resource containing at least:

- target namespace and Rollout name;
- release identifier and immutable container image digest(s);
- allowed rollout strategy and traffic router;
- maximum exposure allowed in the current release phase;
- the trusted identities allowed to change release phase or Rollout status;
- parameter UID/generation, validity interval, and terminal state.

A policy can require the Rollout to reference that boundary, require digest-pinned images, reject `setWeight`, header, mirror, experiment, blue-green, no-step, or full-promotion paths outside it, and protect relevant child and routing resources. Kyverno can additionally resolve tags to digests and verify signatures or attestations; its documentation explicitly treats a digest as the immutable identity that keeps the deployed image equal to the image that was scanned ([Kyverno image verification](https://kyverno.io/docs/policy-types/cluster-policy/verify-images/overview/)).

This validates **configuration**, not traffic reality. Argo itself documents that basic canary routing uses replica ratios and is only approximate; fine-grained routing is delegated to ingress or service-mesh resources ([Argo basic usage](https://argoproj.github.io/argo-rollouts/getting-started/), [traffic management](https://argoproj.github.io/argo-rollouts/features/traffic-management/)).

## Lifecycle coverage

| Transition | What actually changes | Minimum enforcement |
| --- | --- | --- |
| Create | A `Rollout` is admitted; Argo later creates ReplicaSets and possibly AnalysisRuns and routing resources. | Validate target, digest, strategy, every reachable exposure step, analysis requirement, and traffic provider on the Rollout. Also validate controller-created ReplicaSets and the selected routing resource so a compromised or misconfigured parent path is not the only control. |
| Update | The Rollout pod template, strategy, pauses, services, or router references can change. | Compare `oldObject` and `object`; forbid release-id/digest rebinding, newly introduced bypass paths, and changes beyond the current boundary. Protect or reject deletion of the boundary while a release is active. |
| Promote | The CLI commonly patches `rollouts/status`, clearing pause conditions, advancing `currentStepIndex`, or setting `promoteFull`; it may also patch `spec.paused`. | Match both `rollouts` **and** `rollouts/status`; restrict status writers and permitted field diffs; always enforce the resulting ReplicaSet/routing mutation. A rule matching only `rollouts` misses this path. |
| Abort | The CLI patches `status.abort=true`; the controller restores stable routing and scales down canary according to the strategy. | Permit the narrow abort transition, deny unrelated status changes, and validate the resulting route/service/scale writes. An abort is normally exposure-decreasing, but the remaining pod template still names the new version and can resume later. |
| Rollback | `undo` patches `spec.template` (or a referenced Deployment, ReplicaSet, or PodTemplate). Reapplying a stable manifest can fast-track, and `rollbackWindow` can skip steps. | Treat rollback as a new artifact transition unless the stable digest has an explicit emergency/full-exposure grant. Validate workload references too. Do not assume an older revision is authorized merely because Argo calls it stable. |

The promotion and abort details above are not hypothetical. Argo's current CLI source patches the status subresource for step advancement and `promoteFull` ([promote source](https://github.com/argoproj/argo-rollouts/blob/master/pkg/kubectl-argo-rollouts/cmd/promote/promote.go)) and patches `status.abort` for abort ([abort source](https://github.com/argoproj/argo-rollouts/blob/master/pkg/kubectl-argo-rollouts/cmd/abort/abort.go)). Its undo implementation restores a prior pod template onto the Rollout or its workload reference ([undo source](https://github.com/argoproj/argo-rollouts/blob/master/pkg/kubectl-argo-rollouts/cmd/undo/undo.go)). Argo also documents that a rollback window skips normal steps and analysis for recent revisions ([rollback window](https://argoproj.github.io/argo-rollouts/features/rollback/)).

Subresources require deliberate webhook matching. Kubernetes specifies that `"*"` matches resources but **not** subresources; `"*/*"`, `"rollouts/*"`, or an explicit `"rollouts/status"` is needed ([webhook matching rules](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#matching-requests-rules)). This is a high-probability bypass in a naive policy because Argo's promote and abort commands write status directly.

## Policy-engine choice

### Native ValidatingAdmissionPolicy (CEL): recommended for the demo

Use Kubernetes `ValidatingAdmissionPolicy` plus a protected parameter CRD and binding for the one demo application. It is stable since Kubernetes 1.30, evaluates in-process rather than making an HTTP call, supports `object`, `oldObject`, request/user data, parameters, denial/audit actions, and computed `messageExpression` responses ([Kubernetes VAP](https://kubernetes.io/docs/reference/access-authn-authz/validating-admission-policy/)). Configure `failurePolicy: Fail` and `parameterNotFoundAction: Deny`.

The limitation is important: a binding points to a parameter by name or selector, and every matching binding/parameter evaluation must pass. Native VAP cannot simply dereference an arbitrary permit name from a Rollout annotation. For a one-app demo, use one fixed boundary parameter and binding per target, with CEL match conditions tying it to the target name. General multi-release use would require binding lifecycle management or a webhook/policy engine that can dynamically look up the referenced release boundary.

### Kyverno: best existing-tool alternative

Kyverno can deny matching admission requests in `Enforce` mode with custom messages, use Kubernetes API calls as policy context, match names and labels, produce reports/events, and verify image digests, signatures, and attestations ([validation](https://kyverno.io/docs/policy-types/cluster-policy/validate/), [external data sources](https://kyverno.io/docs/policy-types/cluster-policy/external-data-sources/), [how Kyverno works](https://kyverno.io/docs/introduction/how-kyverno-works/)). That makes dynamic lookup of a release-boundary resource easier and aligns with the principle of integrating an existing tool.

The cost is another webhook/control-plane dependency, Kyverno-specific policy and RBAC, exception surfaces, and more moving parts in the demo. Background scans and policy reports are detection, not prevention; the admission rule must be enforced and fail closed. Kyverno is attractive if the demo already needs its Sigstore/image-attestation features, but it is not necessary to prove the core rejection flow.

### OPA/Gatekeeper: capable, but a poor default for this prototype

Gatekeeper can express the rules in Rego and returns constraint violation messages; `enforcementAction: deny` is its default ([Gatekeeper constraints](https://open-policy-agent.github.io/gatekeeper/website/docs/howto/)). It is useful when an organization already operates Gatekeeper.

Its stock posture is dangerous for SafeLane's promise: Gatekeeper documents that it defaults admission errors to `failurePolicy: Ignore`, so constraints are unenforced while the webhook is unavailable, with audit expected to detect violations later ([Gatekeeper failing closed](https://open-policy-agent.github.io/gatekeeper/website/docs/failing-closed/)). Its common webhook rule uses `resources: ["*"]`, which excludes status subresources under Kubernetes matching semantics. Referential rules also depend on replicated inventory or an external provider, creating freshness or availability concerns; Gatekeeper audit is periodic and may omit individual violations, so it cannot prove prevention ([Gatekeeper audit](https://open-policy-agent.github.io/gatekeeper/website/docs/audit/), [external data](https://open-policy-agent.github.io/gatekeeper/website/docs/externaldata/)).

If selected, explicitly add status and required routing subresources, set `failurePolicy: Fail`, remove namespace/exemption bypasses, and protect Gatekeeper configuration. This is more ceremony than native CEL for one demonstration.

### Cedar: not the enforcement foundation

Cedar is an authorization language and engine: an integrating component must map the admission request to principal/action/resource/context, supply entity data and policies, and enforce the result ([Cedar authorization model](https://docs.cedarpolicy.com/auth/authorization.html)). The official `cedar-access-control-for-k8s` project supplies such authorization and admission webhooks, but its own README says it is not production-ready and should be an additional access-control tool rather than a replacement for Gatekeeper or Kyverno for resource restrictions; it specifically notes limitations for collection-style container constraints ([Cedar for Kubernetes](https://github.com/cedar-policy/cedar-access-control-for-k8s)).

Cedar is interesting later for human-readable authorization such as who may promote which environment. It adds no hackathon advantage for validating nested Rollout steps, digests, and router weights.

## Bypasses and trust assumptions

### Deployer privilege

| Deployer capability | Result |
| --- | --- |
| Only update the named Rollout through its main resource; no status, routing, Service, ReplicaSet, policy, or boundary writes | Strongest and simplest enforcement. SafeLane/controller performs transitions. |
| May promote/abort through `rollouts/status` | Feasible only with explicit status admission rules and strict allowed field transitions. Ordinary RBAC cannot constrain fields. |
| May edit Services, Ingress/Gateway/mesh CRDs, ReplicaSets, Deployments, Pods, HPA, or workload references | Each alternate exposure surface must be denied or independently bound to the same release boundary. Missing one invalidates the claim. Namespace boundaries alone are weak. |
| May edit the boundary, policy/binding, webhook configuration, Kyverno/Gatekeeper exceptions, namespaces/exemption labels, or RBAC | Can disable or rewrite enforcement. These must be platform-admin-only and ideally managed outside the release namespace. |
| `cluster-admin`, `system:masters`, impersonation/escalation/bind privileges, controller credentials, node/kubelet, or etcd access | Out of threat model. Kubernetes warns that `system:masters` bypasses authorization checks, webhook configuration is security-sensitive, and node proxy/kubelet and direct etcd access bypass admission ([RBAC good practices](https://kubernetes.io/docs/concepts/security/rbac-good-practices/), [API-server bypass risks](https://kubernetes.io/docs/concepts/security/api-server-bypass-risks/)). |

### Traffic-routing boundary

Argo modifies Services and provider-specific resources as it progresses. These writes are visible to admission when the provider is Kubernetes-native. The demo should select exactly one provider and enumerate its resources—for example, Istio `VirtualService` plus stable/canary Services—rather than claim generic router support.

Do not use Argo's third-party traffic-router plugin path for the enforcement proof. The plugin interface can perform `SetWeight` outside Kubernetes objects, and `VerifyWeight` is explicitly optional ([plugin reconciler source](https://github.com/argoproj/argo-rollouts/blob/master/rollout/trafficrouting/plugin/plugin.go)). Admission cannot intercept an external API side effect. Multi-cluster Istio also allows the Rollouts controller to patch a remote primary cluster, so the policy must exist at that actual enforcement point ([Argo Istio integration](https://argoproj.github.io/argo-rollouts/features/traffic-management/istio/)).

Header routes, mirror routes, experiments, direct canary-Service access, alternate Ingress/Gateway routes, and non-HTTP channels can expose more than a simple `setWeight` suggests. The safe demo policy should forbid them rather than attempt a generic exposure calculation. Argo itself states it does not control traffic flows it does not understand, such as binary or queue channels ([Argo concepts](https://argoproj.github.io/argo-rollouts/concepts/)).

## TOCTOU and failure behavior

Admission evaluates one API request against state available at that moment. The boundary lookup and the admitted mutation are not an atomic consume operation. Important races remain:

1. A valid Rollout can be admitted immediately before its boundary expires or is revoked and then remain stored without another admission event.
2. A boundary can change between parent Rollout admission and later controller writes.
3. Promotion status can be admitted, followed by a router write that is denied; the Rollout may be left paused/degraded between states.
4. The traffic controller and data plane converge after the Kubernetes object changes; admission proves only the accepted desired state.
5. Concurrent promotions can each observe the same phase. Admission does not provide a one-shot permit transaction across multiple resources.

Mitigate these by making a release boundary immutable, binding objects to its UID/generation and digest, requiring resource-version preconditions on SafeLane-controlled phase changes, and using a trusted reconciler to observe expiry/revocation and drive abort. Still validate every child/routing write; never exempt the Argo controller merely because it is trusted. A reconciler closes the persistence gap but is not instantaneous, so revocation is not a zero-time guarantee.

Fail closed. Kubernetes webhooks can return `Ignore` or `Fail` on timeouts/network errors, while an explicit `allowed: false` always denies; webhook timeouts are bounded ([failure policy](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy), [timeouts](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#timeouts)). Native VAP avoids a network hop but still needs `failurePolicy: Fail`, a denying missing-parameter action, and protected policies/bindings. Fail-closed protection trades release availability for safety and needs a documented recovery path.

## Actionable rejection contract

Actionable denials are buildable without a dashboard. A webhook may set the returned HTTP code and status message, and Kubernetes returns it to the caller ([webhook response](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#response)). VAP `messageExpression` can include object and parameter values; Kyverno and Gatekeeper also emit custom violation messages.

For autonomous correction, use a stable, terse message contract rather than prose alone, for example:

```text
SAFELANE_EXPOSURE_LIMIT: Rollout/store-api step[2].setWeight=50 exceeds allowed=10 for release=rel-123 artifact=sha256:...; set weight <=10 or request a new boundary
```

Include a stable reason code, exact JSON/YAML field, observed value, allowed value, release id, artifact digest, and correction. For a direct Rollout create/update, the agent receives this synchronously. If a later Argo-controller write is denied, the immediate client is the controller, not the engineering agent; SafeLane must watch Rollout conditions/events and controller reconciliation errors to relay the rejection. Do not assume `kubectl argo rollouts status` alone preserves a structured admission result.

## Smallest convincing experiment

1. Use one namespace, one Rollout, digest-pinned images, and one Kubernetes-native router (prefer Istio or Gateway API; do not demonstrate provider neutrality).
2. Install a fail-closed native VAP/binding and one protected fixed boundary parameter for that target. Include explicit rules for `rollouts`, `rollouts/status`, owned ReplicaSets, Services, and the chosen routing CRD.
3. Give the demo engineering agent only the permissions required to submit the Rollout and observe it. Keep policy, parameter, route, child-workload, RBAC, and controller credentials out of its reach.
4. Submit a Rollout with a permitted digest but an excessive `setWeight`; assert synchronous denial with the correction contract.
5. Have the agent reduce the weight and resubmit. Let Argo reconcile the admitted plan.
6. Exercise normal promote, full promote, abort, and undo attempts. Assert that status-subresource and rollback shortcuts cannot exceed the active boundary.
7. Record Kubernetes audit/admission decisions, boundary UID/generation, Rollout/resource versions, router desired weights, Argo status, and trusted probe observations in the release proof.
8. Phrase the result as “all admitted Kubernetes configurations stayed within the boundary; probes observed X,” not “exactly X% of requests and never more.”

## Kill and reframe criteria

Kill the artifact-bound cluster-guardrail claim, or reframe SafeLane as advisory orchestration, if any of these are required:

- the deployment agent must retain cluster-admin, policy, boundary, direct route, arbitrary workload, controller-token, node, or etcd privileges;
- the selected traffic provider changes exposure through an API that the enforced Kubernetes API server does not mediate;
- the product promise requires cryptographic proof of actual request-level exposure rather than proof of admitted configuration plus observations;
- SafeLane cannot enumerate and lock down every exposure surface used by its supported release profile;
- fail-closed admission availability is unacceptable and the organization chooses fail-open behavior;
- direct status promotion, rollback-window fast paths, header/mirror/experiment routes, or workload references cannot be forbidden or validated.

## Bottom line

The SafeLane idea is technically buildable **as a constrained release profile, not as a universal Kubernetes shield**. The admission layer can deterministically stop the unsafe manifest in the desired demo and remain independent of the coding agent. The valuable architectural seam is: SafeLane autonomously chooses and repairs a rollout; Kubernetes enforces a small, explicit artifact-and-exposure contract; Argo and existing probes execute and evaluate it; SafeLane records both admission proof and runtime observation.
