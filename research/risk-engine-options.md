# SafeLane risk engine: options and Gate 2 recommendation

**Decision date:** 2026-08-01
**Decision owner:** Andrew
**Decision needed:** What established risk-engine architecture should SafeLane use, and what is the smallest honest version that can pass Gate 2 by 2026-08-16?

## Executive decision

For Gate 2, SafeLane should use a **deterministic ordinal failure-propensity classifier followed by explicit policy floors for consequence, recoverability, operational conditions, and evidence completeness**.

This is not a predictive model and must not be presented as one. It is a versioned rollout policy with three coarse bands. It preserves the current contract and can be implemented, explained, tested, and demonstrated by the deadline without inventing statistical credibility.

The current six-signal plan has the right raw ingredients but the wrong aggregation. It combines unlike concepts into one precise-looking score:

- change size, diffusion, and incident history may inform the likelihood that the change causes a failure;
- service criticality and downstream dependency exposure describe the potential consequence;
- reversibility describes recovery cost;
- timing and support coverage are operational constraints;
- missing evidence describes confidence in the assessment.

Those dimensions should remain separate until a final rollout lane is selected. The final tier can be conservative without pretending that every guard raises the probability of failure.

The proposed `72`-style continuous score and DeployWhisper-derived aggregation should be rejected for Gate 2. A maximum contributor plus decayed secondary contributors and bonuses is a reasonable handcrafted prioritization heuristic, but it is neither a probability nor calibrated against SafeLane outcomes. Copying it would add false precision and obscure why a lane was selected.

## What the evidence supports

### 1. Established change-risk systems predict or rank failure propensity

The strongest established pattern is change-level prediction: estimate whether a change is likely to introduce a defect or severe incident, then review or gate the highest-risk fraction.

Kamei et al.'s large-scale just-in-time defect-prediction study used change diffusion, size, purpose, history, and developer-experience factors to predict defect-inducing changes. Its results established that process and change metrics can be useful, but also showed meaningful variation between projects and sensitivity to low base rates. That supports using change size, diffusion, and history as candidate predictors; it does not justify importing universal thresholds or claiming a portable probability. [Kamei et al., *A Large-Scale Empirical Study of Just-in-Time Quality Assurance*](https://sail.cs.queensu.ca/data/pdfs/TSE_ALarge-ScaleEmpiricalStudyOfJust-in-TimeQualityAssurance.pdf)

Meta's Diff Risk Score (DRS) explicitly targets the likelihood that a diff causes a production SEV. Its baseline used roughly a dozen selected predictors such as churn, diffusion, author experience, prior SEVs, and service criticality. It evaluates the fraction of SEVs captured while gating a fixed fraction of changes, which is an operational ranking objective rather than proof of calibrated probabilities. Meta also reports that directly prompting an LLM for a numeric score was unreliable and uncalibrated. [Meta DRS paper](https://arxiv.org/html/2410.06351) [Meta engineering overview](https://engineering.fb.com/2025/08/06/developer-tools/diff-risk-score-drs-ai-risk-aware-software-development-meta/)

Mozilla's Bugbug regressor is the most useful open-source implementation reference. It mines repository and Bugzilla history, derives change, author, reviewer, diffusion, source, test, and historical-regression features, trains an XGBoost classifier, and can apply isotonic probability calibration. It also excludes recent changes whose regression labels may not have matured and builds risk bands from observed regression rates with minimum sample sizes. The transferable lesson is the architecture—explicit outcome, mature labels, temporal discipline, calibration, and empirically supported bands—not the Mozilla-specific pipeline. [Mozilla Bugbug repository](https://github.com/mozilla/bugbug) [Bugbug regressor documentation](https://github.com/mozilla/bugbug/blob/master/docs/models/regressor.md)

A July 2026 Prime Video case study is promising but not production validation. It used 149 risky and 2,831 safe commits, extracted 28 diff-aware features, and reported strong cross-validation results for XGBoost and random forest. Structural code-complexity features were more useful than raw change volume alone, while an end-to-end LLM performed much worse. The paper explicitly proposes shadow deployment and later controlled evaluation; its random cross-validation also does not establish temporal generalization. SafeLane should treat this as a future feature-engineering lead, not a reason to build an ML model for Gate 2. [Prime Video deployment-risk case study](https://arxiv.org/html/2607.06766)

### 2. Production rollout control is a separate decision layer

Google SRE's canary guidance treats risk exposure as a function of how much traffic or population is exposed and for how long. It recommends beginning with small exposure when confidence is low, increasing exposure as health evidence accumulates, and choosing the simplest analysis model that meets the objective. This is evidence for keeping a failure-likelihood assessment separate from the controls that limit damage. [Google SRE Workbook: Canarying Releases](https://sre.google/workbook/canarying-releases/)

NIST's risk-assessment framing similarly distinguishes likelihood, adverse impact, and uncertainty. A final risk decision may combine them, but they should not be collapsed into an unexplained number at the evidence layer. [NIST SP 800-30 Rev. 1](https://csrc.nist.gov/pubs/sp/800/30/r1/final)

Meta's later RADAR system is especially relevant architecturally. It uses a layered funnel: eligibility and source classification, static checks, DRS, LLM review, and deterministic validation. Thresholds vary by source, and high-confidence autonomous changes still face hard rejection when a risk signal appears. The published safety comparisons are observational within a selected eligible cohort, so they do not prove that DRS alone caused the outcome. Nevertheless, the funnel validates SafeLane's proposed shape: assessment first, then deterministic eligibility and safety policy. [Meta RADAR paper](https://arxiv.org/html/2605.30208v2)

### 3. Current commercial products support the workflow, not SafeLane's exact claim

ServiceNow's change-risk products combine test, security, coverage, commit, incident, outage, and risk information to produce approval, rejection, or manual-review outcomes. Its traditional change-risk calculation also supports scored questionnaires and low/moderate/high bands. This is established ITSM approval automation, not transparent shaping of rollout parameters. [ServiceNow multimodel change approval](https://www.servicenow.com/docs/r/it-service-management/devops-change-velocity/devops-change-multimodel.html) [ServiceNow change-risk configuration](https://www.servicenow.com/docs/r/it-service-management/configure-risk-change-mgmt.html)

Digital.ai markets change-failure prediction from delivery and incident history and uses it to fast-track low-risk changes or focus manual review. It separately has a during-release health score driven by task failures, retries, lateness, and flags. The separation is instructive: pre-deployment failure propensity is not the same thing as live execution health. Public material does not expose enough model, calibration, or validation detail to treat its score as a reusable design. [Digital.ai Change Risk Prediction](https://digital.ai/products/intelligence/change-risk-prediction/) [Digital.ai release-risk calculation](https://docs.digital.ai/release/docs/how-to/configure-risk-calculation)

Akuity's Promotion Advisor is a close current market analogue: it uses GitOps diffs, commits, and promotion history to explain risk and recommend whether to promote. The human remains the decision-maker, and public documentation does not show a calibrated model or automatic parameterization of Argo Rollouts. This narrows any novelty claim SafeLane makes. SafeLane's defensible distinction is the explicit, auditable conversion of change evidence into a constrained rollout policy—not being the first tool to assess deployment risk. [Akuity Promotion Advisor](https://docs.akuity.io/intelligence/akuity-agents/promotion-advisor) [Akuity GitOps agents overview](https://akuity.io/blog/beyond-dashboards-ai-agents-for-gitops-operations)

## Reusable open-source components

### DeployWhisper

DeployWhisper is a small, active MIT-licensed advisory tool for infrastructure-as-code changes. It provides useful concepts: topology traversal for blast radius, rollback assessment, incident matching, explicit context confidence, and a conservative response to low confidence. [DeployWhisper repository](https://github.com/deploywhisper/deploywhisper)

Reuse:

- breadth-first traversal of a service topology to find dependents;
- the idea that incomplete context must prevent an optimistic result;
- evidence-backed, human-readable reasons;
- possibly small topology helpers, with MIT attribution if code is copied.

Do not reuse:

- its overall scoring formula (largest contributor plus decayed contributors and bonuses);
- token-similarity incident matching as evidence of causal similarity;
- its infrastructure-specific change representation;
- the surrounding platform for Gate 2.

DeployWhisper's formula is a policy heuristic, not a measured failure probability. More importantly, its inputs mix change likelihood with impact and rollback difficulty, which is exactly the ambiguity SafeLane needs to remove.

### Mozilla Bugbug

Reuse the conceptual pipeline later: mature outcome labels, bounded history, temporal evaluation, calibrated probabilities, and bands based on observed rates. Do not vendor or adapt its training pipeline for Gate 2. It is tied to Mozilla's Bugzilla, repository-mining, SZZ-style labeling, and organizational history; its MPL-2.0 license also requires care if source files are copied. [Mozilla Bugbug repository](https://github.com/mozilla/bugbug)

### PyDriller

PyDriller is an actively maintained Apache-2.0 repository-mining library. It can extract commits, changed files, diffs, churn, contributor counts, and other process metrics. It is an extractor, not a risk engine. For two Gate 2 fixtures, native Git diff output and path classification are smaller and more deterministic. PyDriller becomes useful once SafeLane has enough historical deployments to construct JIT features or outcome datasets. [PyDriller repository](https://github.com/ishepard/pydriller) [PyDriller process metrics](https://pydriller.readthedocs.io/en/latest/processmetrics.html)

## Critical review of the current six signals

| Current signal | Keep? | Correct semantic role | Gate 2 treatment |
|---|---|---|---|
| Size: lines and files changed | Yes | Failure propensity proxy | Use only in coarse, versioned bands. State that thresholds are demo policy, not learned truth. |
| Services touched and dependents | Split | Direct services touched is change diffusion; downstream dependents and service criticality are consequence/exposure | Direct touch count informs propensity. Dependents/criticality apply a policy floor. |
| Config versus code | No, not as a universal weight | Change-type classification; particular hazards may affect propensity or recoverability | Do not award generic risk points. Recognize explicit hazards such as migrations, persisted-state changes, retry/timeout behavior, or security-policy changes. |
| Incident history | Yes, narrowly | Failure-propensity evidence if the match is causally meaningful | Use exact configured path/service matches and a fixed lookback. Avoid fuzzy token matching in Gate 2. |
| Reversibility | Yes | Recoverability/mitigation, not failure propensity | Irreversible or persisted-state change applies a risky floor. |
| Timing | Yes, as policy | Operational readiness and representativeness, not failure propensity | Unsupported or late shipping windows apply a guarded floor. Do not claim that they increase the predicted probability. |

Two related corrections are essential:

1. **Split "blast radius."** The number of files, components, or services directly changed is diffusion and has support in JIT defect prediction. The number of downstream dependents, traffic exposure, and criticality describes potential consequence. Calling both blast radius hides a useful distinction.
2. **Define confidence as evidence completeness.** `high` or `low` should answer whether SafeLane had the required inputs and classifications, not how certain a model is about its probability. A future probabilistic engine may add a separate uncertainty measure.

## Candidate architectures

### Option A — Ordinal propensity classifier plus policy floors

**Shape:** deterministic predicates assign low, medium, or high failure propensity. Independent impact, recoverability, operations, and confidence rules can only raise the final rollout tier.

**Strengths:** deterministic, monotone, byte-stable, explainable, small enough for the deadline, and honest about the absence of outcome data. It fits the existing `score`, `tier`, `confidence`, and `reasons` contract.

**Weaknesses:** thresholds are policy choices rather than learned cutoffs; it cannot claim predictive accuracy; interaction effects are limited.

**Verdict:** recommended for Gate 2.

### Option B — Likelihood-by-impact risk matrix

**Shape:** independently assign likelihood and consequence bands, combine them with a small published matrix, then apply recoverability and operations constraints.

**Strengths:** aligns closely with established risk-management language and keeps consequence visible.

**Weaknesses:** the matrix introduces another layer of subjective policy and can create arbitrary cliff effects. It also complicates the current three-lane demo without additional evidence.

**Verdict:** credible alternative if the UI must explicitly display both axes; otherwise postpone.

### Option C — Historical risk ranker

**Shape:** learn or hand-build a rank over changes, then gate the highest-risk fraction, similar to Meta's capture-at-gating objective.

**Strengths:** directly optimizes operational review capacity and does not require probabilities to be calibrated.

**Weaknesses:** requires a real deployment cohort, stable outcome labels, and enough incidents to estimate capture. A rank is relative to a population and cannot be honestly demonstrated from two fixtures.

**Verdict:** best likely next architecture once SafeLane has production history.

### Option D — Calibrated failure-probability model

**Shape:** train logistic regression, XGBoost, or a similar supervised model on change outcomes; evaluate chronologically and calibrate on held-out data.

**Strengths:** gives the clearest semantics: an estimated probability that a change causes a defined failure within a defined outcome window. Calibration can make thresholds economically meaningful.

**Weaknesses:** labeling is hard and delayed, incident attribution is noisy, positive events are rare, distributions drift, and calibration requires independent data. A model trained on fabricated data would be worse than a transparent rule set.

**Verdict:** target architecture only after collecting sufficient real outcomes. Scikit-learn's guidance is a useful baseline: a well-calibrated 0.8 prediction should correspond to about 80% positives, and time-ordered evaluation prevents future data leaking into the past. [Scikit-learn probability calibration](https://scikit-learn.org/stable/modules/calibration.html) [Scikit-learn time-series split](https://scikit-learn.org/stable/modules/generated/sklearn.model_selection.TimeSeriesSplit.html)

## Exact Gate 2 minimum

### Contract semantics

Keep schema v2, but define each field precisely:

- `risk.score`: a compatibility projection of the **ordinal failure-propensity band**, limited to `20`, `50`, or `80`. It is not a probability, percentile, expected loss, or calibrated score.
- `risk.tier`: the final rollout-policy decision after applying impact, recoverability, operations, and confidence floors.
- `risk.confidence`: `high` when all policy-required inputs are present, fresh enough, and classified; otherwise `low`.
- `risk.reasons`: one to four evidence-backed strings identifying the dimension, observation, threshold, and any floor applied.

This deliberately permits `score: 20` with `tier: risky`: the change may have low observed failure propensity while an irreversible migration makes the acceptable rollout policy conservative. That difference is a feature, not an inconsistency.

### Propensity decision table

Use a small, explicit table. The exact numeric thresholds below are proposed demo-policy defaults and must be versioned; they are not empirical facts.

Assign **low propensity** only when all are true:

- at most 2 changed files;
- at most 50 changed lines;
- exactly 1 directly touched service;
- no exact path-prefix or service incident match in the configured 90-day lookback;
- no unclassified changed path.

Assign **high propensity** when any are true:

- at least 10 changed files;
- at least 500 changed lines;
- at least 3 directly touched services;
- an exact path-prefix or service incident match exists in the configured 90-day lookback.

Assign **medium propensity** otherwise.

Map low/medium/high propensity to scores `20`/`50`/`80`.

Threshold equality must be covered by tests. Documentation should say that these cutoffs exist to create a deterministic Gate 2 policy and will be replaced or revised from observed SafeLane data.

### Policy floors

Order tiers as `safe < guarded < risky`. Compute the final tier as the highest of the propensity tier and every applicable floor:

- **Impact floor — risky:** a critical service is directly touched, or the changed service has at least 3 downstream dependents.
- **Recoverability floor — risky:** a migration, persisted-state change, or explicit irreversible marker is present.
- **Operations floor — guarded:** the requested shipping time falls in the configured unsupported or late window, including the current Friday/Saturday policy if retained.
- **Confidence floor — guarded:** confidence is low.
- **Contract failure — risky/low:** required decision input is missing or invalid, preserving the current fail-closed behavior.

Do not create additive points for these rules. A floor says what rollout behavior is permitted, not how much the failure probability increased.

### Confidence rule

Confidence is `high` only if all of the following are true:

- the diff parsed successfully;
- policy and schema versions loaded;
- every changed path was classified;
- every non-documentation path mapped to a service;
- the shipping time parsed;
- every policy-enabled optional source—such as topology or incidents—loaded and is within its configured freshness limit.

Otherwise confidence is `low`. A deliberately disabled optional signal does not lower confidence; a required signal that silently failed does.

### Reason examples

- `Failure propensity: 12 files changed (risky at 10 or more).`
- `Impact: payouts-api has 4 downstream dependents; risky floor applied.`
- `Recoverability: migration changes persisted state; risky floor applied.`
- `Operations: requested time is outside the supported shipping window; guarded floor applied.`
- `Confidence: services/payment/** has no service mapping; guarded floor applied.`

Reasons should be derived from the exact predicates that produced the result and sorted by strongest policy effect. No LLM is needed to write them.

### Gate 2 verification

The minimum honest verification is:

- golden tests for the two demo fixtures and their byte-stable `decision.json` outputs;
- boundary tests immediately below, at, and above every numeric threshold;
- invariants: adding an impact/recovery guard cannot lower the tier; low confidence cannot produce `safe`; a risky floor always produces `risky`; identical inputs produce identical bytes; every reason has supporting evidence;
- manual review that extracted paths, counts, services, topology links, incidents, and hazards match both fixture diffs;
- schema validation and fail-closed behavior for missing or invalid input.

Seeded outcome rows may demonstrate that a dashboard computes and renders fields. They **cannot validate predictive accuracy, change-failure-rate improvement, precision, recall, or calibration**. Label those displays `synthetic demonstration — not model validation`, or omit predictive metrics at Gate 2.

## What to measure after Gate 2

Before training anything, define the outcome precisely: for example, whether a deployment causes a rollback, verified incident, or SLO breach within a fixed window, with a documented attribution process.

Then evaluate on chronological holdouts and report:

- intervention or gating rate: fraction of changes routed to stricter treatment;
- capture/recall: fraction of verified failures routed to stricter treatment;
- precision: fraction of routed changes that later have a verified failure;
- relative lift over the cohort base rate;
- confidence intervals, especially when positive counts are small;
- performance by service and time period to expose distribution shift.

Meta's phrase “capture Y% of incidents while gating X% of changes” is not a precision/recall pair. `Y` is capture/recall; `X` is the intervention rate. Precision additionally requires the number of failures among gated changes.

If SafeLane later claims probabilities, add reliability diagrams, Brier score or log loss, and calibration on data separate from model fitting. After offline validation, run shadow mode before allowing the model to change rollout policy, then use a controlled rollout where operationally safe.

## Corrections to current artifacts

The following current ideas should be corrected in the plan and demo language:

- `score: 72` appears precise but has no measured meaning. Replace it with an explicitly ordinal projection or remove the score in a future schema.
- DeployWhisper's score aggregation is not “worth mimicking” for SafeLane. Its topology and confidence concepts are useful; its combined score is not.
- “Blast radius” currently combines direct change diffusion with downstream consequence. Split them.
- “Config versus code” has no universal ordering. Meta itself treats different sources/configuration systems with source-specific models and thresholds. Replace the generic weight with explicit hazard classification.
- Reversibility and shipping time should constrain rollout policy, not inflate a claimed failure probability.
- Synthetic outcomes cannot establish CFR improvement, precision, recall, or calibration.
- Meta's `capture Y at gating X` metric should not be described as precision and recall.
- RADAR's reported safety ratios are encouraging observational outcomes in an eligible, multiply gated cohort, not a causal estimate of DRS effectiveness.
- The Prime Video result is not yet production validation and did not use a chronological holdout.
- The absence of a directly identical open-source product is uncertain and should not be a headline claim. Akuity and established ITSM tools already occupy nearby territory.
- A Kupiec value-at-risk test is not appropriate here. It tests financial VaR exception rates and adds no credibility to a tiny, synthetic deployment dataset.

## Explicitly rejected for the 2026-08-16 deadline

- training logistic regression, XGBoost, random forest, or an LLM scorer;
- using an LLM to produce the numeric decision or authoritative reasons;
- copying Bugbug's Mozilla-specific training and labeling pipeline;
- adding PyDriller to the scoring hot path for two fixtures;
- adopting DeployWhisper's composite formula or fuzzy token incident matcher;
- building AST/structural-complexity extraction solely because it performed well in the Prime Video case study;
- author identity, tenure, or experience features, which bring privacy and gaming concerns without helping this demo;
- CVE, SARIF, dependency-vulnerability, or generalized security scoring;
- a continuous 0–100 score with one-point distinctions;
- a synthetic “backtest,” Kupiec test, or predictive accuracy claim;
- automated policy learning, dynamic canary equations, or self-healing thresholds;
- a universal config-versus-code risk weight;
- SZZ-style defect attribution and a full outcome-labeling platform.

These may be valid future work only when driven by real repeated use and sufficient data. None is required to prove the Gate 2 contract: evidence in, deterministic decision out, and a rollout lane that never becomes less conservative when material risk or uncertainty is added.

## Remaining uncertainty

- No public market survey can prove that an identical product does not exist, especially for internal enterprise systems. Novelty claims should remain narrow.
- Commercial vendors do not publish enough model or calibration detail for an apples-to-apples comparison.
- SafeLane has no production outcome dataset, so no proposed threshold has demonstrated predictive validity.
- Incident matches can be correlated with risky areas without being causally relevant to a particular diff; exact matching improves auditability but not causality.
- The proposed `2/50/1` and `10/500/3` cutoffs are intentionally coarse. Fixture review may reveal that different values communicate the demo better, but any change remains policy tuning, not research validation.
- A critical-service or three-dependent risky floor is a product-policy choice. It should be confirmed with the team that owns the demo topology.
- Shipping-window policy depends on actual support coverage. Friday/Saturday should remain only if it reflects the team's operating context.

## Final recommendation

Implement Option A and freeze its semantics for Gate 2:

> SafeLane classifies observable failure propensity into three coarse, deterministic bands, then applies explicit conservative floors for consequence, recoverability, operational readiness, and missing evidence to select a rollout lane.

That is the smallest architecture that is technically defensible and visibly useful. It borrows established ideas without pretending to have the data of Meta, Mozilla, or Prime Video. The next research decision should occur only after SafeLane records real deployment outcomes: whether the accumulated sample supports a historical ranker, a calibrated probability model, or merely better versioned rules.
