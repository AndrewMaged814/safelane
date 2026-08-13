# Assess pull requests through a base-owned repository safety contract

SafeLane will expose one deep `ChangeSafety` module with three lifecycle operations:
`assess(PullRequestRef)`, `resolve(ResolutionCommand)`, and
`compile(ReleaseBinding)`. Callers identify a pull request or an earlier assessment; they do not
supply diffs, policy paths, hashes, timestamps, tiers, or rollout stages.

The module reads `.safelane/policy.yaml` from the exact pull request base SHA. The pull request head
may change that file for a future revision, but cannot weaken the policy used to assess itself.
Every change assessment binds the repository, pull request, full base and head SHAs, canonical diff
hash, policy version, policy source revision, and policy hash. The head is rechecked before the
assessment is published.

## Consequences

- GitHub and local Git are adapters behind the pull-request host seam.
- Studio and the CLI consume the same change assessment and rollout decision rather than assembling
  evidence or maintaining parallel policy engines.
- Normal backend policy proposes the risk tier and minimum rollout profile. AI contributes only the
  bounded, source-backed safety case described by ADR 0001.
- Approval and rejection bind to an immutable assessment handle. Approval may emit a rollout
  decision; rejection and decide-later never authorize release.
- GitHub Checks are a read-only projection of the current assessment. They do not become an
  authority for release.
- Rollout compilation consumes only a valid exact-head rollout decision plus an explicit immutable
  release binding verified by GitHub artifact attestation against a base-policy-pinned signer
  workflow and recorded in a signed server-owned
  image catalog. Human authorization is HMAC-protected with a repository-specific key stored outside
  the mutable assessment directory. The local OS user is trusted; this is tamper evidence, not RBAC.
