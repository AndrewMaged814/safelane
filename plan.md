# SafeLane — Pre-final Plan (decisions and owners)

**v2, revised 2026-07-31** after prior-art research and technical verification.

- **The day-by-day schedule, diagrams, file tree, demo commands, risks and cut list live in
  [`detailed-plan.md`](./detailed-plan.md).** That is the build document. This file holds only the
  decisions, the owners, and the open items — so there is never a second competing schedule.
- Prior-art landscape and the positioning problem: [`research/prior-art.md`](./research/prior-art.md)
- The interface between the two people: [`contract.md`](./contract.md)

---

## The deadline

Passed screening 2026-07-30. Pre-final is a **20-minute virtual assessment**, live demo, **in English**,
scheduled somewhere in **23 Aug – 8 Sep 2026**. Exact slot TBA by email.

**We plan to Sunday 23 August.**

That is **15 working days**, not 22. The Egyptian weekend is Friday–Saturday, which removes 7 days,
and **both 1 and 22 August are Saturdays** — 22 August was the reserved rehearsal day. Realistic
budget is about **70 hours per person**.

## Definition of done

> On a live call, two pull requests go through SafeLane. The trivial one ships straight to 100% and
> stays green. The risky one is scored risky, released to 1 pod of 5, fails its error-rate check, and
> is rolled back automatically by Argo Rollouts — while a panel shows the score, the reasons, and why
> those rollout steps were chosen.

If that is true, we are done. Nothing else is required.

## Owners

| | |
|---|---|
| **Ahmed Anany** (DevOps) | kind, Argo Rollouts, Prometheus, demo service, fault injection, auto-rollback, rendering and applying the Rollout. Consumes `decision.json`. |
| **Andrew** (Dev) | Risk scorer, policy YAML, lane generator, CI glue, the SafeLane risk panel. Produces `decision.json`. |

The two halves meet **only** at `decision.json`. Gate schedule: Gate 1 Sun 2 → Sun 9 Aug ·
Gate 2 Mon 10 → Sun 16 Aug · Gate 3 Mon 17 → Sat 22 Aug. Details in `detailed-plan.md` §4.

## Decisions made 2026-07-31

1. **Real kind cluster + real Argo Rollouts**, not a simulated rollout engine.
2. **No "1%".** With no traffic router, Argo approximates weight by pod count — at 5 replicas, weights
   of 1, 5, 10 and 20 all give one canary pod. Risky lane is **20% → 40% → 60%** (1 → 2 → 3 pods),
   which is both visible and literally true. Fine-grained weights via ingress-nginx stay an **option
   to decide on Sunday 2 August**, not a commitment.
3. **Render and apply the Rollout manifest. Never patch it.** Patching while aborted clears the abort
   and resumes the broken version.
4. **Don't build the rollout dashboard** — `kubectl argo rollouts dashboard` already ships one, and
   it's interactive. Build only the SafeLane risk panel. ~1 day of UI instead of 3.
5. **Headline metric changes.** Instead of "change failure rate went down" — which is arguable on
   seeded data — lead with **"we slowed down X% of changes and that caught Y% of the failures."**
   That describes how well the scoring aims, which seeded data can honestly demonstrate. DORA stays
   as a static backup slide, since the rules name it.
6. **Two scoring signals added:** reversibility (hard-to-undo earns a slow lane regardless of size)
   and timing (shipping into the Fri–Sat weekend raises the tier).
7. **Backup video moves to Tuesday 19 August** and is treated as a hard deadline.

## Gate checkpoints

- **Sun 9 Aug — Gate 1:** auto-rollback demonstrably works from a script on a fresh cluster. If not,
  the demo becomes a simulated rollout engine and we say so on stage. Decide on the 9th, not the 20th.
- **Sun 16 Aug — Gate 2:** both demo PRs produce correct, stable `decision.json` with reasons a judge
  can read aloud.
- **Tue 19 Aug — backup video recorded.** Hard deadline.
- **Thu 20 Aug — two full rehearsals** with a stopwatch, after rereading `safelane-qa.md`.

## Out of scope — say this plainly if asked

Multi-cluster. Auth/RBAC. Real incident ingestion. Any ML or trained model. Terraform/IaC parsing.
More than one demo service. LLM narrative generation. Comparisons against Kargo/Flagger in code.
**Service mesh of any kind** — if fine-grained weights are needed it is ingress-nginx or nothing.
**More than one Rollout object.** **Argo CD** — this is Argo *Rollouts*, a different product.
The back-test loop stays **stretch**; until it exists it is a seeded chart and we say so.

## Open items

- [ ] **Ahmed runs kind + Argo Rollouts on his own machine.** Target Sun 2 Aug. Every estimate in
      `detailed-plan.md` is soft until this happens. This is the single biggest unknown.
- [ ] **Sun 2 Aug joint decision:** `traffic_router` = `none` or `nginx`.
- [ ] `make` is not installed on Andrew's machine — so `make cluster` is not currently a command
      anyone can run. Install it or use plain scripts.
- [ ] Names and roles into `safelane-abstract.md`.
- [ ] Credit DeployWhisper as design inspiration for the service graph, incident-pack shape, and
      missing-context rule. Phase 1 copies no upstream code, so no notice file is currently needed.
      If that boundary changes, add the full MIT notice and per-file provenance before committing.
- [ ] **Sun 16 Aug: re-check Akuity / Kargo.** Their Promotion Advisor (blog dated 29 Jul 2026) already
      scores risk from the diff plus deployment history, advisory-only today. This is the prior-art row
      most likely to move before the assessment.
- [x] **Positioning rewrite — done 2026-07-31.** "Nobody connects them" is gone from
      `safelane-abstract.md`, replaced with *"the first open-source tool that compiles a risk score into
      real rollout parameters for Argo Rollouts."* `safelane-qa.md` now opens with the five nearest
      neighbours and answers for the three hardest prior-art questions. See `research/prior-art.md`.
