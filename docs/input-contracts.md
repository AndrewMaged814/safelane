# Pre-final input contracts

**Version:** 2 · **decision date:** 2026-08-09

This document owns caller inputs and the validated policy surface. Wire outputs are owned by
[`contract.md`](../contract.md).

## Assessment invocation

```text
safelane assess --worktree <clean-path> --request <request-v2.json> --assessed-at <rfc3339-utc> --output <workspace>
```

The request shape is frozen in `contract.md`. Git owns changed paths, line counts, source text, and
commit relationships. Caller metadata cannot override them. The worktree must be clean, both full
SHAs must exist, and the demo head must be a direct child of its base.

The canonical diff is the raw stdout bytes from this exact operation in the clean worktree, with the
two validated SHAs passed as separate argv values rather than interpolated shell text:

```text
git -c core.quotePath=true diff --no-ext-diff --no-textconv --no-color --no-renames --unified=3 --src-prefix=a/ --dst-prefix=b/ <base_sha> <head_sha> --
```

Run with Git's pager disabled and locale fixed to `C`; nonzero exit or stderr is a typed input error.
Binary patches are unsupported. Hash and byte length always use the raw stdout bytes. Only after that
does the engine attempt strict UTF-8 decoding for path/span parsing and model input. Invalid UTF-8, an
unrecognized path, or a diff over 16,384 bytes is handled by `docs/risk-signals.md`; the engine never
truncates or chunks model input.

## `policy.yaml` v2

The pre-final accepts exactly this closed-world policy shape:

```yaml
schema_version: "2"
policy_version: "2026.08.3"

release_service:
  name: demo-api
  replicas: 5
  critical: false
  downstream_dependents: []
  path_prefixes:
    - src/demo_api/

scope:
  small_max_files: 2
  small_max_lines: 50
  large_min_files: 10
  large_min_lines: 500

ai:
  provider: ollama
  model: qwen2.5-coder:7b
  max_diff_bytes: 16384
  timeout_seconds: 60
  attempts: 1
  temperature: 0
  seed: 42
  num_ctx: 8192
  num_predict: 768
  accepted_finding_kinds:
    - breaking_api

incident_history:
  enabled: false

profile_for_tier:
  safe: Fast
  guarded: Guarded
  risky: Strict

rollout:
  traffic_router: none
  max_surge: 1
  max_unavailable: 0

profiles:
  Fast:
    source: built_in
    stages:
      - {set_weight: 100, exposure_pods: 5, analysis: false}
    analysis: null
  Guarded:
    source: built_in
    stages:
      - {set_weight: 40, exposure_pods: 2, analysis: true}
      - {set_weight: 100, exposure_pods: 5, analysis: false}
    analysis_probe_id: demo-api-public-quote-v1
  Strict:
    source: built_in
    stages:
      - {set_weight: 20, exposure_pods: 1, analysis: true}
      - {set_weight: 40, exposure_pods: 2, analysis: true}
      - {set_weight: 60, exposure_pods: 3, analysis: true}
      - {set_weight: 100, exposure_pods: 5, analysis: false}
    analysis_probe_id: demo-api-public-quote-v1

trusted_probe_catalog:
  path: demo/trusted-probes.yaml
  non_fast_fallback_probe_id: demo-api-public-quote-v1

job_analysis:
  attempts: 3
  interval_seconds: 10
  failure_allowance: 1
  request_timeout_seconds: 2
  active_deadline_seconds: 45
```

### Policy validation

- Every object rejects unknown fields and every listed field is required.
- `release_service` must be exactly the one non-critical, five-replica service shown above, with no
  downstream dependents and at least one non-overlapping path prefix.
- Thresholds are non-negative integers and `small` must not overlap `large`.
- One model attempt is mandatory; no retry, fallback model, truncation, chunking, or best-of path is
  configurable.
- `incident_history.enabled` must be `false`. No incident file is accepted.
- Profile names, stages, weights, pod counts, analysis flags, tier mapping, and analysis identities must
  equal the built-ins above. There is no custom-profile or override parser.
- Rollout strategy is fixed to no traffic router, `maxSurge: 1`, and `maxUnavailable: 0`.
- Fast analysis is `null`; Guarded and Strict use the one trusted compatibility probe.
- The whole validated catalog hash is included in `assessment_input_sha256`; the resolved entry hash
  is copied into any accepted assessment safeguard, while every resolved non-Fast decision carries
  the hash of its AI-selected or policy-fallback entry.
- Catalog hashing follows `contract.md`: parse and validate YAML, reject duplicate keys and YAML
  aliases/merges, convert the model to canonical JSON, then hash either the whole model or one entry.
- The Ollama context/prediction pins are the measured 8,192/768 configuration from the nominated demo
  laptop. Changing them requires a new policy version and a repeated live-model gate.

## `trusted-probes.yaml` v1

The shared catalog contains exactly one entry before the pre-final:

```yaml
schema_version: "1"
catalog_version: "2026.08.1"
probes:
  - id: demo-api-public-quote-v1
    binding:
      service: demo-api
      finding_kind: breaking_api
      hypothesis_kind: removed_http_route_unavailable
      verification_intent_kind: preserve_removed_http_route
      method: GET
      path: /v1/quote
    assertion:
      expected_status: 200
    execution:
      target: http://demo-api-canary.safelane-demo.svc.cluster.local
      probe_image_key: demo-api-public-quote-probe@2026.08.1
```

The engine reads the binding and assertion only. It resolves the entry after extracting method/path
from verified source. The compiler revalidates the canonical entry hash, reads the target, and
resolves `probe_image_key` against image catalog v1. The trusted-probe entry hash covers the complete
`id`/`binding`/`assertion`/`execution` entry. The model receives no catalog execution values and cannot
return the probe ID.

## `image-catalog.json` v1

The catalog produced by `prepare-demo.ps1` uses the exact JSON shape frozen in `contract.md`:

```json
{
  "schema_version": "1",
  "catalog_version": "2026.08.1",
  "application_images": [
    {
      "repository": "AndrewMaged814/safelane-demo",
      "service": "demo-api",
      "source_revision": "a1b2c3d4e5f6789012345678901234567890abcd",
      "image_ref": "safelane-demo:a1b2c3d4e5f6789012345678901234567890abcd",
      "image_id": "sha256:...",
      "runtime_image_id": "sha256:...",
      "oci_revision": "a1b2c3d4e5f6789012345678901234567890abcd"
    }
  ],
  "probe_images": [
    {
      "key": "demo-api-public-quote-probe@2026.08.1",
      "probe_id": "demo-api-public-quote-v1",
      "image_ref": "safelane-http-probe:2026.08.1",
      "image_id": "sha256:...",
      "runtime_image_id": "sha256:..."
    }
  ]
}
```

The real catalog has exactly three application entries—Warm-up, Fast, Strict—and one probe entry.
`image-catalog-v1.schema.json` rejects unknown fields, duplicate identities/keys, invalid full SHAs,
image IDs, or runtime IDs, a tag not derived from the full application SHA, an OCI revision different
from the source revision, and a probe key/ID mismatch with `trusted-probes.yaml`. The catalog is already
JSON, so its SHA-256 uses the canonical JSON bytes defined in `contract.md`.

## Image preparation

`prepare-demo.ps1` builds each fixture from a detached clean worktree at its full SHA, labels the OCI
image with `org.opencontainers.image.revision`, records the inspected image ID, preloads it into kind,
records the normalized containerd runtime image ID, and writes image catalog v1. It also records the
separately built and preloaded probe image in that
catalog; `trusted-probes.yaml` owns only its key. The release command looks up both images
independently; it never copies an image reference from `decision.json`.

## Evaluation inputs

Three small AI fixtures are checked in:

1. `fast-copy` — bounded response-copy change; expects no finding or proposal.
2. `additive-route` — adds `/v2/quote` while retaining `/v1/quote`; expects no breaking finding or
   proposal.
3. `quote-contract-break` — removes `/v1/quote` and adds `/v2/quote`; expects the exact finding and
   proposal defined in `contract.md`.

Each manifest contains its stable ID, canonical diff fixture and hash, expected normalized AI result,
exact accepted spans, and forbidden result. Fast and breaking cases also name their frozen demo SHAs.
`additive-route` is adapter-evaluation input only; it creates no fourth demo commit, application image,
or release path. The manifests contain no incident candidates, historical repository, profile draft,
or expected natural-language prose.
