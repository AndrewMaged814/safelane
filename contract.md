# SafeLane — the interface between the two halves

**v3, revised 2026-08-01** after verification against Argo Rollouts source and the Phase 1 risk-policy and Studio decisions.
Andrew's half writes `decision.json`. Ahmed's half reads it and nothing else.
Neither person needs the other's code to make progress. Changes need both people to agree.

While Andrew's scorer doesn't exist yet, Ahmed hand-writes `decision.json` files and works against those.
While Ahmed's cluster doesn't exist yet, Andrew validates output against the schema and eyeballs the render.

> **One open decision, due Sunday 2 August**, with Ahmed's machine in front of you: `traffic_router`.
> `none` (default, honest, zero extra work) or `nginx` (real fine-grained traffic weights, ~2 working
> days, 25% odds). Everything below works either way. Do not write code that assumes `nginx`.

---

## `decision.json`

```json
{
  "schema_version": "2",
  "policy_version": "2026.08.1",
  "change": {
    "sha": "a1b2c3d",
    "pr": 42,
    "title": "Bump payout retry limit",
    "files_changed": 3,
    "lines_changed": 47,
    "services": ["payouts-api"],
    "shipping_at": "2026-08-20T21:40:00+03:00"
  },
  "risk": {
    "score": 80,
    "tier": "risky",
    "confidence": "high",
    "main_risk": {
      "category": "availability",
      "title": "Retry protection was removed",
      "explanation": "Without a retry limit, workers may exhaust the connection pool during an upstream failure. The same behavior caused incident INC-003.",
      "source": "ai",
      "evidence_verified": true,
      "evidence": ["config/retries.yaml:18", "INC-003"]
    },
    "reasons": [
      "AI finding: retry limit was removed in config/retries.yaml.",
      "Incident connection: INC-003 identifies unlimited retries as the earlier trigger.",
      "Impact: payouts-api has 3 downstream dependents."
    ]
  },
  "lane": {
    "name": "strict",
    "profile_source": "built_in",
    "traffic_router": "none",
    "replicas": 5,
    "steps": [
      { "set_weight": 20, "exposure_pods": 1, "pause_seconds": 30 },
      { "set_weight": 40, "exposure_pods": 2, "pause_seconds": 30 },
      { "set_weight": 60, "exposure_pods": 3, "pause_seconds": 30 },
      { "set_weight": 100, "exposure_pods": 5, "pause_seconds": 0 }
    ],
    "analysis": {
      "error_rate_threshold": 0.05,
      "interval_seconds": 10,
      "measurement_count": 3,
      "failure_limit": 1,
      "consecutive_error_limit": 2
    }
  }
}
```

### Field rules

- **`tier`** — exactly one of `safe` | `guarded` | `risky`. Nothing else, ever.
- **`lane.name`** — the resolved rollout profile. Built-ins are `fast` | `guarded` | `strict`; custom names are allowed after policy validation.
- **`profile_source`** — `built_in` | `custom` | `ai_assisted`. AI-assisted still means human-approved and normal-code validated.
- **`score`** — integer 0–100. Advisory to humans; **the tier is what drives behaviour.** Never branch on the raw score.
- **`main_risk`** — Studio's first explanation: the strongest verified failure scenario after considering the whole assessment. It is not a predicted incident root cause. Categories are `availability` | `data` | `security` | `compatibility` | `performance`; `source` is `ai` | `rule`. An AI-sourced Main risk is displayed only after normal code verifies every cited evidence reference.
- **`reasons`** — 1 to 4 supporting plain-English strings, each readable aloud on stage. Studio collapses these by default so they do not compete with `main_risk`.
- **`confidence`** — `high` | `low`. **`low` requires at least the guarded profile, whatever the score says.** Default-safe posture; must be visible in the code, not just the pitch.
- **`policy_version`** — bump whenever thresholds change, so a past decision stays explainable.
- **`traffic_router`** — `none` | `nginx`. Documents what the weight numbers *mean*, so nobody has to guess. With `none`, weight is approximated by replica count — see below.
- **`exposure_pods`** — the honest number when `traffic_router: none`. Always state it alongside `set_weight` so the two can never drift.
- **`shipping_at`** — ISO timestamp with offset. Feeds the timing signal (see scoring inputs).
- Unknown fields are ignored by the consumer. Missing required fields are a hard error, never a silent default.

### Weights are approximate, and the numbers must admit it

With no traffic router, Argo Rollouts does **not** split traffic by percentage — it approximates the
weight using replica counts (`utils/replicaset/canary.go`). At `replicas: 5`, weights of 1, 5, 10 and
20 **all produce exactly one canary pod**.

So the vocabulary of legal `set_weight` values at 5 replicas is **{20, 40, 60, 80, 100}** and nothing
else. Any other number is a lie that a judge can catch with `kubectl get pods`.

### Tier → profile, the demo defaults

At `replicas: 5`, `traffic_router: none`:

| tier | profile | steps (weight → pods) | health behavior |
|---|---|---|---|
| `safe` | fast | 100% (`all`) immediately | Kubernetes readiness only |
| `guarded` | guarded | 40% (2 pods) → checkpoint → 100% (`all`) | service limit |
| `risky` | strict | 20% (1) → checkpoint → 40% (2) → checkpoint → 60% (3) → checkpoint → 100% (`all`) | service limit |

The demo service limit is a configurable 5% maximum error rate. Each checkpoint lasts about 30
seconds and reads Prometheus every 10 seconds. `failureLimit: 1` rolls back on the second unhealthy
or empty reading during a checkpoint. `consecutiveErrorLimit: 2` rolls back on the second
consecutive Prometheus connection or query error. Missing data is never considered healthy.

These values are versioned defaults in `policy.yaml`, not hardcoded constants or universal
recommendations. Full profile behavior and custom-profile validation are defined in
[`docs/rollout-profiles.md`](docs/rollout-profiles.md).

### Scoring inputs (Andrew's side)

Risk inputs, bounded AI findings, evidence requirements, and tier effects are defined in
[`docs/risk-signals.md`](docs/risk-signals.md) and
[`docs/adr/0001-bound-ai-to-risk-findings.md`](docs/adr/0001-bound-ai-to-risk-findings.md).

## Handoff mechanics

- Andrew writes `decision.json` to the demo repo root, and prints it to stdout.
- Ahmed's script **renders a complete `Rollout` manifest** from a template, runs
  `kubectl argo rollouts lint`, then a **single `kubectl apply -f`**.
- **Never `kubectl patch` the Rollout.** Patching `steps` mid-flight resets the step index to 0, and
  patching while the rollout is aborted **clears the abort and resumes toward the broken version** —
  which destroys the one thing the demo exists to prove.
- **Never mix `apply` with `kubectl argo rollouts set image`.** The two silently undo each other.
- **A warm-up revision is required.** Argo skips canary steps on a service's very first deploy and
  goes straight to 100%. Burn one throwaway revision during setup, off-camera.
- **Failure mode:** no `decision.json`, or one that fails schema validation → treat as `tier: risky`,
  `confidence: low`, strict profile. Absence of a decision is never permission to go fast.
