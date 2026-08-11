# Pre-final SafeLane Studio

**Version:** 2 · **decision date:** 2026-08-09

SafeLane Studio is one local review surface for the current assessment. It explains why a specific
change needs a specific safeguard and records explicit approval when required. It does not deploy or
monitor releases.
The selected workspace remains explicit throughout the review flow.

## Navigation and lifecycle

The runtime has one current-assessment route; an inbox may be added only if time remains. It loads a
fixed local workspace. A new head SHA creates a new assessment identity and invalidates the prior
page and approval. All workspace commands share one exclusive local lock; after validating the new
request/Git/policy inputs, `assess` removes the prior decision before publishing the replacement
assessment, so the old head cannot remain releasable beside a newer unresolved page.

Fast resolves automatically. Guarded/Risky remains unresolved until the user approves a built-in
profile at least as careful as the policy minimum. The page always says that approval records the
rollout plan but does not deploy it.

## Fast view

Show:

- repository, pull request, full head SHA, and policy version;
- every positive Fast eligibility check;
- Safe tier and Fast profile preview; and
- `Resolved automatically`.

Do not invent a failure hypothesis, safeguard, approval question, or remediation for Fast.

## Risky safety-case view

Use one linear reading order:

```text
Breaking contract
    -> removed and added source spans
    -> AI-proposed failure hypothesis
    -> 2/2 source references verified
    -> trusted compatibility probe selected by SafeLane
    -> first exposure: 1 of 5 pods
    -> approval question and bounded remediation
    -> Approve Strict rollout
```

The page shows the deterministic policy trace and exact rendered strings owned by `contract.md`.
Every selectable stage preview comes from the assessment's normal-code `rollout_options`; Studio does
not load or reinterpret policy.
Labels must distinguish:

- **AI proposed** — the semantic hypothesis/intent came from the bounded model response;
- **source references verified** — normal code found the exact cited diff spans; and
- **trusted probe selected by SafeLane** — normal code resolved the verified intent to the catalog.

The probe preview reads only the normal-code `selected_safeguard.probe_preview` projection inside the
assessment: `GET /v1/quote`, expected 200, three attempts, and canary-only targeting. Studio does not
load the raw model response, policy, or either catalog, and it never displays raw model content as
executable configuration.

The approval question is informational; Studio does not collect an answer. The remediation is
advisory; Studio does not generate or apply a patch and never promises that it will earn Fast.

## Non-Fast views without an AI-linked safeguard

Whenever `selected_safeguard` is null on a Guarded/Risky assessment, Studio shows `Policy fallback
analysis will run after approval`; it does not invent a hypothesis or selected safeguard. This covers
both kinds of state:

- a medium Guarded or high Risky scope baseline may have complete empty AI evidence, high confidence,
  and no uncertainty floor; show the baseline reason and its minimum profile; and
- a low-confidence assessment shows every uncertainty floor. If a verified Risky finding survived a
  rejected proposal, show that finding and floor, label the AI-linked safeguard unavailable, and never
  render rejected proposal values.

In every case the page offers only built-in profiles at least as careful as the minimum and remains
unresolved until approval.

## Approval contract

The only state-changing production UI action is **Approve selected rollout** (shown as **Approve
Strict rollout** in the demo Risky case). The browser submits the
expected assessment ID, head SHA, assessment-input hash, and assessment-result hash. The server loads
the current assessment, compare-and-swaps all four values, and calls `SafeLaneEngine.approve`; it does
not accept a client-supplied assessment object.

The server atomically replaces the resolved assessment first and creates/replaces `decision.json`
last. A stale page, wrong hash, wrong SHA, invalid profile, or already changed workspace is rejected.
After success, Studio shows `Resolved` and the decision path.

## Visual requirements

- Present source evidence, safeguard or fallback notice, and rollout preview in one column that works at laptop width.
- Pair color with text/icon; Fast is green and Strict is red, but color is never the only signal.
- Render code spans in a readable monospace block with removed/added labels.
- Keep the causal chain visible without animation; polish must not introduce a frontend framework.

## Out of scope

- policy/profile creation, editing, overrides, or Generate with AI;
- chat, multiple AI alternatives, self-critique, or free-form narrative;
- deploy buttons, kubectl, rollout polling, verification-receipt ingestion, history, accounts, RBAC,
  database, notifications, and analytics; and
- generated tests, commands, patches, or arbitrary probe configuration.
