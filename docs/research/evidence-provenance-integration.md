# Evidence and provenance integration for SafeLane

Status: research note, not an implementation design  
Checked: 2026-08-14  
Question: Can existing standards bind review and test evidence to an exact source revision and deployable artifact, and do they already solve SafeLane's evidence or permit layer?

## Executive conclusion

Existing standards solve most of the **evidence envelope, artifact identity, signing, verification, and distribution mechanics**. SafeLane should not invent another evidence format, signing scheme, provenance graph, or attestation store.

They do **not** solve the release decision SafeLane is interested in. None of SARIF, SLSA, in-toto, Sigstore, or OCI defines a short-lived, environment-scoped authorization such as “this image digest may receive at most 10% production traffic in service X until time Y under policy revision Z,” nor do they prove that the runtime stayed inside that boundary. Those semantics, their organizational policy, and the trusted observation needed to prove compliance remain SafeLane-specific.

The viable standards-based evidence chain is:

```text
source revision
  <- signed source/review/test attestations
  <- trusted SLSA build provenance -> OCI image manifest digest
  <- SafeLane policy evaluation -> release permit for that digest and release target
  <- admission + rollout observation -> signed/append-only outcome record
```

Every arrow must be verified. Merely collecting URLs, agent statements, CI status text, image tags, or unsigned SARIF is insufficient.

## Architecture-invalidating findings

### 1. A generic SafeLane evidence format or evidence store would be redundant

The [in-toto Attestation Framework](https://github.com/in-toto/attestation/blob/main/spec/README.md) already separates arbitrary predicate data from a statement that binds it to digest-identified subjects, an authenticated envelope, and a bundle. Its [Statement v1 specification](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md) requires each subject to have a digest and defines the predicate type by URI. [Sigstore/cosign](https://github.com/sigstore/cosign/blob/main/doc/cosign_attest.md) can sign custom or standard in-toto predicates and attach attestations to OCI images. The [OCI Distribution referrers API](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers) provides digest-based discovery of related artifacts.

**Consequence:** kill any architecture in which SafeLane's core value is normalizing evidence into a proprietary envelope or hosting another provenance database. For the prototype, consume in-toto/SLSA where available, wrap only genuinely non-attestable source outputs at a trusted adapter boundary, and refer to large source documents by content digest.

### 2. “Kubernetes rejects an image without acceptable evidence” already exists

Sigstore's [policy-controller](https://docs.sigstore.dev/policy-controller/overview/) is a Kubernetes admission controller that verifies signatures and attestations, resolves tags so the admitted image cannot later drift, and evaluates CUE or Rego policy over custom attestation predicates. It can select trusted signer identities and apply multiple policies per namespace.

**Consequence:** a demo whose only safety claim is “CI evidence becomes a signed image attestation, then Kubernetes admits or rejects the image” is not a new SafeLane product. SafeLane must demonstrate a dynamic release-specific boundary that ordinary image admission does not express: limited traffic/exposure, target environment and workload, expiry or single-use semantics, repair guidance, and proof of observed compliance.

### 3. SARIF is not a universal evidence interchange

[SARIF 2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/sarif-v2.1.0-errata01-os-complete.html) standardizes static-analysis results. A run can identify scanned repository revisions through `versionControlProvenance`, and artifacts can carry hashes, including SHA-256. This makes SARIF useful input for a policy that cares about static-analysis findings on known source.

SARIF does not standardize code-review approval, test execution, build provenance, signer identity, or release authorization. `revisionId` is recommended rather than required, and a SARIF log is not authenticated merely because its JSON is valid.

**Consequence:** do not make SARIF the SafeLane evidence model. Treat it as one source document. A trusted producer or adapter must attest its digest and the exact subject it applies to, and policy must still decide which tool, ruleset, invocation, and results are acceptable.

### 4. Source evidence and artifact evidence require a verified provenance edge

Review and many tests concern a source revision; Kubernetes deploys an OCI image manifest. [SLSA Build Provenance v1](https://slsa.dev/spec/v1.2/build-provenance) makes the build outputs the attestation subjects and can record a source repository plus exact `gitCommit` in `resolvedDependencies`. The SLSA verifier is expected to check that the subject digest matches the artifact being consumed and to accept only trusted signer/builder pairs; see [verifying artifacts](https://slsa.dev/spec/v1.2/verifying-artifacts).

That edge is only as credible as the builder. SLSA explicitly says `builder.id` represents the transitive trusted build platform, external parameters must be verified by consumers, and dependency completeness is best effort through Build L3.

**Consequence:** SafeLane cannot safely infer `commit -> image` from an image label, tag, agent assertion, or CI URL. Without provenance from a builder the organization trusts, reject artifact-level release authority or clearly downgrade the demo to a non-adversarial claim.

### 5. MCP and A2A are integration protocols, not evidence roots of trust

MCP gives provider-neutral access to JSON-schema-described tools, structured tool results, and URI-addressed resources; see the [MCP tools specification](https://modelcontextprotocol.io/specification/2025-11-25/server/tools) and [resources specification](https://modelcontextprotocol.io/specification/2025-11-25/server/resources). MCP's tool annotations are explicitly hints and untrusted unless the server itself is trusted. OAuth resource indicators bind access tokens to an MCP server, not engineering evidence to a revision or artifact; see [MCP authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization).

[A2A 1.0](https://a2a-protocol.org/latest/specification/) is useful for agent discovery, long-running task state, messages, artifacts, and extensions across opaque agent implementations. Its artifact identity is a task-local `artifactId`; the core Artifact fields do not define a content digest or signature. The core protocol signs Agent Cards optionally, not arbitrary task artifacts.

**Consequence:** expose SafeLane request/status/correction operations through MCP first if convenient, and consider A2A only if agent-to-agent task delegation is demonstrably needed. Neither protocol may be the trusted evidence transport without carrying independently verifiable in-toto/Sigstore material. Agent neutrality does not require both in the prototype.

## What each standard can and cannot establish

| Standard | Useful binding or guarantee | What it does not decide |
| --- | --- | --- |
| SARIF 2.1.0 | Static-analysis run to repository revision; findings to hashed analyzed files | Authenticity, review/test status, trusted tool configuration, deployable image, release permission |
| in-toto Statement/Envelope | Authenticated predicate to one or more immutable digest subjects; arbitrary typed predicates | Whether issuer and predicate are acceptable; deployment target/exposure semantics |
| in-toto Test Result | Test invocation result and configuration to exact tested source/artifact subjects | Which suites/configurations are required; whether a URL is trustworthy; organization pass criteria |
| classic in-toto layout/link | Signed supply-chain steps, functionary thresholds, and hash continuity between materials and products | Production traffic bounds and live rollout behavior; practical adoption by existing CI tools |
| SLSA Source Track / Source VSA | High-level properties and exact source revision; final-revision review can be represented | Detailed source provenance is intentionally SCS-specific; organization properties remain organization-defined |
| SLSA Build Provenance | Exact output artifact digest to exact source/dependency digest, build type, parameters, builder identity | Release readiness, environment authorization, runtime health, exhaustive dependencies below some trust levels |
| Sigstore/cosign | Attestation signing, signer identity verification, transparency/verification material, OCI attachment | Truth of signed predicate; which signer/predicate/policy is acceptable; permit revocation and rollout constraints |
| OCI descriptors/referrers | Content-addressed manifest identity and discovery of artifacts referring to its digest | Authenticity or truth of the referrer; authorization; guaranteed retention or universal registry support |
| MCP | Agent-neutral tool invocation and retrieval of structured results/resources | Cryptographic evidence identity, durable audit record, release policy |
| A2A | Agent-neutral discovery and stateful task/artifact exchange | Content-addressed or signed evidence semantics in the core protocol; release policy |

## Binding review and test evidence to the deployed artifact

### Exact source revision

Use the repository URI plus immutable revision identifier as a pair. A Git commit digest alone can appear in multiple repositories, so repository context matters. SLSA Source v1.2 requires Source VSA subjects to include the revision identifier and the predicate's `resourceUri` to identify the repository; see [Source requirements](https://slsa.dev/spec/v1.2/source-requirements#source-verification-summary-attestation).

For test evidence, use the [in-toto Test Result predicate](https://github.com/in-toto/attestation/blob/main/spec/predicates/test-result.md). Its subject is the source or artifact tested, and it carries a result, test configuration descriptors, an optional run URL, and optional named test lists. The specification explicitly leaves test-name meaning and interpretation of the run URL to producer and consumer.

For review evidence, SLSA Source v1.2 provides useful high-level properties and requires final-revision, context-specific review at the applicable level, but it deliberately leaves detailed source-provenance attestation formats to each source-control system. It lists a code-review attestation as an example rather than defining a portable predicate. Therefore either:

1. consume a trusted SCS-issued Source VSA that asserts the required standardized or `ORG_SOURCE_...` property for the exact revision; or
2. define a narrow adapter for the chosen SCS that emits a signed in-toto statement for the exact revision and preserves native evidence references.

Do not accept a pull-request number or “approved” text by itself. Policy must specify whether approvals reset when the revision changes and which identities/teams count.

### Exact deployable artifact

For the demo, define the deployable artifact as the **OCI image manifest digest**, not a tag and not merely the digest of one image layer. OCI content descriptors define a digest as a content identifier and require consumers to verify retrieved bytes against it; see the [OCI descriptor specification](https://github.com/opencontainers/image-spec/blob/main/descriptor.md). A multi-platform index and a platform-specific image manifest are different digests, so the policy must state which one is the release subject.

Require SLSA Build Provenance whose subject matches that exact OCI digest and whose `resolvedDependencies` includes the expected repository and source revision. Then verify:

1. the attestation envelope/signature and signer identity;
2. the in-toto subject equals the requested OCI digest;
3. `predicateType` is the expected SLSA provenance version;
4. the signer and `builder.id` pair is trusted;
5. the recorded source repository and revision equal the source evidence subjects;
6. build type and externally controlled parameters satisfy organizational expectations.

This is a graph join over digest identities, not a loose aggregation of “evidence for this release.”

### Storing and discovering the graph

Use OCI referrers when evidence naturally belongs with the image. An OCI manifest `subject` associates a referring artifact with another manifest digest, and `/referrers/<digest>` discovers the related descriptors. Registry support can fall back to the specified referrers-tag scheme.

OCI association is not authentication. Fetch the referenced bytes, verify their descriptor digest, verify the Sigstore/in-toto envelope, then evaluate the predicate. For evidence too large or controlled by another system, use an in-toto [Reference predicate](https://github.com/in-toto/attestation/blob/main/spec/predicates/reference.md) with the external document's digest and media type rather than copying mutable URLs into a permit.

## What remains organization-specific

Standards deliberately leave the following decisions open, so these are the actual SafeLane policy inputs:

- trusted issuers, builders, source-control systems, and identity mappings;
- required review topology, reviewer eligibility, and revision-change invalidation;
- required test suites, configurations, freshness, allowed warnings, and retry semantics;
- SARIF tools, versions, rule profiles, severity thresholds, and suppressions;
- accepted SLSA source/build levels and expected build types/parameters;
- artifact type and platform rules for multi-architecture images;
- environment, namespace, service, and workload allowed to consume a permit;
- maximum initial exposure, step sizes, time bounds, and allowed rollout actions;
- permit expiry, replay prevention, cancellation/revocation, and failure behavior;
- which runtime probes are trusted, their freshness, and promotion/abort thresholds;
- retention and audit requirements for decisions and rollout observations.

Signing proves who made a claim and that it was not altered. It does not make a weak scanner, compromised builder, stale test, or agent-generated judgment true. SafeLane policy must distinguish evidence facts from the authority assigned to each issuer.

## The permit gap

A SafeLane permit can reuse the in-toto/Sigstore envelope, but its predicate is necessarily release-specific. The minimum semantics to test in the prototype are:

- subject: exact OCI manifest digest;
- source: exact repository and revision, cross-checked through trusted build provenance;
- target audience: cluster/environment, namespace, service/workload, and rollout identity or request nonce;
- authority: permitted rollout mechanism and maximum initial/step/final exposure;
- validity: issue time, expiry, single-use or bounded-reuse rule, and cancellation state;
- decision basis: digest/version of organizational policy plus digests of accepted attestations;
- issuer: a SafeLane policy-evaluator identity distinct from the requesting engineering agent.

Attaching this permit only to the image is replay-prone: the same image can be used in many namespaces and clusters. The admission/enforcement path must compare the request's target and rollout parameters with the signed permit, not just check that some permit exists for the image.

An immutable signed permit also does not itself implement revocation or continuous enforcement. Short expiry reduces risk but cannot prove exposure stayed bounded after admission. SafeLane therefore still needs a trusted enforcement/observation seam that evaluates rollout mutations and records the observed exposure timeline against the permit identifier.

## Recommended prototype boundary

Use existing standards and components for every commodity layer:

1. Accept one SCS/CI integration, with signed in-toto Test Result and SLSA provenance for one image digest. A signed Source VSA is preferable if the chosen platform can produce it; otherwise use a narrowly scoped trusted adapter for review evidence.
2. Verify the evidence chain and evaluate a small deterministic organizational policy.
3. Emit a custom, signed in-toto release-permit predicate for the image digest and exact deployment target.
4. Reuse an existing admission engine where possible. The novel check is rollout exposure versus the permit, not generic image-signature verification.
5. Record a content-addressed outcome statement that references the permit, image digest, rollout identity, runtime-probe evidence, and observed maximum exposure.
6. Use MCP as the engineering-agent-facing adapter only if it makes the demo easier. Keep the exact same release API callable by CLI/HTTP so SafeLane remains agent-provider-independent. Defer A2A unless a second autonomous service truly needs task delegation.

## Falsification tests for the prototype

These tests determine whether the remaining SafeLane layer is buildable and distinct:

1. **Revision substitution:** valid review/test attestations for commit A plus an image built from commit B must be denied.
2. **Tag drift:** a permitted tag moved to another digest must not deploy the new digest.
3. **Cross-target replay:** a permit for staging or service A must fail in production or service B.
4. **Exposure excess:** a rollout requesting more than the permit's initial or step exposure must be rejected with a machine-actionable correction.
5. **Untrusted evidence:** a correctly signed attestation from an untrusted issuer, or an unsigned but syntactically valid SARIF file, must not satisfy policy.
6. **Stale/revoked permit:** admission after expiry or cancellation must fail.
7. **Outcome integrity:** the final record must reveal any observed exposure above the permitted maximum rather than merely echoing the intended rollout spec.

If existing Sigstore policy-controller plus static Rego/CUE can implement the complete chosen demo without a distinct release-decision or continuous-boundary component, the current SafeLane product boundary is invalid: the work is an integration/demo of existing supply-chain policy, not a separate product. If the exposure-scoped, replay-resistant permit and observed-compliance proof require a coherent component not provided by those tools, that is SafeLane's defensible technical seam.

## Primary sources

- [OASIS SARIF 2.1.0 Plus Errata 01](https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/sarif-v2.1.0-errata01-os-complete.html)
- [in-toto Attestation Framework](https://github.com/in-toto/attestation/blob/main/spec/README.md)
- [in-toto Statement v1](https://github.com/in-toto/attestation/blob/main/spec/v1/statement.md)
- [in-toto Test Result predicate](https://github.com/in-toto/attestation/blob/main/spec/predicates/test-result.md)
- [in-toto Reference predicate](https://github.com/in-toto/attestation/blob/main/spec/predicates/reference.md)
- [in-toto supply-chain layout/link specification](https://github.com/in-toto/docs/blob/master/in-toto-spec.md)
- [SLSA v1.2 Build Provenance](https://slsa.dev/spec/v1.2/build-provenance)
- [SLSA v1.2 artifact verification](https://slsa.dev/spec/v1.2/verifying-artifacts)
- [SLSA v1.2 Source requirements](https://slsa.dev/spec/v1.2/source-requirements)
- [SLSA v1.2 source verification](https://slsa.dev/spec/v1.2/verifying-source)
- [Sigstore cosign attest command](https://github.com/sigstore/cosign/blob/main/doc/cosign_attest.md)
- [Sigstore policy-controller](https://docs.sigstore.dev/policy-controller/overview/)
- [OCI Content Descriptors](https://github.com/opencontainers/image-spec/blob/main/descriptor.md)
- [OCI Distribution referrers](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#listing-referrers)
- [MCP 2025-11-25 tools](https://modelcontextprotocol.io/specification/2025-11-25/server/tools)
- [MCP 2025-11-25 resources](https://modelcontextprotocol.io/specification/2025-11-25/server/resources)
- [MCP 2025-11-25 authorization](https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization)
- [A2A 1.0 specification](https://a2a-protocol.org/latest/specification/)
