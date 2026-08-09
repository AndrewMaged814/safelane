# SafeLane — Pre-final execution plan

**Version 3.2 · revised 2026-08-09 · finishing is the primary goal**

This is the only summary schedule for the pre-final build. The expanded architecture, frozen
contracts, slices, acceptance checks, risks, and demo flow are in
[`detailed-plan.md`](detailed-plan.md). The pre-August-9 implementation plan is historical and remains
available in Git history.

The accepted Wayfinder decisions remain useful design input, but they describe more product than the
pre-final needs. Version 3.2 keeps the v3.1 delivery boundary and strengthens one vertical feature:
AI turns verified code evidence into a bounded safety case and trusted verification intent, fixed
policy chooses the rollout, and Argo enforces it. The model does not generate deployment behavior.

## Definition of done

> On one nominated laptop, SafeLane assesses two checked-in, SHA-backed changes to one five-replica
> demo service. The Fast fixture resolves automatically to the Fast profile. For a consumer-facing
> `/v1/quote` → `/v2/quote` rename, one bounded Ollama call produces a source-backed safety case:
> the endangered contract, predicted client impact, trusted verification intent, approval question,
> and bounded remediation. Normal code verifies both route spans, binds the intent to an allowlisted
> compatibility probe, and renders the explanation. SafeLane Studio shows that causal chain and waits
> for explicit Strict approval. The SHA-bound decision causes a real Argo Rollout to stop at its first
> one-pod exposure stage after the canary-only probe observes the predicted contract failure. Argo
> automatically aborts the update; the Rollout reports `Degraded`, the failed ReplicaSet scales down,
> and the stable ReplicaSet serves again. A normal-code receipt binds the prediction, exact decision,
> trusted probe, observed statuses, and Argo resources. The complete sequence succeeds twice from a
> defined namespace reset, and a backup recording exists.

“Fast” means that every check in the bounded demo policy completed and no configured safety floor
fired. It is not proof that a change is safe. With no traffic router, one canary pod is a pod-count
stage, not a guaranteed percentage of requests or users.

## Pre-final scope

The build must contain:

- one local repository and one non-critical deployable demo service with five replicas;
- a healthy warm-up revision, one Fast revision, and one consumer-facing `/v1/quote` → `/v2/quote`
  revision, each built from and identified by its full Git SHA;
- one bounded local `qwen2.5-coder:7b` call per assessment that returns only a typed
  `breaking_api` safety case with exact diff spans, failure-hypothesis kind, verification intent,
  approval-question kind, and remediation kind;
- deterministic rejection of fabricated evidence and unknown fields, deterministic rendering of all
  user-facing text, and normal-code binding from the verified intent to one trusted probe;
- the small retained rule table in `detailed-plan.md`, with one-way safety floors;
- `assessment.json` for review and a separate approved `decision.json` runtime handoff;
- automatic Fast resolution and explicit human approval for the Risky/Strict path;
- a minimal Studio assessment view with the evidence → impact → safeguard → rollout preview, a
  visible validation ledger, and one approval action;
- trusted local image and probe catalogs, strict decision validator, and complete-manifest Rollout
  compiler;
- a real kind cluster, Argo Rollouts v1.9.1, and a pinned canary-only compatibility Job with bounded
  retry and deadline behavior;
- fixture-level tests, one end-to-end integration test, a causally bound verification receipt, a safe
  namespace reset, two complete runs, and a backup recording.

Job analysis is locked for the pre-final. Prometheus is not a conditional branch in this schedule.

## Ownership

| Owner | Owns |
|---|---|
| **Andrew** | `SafeLaneEngine`, Ollama adapter, safety-case validation and rendering, policy and artifact schemas, demo Git fixtures, minimal Studio, and expected assessment/decision fixtures. |
| **Ahmed** | `RolloutCompiler`, independently created release requests, trusted image/probe catalogs and image preparation, kind/Argo manifests, canary compatibility Job, receipt evidence collection, lint/dry-run/apply command, reset script, and rollout runbook. |
| **Both** | Contract v5 sign-off, one fixture handshake before implementation diverges, organizer clarification, end-to-end integration, recording, slides, and rehearsals. |

`decision.json` is the only runtime SafeLane-to-release handoff. Schemas, frozen fixture SHAs, release
requests, and the trusted image/probe catalogs are shared integration contracts. Andrew does not render
Kubernetes YAML; Ahmed does not recompute risk tiers or map them to profiles.

## Gates

| Gate | Target | Pass condition |
|---|---|---|
| **0 — decision lock and machine** | **10 Aug** | Contract-v5 semantics and wire versions—including the typed AI safety case and trusted-probe binding—are signed off; decision and release-request schemas validate one placeholder-identity Fast/Strict schema-example pair (the real identities freeze in Gate 1); the demo laptop runs Docker, kind, Ollama, kubectl, and the pinned Argo plugin; questions about the commercial-solution rule and presentation duration have been sent to the organizer. |
| **1 — infrastructure kill shot** | **11 Aug** | From reset, a hand-written Fast revision first promotes as the stable base; the following hand-written Strict Rollout selects one canary pod, the pinned Job exits nonzero with retries disabled, and Argo automatically aborts and reports `Degraded` while that Fast stable ReplicaSet serves again. The sequence passes twice. |
| **2 — walking skeleton** | **12 Aug** | A fake AI adapter drives assess → verified safety case or Fast result → automatic or human resolution → decision → compiled manifest. Fabricated spans, unknown intent kinds, and untrusted probe keys cannot reach render/apply. The three Git revisions, remaining executable schemas, and canonical goldens exist. |
| **3 — Andrew decision spine** | **15 Aug** | Real Git evidence and live Ollama produce the expected Fast result, additive-route non-break result, and complete `/v1/quote` safety case twice each; Studio shows the validated causal chain and approval emits the expected Strict decision. Interface-level tests are green. |
| **4 — integrated proof** | **17 Aug** | Each run follows reset → warm-up → Fast promotion → approved Strict rollout → trusted probe observes the predicted contract failure → first-stage automatic abort. The whole sequence passes twice and writes/renders the bound verification receipt. |
| **5 — insured demo** | **19 Aug** | Prior-art and eligibility due diligence was frozen by 18 Aug; an eight-minute backup recording and first English slide deck exist. No new feature work after this gate. |
| **6 — frozen** | **22 Aug** | Two timed rehearsals fit the confirmed slot, or both 10- and 20-minute run sheets are rehearsed if no answer arrived; demo state, reset/runbook, recording, and exact commands are frozen. |

Missing a gate is not “one day behind”; it invalidates the remaining schedule. Re-scope that day
rather than compressing every later gate.

## Explicitly out of the pre-final critical path

- custom profile creation or editing;
- Generate profile with AI;
- incident-history ingestion or incident matching;
- shipping-window risk or time-bounded decision reuse;
- stored-data, access-control, and retry/backoff finding types;
- multi-chunk or over-16-KiB AI analysis;
- the 12-case × two-run model challenge and the Trellis historical smoke run;
- arbitrary AI-authored prose, probes, tests, code, shell, URLs, images, credentials, Kubernetes YAML,
  rollout stages, tiers, or approvals;
- a second model pass, chat interface, self-critique loop, or AI-generated post-abort diagnosis;
- payout-idempotency runtime behavior or probes in addition to the route-compatibility scenario;
- backtesting, self-learning, DORA/CFR/MTTR, synthetic outcome charts, or a SafeLane rollout dashboard;
- GitHub API ingestion, Actions, PR comments, webhooks, or CI glue;
- Prometheus, nginx, a service mesh, fine-grained traffic routing, or exact request-percentage claims;
- multi-service or multi-cluster release decisions;
- custom profile overrides, accounts, RBAC, a database, or deployment controls in Studio;
- a trained model, continuous risk probability, security-scanner ingestion, or secret deployment;
- copied third-party source code.

These are not hidden promises. After the complete pre-final demo passes twice, the first final-round
product choice is whether to **replace** the route fixture with a payout-idempotency invariant; never
carry both demo implementations at once. Broader model evaluation and incident context come next,
then profile authoring and CI integration. DORA or predictive claims wait for real deployment
outcomes.

## Locked implementation decisions

The planning pass applied these decisions to contract v5 and the canonical domain documents. Gate 0
signs them off and creates the executable schemas; it does not reopen them:

1. Policy v2 accepts only the non-critical demo release service with no downstream dependents, so
   Fast is reachable and unused topology branches do not enter the runtime.
2. Missing, invalid, stale, identity-mismatched, or unapproved decisions reject release; Strict is not
   a substitute for authorization. Workspace commands share a lock, and a successful replacement
   assessment invalidates the prior decision before publish.
3. Phase 1 releases exactly one directly changed service from a linear Warm-up → Fast → Strict Git
   graph. Decisions and independent release requests bind both base and head SHAs; the release
   adapter verifies the current stable ReplicaSet is the assessed base. A clean-worktree preparation
   script ties each head SHA to one inspected local image tag, OCI revision label, Docker image ID,
   and kind/containerd runtime image ID.
4. Assessment output contains a deterministic policy trace, exact source spans, one validated safety
   case, and a result hash over the immutable reviewed result. The quote-route rename requires both
   the removed old route and added new route. The model proposes typed meanings and exact source-span
   objects plus one bounded finding index; normal code verifies them, renders prose, and never labels
   semantic interpretation as verified. A shallow envelope and finding/proposal components validate
   separately, so a bad proposal cannot erase a valid dangerous finding.
5. Incident history is explicitly `disabled_by_policy`, uses a fixed hashed sentinel, and neither
   lowers confidence nor blocks Fast. Shipping time is not a retained signal.
6. The incompatible request, policy, AI-response, assessment, and decision shapes receive new wire
   versions; no migration compatibility is required because no runtime exists.
7. The `breaking_api` safety-case schema, allowed hypothesis/question/remediation kinds,
   intent-to-probe binding, Job-analysis decision shape, trusted probe catalog, retry/deadline
   controls, and Argo outcome mapping are frozen before compiler work. Ollama cannot supply
   executable fields.
8. Evaluation gates assert assessments. Decision goldens add an explicit automatic or human
   resolution event with a fixed timestamp.
9. Canonical JSON serialization is fixed before golden files are written.
10. Image catalog v1 alone owns application/probe image references and inspected IDs; the trusted
    probe catalog owns semantic/execution binding and references the probe image by a versioned key.
11. Resolved profile stages carry an analysis boolean, not an ambiguous timer. Policy-owned Job
    settings—including the 45-second deadline—are copied into every non-Fast decision.
12. Receipt v1 is verdict-discriminated. Positive proof requires the annotated Rollout plus the full
    Rollout → AnalysisRun → Job → Pod UID chain, equal generations, actual runtime images, a
    probe-time canary-to-head snapshot, structured HTTP evidence, and Argo's analysis-triggered abort
    fields. Transport-only, mixed, fallback, missing, or external-abort evidence is inconclusive.
13. The decision schema and compiler both enforce the only authorization matrix: Safe/Fast/automatic,
    Guarded/(Guarded or Strict)/human with policy fallback, and Risky/Strict/human with AI-selected or
    fallback analysis; analysis is null only for Fast.

## Current facts, not assumptions

- Runtime engine and end-to-end integration have not started in this repository.
- On Andrew's machine, Python 3.12, `uv`, Ollama 0.32.1, the 7B model, Docker CLI, and kubectl exist.
- The measured live-model configuration is `num_ctx: 8192` and `num_predict: 768`; policy v2 keeps
  those pins unless a replacement configuration passes the same laptop gate.
- Docker Desktop is currently stopped; `kind`, Helm, and `make` are absent.
- The exact assessment slot and presentation duration are still unknown, so the plan targets the
  earliest possible date, 23 August 2026, and treats the 20-minute format as provisional.
- No pushed artifact proves Ahmed's earlier Argo/Prometheus gate. The revised plan removes
  Prometheus and moves the real canary-Job abort to the first infrastructure slice.
