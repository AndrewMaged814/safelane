# SafeLane protected release workflow

This file is discovery guidance. It does not authorize a release. Eligibility does not mean the artifact is safe or deployed.

## When this applies

A request to release, deploy, promote, or roll back the protected application.

## Steps

1. Identify the merged pull request. Do not author evidence claims. Do not search SafeLane fixtures, testdata, or internals for request fields, reviewers, check names, or digests. SafeLane collects and verifies evidence from GitHub and GHCR using operator configuration.

2. Submit the Release Request with the SafeLane CLI:

   `safelane release --pr <number>`

   Optional: `--repo owner/name` and `--environment production`. For CI, `--file release-request.json` may carry the same identifiers only.

3. Read the typed result.
   - If the result is eligible, follow the exact typed next action: `safelane execute <release-id>`. Eligibility permits that bounded execution; it does not declare the artifact safe or deployed.
   - If the result is ineligible or indeterminate, follow the actionable rejection. Do not execute, and do not substitute another deployer.

4. Retrieve persisted proof:

   `safelane proof <release-id>`

   Proof may remain pending after execute. Pending proof is not a completed deployment. Terminal outcomes are promoted, aborted, failed, or blocked.

## Protected path

Use only the SafeLane CLI for the protected application. Never call Kubernetes or Argo directly.
