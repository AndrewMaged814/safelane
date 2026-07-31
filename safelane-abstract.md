# SafeLane

**Tagline:** *Every change gets the rollout it deserves.*

**Track:** 1 — Automate Deployment & Operations
**Event:** DevOpsDays Cairo 2026 Hackathon

---

## The problem (in plain words)

Teams ship code faster than ever, but the safety net at deploy time hasn't kept up. When a change goes to production, most teams roll it out the *same way every time* — the same slow-or-fast steps, written by hand in advance — no matter whether the change is a tiny tweak or something that could take down a critical service. A dangerous change and a harmless one get treated identically, and the risky one only gets caught *after* it's already hurting users.

## What SafeLane does

SafeLane looks at each change *before* it ships and asks one question: **how risky is this specific change?** It answers using what the change actually touches — how big it is, which services it hits, and whether those files have caused incidents before. Then it automatically puts the change in the right lane:

- **Safe change → the fast lane.** Ship quickly, minimal friction.
- **Risky change → a guarded lane.** Roll out slowly, watch it closely, and roll it back automatically at the first sign of trouble.

SafeLane hands the actual release to Argo Rollouts (a trusted, existing tool) and shows a live dashboard of the results.

## Why it's new

Two pieces already exist separately: tools that *score* how risky a change is, and tools that *do* careful rollouts. Connecting them is not unheard of — Meta published a system in 2024 that scores every change before it ships and uses that score to pick one of four release-gating levels, and both ServiceNow and Digital.ai sell risk-based automation of release *approvals*.

But every one of those turns risk into a **path choice**: approve or hold, gate or don't gate, review or auto-land. None of them turns it into the **actual rollout settings** — how much traffic the new version gets, how many steps, how long each pause, how strict the health check has to be. And none of it is available to anyone outside a large company's internal platform.

SafeLane is **the first open-source tool that compiles a risk score into real rollout parameters for Argo Rollouts.** Then it **checks its own work**: it measures whether the changes it called "risky" actually failed more often, and says so when the policy is wrong.

## Impact

The headline number is how well the scoring *aims*:

> **We slowed down X% of changes, and that caught Y% of the failures** — where Y is far larger than X.

That is a property of the policy itself rather than a claim about the world, so it holds up even on seeded history, and it is reported against a control group of changes deliberately left unscored.

Alongside it, the number that makes the point viscerally: **how many requests were served the broken version before the rollback fired**, with SafeLane and without it.

DORA metrics — **Change Failure Rate** and **Mean Time to Recovery**, split by risk level — are the secondary view.

## Team

_[Two members — add names/roles]_

---

## High-level diagram

```
                                    SafeLane
   ┌─────────────────────────────────────────────────────────────────┐
   │                                                                   │
   │   A change is        1. SCORE IT          2. PICK THE LANE       │
   │   about to ship  ──►  How risky?      ──►  safe  → fast lane     │
   │   (a pull request)    (what it touches,    risky → guarded lane  │
   │                        past incidents)      (slow + auto-rollback)│
   │                                                   │               │
   │                                                   ▼               │
   │   4. LEARN            3. RELEASE IT                                │
   │   Were the "risky"    Argo Rollouts does the actual rollout       │
   │   ones really    ◄──  and rolls back automatically if the         │
   │   riskier? Get        change starts failing                       │
   │   smarter.                │                                        │
   │        ▲                  ▼                                        │
   │        └──── Dashboard: fewer failures, faster recovery ──────────┘
   │             (DORA metrics, split by risk level)                    │
   │                                                                    │
   └────────────────────────────────────────────────────────────────────┘

   Safe change  →  FAST LANE     ▓▓▓▓▓▓▓▓▓▓  ships fast, done
   Risky change →  GUARDED LANE  ▓░░░░░░░░░  20% → watch → 40% → ⚠ → auto rollback
```
