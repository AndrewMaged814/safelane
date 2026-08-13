# SafeLane Studio

**Version:** 4 · **decision date:** 2026-08-12

SafeLane Studio is the local review and compilation surface for open pull requests in one connected
GitHub repository. It explains why the backend proposed a rollout, records approve/reject/later
review intent, compiles an approved exact-head decision into Argo Rollout YAML, and summarizes bound
outcome receipts. It does not merge a pull request or apply a manifest to a cluster.

## Repository and PR lifecycle

Studio connects through the authenticated GitHub CLI to a local checkout's `origin`, a GitHub URL,
or an `owner/repository` slug. Changes lists only open pull requests and never shows an uncommitted
working-tree diff.

Every connected repository must own `.safelane/policy.yaml` and its referenced trusted-probe
catalog. `ChangeSafety` reads both files from the exact PR base SHA, fetches the exact base/head diff,
and rechecks the head before publishing. A PR may change its contract for future assessments but
cannot weaken the contract assessing itself.

Studio and `safelane assess-pr` consume the same `change-assessment-v1` bytes and
`rollout-decision-v1` authorization. A new head removes the earlier decision before the replacement
assessment is published. Closed PRs disappear from the inbox without deleting their audit records.

## Navigation

- **Changes** — live open-PR inbox, backend proposal, policy reason, and review state.
- **PR dossier** — exact revision, policy/probe provenance, source-verified AI safety case, rule
  result, rollout preview, review controls, and approved-release compiler.
- **Profiles** — read-only repository-owned Fast, Guarded, and Strict definitions.
- **Outcomes** — receipt counts by tier. The API also exposes rule/finding calibration buckets.

## Evidence and policy presentation

The backend—not AI—chooses the risk tier and minimum profile. The local model may return one bounded
safety-case category plus exact changed-line references. Normal code verifies those references,
maps the category through base-owned policy, selects a versioned trusted probe, and renders the
explanation. The model cannot emit severity, a tier, a profile, a probe, a command, or a manifest.

When no model finding survives validation, show the deterministic fallback and AI evidence status.
Use **Evidence confidence** only for evidence completeness; never imply model probability. Every
preview comes from the assessment's repository-owned `rollout_options`.

## Review and release contract

Fast resolves automatically. Guarded and Risky support:

- **Approve** — binds actor, allowed profile, assessment ID, result hash, and exact head; emits a
  rollout decision.
- **Reject** — records rejection and emits no rollout decision.
- **Decide later** — performs no mutation and returns to the inbox.

An approved reviewer can submit an immutable image digest only after trusted CI registers it in the
signed local repository image catalog. The catalog entry binds repository, service, full source
revision, digest, and OCI revision. The server revalidates the assessment, signed decision, current
PR head, base policy, trusted-probe catalog, signed image identity, and exact derived manifest before
writing a schema-valid Argo Rollout. Missing, stale, rejected, or mismatched authorization emits no
manifest. A new assessment removes any compiled manifest for the older authorization.

When GitHub App credentials have Checks write permission, SafeLane creates an exact-head Check Run.
Unresolved reviews are `action_required`, approvals are `success`, rejections are `failure`, and a
new head cancels the earlier run before creating the replacement. Check delivery is a projection;
it never becomes release authority. Studio visibly reports unavailable delivery; authenticated-user
and OAuth tokens cannot create Check Runs, so demos that need this projection must use GitHub App
credentials.

Outcome ingestion accepts only observations whose stages match the compiled profile. The resulting
receipt binds assessment, decision, manifest, image, probe, rule IDs, finding IDs, and exact Git
revision. Summary counts describe observed outcomes; they are not a model accuracy score.

## Visual requirements

- Preserve the prototype's Changes, dossier, and Profiles information architecture.
- Pair color with text; Safe is green, Guarded is amber, and Risky is red.
- Show full head SHA and base-owned policy/probe provenance.
- Render source spans in monospace blocks with removed/added labels.
- Work at laptop and narrow viewport widths without a frontend framework.

## Out of scope

- policy/profile editing or model selectors;
- chat, AI-generated fixes, commands, or executable probes;
- merging, production cluster application, automatic Argo polling, or rollback ownership;
- accounts, RBAC, notifications, DORA analytics, or multi-repository dashboards.
