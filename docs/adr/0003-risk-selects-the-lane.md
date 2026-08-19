# Risk selects the lane

Supersedes [0002-eligibility-not-risk](0002-eligibility-not-risk.md); that ADR's real
content survives below, restated as part of a larger split.

Two separate questions decide a release, not one:

- **Eligibility** — decided from evidence — answers whether a change may enter
  production at all. The outcomes are eligible, ineligible, and indeterminate. Evidence
  completeness is still not risk, and still does not by itself choose a rollout
  envelope. This is 0002's original claim, unchanged.
- **Assessment** — decided from Change Facts — answers a second question: how far an
  eligible change may ship per step. The operator declares every lane in advance
  (`policy.yml`); an assessment selects among them by name. No assessor may emit weights,
  and no caller may name a lane.

Two assessors run for every eligible change: a deterministic heuristic the operator owns
sets a floor from facts (files touched, lines changed, path rules, agent-authorship). A
model assessor may run alongside it and may only raise the risk above that floor, never
lower it. The two verdicts combine through one function, `Worse`, and there is no other
path — no override flag, no "trust the model when it is confident". A model can only
ever narrow a lane. It can never widen one, and it never chooses whether the change may
ship at all.

Missing, malformed, or failed assessment resolves to the most cautious configured lane —
never the widest, and never a blocked release. Risk decides width, not entry.
