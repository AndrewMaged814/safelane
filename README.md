# SafeLane

**Every change gets the rollout it deserves.**

SafeLane scores a change *before* it ships, then compiles that score into real
[Argo Rollouts](https://argo-rollouts.readthedocs.io/) parameters — canary steps, pause durations and
analysis thresholds. A safe change goes straight out. A risky change is exposed to a fraction of
traffic, watched closely, and rolled back automatically at the first sign of trouble.

Built for the **DevOpsDays Cairo 2026 DevOps Hackathon**, Track 1 — Automate Deployment & Operations.

---

## The gap this fills

Tools that score how risky a change is already exist. Tools that run careful progressive rollouts
already exist. Connecting them isn't unheard of either — Meta published a system in 2024 that scores
each change and picks one of four release-gating levels, and ServiceNow and Digital.ai both sell
risk-based automation of release *approvals*.

But all of those turn risk into a **path choice**: approve or hold, gate or don't gate, review or
auto-land. None of them turns it into the **rollout's actual numbers** — how much traffic the new
version gets, how many steps, how long each pause, how strict the health check has to be. And no
open-source implementation of any version of this exists.

SafeLane is the first open-source tool that compiles a risk score into real rollout parameters for
Argo Rollouts.

## How it works

1. **Score** — six deterministic signals over the git diff: size, config-vs-code, blast radius across
   the service graph, incident history on the touched paths, how hard the change is to reverse, and
   when it's shipping. No LLM in the decision path; thresholds live in one versioned policy file.
2. **Decide** — the score becomes a tier (`safe` / `guarded` / `risky`) and a lane, written to
   `decision.json` with plain-English reasons.
3. **Compile** — the tier is rendered into a complete Argo `Rollout` manifest: canary steps, pauses,
   and per-tier analysis thresholds.
4. **Release** — Argo Rollouts executes it and aborts automatically when the error rate breaks the
   threshold. SafeLane doesn't reimplement progressive delivery; it decides how progressive delivery
   should behave.
5. **Check its own work** — outcomes are recorded per tier, so the policy can be measured: *how many
   changes did we slow down, and what share of the failures did that catch?*

Default-safe by construction: low confidence in a score always routes to the guarded lane, and a
missing or invalid decision is treated as risky. Being wrong sends a change to a slower rollout,
never a faster one.

## Honest limitation, stated up front

On a local cluster with no service mesh or ingress controller, **Argo Rollouts approximates canary
weight using replica counts** — it does not split traffic by percentage. At five replicas, a weight of
20% means one pod of five. The policy emits real traffic weights unchanged once a traffic router is
configured; that's a configuration line, not a redesign.

The steps are therefore `20 → 40 → 60` rather than `1 → 5 → 25`: at five replicas those are the only
values that are literally true, and each one visibly moves a pod.

## Status

Pre-final prototype, in development. Nothing here is production software.

| Phase | What it proves | State |
|---|---|---|
| 1 | Automatic rollback works on a real cluster | not started |
| 2 | A scored change becomes a rollout lane | not started |
| 3 | Demo-ready: risk panel, one-command demo, rehearsed | not started |

## Repository layout

| Path | What it is |
|---|---|
| `plan.md` | Decisions, owners, gates, open items |
| `contract.md` | The frozen `decision.json` interface between the two workstreams |
| `detailed-plan.md` | Verified implementation plan — architecture, day-by-day, risks, cut list |
| `research/prior-art.md` | Competitive landscape and the novelty audit behind the claim above |
| `safelane-abstract.md` | The submitted hackathon abstract |
| `safelane-qa.md` | Anticipated judge questions, with the soft spots marked honestly |
| `safelane-brief.html` | One-page visual brief (a fragment — see below) |
| `build-preview.sh` | Wraps the brief into a browser-openable page with Mermaid |
| `hackathon.md` | The hackathon's own rules, dates and evaluation criteria |

`safelane-brief.html` has no `<head>` or `<body>` by design; it is published as a hosted artifact and
the host supplies those. To read it locally, run `bash build-preview.sh` and open the generated
`safelane-brief.local.html`.

## Before this repo goes public

It is private for now, and several files are internal working documents — they contain honest odds,
soft spots and a pre-decided cut list that are useful to the team and unhelpful to publish. Remove
these first:

```
detailed-plan.md
research/prior-art.md
safelane-qa.md
plan.md
```

Keep `contract.md`, `safelane-abstract.md` and this README.

## Licence and attribution

MIT — see [`LICENSE`](./LICENSE). MIT was chosen because the code SafeLane borrows from is MIT, which
keeps the attribution story simple.

Any code derived from third-party projects is recorded in `THIRD_PARTY_NOTICES.md` with a per-file
header naming the original function and what was changed. In particular
[DeployWhisper](https://github.com/deploywhisper/deploywhisper) (MIT) is the reference for blast-radius
traversal and the seeded-incident file format.
