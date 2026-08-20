# DeployWhisper as a SafeLane risk-analysis provider

**Research question.** Can DeployWhisper provide a useful, independently maintained pre-release risk signal for SafeLane, and where does SafeLane still need to own the release decision and enforcement boundary?

**Evidence basis.** This report is based on the public DeployWhisper repository and its first-party documentation/source. The repository's current development branch identifies the implementation as v1.3.0. Because the project is moving, an eventual adapter must pin both a provider version and the provider's report schema rather than relying on an unversioned endpoint.

## Conclusion

DeployWhisper is a credible integration candidate for a SafeLane prototype, but it is not a SafeLane core replacement. It already provides a local/self-hosted pipeline that parses several infrastructure artifact formats, normalizes changes, applies deterministic heuristic checks, optionally asks an LLM for bounded assistance, and returns a structured advisory report with evidence and uncertainty. Its own guardrails explicitly make it advisory-only: it does not approve, deploy, or enforce a release.

For a Podinfo demonstration, DeployWhisper can inspect **rendered Kubernetes YAML** containing an Argo Rollout (and its Services, AnalysisTemplate, or other resources). It should be treated as a generic Kubernetes-manifest analysis, not as an Argo-aware canary controller. The current Kubernetes parser identifies API objects and produces generic change records; it does not appear to reason about rollout steps, `setWeight`, traffic routing, AnalysisRuns, immutable OCI digests, or actual cluster/traffic state.

The product boundary therefore survives in a narrower form:

> DeployWhisper can be a replaceable advisory evidence provider. SafeLane must still bind that evidence to the exact release artifact and target, apply organization policy, decide whether authority may be granted, and enforce the resulting boundary through the production release path.

SafeLane should integrate DeployWhisper rather than duplicate its parser, finding, narrative, and report UI. It should not put a DeployWhisper `go` or `no-go` field directly into a permit without checking identity, completeness, and policy.

## What DeployWhisper already solves

### Supported artifacts and normalization

The official parser registry currently maps these direct parser families:

| Artifact family | Current first-party implementation | SafeLane implication |
|---|---|---|
| Terraform | Supported | Can submit plans/configuration for advisory analysis. |
| Kubernetes YAML | Supported | Rendered Rollout/Service/AnalysisTemplate manifests are the relevant path for the demo. |
| Ansible | Supported | Reusable only where a release also contains Ansible changes. |
| Jenkins | Supported | Reusable for pipeline artifacts, not a release authority. |
| CloudFormation | Supported | Reusable for AWS infrastructure changes. |

The README and site describe a broader ecosystem, including OpenTofu, Helm, Argo CD, and Flux. The implementation-level registry is the safer compatibility statement: it lists Terraform, Kubernetes, Ansible, Jenkins, and CloudFormation. Helm or Argo CD output should be treated as supported only after rendering/exporting it into a parser-supported artifact and testing the exact provider version. SARIF/Semgrep data is imported as external context; it is not the same thing as a native deployment-artifact parser.

The first-party Kubernetes parser detects YAML documents with `apiVersion` and `kind`, then normalizes each object as a Kubernetes change with resource identity, namespace/name where present, an `apply` operation, and the fact that previous cluster state is unknown. That is useful for a manifest-level risk briefing. It is not a semantic model of every Kubernetes CRD.

Sources: [parser registry](https://github.com/deploywhisper/deploywhisper/blob/develop/parsers/registry.py), [Kubernetes parser](https://github.com/deploywhisper/deploywhisper/blob/develop/parsers/kubernetes_parser.py), [README](https://github.com/deploywhisper/deploywhisper/blob/develop/README.md), [project site](https://deploywhisper.dev).

### Invocation and deployment model

DeployWhisper is self-hostable and MIT-licensed. The official README documents a Python installation and a Docker image (`ghcr.io/deploywhisper/deploywhisper:1.3.0`). It exposes a browser UI, OpenAPI documentation, health endpoint, CLI, and HTTP API. It claims no SaaS dependency or telemetry requirement; an LLM provider is optional.

CLI examples from the README:

```bash
deploywhisper analyze --project payments path/to/plan.json path/to/deployment.yaml
deploywhisper analyze --agent-json --project payments path/to/plan.json
```

The stable agent-oriented HTTP path is:

```text
POST /api/v1/agent/analyses
GET  /api/v1/agent/reports/{report_id}
```

The submission is multipart form data. It requires a project key or project ID and accepts uploaded files; optional `artifact_paths` label uploads but do not cause the server to read an arbitrary client filesystem path. The general analysis endpoint is `POST /api/v1/analyses`. The agent endpoint accepts project-role/project-key headers for its bounded interface. There is no current standalone MCP listener; the repository calls this HTTP surface MCP-equivalent.

An operational error, authentication error, timeout, malformed response, unsupported schema, or unsupported artifact must be treated as a failed review. The provider's own agent contract says an error is not a low-risk result and must not be replaced with model judgment.

Sources: [README and quick start](https://github.com/deploywhisper/deploywhisper/blob/develop/README.md), [agent HTTP routes](https://github.com/deploywhisper/deploywhisper/blob/develop/api/routes/agent.py), [MCP-equivalent safety contract](https://github.com/deploywhisper/deploywhisper/blob/develop/docs/ai-safety/mcp-server.md), [MIT license](https://github.com/deploywhisper/deploywhisper/blob/develop/LICENSE).

### Prototype invocation decision

For the hackathon spike, run DeployWhisper as a separate pinned service/runtime but invoke it through the CLI's structured `--agent-json` mode. The SafeLane adapter must capture the process exit code, timeout, stderr, and raw JSON; validate the pinned report/agent schema; and normalize only validated fields into a SafeLane-owned contract. It must never parse human-readable CLI text.

The provider mechanism remains behind one interface so the same adapter can later call the self-hosted HTTP API for concurrency, centralized persistence, remote deployment, or operational separation. A CLI error, timeout, malformed response, unsupported artifact, or unknown schema is failed/unsupported/unknown risk and cannot be coerced to low risk.

### Canonical report and evidence model

The persisted report schema is versioned as **report schema v2**. The canonical report contains (among other report metadata):

- report ID and `report_schema_version`;
- `severity`, `recommendation`, numeric `score`, and `confidence`;
- `top_risk`, contributors, and context completeness;
- findings and evidence records;
- blast radius and rollback-plan fields;
- advisory/share and audit metadata.

The agent-facing stable JSON contract additionally exposes:

- `schema_version` and `report_schema_version`;
- `scope`;
- a `verdict` containing risk score, severity, recommendation, and top risk;
- immutable safety flags: `advisory_only`, `deployment_approval`, `human_decision_required`, and `approval_statement`;
- `evidence_law`, `evidence`, `findings`, `confidence`, `uncertainty`, `context_todos`, and `verification_guidance`.

An evidence item identifies the analysis/finding, source and artifact references, location/resource/operation, deterministic level (`deterministic`, `heuristic`, or `inferred`), redaction status, summary, severity hint, deterministic boolean, confidence, and related change IDs. A finding identifies its severity/category, deterministic status, confidence, uncertainty note, evidence classification, evidence references, and guidance. A risk assessment contains overall severity, recommendation (`go`, `caution`, or `no-go`), score, confidence, top contributors, and context completeness.

The adapter should consume these structured fields. It should not parse a narrative paragraph or a UI/share-summary banner as an authorization decision. It should pin and validate the provider's report schema before interpreting fields.

Sources: [report schema v2](https://github.com/deploywhisper/deploywhisper/blob/develop/docs/schemas/report-v2.md), [agent JSON output contract](https://github.com/deploywhisper/deploywhisper/blob/develop/docs/ai-safety/agent-json-output.md), [evidence models](https://github.com/deploywhisper/deploywhisper/blob/develop/evidence/models.py).

## Determinism, LLM assistance, and uncertainty

DeployWhisper's important design claim is “evidence decides; AI explains.” Its pipeline has deterministic parsing and heuristic scoring. LLM assistance is optional and bounded: the scorer can use an LLM to assist contributor severity/narrative, and records whether the source was `heuristic-only` or `heuristic+llm`. If the LLM fails, the implementation falls back to heuristic scoring with a warning. The LLM is not a deployment authority.

The evidence model distinguishes deterministic, heuristic, and inferred evidence. High/critical findings are constrained by the project's Evidence Law: the provider's documentation says it should not produce a high/critical finding without deterministic evidence. External scanner evidence can enrich context, but does not automatically become deterministic DeployWhisper proof.

Context gaps are first-class output. The scorer can mark partial/insufficient context, reduce confidence, emit warnings and context TODOs, and move a `go` recommendation toward `caution`; the contract also exposes `context_completeness`, `partial_context`, and uncertainty flags. A missing/unsupported artifact is an operational failure such as `no_supported_artifacts`, not a safe low-risk report. This matters for a release gate: absent data is unknown, not “green.”

Sources: [risk scorer](https://github.com/deploywhisper/deploywhisper/blob/develop/analysis/risk_scorer.py), [agent safety contract](https://github.com/deploywhisper/deploywhisper/blob/develop/docs/ai-safety/agent-json-output.md), [MCP-equivalent contract](https://github.com/deploywhisper/deploywhisper/blob/develop/docs/ai-safety/mcp-server.md).

## Podinfo plus Argo Rollouts: what is and is not covered

DeployWhisper can likely analyze a Podinfo release after the deployment source has been rendered into Kubernetes manifests. A minimal submission could include:

- an Argo `Rollout` object (`apiVersion: argoproj.io/v1alpha1`, `kind: Rollout`);
- the stable/canary Services and any traffic-routing objects;
- an Argo `AnalysisTemplate` or other analysis resources;
- the Podinfo workload template, including its image reference;
- optional CI/SARIF evidence.

The parser will see these as Kubernetes API objects and produce generic resource-level changes. That is enough to demonstrate “the release was reviewed and a manifest has risk findings.” It is not enough to claim that DeployWhisper evaluated the canary strategy. The current parser does not establish that:

- the Rollout's step sequence stays within a permitted exposure limit;
- an Argo AnalysisRun or runtime probe will run or pass;
- the Service/traffic router will expose only the intended percentage;
- the image reference resolves to the exact immutable OCI digest;
- the submitted object matches the object actually applied to the target cluster;
- a live cluster's current state, traffic, or health is safe.

Rendering Helm output before submission is therefore the right prototype experiment. Use a pinned DeployWhisper version and record the exact files uploaded. The experiment should include both a normal Podinfo Rollout and an intentionally unsafe manifest to see which generic findings are emitted. The result should be described as provider evidence, not an Argo-specific safety proof.

The submitted bundle is the final rendered Kubernetes object set SafeLane intends to apply, not Helm/source templates. The Rollout normally contains the Podinfo pod template, so it need not be uploaded as a separate object. Include the Rollout, stable/canary Services, any active traffic-routing object, the AnalysisTemplate, and materially relevant non-secret referenced resources. SafeLane records a content hash for each submitted resource and binds the report to the release ID, source revision, OCI digest, cluster, namespace, and application. Runtime `AnalysisRun` results, live metrics, and observed traffic are collected later and remain separate Release Proof evidence.

Podinfo's own health, metrics, and fault-injection behavior can support Argo's runtime analysis, but those runtime observations are outside DeployWhisper's static parser. Exact image/provenance binding likewise remains a SafeLane or deployment-pipeline responsibility.

## Minimal SafeLane adapter contract

This is an integration seam, not a SafeLane risk engine.

### Input

The adapter submits:

```text
adapter_version
provider = deploywhisper
provider_version
request_id
release_id
target: application, environment, cluster, namespace, rollout_name
artifact_identity: source_revision, oci_image_digest
artifact_set: rendered Kubernetes manifests and required supporting evidence
optional: SARIF/CI evidence and provider project key
```

`source_revision` and `oci_image_digest` are supplied by the caller and must not be inferred from a filename, tag, narrative, or an LLM. The adapter should record the exact bytes or content hashes of submitted manifests and the provider project/report IDs.

### Output

The adapter returns a bounded, typed envelope:

```text
provider_status: ok | degraded | failed | unsupported
provider_version
report_id
report_schema_version
agent_schema_version (when using the agent endpoint)
risk: score, severity, recommendation, confidence, source, top_risk
determinism: deterministic_evidence_count, deterministic_high_or_critical, evidence_law_status
findings: id, severity, deterministic, confidence, evidence_refs, resource, location
uncertainty: partial_context, insufficient_context, context_todos, warnings
identity_check: submitted artifact identity, parsed artifact refs, identity_verified
raw_report_ref: report ID and/or content hash
```

`identity_verified` must remain false unless a separate verifier has checked the submitted artifact and target identity. DeployWhisper's generic Kubernetes parser should not be credited with verifying OCI identity or live-cluster correspondence. The adapter rejects missing report IDs, unknown/truncated schemas, scope mismatches, provider failures, and absent required artifact identity. It retains a content-addressed raw report reference for audit, rather than copying the entire provider report into a release permit.

## Conservative aggregation rule

SafeLane's policy layer should aggregate provider output monotonically:

```text
risk = max(policy_baseline, every deterministic finding severity, provider advisory severity)

if required evidence is missing or unknown:
    uncertainty = unknown
    decision cannot be go

if provider fails, times out, returns malformed/unsupported schema, or scope does not match:
    provider_status = failed/unsupported
    decision cannot be go

narrative, confidence inflation, or a provider "go" result never lowers risk
```

Use an explicit severity order (`low < medium < high < critical`) and keep `unknown` separate from that order. A `no-go` advisory is a risk-increasing signal; it must not be averaged away by another provider's `go`. LLM-assisted or inferred findings may raise attention, but cannot lower a deterministic high/critical finding. A deterministic critical finding is a hard deny for the prototype. For other `no-go`/high findings, the active Release Policy decides whether the outcome is deny, require additional evidence, or require human approval; DeployWhisper itself cannot make that choice.

The key safety rule is fail-closed on required evidence: unknown is not low risk. If DeployWhisper cannot parse the required artifact set, SafeLane must preserve an `unknown`/failed review and withhold authority until policy-approved evidence is available. This rule belongs in the adapter/policy contract, not in an attempt to modify DeployWhisper's own scoring.

The prototype policy is captured in [`docs/policy/safelane-policy.yml`](../policy/safelane-policy.yml). It gives low-risk changes a 10% start, medium/high changes a 5% start, requires approval before high-risk releases enter the 50% stage, denies critical risk, withholds authority for unknown risk, requires merged-PR/CI/digest evidence for every tier, and runs runtime analysis after every stage.

## What SafeLane should and should not own

### Reuse directly

- DeployWhisper's parsers and normalized evidence model for advisory pre-release analysis.
- Its report schema and agent JSON contract, subject to version pinning and validation.
- Its deterministic/heuristic/LLM provenance and context-completeness signals.
- Its self-hosted Docker/CLI/HTTP deployment model.
- Existing Argo Rollouts for workload progression and runtime analysis.
- Kubernetes RBAC and admission policy for independent cluster-side checks.

### Keep in SafeLane

- exact release identity: source revision, immutable OCI digest, target application/environment/cluster/namespace;
- organization policy and the authorization decision;
- a permit or equivalent release authority bound to that identity and policy version;
- enforcement of the allowed production path and exposure boundary;
- correlation of static provider evidence with the actual Argo Rollout and runtime outcome;
- the auditable proof that the granted boundary was not exceeded.

### Remove from SafeLane scope

- rebuilding DeployWhisper-like IaC parsing, finding catalogs, risk narratives, or a second scanner UI;
- making an LLM the release authority or asking it to write arbitrary Kubernetes patches;
- claiming that static manifest analysis proves canary traffic safety or successful runtime promotion;
- treating DeployWhisper's `go` as a permit or its `no-go` as a replacement for policy;
- accepting mutable image tags or unverified artifact identity because a report exists.

## Verdict: reframe, do not kill

**Verdict: REFRAME / prototype the adapter; do not build a separate risk engine.**

The original broad “smart agentic way of deploying rollout” is not validated by DeployWhisper. Much of the analysis surface already exists, and Argo Rollouts already owns progression. However, DeployWhisper makes the remaining seam clearer: SafeLane can be the policy-and-authority layer that combines external evidence, exact artifact identity, and runtime rollout facts into a constrained release decision. That is a buildable experiment if the first demo proves one concrete missing capability: an advisory provider report is bound to one exact Podinfo artifact and cannot authorize an Argo Rollout outside the policy-defined exposure boundary.

The next experiment should be a thin adapter plus a happy-path/failure-path evidence trace. If that trace can be reproduced with only Stakpak/DeployWhisper, Argo, RBAC, and admission policy—and no SafeLane-specific binding/enforcement—then SafeLane's core is redundant and the hypothesis should be killed. If the external tools can analyze and explain but cannot produce an independently enforceable, artifact-bound release authority, the narrowed SafeLane hypothesis survives.
