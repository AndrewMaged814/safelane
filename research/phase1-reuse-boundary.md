# SafeLane Phase 1 reuse boundary

**Decision date:** 2026-08-01
**Decision owner:** Andrew
**Deadline constraint:** Gate 2 ends 2026-08-16.
**Question:** What should SafeLane copy, adapt, learn from, or build itself for the Phase 1 risk engine?

## Decision

**Copy no upstream source code in Phase 1.** Reuse DeployWhisper's service-graph shape, honest synthetic-incident format, and fail-safer-on-missing-context rule as design references. Implement the small SafeLane versions from scratch.

This is not “reuse is gone.” It is a tighter form of reuse:

- reuse the parts that already answer a general problem well;
- avoid importing IaC, database, web-app, and scoring machinery that SafeLane does not need;
- do not copy small generic functions when adapting them would take longer than writing and testing the SafeLane version;
- keep DeployWhisper named as prior art and design inspiration.

The earlier estimate in `detailed-plan.md`—about 420 copied or closely derived lines and 20–25% reuse—is now stale. It predates the bounded Ollama decision, exact-evidence incident matching, and the fixed risk rules in `docs/risk-signals.md`.

## Sources inspected

The review used current upstream source, pinned so every link remains stable:

- DeployWhisper `develop` at [`005868402650006e6d40fc72e0bdd59ea14fb353`](https://github.com/deploywhisper/deploywhisper/tree/005868402650006e6d40fc72e0bdd59ea14fb353), plus released v1.3.0. License: [MIT](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/LICENSE).
- Mozilla Bugbug `master` at [`a1b7aa1a83434c7d6ad61721b99eb38a534457c5`](https://github.com/mozilla/bugbug/tree/a1b7aa1a83434c7d6ad61721b99eb38a534457c5). License: [MPL-2.0](https://github.com/mozilla/bugbug/blob/a1b7aa1a83434c7d6ad61721b99eb38a534457c5/LICENSE).
- PyDriller `master` at [`56b186d11119c5276e41ba8be0586feffedfac1a`](https://github.com/ishepard/PyDriller/tree/56b186d11119c5276e41ba8be0586feffedfac1a), current release 2.10. License: [Apache-2.0](https://github.com/ishepard/PyDriller/blob/56b186d11119c5276e41ba8be0586feffedfac1a/LICENSE).
- Git's official [`git diff` documentation](https://git-scm.com/docs/git-diff), including the machine-readable `--numstat` and `-z` formats.

## File/function-level boundary

Estimates are focused implementation time for one developer and include small unit tests.

| Decision | Exact upstream path or symbol | What SafeLane needs | Fit and Phase 1 action | License / attribution | Estimate |
|---|---|---|---|---|---:|
| **COPY: none** | — | A small, auditable risk engine | No inspected function fits without changing its inputs, outputs, or safety meaning. Copying would create attribution work without reducing delivery risk. | Keep SafeLane's own license. Continue naming DeployWhisper as prior art. | 0 |
| **ADAPT / LEARN** | DeployWhisper [`data/topology/service_topology.json`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/data/topology/service_topology.json) and [`analysis/blast_radius.py::compute_blast_radius`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/analysis/blast_radius.py#L345-L477) | Map changed paths to services; count directly changed services and their downstream dependents; mark critical services | Keep the `services[]` + `downstream[]` graph idea. Replace IaC `resource_keys` with SafeLane `path_globs`, add `critical`, and write a tiny path mapper plus breadth-first walk. Do not port the 477-line module: it depends on `UnifiedChange`, Pydantic models, topology freshness, owners, warnings, and IaC aliases. | No MIT code copied, so no license text is legally triggered by this plan. Cite DeployWhisper in the README/design note. If implementation later copies code, preserve its MIT copyright and full license in `THIRD_PARTY_NOTICES.md`, and add a provenance header to the derived file. | 3–4 h |
| **ADAPT / LEARN** | DeployWhisper [`samples/incidents/safe-pack-v1/*.md`](https://github.com/deploywhisper/deploywhisper/tree/005868402650006e6d40fc72e0bdd59ea14fb353/samples/incidents/safe-pack-v1) and [`manifest.json`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/samples/incidents/safe-pack-v1/manifest.json) | Seeded incidents with date, service, component, summary, root cause, trigger, and text that Ollama can quote | Keep the explicit `Sample data`, provenance, limitations, root-cause, trigger-change, and affected-services fields. Author SafeLane's own records and wording. Add stable incident IDs and component/path fields so normal code can select at most five same-service incidents from 180 days. | Do not copy DeployWhisper's sample prose. Credit its sample-pack format as inspiration. Directly copying a sample or substantial wording would require preserving the MIT notice. | 2–3 h |
| **ADAPT / LEARN** | DeployWhisper [`incident_matcher.py::_extract_markdown_list_section`, `_normalize_section_title`, `_section_title_aliases`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/analysis/incident_matcher.py#L193-L228) and [`incident_service.py::_extract_title`, `_extract_severity`, `_extract_incident_date`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/services/incident_service.py#L84-L116) | Load and validate a tiny local incident corpus | Learn the parser shape, but write one SafeLane loader for its own fixed format. SafeLane also needs full root-cause and trigger text for evidence verification; the upstream list-section helper keeps only a list or the first non-list line, so copying it would be incomplete. | Same MIT rule as above. Fresh SafeLane code plus a source citation needs no copied-code notice. | 2–3 h, included above |
| **ADAPT / LEARN** | DeployWhisper [`analysis/risk_scorer.py::apply_context_uncertainty`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/analysis/risk_scorer.py#L173-L210) | Missing history, unknown paths, invalid Ollama output, or incomplete evidence must never produce `safe` | Keep only the monotone control rule: missing evidence raises the minimum tier to `guarded`; existing `risky` facts remain risky. Implement it as SafeLane policy floors, not a context percentage. This matches the agreed ADR and risk-signal decision. | Design idea only; cite upstream. Copying the function would import incompatible assessment models and would trigger MIT notice duties. | 1–2 h |
| **ADAPT / LEARN — future only** | Bugbug [`docs/models/regressor.md`](https://github.com/mozilla/bugbug/blob/a1b7aa1a83434c7d6ad61721b99eb38a534457c5/docs/models/regressor.md) and [`bugbug/models/regressor.py::RegressorModel`](https://github.com/mozilla/bugbug/blob/a1b7aa1a83434c7d6ad61721b99eb38a534457c5/bugbug/models/regressor.py#L39-L357) | A future model trained on real SafeLane deployment outcomes | Learn the discipline: define outcome labels, allow labels time to mature, evaluate on later data, calibrate probabilities, and derive bands from observed outcomes. Phase 1 has no real outcome dataset and explicitly excludes a trained risk model and author scoring. Do not install, vendor, or port Bugbug. | No obligation when only studying it. Copied or modified Bugbug source files remain covered by MPL-2.0 and their source must remain available under MPL when distributed. Avoid that scope now. | More than 1 week plus real data; **not Phase 1** |
| **ADAPT / LEARN — fallback only** | PyDriller [`Repository.traverse_commits`](https://github.com/ishepard/PyDriller/blob/56b186d11119c5276e41ba8be0586feffedfac1a/pydriller/repository.py), [`ModifiedFile.added_lines`, `deleted_lines`, `diff_parsed`](https://github.com/ishepard/PyDriller/blob/56b186d11119c5276e41ba8be0586feffedfac1a/pydriller/domain/commit.py), and [process metrics](https://github.com/ishepard/PyDriller/blob/56b186d11119c5276e41ba8be0586feffedfac1a/docs/processmetrics.rst) | Current diff, changed paths, line counts, and exact added lines | PyDriller is a repository-mining library, not a risk engine. Its historical metrics are excluded from Phase 1. Use native `git diff --numstat -z` for counts and a normal patch for Ollama/evidence verification. Reconsider PyDriller only if later history mining becomes a real requirement. | No Phase 1 dependency, so no added obligation. If distributed later, retain Apache-2.0 notices and mark modified upstream files as changed. | Native Git wrapper: 3–4 h. PyDriller integration: 0 h now |
| **BUILD** | No upstream equivalent | Diff reader and canonical evidence store | Run native Git once for machine-readable paths/counts and once for the patch. Normalize added lines in one place so the prompt and verifier use exactly the same evidence. Treat binary/unparseable files as unknown. | SafeLane code only. | 3–4 h |
| **BUILD** | No compatible DeployWhisper symbol | Fixed policy evaluator | Implement the exact small/medium/large, service/dependent, criticality, timing, evidence, incident, and four AI-finding rules from `docs/risk-signals.md`. Rules only keep or raise a tier. Do not add weighted points. | SafeLane code only. | 1 day |
| **BUILD** | No upstream equivalent | Bounded local-AI adapter | Call Ollama `qwen2.5-coder:7b` with the locked limits; require the small JSON Schema; accept only the four agreed finding types plus incident connections. AI finds evidence but never chooses a lane. | SafeLane code only; Ollama/model licensing is already covered by `research/ollama-phase1.md`. | 1 day |
| **BUILD** | No upstream equivalent | Evidence verifier | Verify every quoted code line against canonical added diff lines and every incident quote against the selected incident text. Invalid evidence makes confidence low and the result at least guarded. | SafeLane code only. | 3–4 h |
| **BUILD** | No upstream equivalent | Incident candidate selector | Before Ollama, normal code filters to affected services, 180-day lookback, newest first, maximum five total. Distinguish “checked, none found” from “history unavailable.” | SafeLane code only. | 2–3 h |
| **BUILD** | No upstream equivalent | `decision.json` writer and validator | Emit schema v2, policy version, tier, confidence, and 1–4 plain reasons. Invalid/missing required input fails closed as risky/low. Rollout mapping is deliberately left to issue #6. | SafeLane code only. | 3–4 h |
| **BUILD** | Use upstream test ideas only | Golden acceptance tests | Test the trivial and risky demo diffs, size boundaries, unknown paths/history, invalid AI output, evidence rejection, service graph counts, and repeatability. Mock Ollama responses in unit tests; keep one local-model smoke test. | SafeLane code only. | 1 day |

Focused total for Andrew's Phase 1 risk side: about **5 working days**, leaving buffer before Gate 2 for integration and demo fixes.

## Explicitly rejected pieces

| Rejected upstream piece | Why it does not belong in SafeLane Phase 1 |
|---|---|
| DeployWhisper [`parsers/base.py::UnifiedChange`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/parsers/base.py#L35-L64) and IaC parsers | It models Terraform/Kubernetes/Ansible resources and actions. SafeLane analyzes application Git diffs. Adapting it creates a false abstraction. |
| DeployWhisper [`risk_scorer.py::_overall_score`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/analysis/risk_scorer.py#L754-L784) and severity anchors | Weighted/decayed scoring mixes unlike facts and creates false precision. SafeLane already chose coarse tiers plus explicit safety floors. |
| DeployWhisper [`incident_matcher.py::find_incident_matches`](https://github.com/deploywhisper/deploywhisper/blob/005868402650006e6d40fc72e0bdd59ea14fb353/analysis/incident_matcher.py#L484-L586) | It ranks incidents using token overlap, severity, recency, and service bonuses. The result can look confident without proving the same behavior caused both events. SafeLane requires exact quotes from both records, verified by normal code. |
| DeployWhisper `interaction_risk.py`, `rollback_planner.py`, settings/database/API services, backtesting service, CLI, and frontend | They solve IaC-tool interactions, human rollback plans, a multi-user product, or presentation concerns. None is required for a stable Gate 2 `decision.json`. |
| DeployWhisper dashboard tokens, score ring, sparkline, and component library | UI reuse is outside this risk-engine decision and does not help Gate 2 correctness. Revisit only after the end-to-end demo works. |
| Bugbug training pipeline | It requires Mozilla/Bugzilla history, SZZ-style labels, XGBoost/calibration dependencies, and enough mature outcomes. Using synthetic labels would make the result less honest than fixed rules. |
| PyDriller process metrics | Phase 1 deliberately excludes author/history metrics and only needs two demo diffs. Native Git is already installed, smaller, and deterministic. |

## License boundary

Under this decision, SafeLane copies **no** third-party code or sample text, so a `THIRD_PARTY_NOTICES.md` file is not required for the Phase 1 risk engine. Attribution still matters: keep DeployWhisper named in the README and pitch as the source of the graph, incident-pack, and missing-context design lessons.

If implementation later copies or closely ports DeployWhisper code or sample content, stop and do all of the following before committing it:

1. add DeployWhisper's full MIT license and `Copyright (c) 2026 deploywhisper` to `THIRD_PARTY_NOTICES.md`;
2. add a header to each derived SafeLane file naming the exact upstream path, pinned commit, and changes made;
3. list the copied files/functions in the README or notice file;
4. do not describe learned ideas as copied code, or copied code as original SafeLane work.

Do not copy Bugbug source in Phase 1. Its MPL-2.0 obligations are file-level and bring source-distribution duties that are unnecessary for this deadline. Do not add PyDriller unless native Git proves insufficient.

## Locked implementation boundary

**Reuse now:** DeployWhisper's service-graph shape, synthetic-incident honesty fields, incident headings, and default-safer-on-missing-context principle—adapted and credited, not copied.

**Build now:** the thin Git reader, service/path mapper, graph walk, incident selector/loader, Ollama schema adapter, quote verifier, monotone policy evaluator, decision writer, and golden tests.

**Postpone:** learned models, historical author/process metrics, security-scanner ingestion, production incident systems, generic IaC analysis, dashboards, backtesting, and any wider DeployWhisper platform integration.

This keeps the useful prior art, removes misleading reuse claims, and leaves SafeLane's real contribution clear: bounded AI evidence plus deterministic change-risk policy that feeds the rollout decision.
