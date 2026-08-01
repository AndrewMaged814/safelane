# Phase 1 SafeLane Studio

**Decision date:** 2026-08-01
**Decision owner:** Andrew
**Status:** approved prototype direction

SafeLane Studio is a small local review and policy tool. Its first job is to help a developer understand why a pull request needs a careful rollout. Its second job is to manage the rollout profiles used by that decision.

It is not a deployment dashboard. Argo remains responsible for showing and controlling live rollouts.

## Navigation

Phase 1 has two main navigation items:

- **Changes** — pull-request assessments that need review, plus resolved assessments.
- **Profiles** — built-in and custom rollout profiles.

Opening a change or creating a profile uses a focused subpage inside those areas. This keeps the main pages simple without forcing complex work into a modal.

## Change lifecycle

Studio stores one assessment per pull request, not one row per push.

- A new push replaces the current assessment for that PR.
- A new push invalidates any earlier human approval.
- `safe` changes resolve automatically with the Fast profile.
- `guarded` and `risky` changes stay in **Needs review** until a person approves the suggested rollout or chooses something more careful.
- Approval records the rollout decision and moves the PR to **Resolved**. It does not deploy anything.

Phase 1 may keep this state in local files or Git-backed artifacts. It does not require accounts or a database.

## Changes page

The default page is one combined **Needs review** list. It is ordered by risk tier first (`risky` before `guarded`) and latest push second. Risky and Guarded are not separate page sections.

Each row shows only:

- service and PR identity;
- change title, branch, and latest-push time;
- one compact risk-tier label;
- one fixed risk category;
- the short Main risk title; and
- the suggested rollout lane.

Risk tier and suggested lane remain separate fields. A developer may choose a stricter profile, a custom profile may be based on Strict, and an organization may configure a tier to use a more careful profile. Therefore `Strict` does not always mean the change was classified `risky`.

The **Resolved** view contains automatically resolved safe changes and human-approved guarded or risky changes. It is ordered newest first.

## Assessment details

The assessment uses one reading column rather than a dashboard grid. Its order is fixed:

1. PR identity and risk tier.
2. Suggested rollout profile.
3. Main risk.
4. Rollout stages and health checks.
5. Collapsed supporting evidence.
6. Approval actions.

The primary action names the plan, such as **Approve Strict rollout**. A secondary **Choose a safer profile** action never allows a faster override. Helper text must say that approval records the plan but does not deploy it.

Exact evidence remains available but is collapsed by default so it does not compete with the Main risk.

## Main risk

The Main risk is the strongest verified failure scenario found after considering the whole assessment. It is not a claim that SafeLane knows the root cause of a failure that has not happened.

For Phase 1, a Main risk contains:

- `category` — one of `availability`, `data`, `security`, `compatibility`, or `performance`;
- `title` — a short 5–10 word description used in the Changes list;
- `explanation` — one or two plain-language sentences used on the assessment page;
- `source` — `ai` or `rule`;
- `evidence_verified` — whether normal code verified every cited source; and
- `evidence` — exact changed lines, incident references, or repository facts that support it.

The category describes what could go wrong, not what type of file changed.

Local Ollama may propose the category, title, explanation, and evidence references. Normal code verifies the references, and fixed policy rules still choose the risk tier and rollout lane. Unsupported AI output is rejected. If Ollama is unavailable or no AI finding survives verification, Studio shows the strongest rule-based reason instead.

The UI label is **AI finding · evidence verified** when both facts are true. Normal verified results do not show a confidence score. Studio shows a warning only when evidence is incomplete or the assessment fell back to rules.

The approved prototype displays one Main risk. The engine evaluation will decide whether real changes require support for more than one independently serious Main risk.

## Profiles page

Profiles are separate from change assessment so profile editing does not distract someone reviewing a PR.

The page shows:

- the Fast, Guarded, and Strict built-ins;
- existing custom profiles;
- a primary **Generate profile with AI** action; and
- a secondary **Start manually** action.

Both creation paths use the same editor and fixed validation. AI pre-fills one draft; manual creation starts from a built-in profile. Both show a YAML preview and require human approval.

## Prototype

The approved throwaway prototype lives in `prototypes/safelane-studio`.

Run it from the repository root:

```powershell
python -m http.server 4173 --directory prototypes/safelane-studio
```

Then open <http://localhost:4173/?page=changes>.

The prototype keeps state in browser memory and never writes policy files or deploys anything.

## Out of scope

- live deployment controls or rollout monitoring;
- accounts, roles, or permissions;
- a database;
- historical analytics;
- drag-and-drop rollout editing;
- AI chat or multiple generated alternatives; and
- deciding the final maximum number of Main risks before engine evaluation.
