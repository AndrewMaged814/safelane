# Pre-final rollout profiles and trusted analysis

**Version:** 2 · **decision date:** 2026-08-09

SafeLane uses three immutable built-in profiles for one five-replica service without a traffic router.
Pod counts are the honest primary unit; `setWeight` is the Argo representation, not a promise of an
exact request or user percentage.

The Rollout fixes `maxSurge: 1` and `maxUnavailable: 0`. “1 of 5 pods” means one canary pod for a
service configured with five desired replicas; surge may temporarily keep an additional stable pod,
so it is never presented as an exact traffic fraction.

## Built-in profiles

| Risk tier | Profile | Resolved stages |
|---|---|---|
| `safe` | `Fast` | all 5 pods immediately; readiness only |
| `guarded` | `Guarded` | 2 pods → trusted Job analysis → all 5 pods |
| `risky` | `Strict` | 1 pod → analysis → 2 pods → analysis → 3 pods → analysis → all 5 pods |

The exact decision stages are:

| Exposure | `setWeight` | Trusted analysis |
|---:|---:|---|
| 1 of 5 pods | 20 | yes |
| 2 of 5 pods | 40 | yes |
| 3 of 5 pods | 60 | yes |
| all 5 pods | 100 | no |

Fast uses only the final row. Guarded uses the second and final rows. Strict uses all rows. The
compiler rejects any different replica count, surge/unavailable setting, weight/pod pair, order,
analysis flag, profile source, or router. Each `analysis: true` emits one inline analysis step directly
after its `setWeight`; no timed pause is emitted. The compiler does not derive stages from the tier; `decision.json`
already contains the resolved profile.

## Trusted compatibility analysis

Guarded and Strict use `demo-api-public-quote-v1`, a `job_http_contract_probe` resolved from the
versioned trusted-probe catalog. Its probe image key resolves through image catalog v1; image
references and IDs do not live in the probe catalog. The AnalysisTemplate launches the pinned Job through
`canaryService`, which must select only the current head-SHA ReplicaSet.

The frozen Job behavior is:

- request `GET /v1/quote` against the trusted canary-Service target;
- expect HTTP 200;
- make exactly 3 attempts, 10 seconds apart, each with a 2-second request timeout;
- permit 1 failed request and exit nonzero when failures exceed that allowance;
- use `restartPolicy: Never`, `backoffLimit: 0`, and `activeDeadlineSeconds: 45`; and
- emit one final schema-checked probe-result-v1 JSON log line for the verification receipt.

The exact log shape is:

```json
{
  "schema_version": "1",
  "probe_id": "demo-api-public-quote-v1",
  "observations": [
    {"attempt": 1, "outcome": "http_response", "http_status": 404},
    {"attempt": 2, "outcome": "timeout", "http_status": null},
    {"attempt": 3, "outcome": "connection_error", "http_status": null}
  ],
  "failures": 3,
  "failure_allowance": 1,
  "result": "failed"
}
```

Every field is required and unknown fields are rejected. Attempts are contiguous from 1 through 3.
`outcome` is `http_response`, `timeout`, or `connection_error`; `http_status` is an integer from 100
through 599 only for `http_response` and otherwise null. `failures` must equal the number of outcomes
other than HTTP 200, and `result` is `failed` exactly when failures exceed the allowance. A missing,
duplicate, malformed, or internally inconsistent final line makes receipt verdict `inconclusive`.
The Job treats transport errors as failures for rollout safety, but receipt semantics do not treat
them as evidence of the predicted HTTP response: the positive prediction verdict needs more than one
actual non-200 HTTP response, and “not observed” needs all observations to be HTTP 200. Mixed or
transport-only evidence is inconclusive.

The application keeps Kubernetes readiness on `/ready`. In the Strict fixture the canary remains
Ready but the compatibility probe observes 404, separating a client-contract regression from a
startup failure.

A Job that starts and exits nonzero may fail the AnalysisRun and trigger Argo's automatic abort. A
Job that never starts is `inconclusive`, not evidence that the predicted application failure occurred.
The compiler annotates the Rollout with canonical decision and release-request hashes under
`safelane.dev/decision-sha256` and `safelane.dev/release-request-sha256`. The release
adapter starts from that exact Rollout, requires metadata/observed generation equality, and follows
Kubernetes owner UIDs through each AnalysisRun, Job, and probe Pod; it never selects a resource by
name prefix or newest timestamp. While each probe runs, it snapshots the canary Service selector and
EndpointSlice, proves every endpoint Pod belongs to the head ReplicaSet, and records application and
probe runtime image IDs for comparison with image catalog v1. To prove
analysis-triggered abort on Argo v1.9.1, the receipt requires `status.abort: true`, non-null
`status.abortedAt`, phase `Degraded`, and the `Progressing=False` condition reason `RolloutAborted`
whose message contains `Step-based analysis phase error/failed`, as well as a preceding linked failed
AnalysisRun completion timestamp. Normal code classifies any abort without that exact signature as
`external_or_unknown`; it never claims Kubernetes state identifies a human actor. Any such abort or
missing link is inconclusive.

## Studio boundary

Studio displays the three built-ins and the resolved pod-count preview. It does not create, edit, or
generate profiles. There is no custom profile, AI profile generator, policy mutation, deploy button,
Argo dashboard, or exact traffic-percentage claim in the pre-final runtime.
