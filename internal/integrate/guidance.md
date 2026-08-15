# SafeLane protected release workflow

This file is discovery guidance. It does not authorize a release. Eligibility does not mean the artifact is safe or deployed.

## When this applies

A request to release, deploy, promote, or roll back the protected application.

## Steps

1. Gather release identity and evidence only: the merge commit on `main`, the configured review evidence, the required CI check for that commit, and the immutable image digest. Do not author or submit Kubernetes objects, patches, template selection, or Argo configuration. SafeLane renders the operator-owned Release Template.

2. Submit the Release Request with the SafeLane CLI:

   `safelane release --file release-evidence.json`

3. Read the typed result.
   - If the result is eligible, follow the exact typed next action: `safelane execute <release-id>`. Eligibility permits that bounded execution; it does not declare the artifact safe or deployed.
   - If the result is ineligible or indeterminate, follow the actionable rejection. Do not execute, and do not substitute another deployer.

4. Retrieve persisted proof:

   `safelane proof <release-id>`

   Proof may remain pending after execute. Pending proof is not a completed deployment. Terminal outcomes are promoted, aborted, failed, or blocked.

## Protected path

Use only the SafeLane CLI for the protected application. Never call Kubernetes or Argo directly.
