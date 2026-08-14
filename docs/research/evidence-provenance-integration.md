# Evidence chain for one exact change and artifact

Status: resolved research decision, not an implementation design

Checked: 2026-08-15

Question: What is the smallest standards-based chain that proves review and test evidence applies to the exact source revision and immutable deployable artifact being released, without SafeLane inventing an evidence format or store?

## Decision

SafeLane should verify a **two-subject chain joined by trusted build provenance**:

```text
review + test facts
        │ exact repository + git commit
        ▼
reviewed source revision
        │ signed SLSA Build Provenance from a trusted builder
        ▼
OCI image manifest digest
        │ exact digest used by the rollout
        ▼
release authority and release proof
```

The two immutable identities are:

- **source revision:** repository URI plus Git commit digest;
- **deployable artifact:** fully qualified OCI image name plus the exact `sha256` manifest digest used by the rollout.

SafeLane must reject, rather than infer, any missing or unequal edge. A pull-request number, branch, image tag, CI URL, agent assertion, or merely well-formed SARIF document is not an immutable join.

For the one-provider prototype, use GitHub as the source/review/test authority and GitHub Actions as the builder:

1. Fetch the review and required check-run facts directly from GitHub using the candidate Git commit SHA. A pull-request review includes the `commit_id` it applies to, and check runs expose `head_sha`; require both to equal the candidate source revision. GitHub documents both fields in its [pull-request reviews API](https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10) and [check-runs API](https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10).
2. Build that same revision and generate GitHub build provenance with [`actions/attest@v4`](https://github.com/actions/attest). The action binds a digest-named subject to SLSA provenance in an in-toto statement and signs it with a short-lived Sigstore certificate.
3. Verify that the signed attestation's subject equals the image manifest digest requested for release, and that its `resolvedDependencies` contains the same repository URI and `gitCommit` accepted in step 1. GitHub's implementation constructs exactly this dependency from the OIDC `repository`, `ref`, and `sha` claims; see [`buildSLSAProvenancePredicate`](https://github.com/actions/toolkit/blob/main/packages/attest/src/provenance.ts).
4. Put the exact digest, never a mutable tag, in the rollout and any SafeLane release authority.
5. Record the GitHub evidence identifiers, attestation identifier/bundle digest, policy revision, source identity, and image digest in the release decision and outcome. GitHub and the OCI registry remain the evidence systems of record; SafeLane does not need an evidence database.

This is the smallest credible chain for the hackathon because it adds only one supply-chain artifact that existing CI may not already emit: trusted build provenance. It consumes review and test state where those facts already live.

## Important prototype constraint: build the revision that was reviewed

GitHub reviews normally apply to a pull request's head commit, while a merge, squash, or rebase can create a different commit. SafeLane must not silently equate those revisions.

For the first demonstration, build and attest the exact reviewed PR head SHA, and require all selected checks to report that same SHA. Run the build/attestation workflow on a `push` of that same-repository candidate branch, so GitHub's OIDC `sha` claim used by `actions/attest` is the actual built head commit. Do not build a manually checked-out head from a `pull_request` merge-ref workflow and then assume the generated provenance describes the checkout: GitHub's provenance implementation uses the OIDC claims, not an inspection of the working tree.

It is acceptable that the artifact is built before final approval: SafeLane grants release authority only after the later review and check facts match the same immutable SHA. If the product later releases a transformed merge revision, it needs either:

- an SCS-issued Source Verification Summary Attestation that covers the resulting revision; or
- a trusted, provider-specific adapter that verifies the merge policy and supplies the missing source edge.

That second path is organization- and source-control-specific. It cannot be made trustworthy by an agent saying that two commits are equivalent.

## Verification algorithm

SafeLane's verifier should apply these checks in order:

1. **Normalize identities.** Accept a repository URI, Git commit digest, and fully qualified OCI image digest. Resolve tags only for user convenience, then continue solely with the returned digest. For the single-platform demo, use the image manifest digest rather than a multi-platform index to avoid a second index-to-manifest join.
2. **Verify the provenance envelope.** Verify the Sigstore bundle/signature, certificate chain and signing time, expected OIDC issuer, and expected workflow identity. The in-toto Envelope layer handles authentication; the Statement alone does not ([in-toto Envelope v1](https://github.com/in-toto/attestation/blob/main/spec/v1/envelope.md)).
3. **Verify the artifact subject.** Recompute or retrieve the OCI manifest digest and require it to match the in-toto Statement subject and the rollout image. In-toto subjects are matched by digest ([Statement v1](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)); OCI descriptors define the digest as the content identifier and require consumers to verify retrieved bytes ([OCI Content Descriptors](https://github.com/opencontainers/image-spec/blob/main/descriptor.md)).
4. **Verify the build expectations.** Require `predicateType == https://slsa.dev/provenance/v1`, an allowed `builder.id`, build type, workflow path/ref, runner environment, and parameters. SLSA explicitly requires downstream verification of builder identity, signature, build type, and external parameters ([SLSA artifact verification](https://slsa.dev/spec/v1.2/verifying-artifacts)).
5. **Join artifact to source.** Require one resolved dependency whose repository URI and `gitCommit` equal the selected source revision. SLSA Build Provenance makes outputs the Statement subjects and provides `resolvedDependencies`, while `builder.id` represents the transitive trusted build platform ([SLSA Build Provenance v1.2](https://slsa.dev/spec/v1.2/build-provenance)).
6. **Verify source facts.** Fetch the current PR/review and check-run records through an authenticated GitHub integration, match each fact to the exact Git commit, and apply the configured producer and freshness rules.
7. **Authorize exactly the joined pair.** The release decision must bind both the repository+commit and image digest, plus its target and policy revision. Any mismatch produces a machine-readable denial; the engineering agent cannot override it.

Signature verification proves the producer and integrity of a claim, not that the claim is adequate. Producer authorization and predicate semantics are separate policy checks.

## What each candidate standard contributes

| Candidate | Decision for the prototype | Exact contribution | What it cannot establish |
| --- | --- | --- | --- |
| SARIF 2.1.0 | Optional input; exclude from the core chain | Static-analysis findings; a run may name scanned repository revisions and artifacts may carry hashes | Authentication, review approval, general test completion, build provenance, or release authority. `versionControlProvenance` and artifact hashes are optional in the [OASIS specification](https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/sarif-v2.1.0-errata01-os-complete.html). |
| in-toto Statement + Envelope | Use as the attestation model | A typed predicate bound to digest-identified subjects, inside an authenticated envelope | Whether the signer, predicate, test suite, or result satisfies organization policy |
| in-toto Test Result | Accept when a CI producer already emits it; do not require it for the GitHub demo | A standard predicate for one invocation of a test suite, bound through the enclosing Statement to tested subjects ([Test Result v0.1](https://github.com/in-toto/attestation/blob/main/spec/predicates/test-result.md)) | Which tests/configuration are required or which producer is trusted |
| SLSA Source VSA | Prefer when the source-control system emits it; do not fabricate one | Source-level claims tied to repository URI and revision; SLSA Source L4 includes two-party review | Portable detailed review provenance. SLSA says detailed source provenance may be SCS-specific ([source verification](https://slsa.dev/spec/v1.2/verifying-source)). |
| SLSA Build Provenance v1 | Required bridge | Exact build output subject to source/dependency digest, build type, parameters, and builder identity | Review/test sufficiency, release readiness, or runtime exposure |
| Sigstore bundle / cosign | Use for signing and verification, not policy | Identity-bearing signatures plus verification material; Sigstore bundles contain what a verifier needs to verify a signature ([bundle format](https://docs.sigstore.dev/about/bundle/)) | Truth of a signed predicate or authority of its signer. For the GitHub path, use GitHub's attestation verifier; `cosign attest` is a standards-compatible alternative producer ([cosign attest](https://github.com/sigstore/cosign/blob/main/doc/cosign_attest.md)). |
| OCI digest + referrers | Digest is required; referrers are optional transport | Content identity, attachment, and digest-based discovery of attestations | Authenticity or truth of a referrer. The OCI Distribution referrers endpoint only returns descriptors whose manifests declare the requested digest as `subject` ([OCI Distribution Spec 1.1](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers)). |

The standards compose; none is the whole chain. SARIF is a payload, in-toto is the assertion envelope, SLSA supplies the build relationship, Sigstore authenticates issuers, and OCI identifies/distributes the artifact and related material.

## Storage and discovery decision

Do not build a SafeLane evidence store.

- GitHub's attestation action uploads signed attestations to the repository's Attestations API, and can also push them beside a container image. The [repository attestations API](https://docs.github.com/en/rest/repos/attestations?apiVersion=2026-03-10) supports lookup by subject digest and predicate type.
- OCI referrers are a portable alternative when the registry supports them. Association is not authentication: fetch the bytes, verify their descriptor digest, then verify the Sigstore/in-toto envelope.
- GitHub remains authoritative for native review and check-run records. The release record should retain stable provider IDs/URLs and the time of evaluation. If a later audit requirement demands a self-contained snapshot, store the raw provider responses as an ordinary content-addressed OCI artifact and reference its digest; that still does not require a proprietary SafeLane database or evidence schema.

The release decision and exposure outcome are SafeLane domain records, not replacements for source or build evidence. They may use an in-toto Statement/Envelope, but their release-specific predicate semantics necessarily remain SafeLane-owned.

## Trust belongs to producers, not file types

The prototype trust map should be explicit:

| Fact | Trusted producer for the demo | Verification expectation |
| --- | --- | --- |
| Pull-request approval | GitHub for one configured repository; eligible reviewer identities/teams | Review `commit_id` equals source SHA; review is not dismissed; required count/topology passes |
| Test/check result | Named GitHub App or allowed workflow/check names | `head_sha` equals source SHA; status completed; allowed conclusion; acceptable age |
| Build provenance | GitHub Actions OIDC issuer plus one pinned reusable workflow identity and builder ID | Sigstore bundle valid; subject digest, repository, source SHA, build type, workflow and parameters match |
| OCI bytes | Registry as a content distributor, not an evidence authority | Retrieved manifest bytes hash to the requested digest |
| Release decision | SafeLane's deterministic policy evaluator, distinct from the requesting agent | Decision binds accepted evidence, policy revision, source, artifact and target |
| Release request | Engineering agent | Untrusted proposal; every referenced fact is independently verified |

Do not trust “GitHub Actions” generically. A repository-controlled workflow can often be modified by the same change being released. Pin or separately protect the reusable build/attestation workflow, and constrain the OIDC workflow identity. SLSA's verification guidance models roots of trust as a signer identity plus `builder.id`, not as the presence of a SLSA-shaped JSON file.

## What remains organization-specific

No standard can choose these policy inputs for SafeLane:

- source-control repositories, protected refs, merge methods, and whether a reviewed head can be released;
- reviewer eligibility, approval count/topology, stale-review behavior, and bypass actors;
- required check names, trusted GitHub Apps/workflows, acceptable conclusions, retry and freshness rules;
- whether SARIF is required and its allowed tools, versions, profiles, severity thresholds, and suppressions;
- accepted in-toto predicate versions and Sigstore trust roots/issuers;
- allowed `builder.id`, reusable workflow identity/version, runner environment, build type, and parameters;
- which OCI registry/repository and whether the release subject is a manifest or index;
- release target, exposure limits, runtime probes, expiry/replay rules, and escalation conditions;
- retention requirements and whether provider-native evidence must be snapshotted for independent audit.

These choices are the Release Policy. SafeLane should provide safe examples, but must not disguise them as universal facts.

## Prototype acceptance tests

The evidence chain is credible only if the prototype denies each of these substitutions:

1. approved commit A plus passing checks for A, but provenance says the image was built from commit B;
2. a review on an earlier PR head after a newer commit is pushed;
3. a successful check with the right name produced by an untrusted GitHub App;
4. a valid Sigstore/SLSA attestation from the wrong workflow or builder;
5. a tag that moved from the attested image digest to another digest;
6. an OCI referrer or SARIF file with correct syntax but no trusted signature;
7. an image-index attestation while the rollout or proof silently substitutes an unbound platform manifest.

If SafeLane cannot make these failures deterministic and explain the mismatched identity to the agent, it does not yet prove that reviewed and tested code is the artifact being released.

## Consequences for SafeLane's product boundary

This research removes evidence normalization and evidence storage from SafeLane's differentiating value. Existing standards and provider APIs already supply the facts, identities, signatures, and distribution mechanisms.

SafeLane's remaining job is to:

- verify the graph rather than collect a bag of evidence;
- apply organization-owned Release Policy to trusted producers and exact subjects;
- bind the accepted source+artifact pair to limited rollout authority; and
- record what that authority allowed and what exposure actually occurred.

A prototype that only rejects unsigned or unattested images is an integration of existing supply-chain policy, not a distinct SafeLane product.

## Primary sources

- [OASIS SARIF 2.1.0 Plus Errata 01](https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/sarif-v2.1.0-errata01-os-complete.html)
- [in-toto Statement v1](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)
- [in-toto Envelope v1](https://github.com/in-toto/attestation/blob/main/spec/v1/envelope.md)
- [in-toto Test Result predicate](https://github.com/in-toto/attestation/blob/main/spec/predicates/test-result.md)
- [SLSA v1.2 Build Provenance](https://slsa.dev/spec/v1.2/build-provenance)
- [SLSA v1.2 artifact verification](https://slsa.dev/spec/v1.2/verifying-artifacts)
- [SLSA v1.2 source requirements](https://slsa.dev/spec/v1.2/source-requirements)
- [SLSA v1.2 source verification](https://slsa.dev/spec/v1.2/verifying-source)
- [Sigstore Bundle Format](https://docs.sigstore.dev/about/bundle/)
- [cosign attest](https://github.com/sigstore/cosign/blob/main/doc/cosign_attest.md)
- [OCI Content Descriptors](https://github.com/opencontainers/image-spec/blob/main/descriptor.md)
- [OCI Distribution referrers](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers)
- [GitHub `actions/attest`](https://github.com/actions/attest)
- [GitHub Actions attestation provenance implementation](https://github.com/actions/toolkit/blob/main/packages/attest/src/provenance.ts)
- [GitHub artifact attestation documentation](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations)
- [GitHub repository attestations API](https://docs.github.com/en/rest/repos/attestations?apiVersion=2026-03-10)
- [GitHub pull-request reviews API](https://docs.github.com/en/rest/pulls/reviews?apiVersion=2026-03-10)
- [GitHub check-runs API](https://docs.github.com/en/rest/checks/runs?apiVersion=2026-03-10)
