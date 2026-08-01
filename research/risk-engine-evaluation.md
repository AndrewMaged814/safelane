# Smallest honest Phase 1 risk-engine evaluation

**Decision date:** 2026-08-01

**Scope:** SafeLane's fixed ordinal policy, bounded local-AI findings, incident connections, evidence checks, and fail-safer behavior.

## Recommendation

Use two separate gates and one real-repository smoke run:

1. **Deterministic conformance and fault tests** prove that the fixed policy implements `docs/risk-signals.md`, preserves every safety floor, rejects invalid AI output, and fails safer when evidence is missing. These tests use mocked Ollama responses and should be fast enough to run on every change.
2. **A 12-case locked Ollama challenge** checks whether the pinned local model and prompt can find the four allowed hazards, reject close look-alikes, and distinguish real incident connections from tempting false ones. Run every case twice and report the 24 results as raw counts.
3. **One `roots/trellis` historical-change smoke run** proves that SafeLane can ingest an authentic public-repository diff selected by a published dataset. It adds realism to the demo, but it is not an accuracy benchmark and does not replace either gate.

This is enough for Gate 2. It can show that the implementation follows its policy, that the exact pinned AI setup passed a small labeled challenge, and that the same path runs on a real historical diff. It cannot show production prediction accuracy, calibration, or incident reduction. NIST's AI RMF calls for documented test sets and metrics, testing under conditions similar to deployment, explicit generalization limits, and safe failure beyond knowledge limits; this split directly follows that boundary ([NIST AI RMF Core, Measure 2](https://airc.nist.gov/airmf-resources/airmf/5-sec-core/)).

## What the evaluation is actually measuring

| Layer | Honest question | Gold answer |
|---|---|---|
| Fixed policy | Did normal code choose at least the minimum tier required by the written policy? | The versioned decision table, not a later production outcome |
| AI finding extraction | Did Ollama return an allowed finding for the intended changed behavior, with the right category and supporting added line? | Human-authored fixture label and exact source span |
| Incident connection | Did Ollama connect only records that repeat the same component or behavior, and distinguish a repeated trigger/root cause from vague similarity? | Human-authored pair label and exact spans from both records |
| Evidence reference check | Does every cited file, added line, incident ID, and incident quote exist in the supplied canonical inputs? | Exact deterministic match |
| Semantic grounding | Does the cited material actually support the finding or connection? | The fixture's human label; exact string existence alone cannot prove this |
| Fallback | On timeout, invalid output, absent history, or incomplete analysis, did SafeLane avoid the fast lane and preserve any existing `risky` floor? | The written fallback rules |

The last evidence distinction matters. Citation research evaluates both whether citations are present and whether they support the claim; these are different properties ([Gao et al., ALCE, EMNLP 2023](https://aclanthology.org/2023.emnlp-main.398/)). SafeLane's normal-code quote checker proves **reference integrity**, not causal or semantic correctness. Phase 1 should therefore display **“Source references verified”** rather than implying that normal code proved the AI's reasoning. The locked challenge can test semantic support against human labels, but the live product cannot guarantee it for unseen changes.

## Gate A — deterministic conformance and fault suite

### Decision-table coverage

Build one table-driven test for every policy branch, with values immediately below, at, and above numeric boundaries where relevant:

- change size: 2/3 files, 50/51 lines, 9/10 files, and 499/500 lines;
- changed services: 1, 2, 3, and an unmapped path;
- downstream impact: 0, 1, 3, and a directly changed critical service;
- supported versus unsupported shipping window;
- incident history checked with none found versus unavailable;
- valid AI/no danger, timeout, malformed output, and unverifiable evidence;
- each of the four allowed AI finding kinds;
- retry changes at the normal `guarded` floor and every `risky` escalation;
- incident candidate only, meaningful connection, repeated trigger/root cause, and vague similarity.

Also test the complete positive-proof fast-lane case. A missing prerequisite must change it to at least `guarded`.

### Properties, not just examples

Generate combinations around those rows and assert:

- adding a danger or uncertainty can never lower the tier;
- removing positive proof can never create `safe`;
- an AI result can keep or raise a tier, never lower a normal-code tier;
- identical canonical inputs and mocked AI output produce identical `decision.json`;
- the selected profile is never faster than the tier's minimum profile.

Property-based testing is appropriate here because the policy is a small pure mapping and its laws can be stated directly; QuickCheck's original design is precisely to generate inputs against program properties ([Claessen and Hughes, QuickCheck](https://research.chalmers.se/publication/?id=155860)).

### Injected failures

Use the same valid base fixture and inject these failures one at a time:

1. Ollama timeout or connection refusal;
2. non-JSON response;
3. schema-invalid or unknown finding kind;
4. fabricated code file or added line;
5. fabricated incident ID or quote;
6. incident store unavailable;
7. unknown changed path/service mapping;
8. incomplete/over-budget diff analysis;
9. missing or schema-invalid final `decision.json`.

Every case must reach its documented safe state: at least `guarded`/low confidence for incomplete evidence, an already-`risky` result must stay `risky`, and an unusable final decision must become `risky`/low. NIST SP 800-53 SI-17 requires defined failure conditions and associated fail-safe procedures, while SI-15 requires software output validation against expected content ([NIST SP 800-53 Rev. 5.1](https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final)).

**Gate A passes only if every row, property run, and injected failure passes.** Report the number passed and the number failed, not “accuracy.”

## Gate B — locked live-Ollama challenge

### Twelve labeled cases

Keep the live set deliberately small and inspectable:

- **4 true hazards:** one database/stored-data danger, one login/permission/secret danger, one breaking API danger, and one retry/timeout/backoff danger;
- **4 close negatives:** one look-alike for each hazard that the policy explicitly says should not become that finding, such as a read-only query, test fixture, new optional field, or bounded retry change;
- **4 incident pairs:** two genuine repeated behaviors/triggers and two hard distractors—one same-service but different behavior, and one with shared words but no meaningful connection.

Each case manifest must contain: stable ID, synthetic/real provenance, diff and parent revision, service facts, incident candidates, expected allowed findings, forbidden findings, exact supporting spans, expected minimum tier, label rationale, author, review status, and creation date. TREC calls relevance judgments the “right answers” of a test collection and warns that judgments must match the collection; SafeLane should treat its incident-pair labels the same way ([NIST TREC relevance judgments](https://trec.nist.gov/data/reljudge_eng.html)). Dataset documentation should record motivation, composition, creation, intended use, and limits, following the original Datasheets for Datasets proposal ([Gebru et al.](https://arxiv.org/abs/1803.09010)).

Run all 12 cases twice with a warm model: **24 observed runs**. The repeated run checks operational stability; it does not turn the fixtures into 24 independent examples.

### Pin the entire evaluated system

Every result must record:

- model tag `qwen2.5-coder:7b` and Ollama artifact digest `dae161e27b0e`;
- `num_ctx: 8192`, `num_predict: 768`, `temperature: 0`, `seed: 42`, and the chunk limits from `research/ollama-phase1.md`;
- Ollama version and demo-laptop hardware;
- exact prompt file plus its SHA-256;
- response JSON Schema version plus its SHA-256;
- `policy_version` (currently `2026.08.1`) and `decision.json` schema version `2`;
- corpus-manifest SHA-256, SafeLane Git commit, and run timestamp.

A model tag, prompt name, or policy name alone is not a reproducible pin.

## Metrics and release gates

The tiers are ordered (`safe < guarded < risky`), so ordinary accuracy hides whether a mistake is a harmless over-triage or a dangerous under-triage. Ordinal-classification research likewise notes that classwise precision/recall ignores ordering, while mean absolute error assumes numeric distances between classes ([Amigó et al., ACL 2020](https://aclanthology.org/2020.acl-main.363/)). With only 12 authored cases, raw counts and the full 3×3 table are clearer than weighted kappa, percentages, or a single score.

Report:

- the **3×3 expected-tier × actual-tier table** as raw counts;
- **under-triage count**, split into one-step and two-step errors;
- **false-fast count:** expected `guarded`/`risky`, returned `safe`;
- over-triage count;
- allowed findings found/missed/wrong-kind, each as `n/N`;
- nonexistent code or incident references emitted, rejected, and **accepted**;
- semantically unsupported findings/connections emitted and accepted;
- true incident connections found/missed and false incident connections made;
- each injected fallback as pass/fail;
- valid-schema runs and timing per run as raw observations.

Phase 1 release gates:

- zero deterministic minimum-tier violations;
- zero live under-triage and zero false-fast results across both runs;
- every allowed finding kind found with valid support in its positive case on both runs;
- zero hazard findings accepted in the four close-negative cases;
- zero invalid references accepted;
- zero false incident connections;
- every fallback reaches the documented safe state.

An over-triage is not a safety failure, but it must still be shown and reviewed because an engine that marks everything risky is not useful.

## No conventional train/test split

The deterministic policy is not trained. Its spec-derived examples are conformance tests, so random train/test splitting or cross-validation adds no value. Cross-validation was developed for estimating and selecting learned classifiers ([Kohavi, IJCAI 1995](https://www.ijcai.org/Proceedings/95-2/Papers/016.pdf)); SafeLane's fixed rules instead need exhaustive branch/boundary tests.

The Ollama prompt and chosen model **can still be tuned to fixtures**. Therefore:

- keep ordinary prompt-development examples in `fixtures/dev/`;
- freeze the 12 cases in `fixtures/challenge/` before the reported run;
- do not edit the prompt, schema, model choice, parsing, or policy after reading challenge results;
- if a challenge failure causes a change, move that case into development, add a new unseen case covering the same behavior, repin everything, and rerun once.

Repeatedly adapting to a holdout can overfit the holdout itself ([Dwork et al., Generalization in Adaptive Data Analysis and Holdout Reuse](https://papers.nips.cc/paper_files/paper/2015/hash/bad5f33780c42f2588878a9d07405083-Abstract.html)). With such a tiny designed set, this separation prevents an obviously misleading claim; it does not create statistical generalization.

## What synthetic fixtures allow SafeLane to claim

Allowed, with exact denominators and pins:

> On 2026-08-__ the pinned SafeLane build passed all _N_ deterministic policy/fallback checks. Across 24 runs of 12 labeled synthetic challenges, it produced _x_ under-triage results, accepted _y_ invalid references, and made _z_ false incident connections. Each of the four allowed finding kinds was exercised by at least one authored case.

Also allowed:

- “The demo is repeatable on this laptop under the recorded setup.”
- “These fixtures demonstrate the intended behavior and known failure handling.”
- “The evaluator verifies that cited source text exists; the challenge labels assess whether it supports the finding.”

Forbidden:

- “SafeLane is _x_% accurate in production” or “predicts deployment failure.”
- “The tiers are calibrated probabilities” or the raw score has measured meaning.
- “SafeLane reduces incidents, rollback rate, or change-failure rate.”
- “No incident connection means the change is safe,” or “a connection proves causality.”
- “Passing synthetic clean changes proves unseen real changes are safe.”
- any generalization to other repositories, languages, teams, incident-writing styles, or diff sizes.

Synthetic cases are valuable for controlled coverage, but their creator chooses their vocabulary and difficulty. NIST requires documenting generalization beyond evaluated conditions; without real deployment outcomes, there is no empirical bridge from these fixtures to production behavior ([NIST AI RMF Core, Measures 2.3–2.6](https://airc.nist.gov/airmf-resources/airmf/5-sec-core/)).

## Real public datasets: useful, but none validates SafeLane's deployment claim

No verified public candidate below contains all of: replayable chronological code changes, production deployment attempts, service topology, linked production incidents/health outcomes, and prior incident history. They can supplement future testing, but none replaces the Phase 1 labeled fixtures or supports rollout-accuracy claims.

| Dataset | Exact usable artifacts and access | What the label really means | SafeLane use and hard limit |
|---|---|---|---|
| **ApacheJIT** | Public [Zenodo v2 record](https://zenodo.org/records/5907847), licensed CC BY 4.0, and [replication repo](https://github.com/hosseinkshvrz/apachejit). `dataset/apachejit_total.csv` contains `commit_id`, `project`, `buggy`, `fix`, `year`, `author_date`, `la`, `ld`, `nf`, and other process metrics. `data/commit_links_*.csv` links fixing and blamed commits. The authors provide 2003–2016 training data and later, unbalanced 2017–2019 test files. | `buggy` is an **SZZ-derived bug-inducing label**. The paper explicitly starts from fixed JIRA bugs and blames earlier changed lines; it is not an observed deployment or incident label ([ApacheJIT paper, data construction](https://arxiv.org/abs/2203.00101)). | Best candidate for a future larger chronological real-commit stress set. Project + hash can often recover diffs from Apache repositories, subject to commit availability. It can test diff ingestion and compare tier ordering with later bug-fix attribution. It cannot validate SafeLane categories, service impact, incident matching, rollout choice, or production failure. |
| **IaC Defect Prediction Dataset 2** | Public [Zenodo record](https://zenodo.org/records/4299908), licensed CC BY 4.0. `repositories.json` provides repository ID, GitHub URL, default branch, and project facts; `fixing-commits.json` provides `sha`, message, date, false-positive flag, and repository ID; `fixed-files.json` provides file path, blamed `bug_inducing_commit`, linked `fixing_commit_id`, and false-positive flag. The record covers 85 Ansible repositories. | A fixing commit was identified and SZZ-style blame linked changed lines back to a possible bug-inducing commit/file. It is a source-defect history, not a production deployment or incident history. | Best Phase 1 **demo selector** because it points to readable real IaC diffs. It has no clean-commit table, service map, incident text, health outcome, severity, or rollout result, so it cannot supply SafeLane's complete gold label or a false-positive rate. |
| **JITLine / OpenStack + Qt** | Public [Zenodo package](https://zenodo.org/records/4596503) under CC BY 4.0 and [repository](https://github.com/awsm-research/JITLine-replication-package). `openstack_metrics.csv`/`qt_metrics.csv` include `commit_id`, `author_date`, churn and process fields; `*_complete_buggy_line_level.csv` includes `repo`, `commit_hash`, tokenized `code_change`, `change_type`, `bug_fixing_commit_count`, and `is_buggy_line`. | Commit and line labels are defect-introducing/blamed-line labels used for JIT defect prediction, not deployment failures. The source paper evaluates defect-introducing commits and defective lines ([JITLine paper](https://arxiv.org/abs/2103.07068)). | Useful for line-level evidence experiments and chronological partitioning via `author_date`. The packaged `code_change` is tokenized rather than a full repository diff; original diffs require resolving hashes in OpenStack/Qt history. No incidents, service graph, or rollout outcomes. |
| **TravisTorrent** | The official [format](https://testroots.github.io/travistorrent-site/page_dataformat/) has one Travis job per row and fields including `git_trigger_commit`, `git_all_built_commits`, `gh_pushed_at`, `tr_status`, `tr_log_status`, test counts, churn, and changed-file counts. The original paper reports 2,640,825 builds linked to GitHub commits ([Beller et al., MSR 2017](https://research.tudelft.nl/en/publications/travistorrent-synthesizing-travis-ci-and-github-for-full-stack-re/)). The tools repository and current data site expose no clear dataset license; verify access and rights before copying a subset. | A Travis **CI job status** or test/build-log result. It is not a production deployment or incident. Infrastructure errors and test failures also have different meanings. | Can replay many diffs from commit SHAs when repositories still exist and test whether SafeLane's outputs associate with later CI status. SafeLane explicitly excludes failed CI as a risk signal because CI should stop the deployment first, so this is only an auxiliary robustness set—not outcome validation. |
| **BugSwarm** | Public [dataset/API](https://www.bugswarm.org/dataset/) and Docker artifacts; the [schema](https://www.bugswarm.org/docs/dataset/database-organization/) includes repository, PR, failed/passed `trigger_sha`, timestamps, failed tests, patch classification, change counts, and reproducibility status. Each artifact contains failed and passing repositories plus scripts to rerun both jobs ([artifact anatomy](https://www.bugswarm.org/docs/dataset/anatomy-of-an-artifact/)). The tool repository is [BSD-3-Clause](https://github.com/BugSwarm/bugswarm/blob/master/LICENSE); confirm the licenses of each contained upstream repository before redistributing source. | A reproducible **CI fail→pass job pair**, sometimes flaky, not a production incident and not necessarily a defect introduced by the failing revision. | Strongest small real-diff replay source for testing ingestion and AI evidence on authentic changes. Filter to reproducible, code-classified pairs. It still cannot establish deployment-risk tiers, incident connections, or rollout benefit. |

The important line is: **SZZ labels, CI outcomes, and deployment incidents are three different targets.** ApacheJIT and JITLine answer “was this commit later blamed for a fixed bug?” TravisTorrent and BugSwarm answer “did this CI job fail?” SafeLane ultimately cares about “how carefully should an already-approved change roll out to limit production harm?” Treating any of the first two as the third would create a larger but less honest evaluation.

## Locked real-repository choice: `roots/trellis`

Use [`roots/trellis`](https://github.com/roots/trellis) for the single Phase 1 real-history demo, not a broad public-data benchmark.

Why this repository wins:

- it remains public and active, has about 2,000 commits, and is [MIT-licensed](https://raw.githubusercontent.com/roots/trellis/master/LICENSE.md);
- it is mostly Ansible configuration for a WordPress deployment stack, so judges can understand small configuration changes without learning a large application;
- joining the three IaC dataset files above identifies 38 distinct non-false-positive bug-inducing commits and 39 linked fixing commits for this repository, enough history to choose a case without claiming the rest of the history is clean;
- the repository's current GitHub history still resolves the selected commits and their parents, so the exact diff can be replayed.

Use this primary case:

- bug-inducing commit [`5e884c1a9508173935096dc7e2fa6a7aab16743d`](https://github.com/roots/trellis/commit/5e884c1a9508173935096dc7e2fa6a7aab16743d), **2 files and 3 changed lines**, adds a configurable shared-path permission and defaults uploads to mode `0775`;
- the dataset links that change to fixing commit [`c0a6ca82085d620cb64c440b7612a3065d4fd0bd`](https://github.com/roots/trellis/commit/c0a6ca82085d620cb64c440b7612a3065d4fd0bd), which removes that default from the normal uploads entry;
- this is the exact counterexample SafeLane needs: a change can be tiny by file/line count while still changing who may write shared deployment files. The AI may propose the allowed login/permission finding, normal code must verify the cited added line, and fixed policy then applies the `risky` floor.

The dataset also shows repeated fixes in the same deployment file, `roles/deploy/hooks/finalize-after.yml`: [`2c5e07d`](https://github.com/roots/trellis/commit/2c5e07d) was followed by fix [`632bbe8`](https://github.com/roots/trellis/commit/632bbe8), that change was followed by fix [`78ff762`](https://github.com/roots/trellis/commit/78ff762), and [`163dcc7`](https://github.com/roots/trellis/commit/163dcc7) was followed by the one-line fix [`f6d7c5a`](https://github.com/roots/trellis/commit/f6d7c5a). This supports the idea that problems can recur in the same area. It is still **commit and bug-fix history, not incident history**. SafeLane must not present these records as past production incidents unless a separate, human-created incident record supplies the trigger, root cause, and exact evidence.

For the demo, fetch `parent..5e884c1` from a shallow clone and pass it through the same production assessment entry point. Add a clearly labeled minimal demo service map only because Trellis predates SafeLane and does not contain SafeLane metadata. Do **not** invent an incident for this run; incident behavior is already tested by the locked designed cases.

The companion negative should be one small documentation-only Trellis commit selected and human-labeled **after** the prompt is frozen. The IaC dataset does not prove that unlisted commits are clean, so call it a “human-reviewed benign comparator,” not a dataset-labeled clean change. If SafeLane is tuned after either real case is viewed, both become development examples and a new untouched pair is required for any holdout claim.

Allowed real-data wording:

> SafeLane processed an authentic historical Trellis diff that a published IaC dataset later linked to a fix. It identified the permission-changing line and chose the policy's minimum careful rollout under the demo context.

Forbidden wording:

- “SafeLane predicted the Trellis failure” — the run is retrospective and the prompt may know common patterns;
- “the change caused a production incident” — the dataset records neither a deployment nor an incident;
- “SafeLane is accurate on open source” — one selected repository and tiny case pair cannot support that;
- “38 known failures” — they are blamed bug-inducing commits, not observed production failures.

## Runnable Phase 1 boundary

Each runner should produce machine-readable JSON plus a short Markdown summary, for example:

```text
python -m pytest tests/evaluation -q
python -m safelane.eval --suite challenge --repeat 2 --output evaluation/results/
python -m safelane.eval --repo fixtures/real/roots-trellis --commit 5e884c1a9508173935096dc7e2fa6a7aab16743d --output evaluation/results/trellis/
```

The deterministic command must not require Ollama or network access. The challenge command must use only the pinned local model and checked-in fixtures. Check the selected Trellis parent/diff into the evaluation fixtures with source URL, commit SHA, parent SHA, upstream license, and dataset DOI so the demo itself does not depend on GitHub availability. Do not add training, cross-validation, a general public-dataset ingestion pipeline, weighted kappa, confidence intervals, or production telemetry for Gate 2. After the hackathon, the first meaningful upgrade is a shadow-mode dataset of real SafeLane decisions joined to actual deployment and incident outcomes; only then can predictive or change-failure claims be evaluated.
