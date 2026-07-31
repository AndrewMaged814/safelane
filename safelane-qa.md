# SafeLane — Anticipated Q&A (Pitch Prep)

One-page prep for the questions judges are most likely to ask. Each answer is written
to be said out loud in ~20–30 seconds. Legend: **[Strong]** = safe, **[Defensible]** =
say it sharply, **[Soft spot]** = be honest, don't oversell.

---

## Prior art — name these yourself, before a judge does

Say this early, in one breath, as part of the pitch. A pitch that names its own nearest
neighbours cannot be accused of not having looked.

| Who | What they do | Where they stop |
|---|---|---|
| **Meta — Diff Risk Score** (arXiv 2410.06351, Oct 2024) | Scores every change before it ships and uses the score to pick one of four release-gating levels, in production | Picks *whether and when* to gate. Not how much traffic. And it only exists inside Meta. |
| **ServiceNow** DevOps Change Velocity + Now Risk Intelligence | ML change-risk prediction; auto-approves low-risk changes, routes risky ones to manual sign-off | The approval layer — who signs off, how many gates. It never touches traffic. |
| **Digital.ai Release** | Sells *"a fast-lane for low-risk software release changes"* — their words, nearly our tagline | Fewer approval gates. Nothing found says it alters rollout mechanics — **don't assert either way**, say the marketing is ambiguous. |
| **Akuity / Kargo — Promotion Advisor** (blog 29 Jul 2026) | Scores risk from the diff plus deployment history, inside a GitOps promotion tool | Advisory. It tells a human. It does not change canary weights or analysis strictness. **Re-check Sun 16 Aug — this is the row most likely to move.** |
| **DeployWhisper** (open source, MIT) | Best open-source pre-deploy scorer: blast radius, incident history, rollback complexity | Refuses to act, by design — exits 0 whatever the verdict, no rollout code at all. Literally the other half of SafeLane. |

The line that ties it together: **"every one of those turns risk into a path choice — approve or
hold, gate or don't. None of them turns it into traffic percentages, step counts and thresholds.
That's the gap, and there is no open-source version of it."**

---

## Novelty — "Isn't this just X?"

**Q: Meta published this in 2024, and Dell patented risk-based deployment-strategy selection in
2020. What's left?** **[Soft spot — concede the concept, claim the implementation]**
Bring this up yourself. What's left is granularity and availability. Meta's Diff Risk Score chooses
*whether and when* to gate a change; SafeLane chooses *how much exposure* it gets — weights, step
count, pause length, analysis thresholds. And none of it is usable by anyone without Meta's internal
platform. We're not claiming we invented the idea. We're claiming this is the first open,
Argo-native implementation of it. Citing your own strongest counter-example reads as rigour.

**Q: Isn't this just a YAML generator plus a policy file?** **[Soft spot — own it completely]**
Mechanically, yes. That is exactly what it is. The value is that the policy is *one* artifact
instead of fifty hand-tuned canary files, and that the back-test tells you when the policy is
wrong. Denying this is worse than conceding it.

**Q: ServiceNow and Digital.ai already sell risk-based release fast-lanes. Isn't this a commercial
product resubmitted?** **[Strong — but only if phrased tightly]**
Both operate on the approval and change-management layer: who signs off and how many gates. Neither
reaches into traffic splitting, and neither is Kubernetes progressive-delivery native. Different
actuator, different layer.


**Q: Isn't this just an Argo AnalysisTemplate?** **[Strong]**
An AnalysisTemplate checks "is the canary healthy right now?" during the rollout. It
never looks at the change itself, and it's the same static config for every change.
SafeLane decides, *before anything ships*, how slow the rollout should be and how
strict that health check should be, based on how risky this specific change is. The
AnalysisTemplate is actually one of the things SafeLane generates — it's the muscle,
we're the brain that sets it.

**Q: Isn't this just Kargo / Harness CV / Flagger?** **[Defensible — the old answer was wrong]**
Flagger and Harness CV are runtime: they verify during or after the deploy and know nothing about
the change itself. Kargo is different now — as of July 2026 Akuity's Promotion Advisor *does* score
risk from the diff and from deployment history. But it stops at advising a human; it doesn't alter
canary weights or analysis strictness. Say that precisely, and don't claim nobody scores pre-deploy —
they do.
(**Re-check Sun 16 Aug.** If Akuity ships actuation before the assessment, the honest fallback is:
"they proved the demand; ours is open source and parameterises the rollout, not just the promotion.")

**Q: You're just wrapping Argo with an LLM.** **[Defensible]**
The LLM only writes the plain-English explanation. The core is a deterministic,
versioned, auditable policy plus a feedback loop. Turn the LLM off completely and
SafeLane still works exactly the same.

---

## Mechanism — the hard ones

**Q: If the AnalysisTemplate already auto-rolls-back on bad metrics, why does the risk
score matter? The runtime check catches it anyway.** **[Defensible — the key answer]**
Because of blast radius before the brake fires. A fast rollout hits 50% of users
before the health check trips. A risk-aware slow rollout catches the same failure at
1–5% — same rollback, far fewer users hurt. And some failures, like data corruption or
slow-burn latency, don't trip a simple error-rate check at all. We decide how much to
expose before we trust the change.

**Q: How do you score risk from a diff? Isn't that just noisy heuristics?** **[Soft spot]**
We don't claim a perfect model — we use transparent, explainable heuristics: size,
services touched, config-vs-code, blast radius, and incident history. What makes it
honest is the back-test: we continuously check whether the changes we called "risky"
actually failed more often. If they don't, the policy is wrong and we say so. The proof
isn't the score, it's that the tiers track real outcomes.

**Q: What if you call a dangerous change "safe" and fast-track it?** **[Soft spot]**
We're default-safe. The fast lane requires positive evidence of safety; anything
unknown or low-confidence goes to the guarded lane. Being wrong sends a change to a
slower rollout, not a riskier one.

**Q: Who writes the policy thresholds? Isn't that the same hand-tuning, moved up a
level?** **[Defensible]**
Today teams write canary config per service — 50 services, 50 hand-tuned files.
SafeLane replaces that with one risk-to-strategy policy across all of them, and the
back-test calibrates it from real outcomes. Less manual work, and it corrects itself.

---

## Impact

**Q: Does this actually improve DORA, or is it just a dashboard over made-up data?**
**[Defensible — the framing does the work]**
Our history is seeded and we say so plainly; the seed file itself is stamped as synthetic. So we
don't lead with "failures went down" — that would be a claim about the world, and made-up data can't
support it. We lead with how well the scoring *aims*: **we slowed down X% of changes and that caught
Y% of the failures**, measured against a control group we deliberately didn't score. That's a
property of the policy, and seeded data can demonstrate it honestly. DORA is the secondary slide.
The mechanism itself — limit exposure for risky changes — is standard Google SRE canary practice.

**Q: Incident history is a key signal, but most teams don't have clean incident data.
Cold start?** **[Soft spot]**
Content-based risk (size, blast radius, config-vs-code) works on day one with zero
history. The incident-memory signal grows as the team uses it. We're upfront that a
brand-new team gets the content signal first and the memory advantage over time.

---

## Practicality

**Q: Two people, ~8 weeks, K8s + Argo + Prometheus + scorer + dashboard + back-test —
realistic?** **[Defensible]**
We build in gates: get auto-rollback working first, then the scorer and policy, then
the dashboard, then the back-test. Each stage has a pass/fail check before we move on.
The riskiest part, the cluster and Argo plumbing, is the very first thing we prove.

---

## The one thing to remember on stage

The soft spots (scoring noise, false negatives, synthetic data) all share one defense:
**the back-test loop and the default-safe posture.** They are not extras — they are what
makes SafeLane honest instead of theater. Lead the pitch with the lane idea; when
pressed, fall back to "we check our own work."

And name the five neighbours in the first two minutes, unprompted. Nothing else in the pitch buys as
much credibility for as little time — and it takes the single most dangerous question off the table
before anyone can ask it.
