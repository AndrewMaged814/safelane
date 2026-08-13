# Change-risk / deployment-risk / PR-risk engines (2026)

**Question:** What engines already assess software-change risk well, how do they use AI vs deterministic policy, how do they set thresholds, how do they name the output, how do they verify LLM claims, and how do they distinguish "danger found" from "analysis incomplete"?

**Research date:** 2026-08-13  
**Scope:** Engine shape, not local-model constraints. A capable hosted LLM (Azure OpenAI) is assumed available.  
**Output path:** `research/change-risk-engines-2026.md`

This note follows every claim to a primary source (official docs, papers, source code, first-party APIs). If a claim could not be sourced, it is marked as such. Secondary blogs are used only as pointers, never as the owning source.

---

## 1. Method

For each system the record answers:

- What it actually predicts or decides
- Inputs
- How AI is used (if at all): score? findings? review comments? eligibility?
- Who makes the final decision
- Threshold philosophy (learned vs policy vs arbitrary)
- How incomplete evidence is handled
- What SafeLane should steal vs avoid

Related SafeLane terms from `CONTEXT.md`: **change assessment**, **AI safety case**, **source reference verified**, **change-scope band**, **safety floor**, **evidence confidence**, **risk tier**, **fast-lane eligibility**. The current glossary still names the carefulest policy result `risky`. This note argues that label is a product mistake if the engine is choosing caution, not predicting failure. That conflicts with the current glossary; see §14.

---

## 2. Short answers before the system records

**What already works in production is a funnel, not a score.** Meta DRS ranks diffs by learned SEV likelihood, then a *policy percentile* chooses a gate. RADAR adds eligibility heuristics, an LLM reviewer, and deterministic validation, and still does not let the LLM pick the landing decision alone. DeployWhisper scores heuristically first, optionally lets an LLM retint severities, then *normal code* rolls up `go` / `caution` / `no-go`. CodeRabbit custom checks emit `Passed` / `Failed` / `Inconclusive`. Copilot reviews never approve or block. Google SRE sizes exposure from error budget, not from a pre-deploy risk number.

**AI is almost never the rollout authority.** Where AI is strongest, it classifies the *change* (safe vs risk signals, findings, narrative). Where a numeric risk score exists, it is either a trained classifier with a historical outcome (SEV, regression, change failure) or a handcrafted heuristic. Prompting an LLM to emit a numeric score is explicitly called unreliable by Meta.

**Thresholds that survive contact with production are operating points, not truths.** Meta gates the top 5% / 10% / 50% of ranked diffs and reports recall of SEVs captured. Mozilla's regressor builds bands from observed regression rates relative to the average, with a minimum sample. Digital.ai's gate threshold is an operator-entered percentage. gitStream's "large PR" and "tiny change" cutoffs are YAML policy. Arbitrary 2-files/50-lines cutoffs are in that last family: they are policy, and they should be named as policy.

**"Risky" is a bad label for a caution engine.** DeployWhisper's rollup is `go` / `caution` / `no-go`. RADAR talks about *eligibility* and *auto-accept*. gitStream talks about *safe-changes* and extra review. Google talks about *exposure*. NIST separates likelihood, impact, and uncertainty. Calling a medium-scope change `risky` implies a failure prediction the engine did not make.

**Citation existence ≠ semantic support.** Gao et al. (ALCE) split citation *recall* (do the cited passages fully support the claim?) from citation *precision* (are the citations relevant?). Qodo validates that walkthrough pointers resolve to real files and lines *before* issue agents run. DeployWhisper sanitizes numeric blast-radius claims against allowed scopes. Meta DRS does not cite code spans at all for the score; the LLM contributes a token-probability score.

**Incomplete analysis is a first-class state.** DeployWhisper refuses to emit a confident `go` when context score < 0.7; it upgrades `go` → `caution` and prefixes `INSUFFICIENT CONTEXT`. CodeRabbit custom checks return `Inconclusive` when the sandbox cannot gather required evidence. RADAR routes any failed layer to human review. Meta imputes a missing LLM score as the mean rather than treating absence as safety. NIST requires uncertainty to be explicit, not buried.

---

## 3. Meta Diff Risk Score (DRS)

**Primary sources**

- Rui Abreu et al., *Moving Faster and Reducing Risk: Using LLMs in Release Deployment*, arXiv:2410.06351, https://arxiv.org/abs/2410.06351 and HTML https://arxiv.org/html/2410.06351
- Engineering at Meta, *Diff Risk Score: AI-driven risk-aware software development*, 2025-08-06, https://engineering.fb.com/2025/08/06/developer-tools/diff-risk-score-drs-ai-risk-aware-software-development-meta/

### What it actually predicts or decides

DRS predicts **how likely a diff is to cause a SEV** (a severe production incident affecting end users). It is a ranking model. The operational claim is not "this diff will fail with probability p." It is: capture **Y% of SEVs by gating X% of diffs, where Y ≫ X**. A naive random gate would capture X% of SEVs by blocking X% of diffs; DRS exists to beat that baseline.

The 2025 engineering post restates the same target: "predicts the likelihood of a code change causing a production incident, also known as a SEV," and reports that DRS helped eliminate major code freezes (example: 10,000+ changes landed during a 2024 partner-event freeze that previously could not have landed).

### Inputs

**Logistic regression (production baseline at paper time):** ~12 selected predictors from ~100 candidates, including log added/deleted SLOC relative to file size, new-file flags, log number of files, log number of authors who modified changed files, previous SEV in file/folder, high-criticality service, logical complexity, language indicators, whether the author created the file, and number of diffs previously landed by the author.

**LLM path:** diff title, test plan (commands the author ran), and unified-diff code changes. No hand-curated metadata features are required for the generative models.

### How AI is used

Three research approaches, not a free-form reviewer:

1. **Logistic regression** on metadata (the production model at paper time).
2. **StarBERT** (RoBERTa-based), fine-tuned to classify SEV-causing diffs. It *underperformed* logistic regression (0.61× / 0.85× / 0.81× SEVs captured at the three gates).
3. **Generative LLMs** (iCodeLlama-34B, iDiffLlama-13B):
   - Embeddings + MLP classifier, or
   - **Risk alignment:** supervised fine-tune to generate a `0`/`1` label after `[DRS][/DRS]` markers. At inference, the **risk score is the token probability of labels 0 and 1**, not a prompted number.

The paper is explicit that prompting a generative model for a numeric score is "quite tedious" and "unreliable, as the model does not have a universal view of risk and may generate uncalibrated scores." Few-shot is infeasible because a diff already fills much of the context window.

Risk-aligned iDiffLlama-13B (change-aware, smaller) beat the larger code-only model and beat logistic regression: 1.40× / 1.52× / 1.05× SEVs captured at weekend / yellow / red gates. The paper also ensembles: LLM score is an extra logistic feature; **if the LLM score is absent (timeout, etc.), it is imputed as the mean**.

The 2025 blog says DRS is now "built on a fine-tuned Llama LLM" and "powers many risk-aware features," including a Risk Awareness Platform with "19 use cases and growing." The blog does not publish a new recall table; the quantitative gating numbers below remain those of the 2024 paper.

### Who makes the final decision

**Policy + human escalation, not the model.** During freeze, any diff above the gated threshold is **blocked from landing**. The developer can wait out the freeze, reduce the score by changing the diff, or escalate to managers with a standard reason. The Phabricator UI shows the score, reasons the diff is considered risky, and potential actions. The engineer writing/reviewing the code is the landing authority at Meta scale; DRS is a gate, not an auto-deploy.

Gating levels named in the paper: **no gating (green), weekend gating (weekend), medium impact on end-users (yellow), high impact on end-users (red)**. Weekend gating exists because fewer engineers are available to handle SEVs. Red (50%) is used around high-traffic periods such as Black Friday.

### Threshold philosophy

**Learned ranking + policy operating points.** The model ranks diffs. The *input parameter* is the gating level: take the top X% riskiest diffs. Reported production logistic numbers:

| Gate | Fraction gated | SEVs captured (logistic) |
|---|---|---|
| Weekend | top 5% | 18.7% |
| Yellow | top 10% | 27.9% |
| Red | top 50% | 84.6% |

These X% values are **chosen for the freeze calendar and on-call capacity**, not derived from a calibrated probability cutoff. The evaluation mines historical diffs that did or did not cause SEVs.

### Incomplete evidence

Sourced: missing LLM score is imputed as the mean of the feature, then logistic regression still runs. Not sourced: how DRS behaves if the diff is truncated, the test plan is empty, or embeddings fail. Do not invent a "fail closed" story for DRS itself; RADAR is where fail-closed funnel behavior is documented.

### Steal vs avoid

**Steal**

- Outcome definition: SEV/incident, not "feels risky."
- Metric: recall@top-X%, always vs a random/not-scored control.
- Do not prompt an LLM for a numeric risk score. If you must score with an LLM, use class-token probabilities after task alignment, or keep the LLM off the score entirely.
- Ensemble: LLM understands the diff; metadata model understands author/file/history. Missing LLM → impute, don't treat as safe.
- Name gates after *operating conditions* (weekend, freeze) rather than after a moral verdict.

**Avoid**

- Copying Meta's 5/10/50 percentiles as if they were portable. They are Meta freeze knobs.
- Claiming SafeLane "predicts SEVs." You do not have Meta's labeled history.
- Letting the model land the diff. DRS blocks; humans escalate.

---

## 4. Meta RADAR (Risk Aware Diff Auto Review)

**Primary source**

- Chris Adams et al., *Automating Low-Risk Code Review at Meta: RADAR, Risk Calibration, and Review Efficiency*, arXiv:2605.30208, https://arxiv.org/abs/2605.30208 and HTML https://arxiv.org/html/2605.30208v2

No first-party Meta engineering blog for RADAR was found as of this research date. The paper is the owning source.

### What it actually predicts or decides

RADAR does **not** predict incidents as its primary output. It decides **whether a diff is eligible for automated review and landing**. It is a multi-stage funnel: authorship classification → eligibility gates → static heuristics → DRS threshold → LLM Automated Code Review (ACR) → deterministic validation → land or route to human review.

Scale reported: 535,290 diffs reviewed, 331,720 landed, peak 25K reviewed/day. Revert rate of RADAR-reviewed diffs is 1/3 that of non-RADAR diffs; production-incident rate is 1/50 (Fisher exact p-values reported). Authors note this is observational, not a causal estimate. All PIs were manually reviewed; none were judged as something a human reviewer would have caught.

### Inputs

Authorship type (human vs bot); automation source (deterministic codemod, AI codemod, RACER runbook); author role/tenure/oncall; scope (open-source, SOX, extra-review paths); diff state (WIP, RFC, rejected, latest, CI allowed); blocklisted phrases and path prefix/suffix lists; DRS percentile; ACR classification of each change against predefined **safe signals** and **risk signals**.

Safe signals include: refactoring without behavioral change, dead-code removal, defensive programming, logging, formatting, docs/comments, import hygiene, test additions, static resource updates. Risk signals include: high calculated review effort (complexity score ≥ 4), substantial structural changes, identified bugs/logic errors, performance risks, security issues (secrets, SQLi, auth bypass).

### How AI is used

**Three distinct AI roles, none of which is "pick the rollout":**

1. **DRS** (learned score) is an *eligibility gate*. Percentile `PX` means only the lowest-risk X% of diffs qualify. Lower P is more conservative (P5 = safest 5%).
2. **ACR / RADAR Review Agent** (LLM) *reads the code* and classifies changes into safe/risk categories, and can accept or reject for the automated pipeline. Auto-accept requires **confidence ≥ 8/10 and all changes in safe categories**. Any risk signal disqualifies auto-accept.
3. **RACER** is a *code-generating* agent whose diffs *enter* RADAR; it is not the risk engine.

Deterministic codemods with Blanket AutoAccept **bypass per-diff AI review** because the transformation was vetted at the codemod level.

### Who makes the final decision

Layered:

- **Policy** (OrgRADARPolicyConfig) sets DRS thresholds, whether deferred review is enabled, which sources are allowed.
- **Funnel**: any failed layer routes to human review.
- **Human author** of a human-authored diff retains control: ship with RADAR Approval, wait for a human, or return to Needs Review.
- Approved bot diffs land after a **configurable delay** during which a human can still reject.
- Runbooks that caused incidents go on a **denylist**. Daily caps (10–2000 diffs/runbook/day) prevent flooding.

Defaults: allowlisted RACER runbooks P50; non-allowlisted bots P20; human-authored diffs **P5**. OrgA can effectively bypass DRS for bot diffs and rely on ACR.

### Threshold philosophy

**Policy operating points on a learned score, tuned by observed yield/safety.** RQ2 reports relaxing DRS from p25 to p50; current RADAR-approve rate 60.31%, Verification pass rate 26.31%. Safety outcomes are reported as remaining at 1/3 revert and 1/50 PI vs non-RADAR — but **not broken out per percentile**, so one cannot claim p50 is as safe as p25 from the paper's tables. The paper treats the threshold as an operator knob: stricter reduces risk and yield; looser increases throughput.

ACR's 8/10 confidence and "any risk signal → reject" are **conservative policy**, not a calibrated probability.

RACER runbook eligibility also uses a **60-day lookback**: zero PIs, low revert rate, low human rejection, minimum landed diffs "to establish statistical confidence." That is empirical gating of *sources*, not of individual diffs.

### Incomplete evidence

Sourced, strongly:

- Failure at **any** of static heuristics / DRS / ACR routes to human review.
- Human diffs require CI in an allowed state; WIP/RFC/rejected/non-latest are ineligible.
- AI-generated (non-deterministic) codemods cannot be blanket-approved; each diff must pass ACE.
- The paper does not document an "LLM timed out therefore auto-land" path. The funnel is conjunctive.

Not sourced: exact behavior when ACR returns malformed JSON. Do not invent it.

### Steal vs avoid

**Steal**

- Funnel: eligibility → heuristics → score gate → LLM semantic review → deterministic validation. All must pass for Fast.
- LLM classifies *what the change is* (safe vs risk signals), not the rollout.
- Conservative auto-accept: high confidence **and** no risk signal.
- Source-type differentiation: mechanical transforms ≠ LLM-generated diffs ≠ human diffs.
- Org-configurable appetite, with a strict default (P5 for humans).
- Delay window after auto-land.
- Report automation yield and safety against a non-automated control.

**Avoid**

- Auto-landing from an LLM accept alone.
- Treating "no comments" as "safe." RADAR requires positive passage through every layer.
- Copying 8/10 as if it were a measured calibration. It is a policy floor.

---

## 5. Mozilla Bugbug regressor

**Primary sources**

- https://github.com/mozilla/bugbug README
- https://github.com/mozilla/bugbug/blob/master/docs/models/regressor.md
- https://github.com/mozilla/bugbug/blob/master/bugbug/models/regressor.py
- Mozilla Hacks, *Testing Firefox more efficiently with machine learning*, 2020-07, https://hacks.mozilla.org/2020/07/testing-firefox-more-efficiently-with-machine-learning/ (this post is about **testselect**, not the regressor; cited only for that distinction)

### What it actually predicts or decides

The **regressor** classifier detects **patches more likely to cause regressions**. README: "It could be used to make riskier patches undergo more scrutiny." That "could" is the owning project's own wording. This research **did not find a first-party source showing the regressor auto-gating landings** the way DRS gates freezes. What *is* in production at Mozilla, sourced from the 2020 Hacks post and `taskcluster/gecko_taskgraph/optimize/bugbug.py` on Searchfox, is **test selection**: bugbug chooses which tests to run. Do not conflate the two models.

Labels come from Bugzilla `regressed_by` plus an SZZ-style regressor finder. Training skips backed-out and WPT-sync commits; skips the most recent 3 months (regressions may not have been filed yet); and, for clean examples, skips patches older than 2 years because "Regressed By" data is only high-quality for ~two years.

### Inputs

Commit features in `RegressorModel`: source/other/test file sizes and added/deleted lines; author and reviewer experience; reviewer count; previous touches of component/directory/file; types, files, components, directories; functions touched; `rust-code-analysis` source metrics. Optional commit-message vectorizer when `interpretable=False`.

### How AI is used

**Classical ML, not an LLM.** XGBoost classifier, optional `IsotonicRegressionCalibrator`, random undersampling. No generative model in `regressor.py`.

### Who makes the final decision

Not sourced as an automated land/block for the regressor. The documented intent is **more scrutiny** for riskier patches. Testselect *does* decide which CI tasks to run, with confidence thresholds (`bugbug-low/medium/high`) and a fallback strategy on timeout (`BugbugTimeoutException`).

### Threshold philosophy

**Empirically derived bands relative to the observed base rate**, with a minimum sample.

From `evaluation()` in `regressor.py`:

- On average "around 1 out of 8 (13%) patches cause regressions."
- Band 1 target: ~half the average rate (~7%).
- Band 2: around the average (~15%).
- Band 3: ~double the average (~35%).
- `MIN_SAMPLE = 200`.
- `find_risk_band()` maps a probability into named bands from the secret `REGRESSOR_RISK_BANDS` (`name-start-end` triples).

This is the opposite of "2 files / 50 lines." The cutoffs are fitted to **observed regression frequency**, then frozen into named bands.

### Incomplete evidence

Sourced for **testselect**: timeout → fallback strategy, not "run nothing" and not "run everything" without a named fallback. For the regressor itself: recent unlabeled commits are excluded from *training*, which is a label-maturity rule, not a runtime incomplete-analysis UI.

### Steal vs avoid

**Steal**

- Explicit binary outcome (did this patch cause a regression?).
- Temporal split: never train on future labels; hold out immature recent commits.
- Isotonic calibration if you ever emit a probability.
- Risk bands defined by **observed rate vs average**, with a minimum sample, not by file-count folklore.
- Separate "this model exists" from "this model gates production." Mozilla's own README is careful.

**Avoid**

- Claiming Mozilla auto-deploys from the regressor. Not sourced.
- Training on unlabeled recent diffs as negatives.
- Importing Mozilla's 13% base rate. It is Firefox-specific.

---

## 6. DeployWhisper

**Primary sources**

- README: https://github.com/deploywhisper/deploywhisper
- Scoring: https://raw.githubusercontent.com/deploywhisper/deploywhisper/main/analysis/risk_scorer.py
- Evidence-aware engine: https://raw.githubusercontent.com/deploywhisper/deploywhisper/main/analysis/risk_engine.py
- Product site (secondary to source): https://deploywhisper.dev/

README at research time describes released version **v1.2.0**. Scoring code is the owning source for how numbers are computed.

### What it actually predicts or decides

An **advisory briefing** for infrastructure-change artifacts (Terraform, Kubernetes, Ansible, Jenkins, CloudFormation): overall score 0–100, severity `low|medium|high|critical`, recommendation `go|caution|no-go`, blast radius, rollback complexity, incident/pattern matches, context completeness, and a plain-English narrative.

It does **not** predict a probability of production failure. It does **not** block a release. README: "keep every verdict advisory rather than automatically blocking a release." GitHub App docs: keep the check advisory-only; "do not add it as a required status check."

### Inputs

Parsed unified changes (resource, action, tool, metadata); optional topology graph; raw files; evidence items with confidence and `deterministic` flags; public risk patterns; incident memory; parser success rate.

### How AI is used

**Deterministic core first; LLM is optional retinting + narrative.**

Pipeline in README: `Artifacts -> Parse -> Normalize -> Score -> Blast Radius -> Rollback -> Narrative -> Advisory Report`.

In `risk_scorer.py`:

- Heuristic severity from action (`create` low, `modify` medium, `replace`/`destroy` high), resource category (ingress/IAM/namespace high, labels low), environment (prod +8), security flags, downstream scope.
- `_apply_llm_scores` may overwrite per-change severity/reasoning. On any exception it **falls back to the heuristic matrix** and records a warning.
- LLM system prompt forbids fabricating downstream counts. `_sanitize_scope_claims` strips numeric "N downstream services" claims that are not in the allowed scope set.
- The LLM is instructed to return only `go` or `no-go`. **Normal code** then computes `_overall_recommendation`: any security flags → `no-go`; else `go` if highest severity ≤ medium, else `no-go`. `caution` is **not** an LLM output; it is introduced by `apply_context_uncertainty`.
- `_overall_score` is a handcrafted rollup: highest contributor + decayed secondaries + cascading bonus + interaction bonus, capped at 100. This is **not** a probability.
- `source` is `"heuristic-only"` or `"heuristic+llm"`.
- A separate narrative layer (Ollama / OpenAI / Anthropic / Gemini / …) writes prose. Scoring still renders if the LLM is off.

### Who makes the final decision

**A human reviewer.** The product's stated job is a first-pass briefing before an approval meeting. CI exits and GitHub checks are non-blocking in v1.

### Threshold philosophy

**Handcrafted policy matrix, not learned.** Severity scores are constants (`low=18, medium=42, high=72, critical=92`). Recommendation is a max-severity rule plus security-flag override. Context uncertainty uses `context_score < 0.7` as the cutoff that forbids a confident low-risk verdict. These numbers are **authors' policy**, not fitted to incident recall. Treat them as an engineering choice that happens to be in source, not as a scientific threshold.

### Incomplete evidence

This is the strongest OSS treatment found:

- `partial_context` when files fail to parse → warning.
- `apply_context_uncertainty`: if `context_score < 0.7`, set `insufficient_context`, cap confidence, append `INSUFFICIENT_CONTEXT_WARNING`, **never leave `recommendation == "go"`** (upgrade to `caution`), bump `low` severity to `medium`, floor the score at medium, prefix `top_risk` with `INSUFFICIENT CONTEXT:`.
- Context TODOs: import topology, refresh stale topology, import incidents, fix parser/evidence gaps.
- Evidence-weighted contribution uses `item.confidence` and a `deterministic` bonus; unverified evidence cannot dominate as if it were fact.
- Topology/state connectors (PR #88) "handle unavailable/malformed/stale state without blocking deterministic analysis."

### Steal vs avoid

**Steal**

- Name the advisory rollup `go` / `caution` / `no-go`, not `safe` / `risky`.
- Heuristic (or policy) score **before** LLM; LLM failure falls back; report still renders.
- `caution` as the incomplete-evidence state, distinct from `no-go` (danger found).
- Sanitize LLM numeric/scope claims against deterministic allowed sets.
- Surface context completeness and TODOs in the UI.
- Keep CI advisory until the org *chooses* to gate.

**Avoid**

- Copying 18/42/72/92 or the 0.7 context cutoff as if they were calibrated.
- The DeployWhisper `_overall_score` decay formula as a "risk probability." SafeLane's own `research/risk-engine-options.md` already rejected this as false precision. That rejection still holds.
- Letting the LLM's `go`/`no-go` be the product verdict. DeployWhisper itself does not: normal code overwrites.

---

## 7. Akuity Promotion Advisor / Kargo Intelligence

**Primary sources**

- Akuity Docs, Promotion Advisor: https://docs.akuity.io/intelligence/akuity-agents/promotion-advisor/
- Akuity Docs index: https://docs.akuity.io/intelligence/akuity-agents/
- First-party blogs (product claims; thinner than the docs page):  
  https://akuity.io/blog/akuity-ai-software-delivery  
  https://akuity.io/blog/kargo-gitops-promotion-layer  
  https://akuity.io/blog/beyond-dashboards-ai-agents-for-gitops-operations  
  https://akuity.io/blog/how-kargo-fixes-gitops-with-promotion

### What it actually predicts or decides

**An advisory risk assessment at promotion time**, not a rollout-parameter compiler. Official docs: when you promote Freight to a stage, the Promotion Advisor provides "a detailed risk assessment, a summary of changes, and actionable recommendations." The human still clicks promote. Slack can receive the report.

Blogs add: enumerate commits, analyze commit messages and diffs, infer a risk score from promotion history across stages, flag CVEs in new images, highlight deprecated Kubernetes APIs. Engineers can ask why a change might be risky and get reasoning "grounded in the actual code and deployment history."

### Inputs

Kargo Freight vs stage; associated commits; diffs; commit messages; promotion history from other stages; (blog) container image CVEs; (blog) deprecated APIs. Exact feature list of the inferred score is **not in the public docs**.

### How AI is used

**LLM advisor.** Docs and blogs describe analysis, summary, inferred score, and conversational Q&A. No public source describes a trained failure-probability model, a percentile gate, or a deterministic policy table. Do not claim otherwise.

### Who makes the final decision

**The engineer promoting in the Kargo UI** (or whatever promotion policy Kargo already had without Intelligence). The Advisor is a tab on the Promote dialog. Nothing sourced says it auto-changes canary weight, analysis thresholds, or blocks promotion.

### Threshold philosophy

**Not published.** "Inferred risk score" is unnamed in scale, unpublished in calibration. Treat as opaque vendor scoring until Akuity documents it.

### Incomplete evidence

**Not documented** in the Promotion Advisor page. Insights dashboards separately surface CVEs and deprecated APIs as fleet facts, which is a different product surface.

### Steal vs avoid

**Steal**

- Attach the assessment to the **promotion act**, not only to the PR.
- Include image CVEs and deprecated APIs as deterministic evidence, not as LLM opinions.
- Keep the human as promoter.

**Avoid**

- Treating Akuity as a score→rollout engine. It is advisory GitOps intelligence.
- Copying an unpublished "inferred risk score."

---

## 8. gitStream / LinearB risk heuristics

**Primary sources**

- https://docs.gitstream.cm/
- Filter functions: https://docs.gitstream.com/filter-functions/
- Automation library: https://docs.gitstream.cm/automations/automation-library/
- Safe-changes / first automation: https://docs.gitstream.com/quick-start/
- Estimated review time example: https://docs.gitstream.cm/automations/provide-estimated-time-to-review/
- Sensitive files: https://docs.gitstream.cm/automations/standard/review-assignment/review-sensitive-files/
- GitHub repo: https://github.com/linear-b/gitstream

### What it actually predicts or decides

gitStream is a **YAML policy engine for review/merge**, not a failure predictor. It auto-labels, assigns reviewers, estimates review time, auto-approves/merges changes the *org declared* safe, and requires extra review for paths the *org declared* sensitive.

There is **no first-party "risk score" API**. Risk is whatever the `.cm` file says it is.

### Inputs

PR/branch metadata, file lists, diffs, labels, contributors. Filters include `estimatedReviewTime`, `codeExperts`, `isFormattingChange`, `allDocs` / tests / assets, regex/path match.

`estimatedReviewTime`: ML model on PR metadata (branch name, commits) "mainly by the number of additions and deletions for each type of change (Code, Data, Configuration, etc.)." Lockfiles, images, and many data extensions are excluded. The model itself is a LinearB API; weights are not public.

`codeExperts`: git-blame + git-commit activity, recency-weighted, score 0–100, up to 2 users, excludes the author.

`isFormattingChange`: minify-and-compare (Prettier-supported types) or whitespace fallback. This is a **deterministic semantic-equivalence check**, not an LLM.

### How AI is used

Two layers:

1. **Optional LinearB `code-review@v1` action** (LLM review comments, optional `approve_on_LGTM`). This is a review agent, not a risk model.
2. **`estimatedReviewTime` / `codeExperts`**: hosted statistical models, not LLMs.

The automations that actually change merge friction are **deterministic YAML**.

### Who makes the final decision

**The repo's `.cm` file + GitHub/GitLab merge rules.** gitStream can `approve`, `merge`, `set-required-approvals`, `require-reviewers`, `request-changes`. A human org author wrote the policy. Free-tier skipped automations conclude the check as `Skipped` rather than fail-closed or fail-open in a documented safety sense — they simply do not run.

### Threshold philosophy

**Explicit org policy, sometimes with an ML helper.**

Examples from first-party docs:

- Safe-changes: docs OR tests OR assets OR formatting → label `safe-changes` and approve.
- Tiny changes: "Approve single-line changes to a single file" (library; the cutoff is the example, not a law of software).
- Estimated review time colors: green < 5 min, yellow 5–19, red ≥ 20 — **example constants in the docs snippet**.
- Sensitive paths: org-supplied list; require security team + 2 approvals.
- `codeExperts(gt=10)`: example threshold 10 on the 0–100 expert score.

These are **defensible as policy** because they are written down, versioned in-repo, and match a review-cost theory (formatting cannot change behavior; auth/ paths need security). They are **not** defensible as failure probabilities.

### Incomplete evidence

Sourced: `codeExperts` drops users it cannot map to the git host, so the list may be shorter than two. Formatting detection falls back for unsupported types. Monthly cap → automations `Skipped`. Not sourced: a dedicated "analysis incomplete" label.

### Steal vs avoid

**Steal**

- Versioned, in-repo policy as the decision layer.
- Positive-proof Fast path: docs/tests/formatting/assets, not "the LLM said nothing."
- Path-sensitive extra care (auth, routing) instead of global line-count panic.
- Expert routing from blame/commit recency.
- Color labels for *review effort*, honestly named as minutes, not as risk.

**Avoid**

- Treating `estimatedReviewTime` as incident risk.
- Copying 5/20-minute color cutoffs as if they were science.
- Auto-merge of "small" code changes without a semantic safe-class (formatting/docs/tests). Two files of auth code is not Fast.

---

## 9. CodeScene commit risk

**Primary source**

- CodeScene 7.3.8 docs, Risk Analysis: https://docs.enterprise.codescene.io/versions/7.3.8/guides/pm/risk.html

(Older 6.3.2 docs repeat the same engine description.)

### What it actually predicts or decides

A **1–10 commit risk classification** used as an early-warning dashboard and CI signal to focus review and testing. It classifies **how unusual the commit is relative to this codebase's history**, "more at how a commit looks than the changed code itself." It is not a predicted probability of a production incident.

### Inputs

Technical: amount of code changed, number of files, diffusion across sub-systems, complexity of changed code (CI blog; the 7.3.8 page emphasizes amount/files/diffusion). Social: author experience on *this* codebase (historical contribution). Two identical diffs can score differently for a newcomer vs a long-time author.

### How AI is used

**Machine learning / self-learning profile**, not an LLM. The CI blog (https://codescene.com/blog/codescene-in-your-continuous-integration-pipeline) calls it a machine-learning algorithm that "automatically adjusts as a developer gains more experience." The 7.3.8 page does not name the algorithm family.

### Who makes the final decision

**Humans using the dashboard / CI consumers.** Docs: use it to "react early and focusing code reviews and testing" and as "input and feedback on planned delivery activities." No first-party source found that CodeScene selects canary steps.

### Threshold philosophy

**Learned relative profile + configurable policy cutoff.** Default: flag ≥ **7** as high risk; "You can change this threshold in the project configuration." Risk is **relative to the analysis period**: a short window produces more "high risk" commits because they stand out locally. That is documented, not a hidden bug.

### Incomplete evidence

**Not really handled as a separate state.** A commit always gets 1–10. Lack of history (new repo, new author) would, by the social-metric design, tend to raise risk — but the docs do not spell out a cold-start rule. Do not invent one.

### Steal vs avoid

**Steal**

- Relative-to-this-repo baseline: "typical change" vs outlier, not universal file-count law.
- Author familiarity as a *review* signal (not as a moral score).
- Configurable high-risk cutoff, default published.

**Avoid**

- Treating 7/10 as an incident probability.
- Shipping author-experience as a Fast-lane bonus that can cancel a breaking API. SafeLane's safety-floor rule (danger cannot be cancelled) should win.
- Planning a release date from a rolling risk average without also reading *why* the average moved (the docs themselves warn about this).

---

## 10. ServiceNow DevOps Change Velocity / change risk

**Primary sources**

- Product: https://www.servicenow.com/products/devops-change-velocity.html
- Predictive Intelligence for Change Management: https://www.servicenow.com/docs/r/it-service-management/change-management/change-mgmt-intelligent-solutions.html
- Calculated Risk Score: https://www.servicenow.com/docs/r/it-service-management/change-management/risk-lookup.html
- FAQ: https://www.servicenow.com/community/devops-articles/faq-for-devops-change-velocity/ta-p/3018723
- Modern Change playbook (community, first-party-adjacent): https://www.servicenow.com/community/itsm-articles/modern-change-management-adoption-playbook-amp-maturity-journey/ta-p/3279260

### What it actually predicts or decides

Two stacked products:

1. **DevOps Change Velocity (DCV):** auto-create change requests from CI/CD, then **auto-approve or reject based on change approval policies** fed by DevOps data (work items, commits, coverage, security scans, tests). It automates the *ITSM paperwork path*, not canary weights.
2. **Change risk engines** on the change request:
   - Risk conditions / calculator (rules).
   - Risk assessment questionnaires (weighted answers vs thresholds).
   - **Predictive Intelligence "Risk for proposed change"** (similarity + categorization ML): "when risk = low, you can use Change Approval Policies to automatically approve this low risk activity."
   - **Calculated Risk Score:** lookup table mapping **Impact × Success Probability band** (High/Medium/Low) to a risk value. Three flavors: Impact+Success Probability, Impact+Change Success Score, Impact+Change Model Success. **All engines run; the system picks the highest risk.** `Not set` when risk is not evaluated.

### Inputs

Change request fields; CI/CD evidence (tests, coverage, scans); historical change success; impact; questionnaires; similar past changes (PI). **Not** a GitHub diff semantic analysis as the core engine.

### How AI is used

**Predictive Intelligence ML** for the "Risk for proposed change" solution (plugin `com.snc.change_management.ml.risk`). This is classical similarity/categorization, not an LLM PR reviewer. DCV itself is policy/flow automation.

### Who makes the final decision

**Change Approval Policies** (Flow Designer), which a change manager configures. Low predicted risk *may* auto-approve. Humans still own exceptions and high-risk paths. DCV "callback to the third-party orchestration tool" after approval — still an approve/hold, not a traffic split.

### Threshold philosophy

**Policy tables + optional ML.** Calculated Risk Score is an explicit 3×3-style lookup the change manager can edit. Questionnaire risk is `sum(actual_value * weight)` vs configured thresholds. Predictive Intelligence outputs a risk category used as a policy predicate. Highest-of-engines is a **conservative composition** rule.

### Incomplete evidence

Sourced: Calculated Risk Score can be **Not set** when not evaluated. DCV FAQ: you must onboard planning + coding + orchestration tools before change outcomes work. Not sourced: whether missing ML prediction fail-closes to High. Do not assume it.

### Steal vs avoid

**Steal**

- Separate **likelihood-like** (success probability) from **impact**, then look up risk. That is NIST-shaped (see §13).
- Highest-of-engines: a reassuring questionnaire cannot cancel a high rule-based risk.
- Auto-approve only as a *policy* over a low-risk category, not as a model side effect.
- `Not set` as a visible state.

**Avoid**

- Equating ITSM auto-approval with rollout care. Different actuator.
- Importing ServiceNow's opaque PI model without labels or recall@X%.

---

## 11. Digital.ai Change Risk Prediction (CRP)

**Primary sources**

- Product: https://digital.ai/products/intelligence/change-risk-prediction/
- Release plugin (v26.1): https://docs.digital.ai/release/docs/how-to/change-and-risk-prediction-plugin
- Data API: https://docs.digital.ai/platform/docs/analytics/data-api

### What it actually predicts or decides

**Probability that a change request will fail**, plus contributing features. The Release plugin can **gate a pipeline** if predicted risk exceeds an operator-set threshold. Docs: "identifying low-risk changes and automating deployment" and "prevent high-risk changes from progressing in the release pipeline."

Get Risk Prediction outputs: whether the prediction exceeds threshold, and probability percentage. Risk Prediction Gate: if prediction **exceeds Threshold %** (example in docs: `25`), the gate blocks further progression.

The inference API (`POST .../ml-inference/store/{project_name}/{model_name}`) takes `business_ids` (change IDs) and returns predicted outcomes, per-feature importance, and `probability_N` / `probability_Y`. Example model name in API docs: `CatBoostClassifier`. Example project name in the plugin: `ChangeFailure`. The **input identifier in the Release task is a ServiceNow change number** (`CHG0030007`). This is ITSM-change prediction, not GitHub-diff prediction.

### Inputs

Historical change/incident/problem/outage data correlated from CI/CD and ITSM (product page lists Digital.ai Release/Deploy, Azure DevOps, ServiceNow, BMC). Feature importances are returned per inference. The public docs do not publish the feature schema.

### How AI is used

**Supervised ML classifier** (CatBoost named in API docs). Not an LLM. Digital.ai marketing says "AI-powered"; the plugin/API shape is classical inference.

### Who makes the final decision

**Release designer + gate threshold.** The plugin automates blocking when over threshold. "Automating deployment" for low-risk is claimed in the plugin intro; the documented task that actually *acts* is the **Gate**, which prevents progression. Whether a complementary task auto-starts a deploy is not specified in the plugin page beyond the intro sentence. Be precise on stage: **sourced as a gate**, **marketing as a fast-lane**.

### Threshold philosophy

**Operator-entered percentage** (example 25%). Not a published recall@X% operating point. The model may be trained per project (`project_name` / `model_name`).

### Incomplete evidence

**Not documented** on the plugin page (timeouts, missing change id, model not trained). Do not invent fail-open/fail-closed.

### Steal vs avoid

**Steal**

- If you ever have labeled change failures, a CatBoost/XGBoost on ITSM+CI features is a known pattern.
- A **named gate task** with an explicit threshold field — honest about being a knob.

**Avoid**

- Using Digital.ai's "fast-lane for low-risk changes" language as if it meant canary parameterization. The sourced actuator is a release-flow gate on a ServiceNow change id.
- A 25% example threshold as a default for SafeLane.

---

## 12. Google SRE canarying / error-budget exposure

**Primary source**

- *Site Reliability Workbook*, Chapter 16, Canarying Releases: https://sre.google/workbook/canarying-releases/

### What it actually predicts or decides

**Whether to proceed with a rollout**, by comparing a small **canary** population to a **control** on chosen metrics. It does not score the diff. It scores *live behavior* of the already-built release candidate.

Canarying = "a partial and time-limited deployment of a change in a service and its evaluation." Requirements: deploy to a subset, evaluate good/bad, integrate the evaluation into the release process.

### Inputs

Production traffic split; SLI-aligned metrics (example: HTTP success and latency, not raw CPU); canary vs control breakdowns; time window matching the canary duration.

### How AI is used

**None.** This is control theory / experiment design.

### Who makes the final decision

**The release automation**, optionally pausing for a human. If canary metrics diverge too far from control → pause, roll back, or page a human. Absolute SLO checks sit beside A/B comparison because isolation is imperfect (canary can hurt the control).

### Threshold philosophy

**Error budget, not a magic 1%.** "Impact on the budget is directly proportional to the amount of traffic exposed to defects." Worked example: 20% error on 5% of traffic → 1% overall error. Duration is bounded by release velocity (daily vs weekly vs 20×/day). Size/duration must be representative (enough queries, right time of day for load defects). After you have history, tune from **typical canary failure rates**, not only worst-case. The chapter warns that over-precise models are a waste: "all models are wrong, but some are useful."

Explicitly **avoid before/after canaries** (time as the A/B axis): diurnal/weekend effects confound attribution.

### Incomplete evidence

If metrics cannot be broken out by canary vs control, a 5% canary at 20% error looks like 1% overall and may be **indistinguishable from noise** — i.e. analysis is incomplete as a canary, even if monitoring is "green." Metric windows longer than canary duration also muddle the signal. Isolation failures mean a "bad canary" verdict is not proof the candidate is at fault.

### Steal vs avoid

**Steal**

- SafeLane's Strict/Guarded profiles should spend a **budgeted slice of error budget**, not a folkloric 1%.
- Evaluate with SLIs, canary vs control, duration ≤ metric window.
- Incomplete monitoring → do not treat the canary as passed.
- One canary at a time.

**Avoid**

- Using canary success as a substitute for pre-deploy change assessment. Different time, different question.
- Before/after "canaries."
- Too many canary metrics (false positives destroy trust).

---

## 13. NIST SP 800-30 Rev. 1 — likelihood, impact, uncertainty

**Primary sources**

- NIST SP 800-30 Rev. 1, *Guide for Conducting Risk Assessments* (final, 2012-09): https://csrc.nist.gov/pubs/sp/800/30/r1/final  
  PDF via DOI: https://doi.org/10.6028/NIST.SP.800-30r1
- Glossary excerpt: https://csrc.nist.gov/glossary/term/likelihood_of_occurrence

This is **information-security** risk assessment, not JIT defect prediction. The transferable engine shape is the factorisation, not the threat-taxonomy.

### What it actually predicts or decides

**Risk = f(likelihood of occurrence, adverse impact).** Likelihood itself combines likelihood of initiation/occurrence **and** likelihood that the event produces impact. Impact is magnitude of harm (operations, assets, individuals, other orgs, the Nation). Organizations then **respond** (accept, mitigate, transfer, avoid) — the assessment does not deploy software.

Qualitative / semi-quantitative scales are provided as **starting points to tailor** (Appendix I, Table I-2): e.g. Very High 96–100, High 80–95, … Descriptions are about expected severity of adverse effects, not about git churn.

### Inputs

Threat sources, threat events, vulnerabilities, predisposing conditions, likelihood, impact, and **uncertainty** of those determinations. Table I-1 lists uncertainty criteria as a first-class input from organization (Tier 1) downward.

### How AI is used

**None.**

### Who makes the final decision

**Organizational officials**, using risk tolerance including **tolerance for uncertainty**. SP 800-39 is cited as the parent risk-management process; 800-30 is the assessment step.

### Threshold philosophy

**Tailored scales + explicit combination of likelihood and impact** (Table I-3). Not ML. Not universal numeric cutoffs. "Organizations determine the levels of risk, types of risk, and degree of risk uncertainty that are acceptable." High uncertainty, especially on likelihood, is called out as a particular concern; organizations must choose an approach rather than hide it.

### Incomplete evidence

**Uncertainty is a documented output, not a missing column.** Assessors "make explicit the uncertainty in the risk determinations." Quantitative methods are warned against when "significant uncertainty surrounds the determination of values." Do not bury uncertainty inside a single score.

### Steal vs avoid

**Steal**

- Three separate displays: likelihood-like evidence, impact/blast, uncertainty/completeness.
- Conservative composition: high impact with uncertain likelihood is still careful.
- Tailor scales; do not pretend a 1–10 is universal.
- Never let a precise-looking number paper over missing evidence.

**Avoid**

- Mapping NIST "High risk" language onto a 50-line PR.
- Using 800-30's 80–95 "High" band as a deploy threshold.

---

## 14. OSS / product LLM PR-review engines (engine shape only)

These products review PRs. They are **not** deployment-risk engines. The useful part is how they structure analysis, verification, and gating.

### 14.1 CodeRabbit

**Primary sources**

- Architecture (first-party docs): https://docs.coderabbit.ai/overview/architecture
- Custom checks: https://docs.coderabbit.ai/pr-reviews/custom-checks
- Pre-merge checks: https://docs.coderabbit.ai/pr-reviews/pre-merge-checks
- Configuration reference: https://docs.coderabbit.ai/reference/configuration

**Predicts/decides:** review comments (summaries, findings, suggested fixes) plus optional **pre-merge checks** that pass/fail/warn. Architecture page: sandboxed clone, 50+ static analyzers/linters/SAST, agentic exploration, parallel agents including **Review, Verification, Chat, Pre-Merge Checks**. That "Verification" agent is named; its internal algorithm is not published beyond the custom-check pipeline.

**AI vs policy:** LLM review is advisory comments. Pre-merge **custom checks** are natural-language policies executed by an agent that must gather evidence with tools (ast-grep, ripgrep, read-only sandbox). Built-in checks include docstring coverage against a **numeric threshold** (config example 85; blog says default 80), title/description requirements, issue alignment.

**Final decision:** modes `off | warning | error`. Error can block merge **only if** the request-changes workflow is enabled. Automatic approve-when-comments-resolved is a config flag, not a risk model.

**Incomplete vs danger:** custom checks emit **`Passed`, `Failed`, or `Inconclusive`**. Docs: instructions that need unavailable capabilities "will return Inconclusive or produce unreliable results." Checks cannot run the test suite or execute repo code. This is the cleanest first-party **analysis-incomplete** enum found in LLM review products.

**Steal:** three-way check outcome; verify-with-tools before decide; keep LLM review comments off the merge gate unless an explicit error-mode check failed; path-scoped instructions.

**Avoid:** treating comment volume as risk; blocking merge on Inconclusive as if it were Failed without saying so (or the reverse).

### 14.2 Qodo (context engine)

**Primary source** (first-party engineering/product blog, not API spec):

- https://www.qodo.ai/blog/how-qodo-builds-the-wisdom-to-govern-the-context-engine/

**Predicts/decides:** governance/review findings, not a deploy score. Independent review layer vs the coding agent (explicitly: the generator should not be the final authority).

**AI vs policy:** specialized agents with a **closed tool registry**, budgeted reasoning steps (hit ceiling → still emit a structured answer, not silence), and a Rules Lifecycle System (described; details deferred to a later article). Context agent produces a walkthrough; **every code reference is validated against the repository before issue agents run** — pointers must resolve to real files and lines. Cross-repo edges are kept only if **deterministic rules** confirm them on both sides; model judgment does not create coupling.

**Final decision:** not sourced as merge authority in this article. Positioning is governance/enforcement of standards, not canary selection.

**Incomplete vs danger:** budgeted exploration still yields a structured result. Unconfirmed cross-repo links are dropped (absence of an edge ≠ "no consumers," and Qodo's text says only confirmed relationships are retained — i.e. they prefer sparse verified maps over guessed blast radius).

**Steal:** validate citations **before** reasoning about issues; closed tools; force a structured answer on budget exhaustion; don't let the authoring model grade itself; deterministic confirmation of blast-radius edges.

**Avoid:** dumping the whole repo into the prompt and calling it governance.

### 14.3 GitHub Copilot code review

**Primary sources**

- Concepts: https://docs.github.com/en/copilot/concepts/agents/code-review
- How-to: https://docs.github.com/en/copilot/how-tos/copilot-on-github/use-copilot-agents/copilot-code-review

**Predicts/decides:** PR review **comments and suggested patches**. Official: Copilot **always leaves a Comment review, never Approve or Request changes**. Reviews **do not count toward required approvals and will not block merging**. GitHub Code Quality (CodeQL, coverage) is the rules-based layer that *can* gate via rulesets.

**AI vs policy:** LLM review with optional custom instructions (`copilot-instructions.md`, path `*.instructions.md`, `AGENTS.md` from the **head** branch). Agentic "full project context gathering" via GitHub Actions; if Actions is unavailable or workflows fail, **reviews still generate but without agentic features** (limited mode). Effort levels: **Lite** (default, fast) vs **Balanced** (higher-reasoning model, more credits) — a thoroughness knob, not a risk score. MCP/skills attributions can appear on comments; session logs show which tools ran.

**Final decision:** humans, plus optional CodeQL/coverage rulesets. Copilot is structurally barred from being the merge authority.

**Incomplete vs danger:** fallback to limited review when Actions fails is documented; it is **not** labeled Inconclusive in the UI from these pages. Docs: "Copilot is not guaranteed to spot all problems… Sometimes it will make mistakes. Always validate… Supplement with a human review." Downvotes do not stop a later re-review from repeating a comment.

**Steal:** structurally impossible for the LLM review to approve or block; pair LLM comments with deterministic SAST/coverage gates; Lite vs Balanced as analysis-depth, not as risk; show which tools/skills grounded a comment.

**Avoid:** using Copilot comment count, or Lite vs Balanced, as a rollout input.

---

## 15. Synthesis for a hackathon product (GitHub repo → Fast / Guarded / Strict)

### 15.1 Best architecture

The production pattern that keeps showing up, and that fits SafeLane's ADRs (`0001` bound AI to findings, `0002` assessment ≠ rollout decision, `0003` explicit authorization), is a **conjunctive funnel with separated factors**:

```text
GitHub PR + mapping
        │
        ├─ Deterministic evidence: size/diffusion, path classes, service mapping,
        │  parser completeness, tests/docs/formatting, CVEs, deleted HTTP routes, …
        ├─ Hosted LLM safety case: typed findings + exact spans + verification intent
        │     (never tier, never profile, never probe command)
        ├─ Citation checks: span exists on the claimed side  (existence)
        │     optional NLI/second-pass: span supports the claim     (semantic)
        └─ Policy (versioned):
              change-scope band  (care baseline, not a probability)
              safety floors      (danger raises care; never lowers it)
              evidence floors    (incomplete → at least Guarded, not Fast)
              → Fast | Guarded | Strict   as required care
                    │
                    └─ Rollout decision (auto Fast, or human for Guarded/Strict)
                         profiles spend error budget, not folkloric 1%
```

This is RADAR's funnel (eligibility × heuristics × score-or-findings × LLM semantics × deterministic validation) **without** Meta's SEV classifier, plus DeployWhisper's incomplete-context upgrade, plus Google's error-budget exposure, plus NIST's likelihood/impact/uncertainty split.

For a hackathon that attaches a GitHub repo and analyzes PRs:

1. **Map the PR to one release service** (already SafeLane). Unsupported mapping → no Fast, and say *unsupported*, not *risky*.
2. **Deterministic change classes** beat file-count: formatting/docs/tests (gitStream), mechanical codemod (RADAR Blanket AutoAccept), vs contract/auth/data-path touches (gitStream sensitive files, SafeLane `breaking_api`).
3. **LLM finds endangered contracts** (ADR 0001), hosted Azure OpenAI, structured output.
4. **Normal code** verifies spans, binds trusted probes, chooses care.
5. **Fast requires positive proof**, RADAR-style: every layer passed, zero accepted findings, complete evidence. Empty LLM output is not proof (already in `docs/risk-signals.md`).
6. **Guarded** is the default for "real code, no accepted danger, evidence complete." That is *care*, not *predicted failure*.
7. **Strict** is for accepted danger **or** high blast (critical service, breaking contract), not for "big diff."

Do not train a DRS. You lack SEV labels, temporal maturity windows, and a freeze calendar. Mozilla's lesson is that the *architecture* of labels/calibration/bands transfers; the model does not.

### 15.2 Give AI more involvement without letting it pick the rollout

Hosted LLM (Azure OpenAI) can do more than today's single finding without violating ADR 0001:

| AI may do | AI may not do |
|---|---|
| Propose multiple typed safety cases (breaking API, authz, data loss, retry semantics) | Emit Fast/Guarded/Strict |
| Classify hunks into RADAR-like safe vs risk *signals* (formatting vs behavior) | Numeric risk score as authority |
| Draft verification intents and approval questions from verified spans | Probe IDs, commands, URLs, rollout weights |
| Walk the repo with closed tools (Qodo) to find callers of a changed symbol | Invent downstream counts |
| Write Bounded remediation prose after spans verify | Apply patches or approve the PR |
| Second-pass NLI: "does this span support this failure hypothesis?" | Override a verified finding because the proposal was bad |

Meta's hard lesson: **do not prompt for a score.** RADAR's lesson: **LLM accept is one conjunct in a funnel.** Copilot's lesson: **make it structurally impossible for the LLM review to be the gate.** DeployWhisper's lesson: **sanitize claims; fall back to heuristics; keep the report.**

Practical hosted-LLM upgrades vs a weak local model, still off the rollout switch:

- Larger diffs (today's 16 KiB budget can grow) with **map-reduce over files** and a deterministic merge of findings.
- Caller/consumer search in the cloned repo, then span-verify every hit.
- Parallel specialized passes (contract, auth, data) like CodeRabbit's parallel agents.
- ACR-style safe-signal classification to *explain* why a large refactor can still be Fast — but Fast still requires the deterministic formatting/equivalence or test-only class, plus zero accepted danger, plus complete evidence.

### 15.3 Replace 2-files/50-lines and 10-files/500-lines

Those cutoffs are **arbitrary policy**, same family as gitStream's "one-line tiny PR" example and DeployWhisper's 72/92 constants. They are not learned. They are also **easy to game** (Meta's comment-only-on-a-hot-file problem) and **easy to miss** (one-line auth change).

Defensible replacements, in increasing honesty:

**A. Named change classes (best hackathon fit, no labels required)**

| Class | How you know | Minimum care |
|---|---|---|
| Mechanical / non-behavior | `isFormattingChange`-style, docs-only, lockfile-only, tests-only | Fast if mapping complete and AI returns zero accepted findings |
| Bounded service code | one mapped service, no sensitive path class, complete evidence, no accepted finding | Guarded |
| Sensitive path / contract | auth, routing, schema, public HTTP, migrations | at least Guarded; Strict if accepted breaking finding |
| Unmapped / truncated / AI incomplete | evidence floors | at least Guarded, confidence low |
| Accepted danger | verified safety case | Strict |

Line counts can remain a **secondary** diffusion signal (Meta/CodeScene: more files / more subsystems → more review), but they should not be able to create Fast or create Strict by themselves.

**B. Repo-relative outlier (CodeScene-shaped, still not a probability)**

If the attached repo has enough history, compute this PR's percentile on files-changed and lines-changed **within that repo**. Label it `scope: typical | large-for-this-repo`. Still a band, but the cutoff is "unusual here," not "50 lines in the abstract." Publish the window (CodeScene: risk is relative to analysis period).

**C. Learned recall@X% (only with labels)**

If you later have incident/revert labels: rank, pick an operating point (Mozilla: half vs double the base rate, min sample 200; Meta: top 5/10/50% vs SEVs captured). Until then, claiming a probability is dishonest.

**D. Error-budget exposure (Google) for the profile, not the tier**

Once the tier is Guarded or Strict, size canary weight × duration from remaining error budget and blast (criticality, downstream count). That is how you stop arguing about 1% vs 5%.

### 15.4 Naming: why "risky" is a bad label

If the engine **predicts failure** (DRS, bugbug regressor, Digital.ai CRP), "risk" is the correct noun and the number should be an operating point on a labeled outcome.

If the engine **chooses caution** (SafeLane today, DeployWhisper recommendation, gitStream extra review, RADAR eligibility), "risky" reads as "this will probably break production." That overclaims. DeployWhisper already split **severity** (how bad a finding is) from **recommendation** (`go` / `caution` / `no-go`). NIST splits likelihood, impact, and uncertainty. Google talks about **exposure**.

Safer product language for SafeLane:

| Current | Honest replacement | Means |
|---|---|---|
| `safe` | `Fast` (already the profile name) | Positive proof, auto-resolve |
| `guarded` | `Guarded` / `careful` | Default care, human resolution |
| `risky` | `Strict` / `danger-accepted` | Verified danger or hard safety floor |
| `risk tier` | `required care` / `rollout care` | Policy result |
| `AI risk score` | forbidden (already) | — |

`caution` should be reserved for **incomplete analysis** (DeployWhisper), not used as a synonym of Guarded. Guarded = we understood the change and still want a human. Caution/incomplete = we did **not** finish understanding it.

This conflicts with `CONTEXT.md` **Risk tier** (`safe` / `guarded` / `risky`) and with `docs/risk-signals.md` labels. Flag it; do not silently override the ADR/glossary. A glossary change is a domain-modeling task.

### 15.5 Citation verification: existence vs semantic support

Gao et al., *Enabling Large Language Models to Generate Text with Citations* (EMNLP 2023), https://aclanthology.org/2023.emnlp-main.398/

- **Citation recall:** concatenated cited passages **entail** the statement (AIS: attributable to identified sources). Missing citations or unsupported statements fail recall.
- **Citation precision:** citations that do not even partially support the statement are irrelevant.
- On ELI5, even the best systems lacked complete citation support ~50% of the time.
- Closed-book + post-hoc citing can look "correct" while citation quality is poor — **truthy prose with a nearby quote is not grounding.**
- They use an NLI model (TRUE) for "fully support," and show correlation with human judges.

SafeLane today: **source reference verified** = the quoted text exists at file/line/side. That is ALCE-style *presence*, not *entailment*. `research/risk-engine-evaluation.md` already said this; it remains correct.

With Azure OpenAI you can add a **second deterministic-ish pass**: given (claim, quoted span), ask a constrained NLI/JSON "supports / partially / does not support." Fail → drop the safety case or mark `unverified_semantics` and apply the evidence floor. Do not advertise this as "risk verified." Qodo's order is the right one: **validate pointers before issue reasoning.** DeployWhisper's sanitizer is the right one for numbers.

### 15.6 Distinguishing danger-found vs analysis-incomplete

Copy the state machine that actually exists in production OSS/docs:

| State | Meaning | Care | UI |
|---|---|---|---|
| **Danger accepted** | Verified finding; spans exist; optional NLI support | Strict (or Guarded if policy says so) | Show the contract, span, hypothesis. Do **not** use the same banner as timeouts. |
| **Clear** | Positive proof: complete evidence, allowed change class, zero accepted findings | Fast | "Fast-lane eligibility met," never "AI found no risks." |
| **Incomplete** | Timeout, malformed JSON, over-budget diff, unmapped path, Inconclusive check, context_score low, missing topology, Actions-fallback review | at least Guarded; confidence low | DeployWhisper's `INSUFFICIENT CONTEXT` / CodeRabbit `Inconclusive` / NIST uncertainty. List **what to fix** (TODOs). |
| **Unsupported** | 0 or N services, policy out of scope | no Fast; maybe no decision | Different from both danger and incomplete. |

Policy laws (already in SafeLane, confirmed by these engines):

- Incomplete can never create Fast (RADAR conjunctive funnel; DeployWhisper `go`→`caution`; NIST uncertainty).
- Incomplete cannot erase danger (ADR 0001; ServiceNow highest-of-engines).
- LLM silence is not Clear (RADAR; Copilot's own disclaimer).
- Show evidence completeness separately from finding severity (DeployWhisper context ledger; NIST).

UI anti-pattern to avoid: a single red `risky` pill for (a) breaking API deleted, (b) Ollama/Azure timed out, and (c) 12-file refactor. Those are three different speech acts.

---

## 16. Claims that could not be sourced (do not repeat as fact)

- **Mozilla regressor auto-gates landings.** README says it *could* increase scrutiny. Production first-party evidence found is **testselect**, not regressor gating.
- **Akuity Promotion Advisor score formula, scale, or fail-closed behavior.** Not in public docs.
- **Digital.ai actually changes canary weights.** Sourced: a Release **gate** on predicted % vs threshold, keyed by ServiceNow change id.
- **CodeScene algorithm family** (beyond "machine learning" / self-learning experience).
- **DRS behavior on truncated diffs** (only LLM-score mean-imputation is sourced).
- **RADAR ACR malformed-output path** (funnel is conjunctive; JSON details unpublished).
- **gitStream `estimatedReviewTime` model weights or accuracy.**
- **Copilot UI treating limited-mode reviews as Inconclusive.** Fallback is documented; the label is not.
- **A first-party Meta blog for RADAR.** Paper only.

DRS-OSS (arXiv:2511.21964) is a third-party reproduction, not Meta's engine; it was not used as a source for Meta behavior.

---

## 17. One-page steal list

1. **Funnel, conjunctive, fail toward care** (RADAR, DeployWhisper, Copilot-cannot-approve).
2. **LLM understands the change; policy chooses care** (ADR 0001, RADAR ACR, DeployWhisper rollup).
3. **Never prompt a numeric risk score** (Meta DRS paper).
4. **Positive proof for Fast** (RADAR; SafeLane fast-lane eligibility).
5. **`Passed` / `Failed` / `Inconclusive`** (CodeRabbit custom checks).
6. **Incomplete upgrades `go` → `caution`, never to Fast** (DeployWhisper `apply_context_uncertainty`).
7. **Citation existence then citation support** (Qodo; ALCE).
8. **Likelihood ≠ impact ≠ uncertainty** (NIST; ServiceNow lookup).
9. **Exposure from error budget** (SRE workbook ch. 16).
10. **If you ever learn a score, evaluate recall@top-X% vs control, with mature labels and min sample** (Meta; Mozilla).
11. **Name outputs as care / eligibility / caution, not as predicted failure**, unless you actually predict a labeled failure.
12. **Keep the human on Guarded/Strict and on promotion** (Akuity, DRS escalation, DCV policies).
