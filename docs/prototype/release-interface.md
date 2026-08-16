# SafeLane prototype release interface

The protected release path has one caller-facing entry point:

```text
safelane release --pr 2
```

CI and tests may use the same intent as a file:

```text
safelane release --file release-request.json
```

The caller may be Codex, Claude Code, CI, an MCP adapter, another agent, or a human. Caller identity does not change SafeLane's release logic, and callers do not receive direct Kubernetes or Argo credentials for the protected application.

## Release Request

`release-request.json` carries identifiers and intent only:

- repository (`owner/name`, optional when git origin or project.yml supplies it);
- pull request number;
- environment (optional when project.yml has a default);
- optional immutable digest pin.

It contains no evidence claims, Kubernetes objects, YAML or JSON patches, template selection, or policy selection. Those fields are rejected rather than ignored.

SafeLane loads operator configuration from `.safelane/project.yml`, then collects and verifies the merge commit, required check, review (when policy requires it), and GHCR digest.

## Trusted bundle

SafeLane renders the deployment bundle itself, from the operator-owned Release Template, pinning the pod template image to the verified immutable digest. It content-hashes every rendered resource.

That rendered bundle is the only bundle in play: it is what Release Proof hashes and what execution later applies. A caller cannot shape the configuration SafeLane will apply, and the bytes that were hashed are the bytes that reach the cluster. SafeLane does not scan this YAML.

## Typed result

The command returns a machine-readable result containing:

- `release_id`;
- eligibility (`eligible`, `ineligible`, or `indeterminate`);
- policy version;
- reason code and actionable message;
- retryable flag;
- the static rollout envelope and next action, when eligible; or
- an actionable rejection explaining what must be corrected.

The result is usable from the CLI, SafeLane API, CI, MCP, or another agent without changing the release-control core. Argo Rollouts and Kubernetes are reached only through SafeLane's protected execution path.

## Release Proof

The live command presents a concise summary readable in 10–15 seconds:

- release ID, application, and environment;
- caller identity;
- PR approval, merged commit, and immutable OCI digest;
- eligibility, policy version, and static envelope when eligible;
- allowed rollout path;
- stage-by-stage health outcome;
- one-line direct-bypass result; and
- final promotion or abort.

The complete record is available as:

```text
safelane proof <release-id> --details
safelane proof <release-id> --json
```
