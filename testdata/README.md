# Shared fixtures

## `release-evidence.json`

The Release Request fixture. This is the file the CLI reads:

```text
safelane release --file testdata/release-evidence.json
```

**It is named for what it carries, not for what it proves.** Every value in it is a
*claim* a caller made. Nothing in this file authorizes anything: SafeLane verifies the
pull request, the approving review, the required check run for the merge commit SHA, and
the OCI digest against GitHub and GHCR, and only the verified results become
`release.ReleaseEvidence`. The file deliberately contains no Kubernetes objects, no
patches, no template selection and no policy selection — those are rejected at intake,
not ignored (`release.ForbiddenRequestKeys`).

The fixture is structurally valid against `release.ReleaseRequest` and is exercised by
`TestFixtureReleaseRequestIsValid` in `internal/release`. It will pass validation and
then **fail verification**, because the evidence it names does not exist yet.

### Values that must be replaced with real evidence

Replace these once the Podinfo fork exists and #46 has produced real evidence. Until
then every one of them is a plausible placeholder.

| Field | Fixture value | Replace with |
| --- | --- | --- |
| `source.repository` | `AndrewMaged814/podinfo` | the real fork's `owner/name`, once the public Podinfo fork is created (#46, first checklist item — the fork does not exist yet) |
| `source.merge_commit_sha` | `4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91` | the actual merge commit SHA on `main` produced when the reviewed PR is merged. Not the PR head SHA — SafeLane verifies the required check against the *merge* commit |
| `review.pull_request_number` / `review.pull_request_url` | `1` / `.../pull/1` | the real PR number and URL |
| `review.author` | `AndrewMaged814` | the real PR author's GitHub login |
| `review.approver` | `ahmed-placeholder` | **Ahmed's real GitHub login.** He must be added as a collaborator on the fork before the PR is opened, or he cannot approve it. Self-approval is not review evidence and SafeLane rejects it |
| `ci.workflow` / `ci.check_name` | `publish` / `publish / build-and-push` | the workflow name and the exact required check-run name emitted by the minimal `push`-to-`main` workflow #46 adds. The check name must match what GitHub reports, character for character |
| `ci.run_id` / `ci.run_url` | `16453210987` | the real Actions run id and URL for the run that executed **for the merge commit** |
| `artifact.image_reference` | `ghcr.io/andrewmaged814/podinfo@sha256:3fbc…50e8` | the real immutable GHCR reference, once #46 publishes the image and makes the package **public**. Both halves change: the repository path is the fork owner's lowercased GHCR path, and the digest is the one emitted by the build/push step |
| `caller.identity` / `caller.tool` / `caller.tool_version` | `codex-cli` / `codex` / `0.0.0-fixture` | whatever caller actually invokes SafeLane in the demo. Caller identity does not change release logic; it is recorded in Release Proof |
| `metadata.request_id` | `req-fixture-0001` | a caller-generated id. It is *not* the release id; SafeLane mints that (`rel_<ULID>`) |
| `metadata.submitted_at` | `2026-08-15T09:30:00Z` | the real submission time. It is recorded as a caller claim; SafeLane stamps its own `created_at` on the Release |

`target.*` (`podinfo` / `production` / `safelane-demo` / `podinfo`) must be confirmed
against Ahmed's demo cluster, since the namespace and cluster names must match the
pre-created Rollout (#55). They are not evidence, so they need confirmation rather than
replacement.

## Release Template fixture

The Release Template fixture lives with the renderer, at
`internal/render/testdata/release-template/`. Its README lists what a real
operator-owned template must provide and which of its values are guesses about the demo
cluster. Ahmed owns the real template; swapping it in is a `render.LoadDir` path change.
