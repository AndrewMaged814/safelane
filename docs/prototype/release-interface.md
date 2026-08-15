# SafeLane prototype release interface

The protected release path has one caller-facing entry point:

```text
safelane release --file release-evidence.json
```

The caller may be Codex, Claude Code, CI, an MCP adapter, another agent, or a human. Caller identity does not change SafeLane's release logic, and callers do not receive direct Kubernetes or Argo credentials for the protected application.

## Evidence request

`release-evidence.json` carries release identity and evidence only:

- application and environment;
- target cluster and namespace;
- merged source revision;
- pull request and review evidence;
- passing CI evidence; and
- immutable OCI image digest.

It contains no Kubernetes objects, no YAML or JSON patches, no template selection, and no policy selection. Those fields are rejected rather than ignored.

The agent may generate the file from repository and CI outputs. SafeLane verifies critical GitHub and registry evidence rather than trusting caller-declared claims.

## Trusted bundle

SafeLane renders the deployment bundle itself, from the operator-owned Release Template, pinning the pod template image to the verified immutable digest. It content-hashes every rendered resource.

That rendered bundle is the only bundle in play: it is what DeployWhisper analyzes, what Release Proof hashes, and what execution later applies. A caller cannot shape the configuration that its own risk assessment will evaluate, and the bytes that were assessed are provably the bytes that reach the cluster.

## Typed result

The command returns a machine-readable result containing:

- `release_id`;
- normalized risk and contributing provider(s);
- evidence-validation status;
- allowed rollout envelope;
- next permitted action; or
- an actionable rejection explaining what must be corrected.

The result is usable from the CLI, SafeLane API, CI, MCP, or another agent without changing the release-control core. Argo Rollouts and Kubernetes are reached only through SafeLane's protected execution path.

## Release Proof

The live command presents a concise summary readable in 10–15 seconds:

- release ID, application, and environment;
- caller identity;
- PR approval, merged commit, and immutable OCI digest;
- normalized risk, contributing provider, and policy version;
- allowed rollout path;
- stage-by-stage health outcome;
- one-line direct-bypass result; and
- final promotion or abort.

The complete record is available as:

```text
safelane proof <release-id> --details
safelane proof <release-id> --json
```

It is organized into four sections:

1. **Artifact proof:** all CI checks, source/merge identity, image digest, Release Template identity, and complete hashes of the SafeLane-rendered bundle. Provenance references are a planned addition.
2. **Decision proof:** normalized provider results, raw provider report hash, policy version, evidence validation, risk aggregation, and allowed envelope.
3. **Execution proof:** timestamps, Argo stages, AnalysisRun details, runtime outcomes, and final promotion/abort.
4. **Boundary proof:** caller identity, requested/allowed/configured/observed traffic, direct-bypass denial and metadata, and proof that protected state was unchanged.

Traffic fields are always distinct. If no explicit traffic router is used, observed traffic is labeled as a replica-based approximation rather than an exact percentage.
