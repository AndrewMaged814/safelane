# SafeLane — the interface between the two halves

**v2, revised 2026-07-31** after verification against Argo Rollouts source (see `detailed-plan.md` §1.3).
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
    "score": 72,
    "tier": "risky",
    "confidence": "high",
    "reasons": [
      "Touches payouts-api, which has 3 incidents in the last 90 days",
      "Modifies retry/timeout configuration (config change, not code)",
      "Hard to reverse: changes persisted state, so rollback is not just a redeploy",
      "Shipping Thursday night, going into the Friday-Saturday weekend"
    ]
  },
  "lane": {
    "name": "guarded",
    "traffic_router": "none",
    "replicas": 5,
    "steps": [
      { "set_weight": 20, "exposure_pods": 1, "pause_seconds": 30 },
      { "set_weight": 40, "exposure_pods": 2, "pause_seconds": 30 },
      { "set_weight": 60, "exposure_pods": 3, "pause_seconds": 30 }
    ],
    "analysis": {
      "error_rate_threshold": 0.01,
      "interval_seconds": 10,
      "failure_limit": 1
    }
  }
}
```

### Field rules

- **`tier`** — exactly one of `safe` | `guarded` | `risky`. Nothing else, ever.
- **`lane.name`** — `fast` | `guarded`. `safe` tier → `fast`; `guarded` and `risky` → `guarded`.
- **`score`** — integer 0–100. Advisory to humans; **the tier is what drives behaviour.** Never branch on the raw score.
- **`reasons`** — 1 to 4 plain-English strings, each readable aloud on stage. This is the demo's money shot; a reason that needs explaining is a bug.
- **`confidence`** — `high` | `low`. **`low` always routes to the guarded lane, whatever the score says.** Default-safe posture; must be visible in the code, not just the pitch.
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

### Tier → lane, the whole policy

At `replicas: 5`, `traffic_router: none`:

| tier | lane | steps (weight → pods) | error-rate threshold |
|---|---|---|---|
| `safe` | fast | 100% immediately | none (Argo health only) |
| `guarded` | guarded | 40% (2 pods) → 100% | 3% |
| `risky` | guarded | 20% (1) → 40% (2) → 60% (3) | 1% |

Analysis for both guarded tiers: `interval: 10s`, `failureLimit: 1`. That is ~30–45 s to a visible
rollback, which is right for a live demo.

**Known honesty limit:** if the injected fault fails ~100% of canary requests, the 1% and 3%
thresholds trip identically. The tier difference is then real in the file but invisible on screen.
Do **not** claim on stage that the threshold difference is demonstrated. Making it visible needs a
~2% fault rate — that is a stretch beat, not baseline.

### Scoring inputs (Andrew's side)

Six signals, all deterministic, no LLM:

1. Size — lines and files changed
2. Services touched, and how many services depend on them (blast radius)
3. Config change vs code change
4. Incident history on the touched paths (seeded, and the seed file says so)
5. **Reversibility** — is this a plain redeploy to undo, or does it touch persisted state / migrations? Hard-to-undo earns a slow lane regardless of size.
6. **Timing** — shipping into a Friday–Saturday weekend, or late at night, raises the tier.

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
  `confidence: low`, guarded lane. Absence of a decision is never permission to go fast.
