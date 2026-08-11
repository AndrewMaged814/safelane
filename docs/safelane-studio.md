# SafeLane Studio

**Version:** 3 · **decision date:** 2026-08-12

SafeLane Studio is a local review surface for the open pull requests of one connected GitHub
repository. It explains why a specific PR revision needs a specific rollout profile and records
explicit approval when required. It does not merge, deploy, or monitor releases.

## Repository and PR lifecycle

Studio connects through the authenticated GitHub CLI to either a local checkout's `origin` or an
explicit GitHub URL or `owner/repository` slug. The Changes route lists only open pull requests. It
never assesses or displays an uncommitted working-tree diff without a pull request.

The repository chip in the top bar opens the connection dialog. A valid local path, GitHub URL, or
repository slug switches the active repository and loads its isolated state directory. Validation
failure leaves the current repository active and shows the provider error inside the dialog.

For every listed PR, Studio fetches the immutable GitHub comparison identified by the discovered
full base and head SHAs. Assessments and decisions are stored by PR number in the local Studio state
directory. A new head SHA creates a new assessment identity and invalidates the earlier review and
decision. Closed PRs disappear from the active inbox without deleting their local audit files.

These `studio-pr-assessment-v2` and `studio-pr-review-v1` records are explicitly scoped to local
Studio review. They are not canonical `assessment-v2` / `decision-v3` release authorization and
must never be consumed by deployment tooling.

Fast path recognition is deliberately conservative across repositories: every changed file must be
either inside the configured release-service prefixes or a Markdown documentation file at
`README.md` or under `docs/`. Other source paths remain at least Guarded until that repository has
an explicit service mapping.

Fast resolves automatically. Guarded and Risky remain unresolved until the user approves a built-in
profile at least as careful as the policy minimum. The inbox separates Needs review from Resolved.
Every approval surface says that approval records a rollout plan but does not deploy it.

## Navigation

- **Changes** — live open-PR inbox, lane, reason, and review state.
- **PR dossier** — exact revision identity, source-verified findings, policy reason, rollout preview,
  and approval.
- **Profiles** — read-only Fast, Guarded, and Strict built-in rollout definitions.

## Fast view

Show the repository, pull request, head SHA, positive bounded-scope evidence, Safe tier, Fast profile,
and `Resolved automatically`. Do not invent a failure hypothesis, safeguard, approval question, or
remediation when the assessment has no verified finding.

## Guarded and Risky views

Show the deterministic policy reason first. When the bounded model returns a valid finding, show its
category, severity, normal-code-rendered explanation, and exact removed or added source spans.
Label these spans as source references verified: normal code confirms only that the cited text exists
at the claimed changed-line identity. It does not claim that the model's interpretation is true, and
model-authored prose is never displayed as trusted explanation.

When no model finding survives validation, show the policy fallback reason and evidence status. Do
not invent a finding or display rejected model values. Every rollout preview comes from the
assessment's server-owned `rollout_options`; the browser does not load or reinterpret policy.

## Approval contract

The only state-changing production UI action is **Approve selected rollout**. The browser submits the
expected assessment ID, head SHA, policy version, assessment-input hash, assessment-result hash, and
selected built-in profile. The server refreshes open PR state, loads the current assessment, and
compare-and-swaps those identities. It does not accept a client-supplied assessment object.

The server atomically replaces the resolved PR assessment first and creates or replaces its local
decision last. A stale page, wrong hash, wrong SHA, invalid profile, closed PR, or changed PR is
rejected. A local approval does not write to GitHub and does not deploy software.

## Visual requirements

- Preserve the prototype's Changes, dossier, and Profiles information architecture.
- Pair color with text; Safe is green, Guarded is amber, and Risky is red.
- Render verified source spans in readable monospace blocks with removed/added labels.
- Work at laptop and narrow viewport widths without a frontend framework.

## Out of scope

- policy or profile creation, editing, overrides, or Generate with AI;
- chat, model self-critique, or free-form executable configuration;
- GitHub writes, merging, deploy buttons, kubectl, rollout polling, and receipt ingestion;
- accounts, RBAC, database, notifications, analytics, and history UI; and
- generated tests, commands, patches, or arbitrary probe configuration.
