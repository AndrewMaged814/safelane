# LLM code-change analysis engines (2025–2026)

**Research date:** 2026-08-13
**Question:** How do production LLM change-analysis engines give the model *more* involvement while remaining trustworthy, and what should SafeLane adopt when moving from one bounded Ollama call to Azure OpenAI?

**SafeLane today (for comparison):**
- One bounded LLM call; the model returns a closed finding kind plus exact diff-line citations.
- Normal code verifies that each cited span *exists* in the parsed diff. It does not verify that the interpretation is correct. This matches the glossary term **source reference verified**: confirmation of cited text at the claimed location, not that the AI interpretation is correct (`CONTEXT.md`).
- Deterministic file/line thresholds set the change-scope band; **safety floors** raise the **risk tier** (`safe` / `guarded` / `risky`) when danger or uncertainty is present. AI failure and real danger both raise the floor; neither may lower it.
- [ADR 0001](../docs/adr/0001-bound-ai-to-risk-findings.md) already forbids the model from returning a probe ID, URL, host, image, command, credential, **risk tier**, **rollout profile**, approval, or rollout setting.

This note does **not** override ADR 0001. “More AI involvement” here means a richer **AI safety case** (more findings, more typed claims, a second verification pass), not AI-chosen rollout behavior.

---

## Recommendation in one page

Give the model more *semantic* work, keep rollout in normal code.

1. **Finder pass** (Azure OpenAI Chat Completions + structured outputs): the model returns *up to five* typed findings with exact `file` / `side` / `line` / `text` spans, plus closed enums for endangered contract, blast-radius *claim*, rollback *claim*, and tests-missing *claim*. It must not return a risk tier or rollout profile.
2. **Deterministic span check** (existing SafeLane behavior, kept): every span must exist in the GitHub PR diff. Invalid or missing evidence drops the finding; it never repairs the citation.
3. **Verifier pass** (second structured-output call, same or cheaper model): given *only* the cited spans plus the decontextualized claim, the model returns `supported` / `not_supported` / `insufficient_evidence`. This is ALCE/AIS-style citation faithfulness, not a second risk score.
4. **Policy** (normal code, unchanged authority): change-scope band + verified finding kinds + verifier outcomes + AI-failure floors choose the **risk tier** and **rollout profile**. Unverified claims, content-filter hits, timeouts, and schema failures raise the floor; they never create **fast-lane eligibility**.

Pin a **gpt-4.1** (2025-04-14) deployment with `versionUpgradeOption: NoAutoUpgrade` for the hackathon. Use Chat Completions `response_format.json_schema` with `strict: true`. Do not send `temperature` if you later switch to an o-series / GPT-5 reasoning deployment (Azure documents those parameters as unsupported). Do not give the model tools that fetch extra GitHub files unless you later expand the analysis path; today the only analysis path is the attached PR diff.

---

## 1. Azure OpenAI / OpenAI structured outputs

### 1.1 What structured outputs actually guarantee

Azure: “Structured outputs make a model follow a JSON Schema definition that you provide as part of your inference API call. Both the Chat Completions API and Responses API support structured outputs. For Chat Completions, define the schema in `response_format`. For Responses, define the schema in `text.format`. This approach contrasts with the older JSON mode feature, which guaranteed valid JSON but couldn't ensure strict adherence to the supplied schema.”
[Azure structured outputs, updated 2026-08-06](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

OpenAI: “Structured Outputs is a feature that ensures the model will always generate responses that adhere to your supplied JSON Schema, so you don’t need to worry about the model omitting a required key, or hallucinating an invalid enum value.” Benefits listed: reliable type-safety, explicit refusals, simpler prompting.
[OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)

**Honest limit:** schema adherence is not claim correctness. SafeLane’s own Ollama research already observed this: a 3B model returned valid JSON, then mislabeled the change and cited the wrong line ([`research/ollama-phase1.md`](ollama-phase1.md)). Azure structured outputs do not close that gap.

OpenAI is explicit that even with structured outputs the model may still fail to produce a matching response: “This can happen in the case of a refusal, if the model refuses to answer for safety reasons, or if for example you reach a max tokens limit and the response is incomplete.”
[OpenAI Structured Outputs, edge cases](https://developers.openai.com/api/docs/guides/structured-outputs)

### 1.2 Chat Completions vs Responses API

| Surface | Schema location | Hackathon fit |
|---|---|---|
| Chat Completions | `response_format: { type: "json_schema", json_schema: { name, strict: true, schema } }` | **Use this.** Matches SafeLane’s one-shot analysis; Azure REST example is a single POST to `/openai/v1/chat/completions`. |
| Responses API | `text.format: { type: "json_schema", name, schema, strict: true }` | Use later if you add tool loops or GPT-5 reasoning+tools. Azure says new reasoning features ship here first. |

Azure REST Chat Completions example (key-based):

```http
POST https://YOUR_RESOURCE_NAME.openai.azure.com/openai/v1/chat/completions
```

with `"model": "YOUR_MODEL_DEPLOYMENT_NAME"` and `response_format.type = json_schema`, `json_schema.strict = true`.
[Azure structured outputs, REST](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

Python key-based example on the same page uses:

```python
client = OpenAI(
  base_url = "https://YOUR-RESOURCE-NAME.openai.azure.com/openai/v1/",
  api_key=os.getenv("AZURE_OPENAI_API_KEY")
)
completion = client.beta.chat.completions.parse(
    model="MODEL_DEPLOYMENT_NAME",  # replace with the model deployment name
    ...
    response_format=CalendarEvent,
)
```

Entra ID uses `get_bearer_token_provider(DefaultAzureCredential(), "https://ai.azure.com/.default")` instead of the API key. The `model` argument is the **deployment name**, not the family name `gpt-4.1`.
[Azure structured outputs](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

Azure also documents that API version `2024-08-01-preview` is the first version that supports structured outputs, and “the latest preview APIs and the latest GA API, `v1`, also support structured outputs.”
[Azure structured outputs, API support](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

**Do not invent a third API.** There is no Azure “code review” endpoint. You send the PR diff as chat content and constrain the reply with JSON Schema.

### 1.3 Function calling / tools vs `response_format`

OpenAI’s rule:

- “If you are connecting the model to tools, functions, data, etc. in your system, then you should use function calling”
- “If you want to structure the model’s output when it responds to the user, then you should use a structured `response_format`”
[OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)

SafeLane’s analysis path is “read this attached PR diff, return a typed safety case.” That is `response_format`, not tools.

GitHub Copilot code review *does* use agentic tool use (full-repo context via GitHub Actions; MCP servers). That is a different product shape: Copilot is a reviewer with repository access, not a bounded one-shot assessor. Copilot docs: “Full project context gathering… analyzes your entire repository.” If Actions fail, “reviews will still be generated. However, they will not include the additional features provided by the agentic capabilities.”
[About GitHub Copilot code review](https://docs.github.com/en/copilot/concepts/agents/code-review)

Azure note: “Structured outputs are not supported with parallel function calls. When using structured outputs set `parallel_tool_calls` to `false`.”
[Azure structured outputs](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

For a hackathon with **GitHub PR attachment as the only analysis path**, do not add tools that `gh api` extra files. Extra context is how Copilot/Gemini get better reviews; it is also how an unbounded agent escapes ADR 0001.

### 1.4 Schema constraints that will bite SafeLane

Azure structured outputs support a **subset** of JSON Schema. Quoted constraints from the Azure page:

- Supported types: String, Number, Boolean, Integer, Object, Array, Enum, anyOf. “Root objects can't be the `anyOf` type.”
- “All fields must be required.” Optional values are emulated with a union that includes `null`, e.g. `"type": ["string", "null"]`.
- “A schema can have up to 100 object properties total, with up to five levels of nesting.”
- “Always set `additionalProperties: false` in objects.”
- Unsupported string keywords: `minLength`, `maxLength`, `pattern`, `format`.
- Unsupported number keywords: `minimum`, `maximum`, `multipleOf`.
- Unsupported array keywords: `minItems`, `maxItems`, `uniqueItems`, and several others.
[Azure structured outputs, JSON Schema support](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

SafeLane’s current `schemas/ai-response-v2.schema.json` uses `minLength`, `maxLength`, `minItems`, `maxItems`, and `minimum`. Those are valid for **application-side** jsonschema validation. They are **not** valid to send as Azure `strict: true` structured outputs. Split the contract:

- **Wire schema** (Azure): enums, required fields, `additionalProperties: false`, optional-via-null.
- **Engine schema** (existing validator): keep length/count bounds and reject oversized quotes locally.

`$defs` / `$ref` **are** supported on Azure (the docs include a worked example). Recursion is also supported, but SafeLane does not need it.

### 1.5 How to pin a model

Azure inference `model` is the **deployment name**. The actual model family + version is set on the Cognitive Services deployment, not in the chat request.

Upgrade policies ([Working with models](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/working-with-models)):

| `versionUpgradeOption` | Behavior |
|---|---|
| `OnceNewDefaultVersionAvailable` | Auto-upgrade when Azure makes a new default |
| `OnceCurrentVersionExpired` | Stay until retirement, then upgrade |
| `NoAutoUpgrade` | “The model deployment never automatically upgrades. Once the retirement date is reached the model deployment stops working.” |

For a demo that must be repeatable, create a deployment with an explicit version and `NoAutoUpgrade`. Azure’s structured-outputs supported-model list includes (among others):

- `gpt-4.1` version `2025-04-14`
- `gpt-4.1-mini` / `gpt-4.1-nano` version `2025-04-14`
- `gpt-5` version `2025-08-07`, `gpt-5-mini`, `gpt-5-nano`
- o-series: `o3` `2025-04-16`, `o4-mini` `2025-04-16`, `o3-mini`, `o1`
[Azure structured outputs, supported models](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs)

**Hackathon pick:** `gpt-4.1` `2025-04-14`. Reasons from the docs, not preference:

- Structured outputs, Chat Completions, and function calling are all listed for the GPT-4.1 series.
- Context window is documented as 1,047,576 tokens, but **standard deployments are listed at 300,000** and provisioned/batch at 128,000. Max output 32,768.
  [Azure models sold directly by Azure, GPT-4.1 series](https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/models-sold-directly-by-azure)
- GPT-4.1 still accepts `temperature`. Reasoning models do not (see §6).

Do **not** put `gpt-4.1` in application code as if it were the API `model` string. Put the deployment name in config/env. Record the pinned version in assessment evidence (`model` + deployment version), the same way today’s Ollama adapter records `model_digest`.

---

## 2. Citation faithfulness and two-pass verification

### 2.1 Gao et al. ALCE (EMNLP 2023) is still the right citation

Tianyu Gao, Howard Yen, Jiatong Yu, Danqi Chen. *Enabling Large Language Models to Generate Text with Citations.* EMNLP 2023. ACL Anthology: [2023.emnlp-main.398](https://aclanthology.org/2023.emnlp-main.398/). arXiv: [2305.14627](https://arxiv.org/abs/2305.14627). Code: [princeton-nlp/ALCE](https://github.com/princeton-nlp/ALCE/).

ALCE evaluates three things separately: fluency, correctness, and **citation quality**. Citation quality splits into:

- **Citation recall:** is the statement fully supported by the cited passages?
- **Citation precision:** are any citations irrelevant?

They operationalize “fully support” with an NLI model (TRUE / T5-11B). They explicitly align this with Rashkin et al.’s AIS framework: if the concatenated citations entail the statement, the statement is treated as true *based solely on those citations*.

Headline result that still matters: “on the ELI5 dataset, even the best models lack complete citation support 50% of the time.”

Other ALCE results that map onto SafeLane:

- Putting retrieved passages in context (“VANILLA”) beats closed-book + post-hoc citing. Closed-book can be *correct* and still fail citation quality, because the text is not similar to any passage you can attach afterwards. SafeLane already requires the model to quote the diff; do not switch to “find first, cite later.”
- Reranking several generations by automatic citation recall improved citation quality (human-confirmed). A cheap hackathon analogue: if the verifier says `not_supported`, drop the finding rather than retrying forever.
- More context does not automatically improve citation quality. ChatGPT-16K did not gain from extra passages; GPT-4 did somewhat. Dumping an unbounded repo into the finder is not a faithfulness strategy.

### 2.2 AIS (Rashkin et al., 2023)

Hannah Rashkin et al. *Measuring Attribution in Natural Language Generation Models.* Computational Linguistics 49(4), 2023. [ACL Anthology](https://aclanthology.org/2023.cl-4.2/). arXiv: [2112.12870](https://arxiv.org/abs/2112.12870).

AIS: an NLG statement about the external world is attributable iff a reader could see that the identified source supports the information. The annotation pipeline is two-stage: first, is the sentence interpretable on its own; second, is it supported by the provided source.

SafeLane’s current check is weaker than AIS. Existence of `text` at `file:line` is a necessary condition for attribution, not AIS itself. A model can quote a real added line and still claim the wrong endangered contract.

### 2.3 LLM-as-judge is a different (and leakier) idea

Zheng et al. *Judging LLM-as-a-Judge with MT-Bench and Chatbot Arena.* arXiv [2306.05685](https://arxiv.org/abs/2306.05685). GPT-4 as a judge reached >80% agreement with humans on open-ended chat quality, with documented **position, verbosity, and self-enhancement** biases.

Use this paper as a warning, not as a rollout oracle:

- A second model that scores “how risky is this PR?” will inherit verbosity/self-enhancement bias and is exactly the “AI risk score” ADR 0001 rejected.
- A second model that answers “does citation C entail claim H?” is ALCE/AIS, not MT-Bench. Keep the verifier’s output a closed entailment enum, not a 1–10 score.

Meta’s DRS paper independently found that prompting a generative LLM for a numeric risk score is “tedious” and “unreliable, as the model does not have a universal view of risk and may generate uncalibrated scores across different examples.”
[Abreu et al., arXiv 2410.06351](https://arxiv.org/abs/2410.06351)

### 2.4 Two-pass architecture that is actually attested

| Pass | Who | Sees | Returns | Trusted for |
|---|---|---|---|---|
| Finder | LLM A | Full bounded PR diff + closed schema | Findings + exact quotes | Candidate **AI safety cases** |
| Span check | Normal code | Parsed diff index | Boolean existence | **Source reference verified** |
| Verifier | LLM B (or A again) | *Only* the quoted spans + decontextualized claim | `supported` / `not_supported` / `insufficient_evidence` | Citation faithfulness, not runtime truth |
| Policy | Normal code | Verified findings + scope band + floors | **Risk tier** + **rollout profile** | Release behavior |

Same-model verifier is acceptable for a hackathon (ALCE’s NLI verifier was a different, smaller model; Zheng used GPT-4 to judge GPT-4 with known self-enhancement risk). If Azure quota allows, use `gpt-4.1` for finder and `gpt-4.1-mini` for verifier to cut cost and reduce “the finder agreeing with itself.”

The verifier prompt must **not** include the full diff. If it can see the whole change, it can rubber-stamp a claim using uncited context, which breaks AIS (“true based solely on concat(C)”).

---

## 3. Production LLM code-review engines

None of these products choose a Kubernetes rollout. They post review comments. Several let the model pick **comment severity**. None of the official docs reviewed here let the model pick merge/ship authority.

### 3.1 GitHub Copilot code review

Primary: [About GitHub Copilot code review](https://docs.github.com/en/copilot/concepts/agents/code-review), [Using Copilot code review](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review).

Engine shape (from GitHub, not blogs):

- Input is a pull request. Copilot “reviews your pull requests, identifies issues, and suggests fixes.”
- Grounding is GitHub’s review-comment model: inline comments on the diff. Official how-to: Copilot “always leaves a **Comment** review, not an **Approve** or **Request changes** review. Its reviews do not count toward required approvals and will not block merging.”
- Agentic extra context is optional: full-repo gathering via GitHub Actions; MCP servers; agent skills. If Actions are unavailable, review still runs, weaker.
- Effort levels: **Lite** (default, common bugs/security/style) vs **Balanced** (“routes pull requests to a higher-reasoning model for longer analysis of complex logic, security-sensitive code, and cross-service changes”).
- “Model switching is not supported” for Copilot code review. The product owns the model mix.
- GitHub’s own validation language: “Copilot is not guaranteed to spot all problems… Sometimes it will make mistakes. Always validate Copilot's feedback carefully. Supplement Copilot's feedback with a human review.”
- Merge gating, when it exists, is **GitHub Code Quality** (CodeQL rules, coverage thresholds, rulesets) — a deterministic layer beside Copilot, not Copilot’s LLM picking severity-to-block.

GitHub does **not** document a public finding schema with severity enums for Copilot code review. Do not claim Copilot lets the model set merge severity; the attested fact is the opposite: Copilot cannot approve or block.

### 3.2 Google Gemini Code Assist on GitHub

Primary: [Review GitHub code using Gemini Code Assist](https://developers.google.com/gemini-code-assist/docs/review-github-code), [Customize behavior](https://developers.google.com/gemini-code-assist/docs/customize-gemini-behavior-github).

Engine shape:

- Gemini-powered agent summarizes PRs and posts in-depth code reviews. It “will automatically retrieve helpful information from the repository and pull request.”
- Grounding: GitHub PR review comments (file/line by construction of the GitHub review API). It will not generate summaries or suggestions for files under `.github/workflows`.
- **The model is allowed to pick comment severity.** Repo `config.yaml` includes:

```yaml
code_review:
  comment_severity_threshold: MEDIUM  # LOW | MEDIUM | HIGH | CRITICAL
  max_review_comments: -1
```

Google’s words: “This field sets the minimum severity for which Gemini Code Assist posts comments… Gemini Code Assist determines the severity of a comment based on the type and significance of the issue under consideration.”

That is the production pattern SafeLane should copy *for findings* and must not copy *for rollout*: the model proposes a severity enum; **product policy** decides whether the comment is shown. Gemini does not document that CRITICAL auto-blocks merge.

### 3.3 Cursor Bugbot

Primary: [Bugbot docs](https://cursor.com/docs/bugbot.md), [Bugbot product page](https://cursor.com/bugbot), [June 2026 update](https://cursor.com/blog/bugbot-updates-june-2026).

Engine shape:

- “Bugbot analyzes PR diffs and leaves comments with explanations and fix suggestions.”
- Grounding is explicit in the analytics API. Dry-run findings include `locations: [{ "file": "src/net.ts", "start_line": 5, "end_line": 9 }]` plus `severity` (`high` / `medium` in the published example).
- The model (or the Bugbot pipeline) **does pick severity**. Posted reviews expose `bugs[].severity`.
- CI check defaults to `neutral` when issues are found. “Requiring the status alone does not block merges on findings because findings default to `neutral`.” Failure-on-unresolved is an org setting, not the model’s decision.
- Effort levels (Default / High / Custom) are a **product** knob for how long the model reasons, analogous to Copilot Lite/Balanced — not a model-chosen rollout profile.
- Marketing page: “Bugbot uses a combination of frontier and in-house models.” Internal routing is not documented; do not invent it.
- Rules live in `.cursor/BUGBOT.md`. That is a prompt-contract file, not a policy engine.

### 3.4 What to steal vs what to refuse

| Pattern | Copilot | Gemini | Bugbot | SafeLane |
|---|---|---|---|---|
| Multi-finding, inline file:line | Yes | Yes | Yes (`file` + line range) | Today: one finding, two spans |
| Model picks comment severity | Not documented | Yes (LOW–CRITICAL), filtered by config | Yes (`high`/`medium`) | **Allow as a finding field; policy maps it** |
| Model picks merge/rollout | No (Comment only) | Not documented as merge | No (check defaults `neutral`) | **Forbidden (ADR 0001)** |
| Agentic extra repo context | Yes, optional | Yes (“retrieve helpful information”) | Diff + rules + PR comments | **Out of scope** (PR attachment only) |
| Failure / miss → conservative | Human still reviews | Human still reviews | Neutral check, not silent skip | **Safety floor** |

---

## 4. Meta RADAR vs DRS — what the LLM is allowed to do

Two different papers, two different jobs for the LLM.

### 4.1 DRS: the LLM as a *calibrated risk ranker* (not a reviewer)

Rui Abreu et al. *Moving Faster and Reducing Risk: Using LLMs in Release Deployment.* arXiv [2410.06351](https://arxiv.org/abs/2410.06351) (Oct 2024).

DRS predicts how likely a diff is to cause a SEV. It drives **four gating levels**: no gating (green), weekend, medium impact (yellow), high impact (red). The logistic-regression production model uses ~a dozen hand-chosen features (churn, diffusion, prior SEVs, author experience, criticality). Evaluation is “capture Y% of SEVs by gating X% of diffs, Y ≫ X” — not a probability the LLM is asked to emit.

When they tried generative LLMs:

- Input to the LLM was **diff title, test plan, and unified diff** — not a severity essay.
- Zero-shot prompting for a numeric score was rejected as uncalibrated.
- What worked was **risk alignment** (fine-tune on past SEV / non-SEV diffs) plus **change-aware pretraining** (iDiffLlama). The 13B change-aware model beat a 34B code-only model.

**Implication for SafeLane:** you do not have SEV labels or a week to fine-tune. Do not ask Azure OpenAI for a DRS-like score. Meta needed historical incidents to make the LLM *ranking* trustworthy. Without that, the trustworthy LLM job is **semantic review**, which is RADAR’s ACR layer, not DRS.

### 4.2 RADAR: the LLM as a *review agent* behind ML + deterministic gates

Chris Adams et al. *Automating Low-Risk Code Review at Meta: RADAR, Risk Calibration, and Review Efficiency.* arXiv [2605.30208](https://arxiv.org/abs/2605.30208) (2026-05-29).

RADAR is a **funnel**, not a single model:

1. Authorship / source classification (human vs bot vs RACER runbook vs deterministic codemod).
2. Eligibility gates (SOX, open-source, blocklists, author tenure, CI state).
3. Static heuristics.
4. **DRS** (ML percentile). Human-authored default: only the safest 5% (P5). Allowlisted RACER runbooks: P50. Non-allowlisted bots: P20.
5. **ACR (LLM Automated Code Review)** / RADAR Review Agent.
6. Deterministic validation; then land or route to humans.

What ACR is allowed to do, in Meta’s words:

- “Reads and interprets the actual code changes… going beyond metadata and static heuristics.”
- Classifies each change against **predefined safe signals** (refactor without behavior change, dead-code removal, logging, formatting, docs, import hygiene, test additions, …) and **risk signals** (review-effort ≥ 4, structural changes, bugs/logic errors, performance, secrets, SQLi, auth bypass, …).
- Auto-accept requires **confidence ≥ 8/10 and all changes in safe categories**. “If any risk signal is detected, the diff is automatically disqualified from auto-acceptance.”
- “The review agent verifies that no business logic requiring human judgment was updated.”
- ACR may *rescue* a large-but-trivial refactor that DRS/static checks would flag, and may *catch* a subtle logic bug that pattern matching misses.

What ACR is **not** allowed to do:

- It does not pick the DRS threshold (org policy / `OrgRADARPolicyConfig`).
- It does not bypass eligibility, blocklists, or SOX.
- A single risk signal vetoes auto-land. The LLM cannot overrule a risk signal to ship faster.
- Deterministic validation still runs after the LLM.

Safety numbers in the paper are observational on the *eligible* cohort (revert rate 1/3 of non-RADAR, PI rate 1/50). They do not prove the LLM caused the safety; the funnel selects easy diffs. Steal the funnel, not the headline ratios.

**Mapping onto SafeLane language:**

| Meta | SafeLane |
|---|---|
| Eligibility + static heuristics | Change-scope band + path mapping + evidence confidence |
| DRS percentile | Not available (no SEV labels). Do not fake it with an LLM score |
| ACR safe/risk signal classification | **AI safety case** finding kinds (closed enums) |
| ACR confidence 8/10 + no risk signal | Verifier `supported` + no unverified claims |
| Org DRS threshold / ACE policy | Versioned **rollout profile** chosen by policy |
| Risk signal → human review | Safety floor → at least `guarded` / `risky` |

ADR alignment: RADAR is the strongest production evidence that “more LLM involvement” still means **the LLM classifies the change; policy decides what happens next.**

---

## 5. Prompt contracts that work for change analysis

### 5.1 Closed category enums vs free-form

Every trustworthy system above uses closed categories at the decision boundary:

- Azure structured outputs exist specifically so the model cannot hallucinate an invalid enum.
- RADAR ACR classifies into a **predefined** safe/risk signal set.
- Gemini severity is `LOW | MEDIUM | HIGH | CRITICAL`.
- SafeLane today: `kind: "breaking_api"` plus a closed safeguard-proposal tuple.

Free-form prose is fine **inside** a finding as an explanation that Studio templates may ignore. It must not be the thing policy parses.

Expand the enum; do not open it. Proposed finding `kind` values (closed; engine maps each to a floor):

| `kind` | Why it is a change-analysis category, not a score |
|---|---|
| `breaking_api` | Existing (removed/renamed HTTP contract) |
| `schema_migration` | Additive/destructive persisted shape |
| `authz_change` | Authn/authz surface |
| `timeout_retry_behavior` | Reliability contract |
| `delete_or_rename_runtime_path` | Route, topic, job name |
| `config_default_change` | Behavioral default in config |
| `test_only` | Tests/docs only (policy may ignore for floors) |
| `unknown_contract` | Model thinks something is endangered but cannot name it — **must raise uncertainty**, never `safe` |

If the model cannot fit a real danger into the enum, `unknown_contract` + verified spans is the conservative outlet. Do not add `"other": string`.

### 5.2 Requiring quotes / spans

Keep SafeLane’s span object. It is already the right shape (and matches Bugbot’s `file` + line range, with the addition of `side` + exact `text` that GitHub review comments do not require):

```json
{
  "file": "apps/demo-api/main.py",
  "side": "removed",
  "line": 14,
  "text": "@app.get(\"/v1/quote\")"
}
```

Rules that the papers and products support:

- Quote must be a substring of the actual diff line (engine already does this).
- Require **at least one span per finding**. Breaking-API can keep the removed+added pair.
- Do not accept line numbers without `text`. ALCE’s POSTCITE failure mode is “correct prose, unfaithful citation.”
- Cap quote length in the **engine** schema (`maxLength`), not in Azure `strict` schema.

### 5.3 Blast radius, affected contracts, rollback, tests missing

These are the extra fields the user wants. Make them **claims with citations**, then let policy combine them with *deterministic* evidence.

| Field | Model returns | Engine trusts only if | Deterministic overlay |
|---|---|---|---|
| `affected_contract_kind` | enum: `http_route`, `db_schema`, `auth`, `queue`, `config`, `unknown` | Verifier supports the claim from cited spans | Path mapping / recognized roles |
| `blast_radius_claim` | enum: `this_service`, `declared_dependents`, `unknown` | Never treated as topology proof | Service graph / mapping completeness already in policy |
| `rollback_claim` | enum: `revert_commit_sufficient`, `data_or_schema_forward_only`, `unknown` | Requires a span if `data_or_schema_forward_only` | File-type heuristics (migrations) can raise the floor even if the model says revert is enough |
| `tests_missing_claim` | enum: `tests_in_diff`, `no_tests_in_diff`, `unknown` | `no_tests_in_diff` accepted only when git says no test paths changed | **Prefer the git check.** The model cannot prove tests elsewhere in the repo from a PR diff |

ALCE and AIS both say: a claim that is not entailed by the cited source is not attributable. “This will take down billing” is not entailed by a one-line route rename unless the diff itself says so. Put blast radius in the schema so Studio can *show* the hypothesis; do not let it move the **rollout profile**.

### 5.4 Multi-finding vs one-finding

Production reviewers are multi-finding (Copilot, Gemini `max_review_comments`, Bugbot `bugs[]`). RADAR classifies *each change* in a diff. SafeLane’s `maxItems: 1` is a Phase-1 demo bound, not a trust property.

Recommendation: allow up to **five** findings on the wire; policy uses the **worst verified kind** (maximum safety floor). Empty `findings` still cannot create **fast-lane eligibility** by itself (`CONTEXT.md`: “An empty AI response alone is insufficient”).

### 5.5 Chain-of-thought vs schema-only

OpenAI’s structured-outputs docs include a **chain-of-thought** example: a `steps[]` array of `{explanation, output}` plus `final_answer`, used for math tutoring — i.e. CoT as *user-visible structured reasoning*, not as a hidden scratchpad.
[OpenAI Structured Outputs, CoT example](https://developers.openai.com/api/docs/guides/structured-outputs)

Azure reasoning-model guidance is the opposite of “ask for step-by-step in the prompt”: “Reasoning models work best when you give them a clear goal, firm constraints, and an explicit output contract. Unlike non-reasoning models, they don't need you to prescribe every intermediate step.”
[Azure reasoning how-to](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/reasoning)

SafeLane ADR 0001: “Every AI danger, hypothesis, question, and remediation shown to a user is derived from verified source references and fixed templates.” So:

- **Do not** put free-form CoT on the Studio verdict path.
- **Do** keep the output schema-only for policy fields.
- Optional: a `scratchpad` string field that is hashed into evidence and never rendered. Useful for gpt-4.1; redundant (and billed twice) on GPT-5/o-series, which already spend hidden reasoning tokens.

Zheng et al. found CoT can reduce some judge mistakes; they also found judges are still biased. For the verifier, prefer a tiny schema `{verdict, unsupported_span_index}` over a reasoning essay.

---

## 6. Azure OpenAI practical constraints for this hackathon

### 6.1 Wiring (do not invent env vars)

From Azure’s own examples, the minimum is:

| Name | Role | Source |
|---|---|---|
| `AZURE_OPENAI_API_KEY` | `api-key` header / `OpenAI(api_key=...)` | [Structured outputs, Python key-based](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs) |
| Endpoint / `base_url` | `https://<resource>.openai.azure.com/openai/v1/` | Same page |
| Deployment name | `model=` argument | Same page: “MODEL_DEPLOYMENT_NAME” |
| Optional Entra | token provider audience `https://ai.azure.com/.default` | Same page |
| Responses + Entra curl | `AZURE_OPENAI_AUTH_TOKEN` Bearer | Same page, Responses REST |

Recommend SafeLane config (not all must be env):

```text
AZURE_OPENAI_ENDPOINT=https://<resource>.openai.azure.com
AZURE_OPENAI_API_KEY=...
AZURE_OPENAI_DEPLOYMENT=safelane-gpt41          # you choose this name
AZURE_OPENAI_VERIFIER_DEPLOYMENT=safelane-gpt41  # or a mini deployment
```

Application config (checked into a non-secret file): `provider=azure_openai`, `api=chat.completions`, `api_version=v1`, `temperature=0` (gpt-4.1 only), `timeout_seconds=…`, `max_completion_tokens=…`.

The v1 surface does not require a dated `api-version` query parameter; Azure’s latest API doc lists `api-version` as optional with default `v1`.
[Azure OpenAI latest API](https://learn.microsoft.com/en-us/azure/foundry/openai/latest)

### 6.2 Temperature 0

For **gpt-4.1**, `temperature: 0` is the documented way to reduce sampling variance (SafeLane already uses this on Ollama). Structured outputs still need it: schema lock prevents invalid keys, not flaky enum choices among legal ones.

For **reasoning models** (o-series and the GPT-5 reasoning line), Azure lists as unsupported: “`temperature`, `top_p`, `presence_penalty`, `frequency_penalty`, `logprobs`, `top_logprobs`, `logit_bias`, `max_tokens`.” Use `max_completion_tokens` (Chat Completions) or `max_output_tokens` (Responses), and `reasoning_effort`.
[Azure reasoning how-to, Not Supported](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/reasoning)

If the hackathon deployment is gpt-4.1, set `temperature: 0`. If someone “upgrades” to o3/gpt-5 and leaves `temperature: 0` in the request, expect an API error, not extra determinism.

GPT-5.1 defaults `reasoning_effort` to `none`. GPT-5.6+ on Chat Completions cannot combine function tools with reasoning unless `reasoning_effort` is `none`; Azure recommends Responses for tool+reasoning.
[Azure reasoning how-to](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/reasoning)

### 6.3 Token limits vs PR diffs

GPT-4.1 series ([models table](https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/models-sold-directly-by-azure)):

- Context: 1,047,576 listed; **300,000 on standard deployments**; 128,000 provisioned/batch.
- Max output: 32,768.
- Known issue: large tool definitions can fail around 300k even when the 1M window is advertised.

Keep SafeLane’s “do not silently truncate” rule from [`research/ollama-phase1.md`](ollama-phase1.md). With a 300k standard window, almost every hackathon PR fits in one finder call. If the compare diff is huge:

1. Count tokens (or bytes as a proxy) before the call.
2. If over budget: **do not trim**. Set `ai_status` to a failure/partial value and apply the existing oversize safety floor.
3. Optional later: chunk by file, merge findings, expose that analysis was chunked (already designed for Ollama).

Output side: five findings of spans + enums is tiny. Set `max_completion_tokens` high enough that `finish_reason: length` cannot silently cut JSON. Azure Chat Completions docs: `finish_reason` may be `stop`, `length`, `content_filter`, `tool_calls`.
[Azure Chat Completions how-to](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/chatgpt)

### 6.4 How to send a GitHub PR diff

SafeLane already does the attested thing. `GitHubCliAdapter.pull_request_diff` calls:

```text
gh api repos/{repo}/compare/{base_sha}...{head_sha}
Accept: application/vnd.github.v3.diff
```

GitHub’s compare-commits API documents the custom media type `application/vnd.github.diff`: “Returns the diff of the commit.” It also warns: “Larger diffs may time out and return a 5xx status code” (on the commits media-type list).
[Compare two commits](https://docs.github.com/en/rest/commits/commits?apiVersion=2022-11-28#compare-two-commits)

The pull-request object itself also supports `Accept: application/vnd.github.diff`.
[Pulls REST API](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28)

List-files fallback (official): `GET /repos/{owner}/{repo}/pulls/{pull_number}/files` — “Responses include a maximum of 3000 files. The paginated response returns 30 files per page by default.” Each file JSON includes a `patch` field when GitHub can produce one.
[List pull request files](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#list-pull-requests-files)

Prompt packaging (from Meta DRS, which is the closest published “how we feed a diff to an LLM”): send **title + test plan + unified diff**. For SafeLane, the analogue is: PR title (optional), canonical unified diff, closed schema, instruction that every span `text` must be copied verbatim from the diff. Do not send HTML. Do not ask the model to call `git`.

### 6.5 Failure modes (map each to a safety floor)

| Failure | How Azure/OpenAI expose it | SafeLane treatment |
|---|---|---|
| Prompt content filter | HTTP **400**; prompt classified at a filtered category | `ai_status=unavailable` (or dedicated `content_filter`); raise floor. Do not retry with a “please ignore safety” prompt. |
| Completion content filter | HTTP 200, `finish_reason: content_filter`, often empty content | Same as unavailable. Azure: “Non-streaming completions calls don't return any content when the content is filtered.” |
| Refusal (structured outputs) | `message.refusal` set; content may not match schema | Treat as unavailable. OpenAI: refusals “do not necessarily follow the schema.” |
| Timeout / network | Client timeout; 5xx | Existing `unavailable` path |
| `finish_reason: length` | Truncated JSON | `partial`; do not json.loads a prefix |
| Schema / missing required / extra keys | Should be rare with `strict: true`; still validate locally | `partial` |
| Span does not exist | Engine check | Drop finding; if all findings drop, `partial` or complete-with-empty + no fast-lane |
| Verifier `not_supported` | Second call | Drop that finding; do not keep an unverified danger *or* an unverified all-clear |
| Verifier call fails | Same as finder failures | Conservative: keep the finding (danger stands) *or* raise uncertainty floor. **Do not** treat verifier failure as “no danger.” RADAR: a risk signal cannot be waived. |

Content filter details: [Azure content filtering](https://learn.microsoft.com/en-us/azure/ai-foundry/foundry-models/concepts/content-filter). Security-looking diffs (injection, exploit tests) can false-positive. That is another reason AI failure must not look like a clean `safe` assessment.

---

## 7. Recommended SafeLane pipeline (more AI, same trust boundary)

```text
GitHub PR (only analysis path)
        │
        ▼
compare(base...head) → canonical unified diff
        │
        ├─ deterministic: files, lines, path mapping, test-path presence
        │                 → change-scope band, evidence confidence
        │
        ▼
Finder (Azure gpt-4.1, Chat Completions, json_schema strict)
        │  findings[0..5], closed kinds, spans, typed claims
        ▼
Span existence check (normal code, unchanged meaning)
        │
        ▼
Verifier (Azure, json_schema; input = claim + cited spans only)
        │  per finding: supported | not_supported | insufficient_evidence
        ▼
Policy (normal code)
        │  worst verified kind + scope band + floors
        │  AI failure / unverified / mapping holes raise floor
        ▼
risk tier → rollout profile   ← model never writes these
```

This is RADAR’s layering (static → optional ML you do not have → LLM review → deterministic validation) with ALCE’s citation check inserted where Meta used an internal ACR confidence score you cannot reproduce.

**ADR check:** compatible with ADR 0001, 0002, and 0004 as long as the model still cannot choose probes, tiers, or profiles. Expanding `findings` from 1 to 5 and adding typed claims is a schema version bump (`ai-response-v3`), not a policy authority change. Flag it as a contract change in the assessment schema, not as a silent override.

---

## 8. Proposed model output schema (beyond one category)

Azure **wire** schema (strict). Engine applies additional min/max locally. All properties required; optionals use `null`.

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["findings", "safeguard_proposal"],
  "properties": {
    "findings": {
      "type": "array",
      "items": { "$ref": "#/$defs/finding" }
    },
    "safeguard_proposal": {
      "anyOf": [
        { "type": "null" },
        { "$ref": "#/$defs/proposal" }
      ]
    }
  },
  "$defs": {
    "span": {
      "type": "object",
      "additionalProperties": false,
      "required": ["file", "side", "line", "text"],
      "properties": {
        "file": { "type": "string" },
        "side": { "type": "string", "enum": ["removed", "added"] },
        "line": { "type": "integer" },
        "text": { "type": "string" }
      }
    },
    "finding": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "kind",
        "spans",
        "affected_contract_kind",
        "blast_radius_claim",
        "rollback_claim",
        "tests_missing_claim",
        "failure_hypothesis_kind"
      ],
      "properties": {
        "kind": {
          "type": "string",
          "enum": [
            "breaking_api",
            "schema_migration",
            "authz_change",
            "timeout_retry_behavior",
            "delete_or_rename_runtime_path",
            "config_default_change",
            "test_only",
            "unknown_contract"
          ]
        },
        "spans": {
          "type": "array",
          "items": { "$ref": "#/$defs/span" }
        },
        "affected_contract_kind": {
          "type": "string",
          "enum": ["http_route", "db_schema", "auth", "queue", "config", "none", "unknown"]
        },
        "blast_radius_claim": {
          "type": "string",
          "enum": ["this_service", "declared_dependents", "unknown"]
        },
        "rollback_claim": {
          "type": "string",
          "enum": ["revert_commit_sufficient", "data_or_schema_forward_only", "unknown"]
        },
        "tests_missing_claim": {
          "type": "string",
          "enum": ["tests_in_diff", "no_tests_in_diff", "unknown"]
        },
        "failure_hypothesis_kind": {
          "type": "string",
          "enum": [
            "removed_http_route_unavailable",
            "incompatible_persisted_schema",
            "authz_bypass_or_lockout",
            "tighter_timeout_or_retry",
            "unknown"
          ]
        }
      }
    },
    "proposal": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "finding_index",
        "verification_intent_kind",
        "approval_question_kind",
        "remediation_kind"
      ],
      "properties": {
        "finding_index": { "type": "integer" },
        "verification_intent_kind": {
          "type": "string",
          "enum": [
            "preserve_removed_http_route",
            "preserve_persisted_schema_compat",
            "preserve_authz_behavior",
            "none"
          ]
        },
        "approval_question_kind": {
          "type": "string",
          "enum": [
            "confirm_callers_migrated",
            "confirm_forward_fix_exists",
            "confirm_authz_matrix",
            "none"
          ]
        },
        "remediation_kind": {
          "type": "string",
          "enum": [
            "retain_removed_route_as_alias",
            "expand_then_contract_schema",
            "none"
          ]
        }
      }
    }
  }
}
```

Verifier wire schema:

```json
{
  "type": "object",
  "additionalProperties": false,
  "required": ["results"],
  "properties": {
    "results": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["finding_index", "entailment"],
        "properties": {
          "finding_index": { "type": "integer" },
          "entailment": {
            "type": "string",
            "enum": ["supported", "not_supported", "insufficient_evidence"]
          }
        }
      }
    }
  }
}
```

Fields **intentionally absent** from the model schema: `risk_tier`, `rollout_profile`, `safe`/`guarded`/`risky`, probe id, URL, command, image, `confidence: 0-10`, numeric blast-radius, “approve merge.”

Gemini/Bugbot-style `severity` is optional. If added, name it `comment_severity` (`low|medium|high|critical`) and document that it only affects Studio ordering, **not** the **risk tier**. RADAR’s lesson: a “low severity” LLM label must not waive a breaking-API floor.

---

## 9. What the model MUST NOT be allowed to decide

From ADR 0001, still binding after “more AI”:

- **Risk tier** (`safe` / `guarded` / `risky`)
- **Rollout profile** (stage weights, pauses, analysis jobs)
- Approval / **rollout decision**
- Probe id, URL, host, image, command, credentials
- Any executable test

From RADAR / Copilot / Bugbot, additionally:

- DRS-style numeric “how likely is a SEV?”
- Waiving a verified danger because the model also said “rollback is easy”
- Declaring **fast-lane eligibility** from empty findings
- Fetching extra repository files via tools (out of the declared PR-only analysis path)

Policy remains the only function that may lower care, and it can do so only from **positive proof** (glossary: **fast-lane eligibility**).

---

## 10. How the second verification pass should work

1. After span existence succeeds, build one verifier user message per finding (or a batched array with the same order):
   - Decontextualized claim, e.g. “The change removes HTTP route GET `/v1/quote` and adds GET `/v2/quote`.”
   - The exact cited spans, labeled `[1]`, `[2]`.
   - Instruction: answer whether the citations **fully support** that claim. Do not use uncited knowledge. If the quotes are real but do not support the *kind*, return `not_supported`.
2. `temperature: 0` on gpt-4.1; structured outputs as above.
3. Engine:
   - `supported` → finding enters `ai_findings` with `source_reference_verified: true` and `citation_entailment: supported`.
   - `not_supported` / `insufficient_evidence` → finding discarded (or kept only as unverified evidence that still raises an uncertainty floor — pick one and test it; RADAR would discard auto-accept, SafeLane should **not** treat it as a clean miss).
   - Verifier transport failure → do **not** interpret as “no issues.” Raise `ai_status` and the uncertainty floor. If a span-verified danger already exists, keep it (RADAR: risk signal cannot be waived).
4. Do not ask the verifier for a better category. Recategorization is a new finder call; otherwise the verifier becomes a second undocumented policy.

This is ALCE citation recall, not MT-Bench judging.

---

## 11. Honest limits: what an LLM still cannot prove

1. **Existence ≠ interpretation.** Source-reference verification will never become AIS by itself. ALCE: even strong models fail complete citation support about half the time on ELI5.
2. **Schema lock ≠ truth.** Structured outputs prevent illegal enums; they do not prevent a legal wrong enum.
3. **Blast radius** is a topology fact. Unless SafeLane already mapped dependents, the model is guessing. Incomplete mapping is already a **safety floor** (`all_paths_recognized`); do not let an LLM override that with `this_service`.
4. **Rollback difficulty** for data/schema needs migration semantics and production data. A quoted `ALTER TABLE` is a hint, not a proof that revert is impossible — and the reverse (model says revert is enough) must not override a migration-path heuristic.
5. **Missing tests** cannot be proven from a PR diff that simply does not include `tests/`. Absence in the diff is not absence in the repo. Use git path inspection; treat the model claim as corroboration only.
6. **Runtime failure** (the **failure hypothesis**) is a prediction. ADR 0004 already routes it through a **trusted probe**, not through model-authored tests. The LLM cannot certify that production will break.
7. **Incident likelihood** is DRS’s job and needs labeled SEVs. SafeLane does not have that dataset. An LLM “risk score” would be the thing Meta measured as uncalibrated.
8. **Empty review ≠ safe.** Copilot: not guaranteed to spot all problems. RADAR: auto-accept only after *positive* safe-signal classification plus DRS plus gates. SafeLane glossary already forbids “no risks found.”
9. **Content filters** can hide a dangerous diff behind `finish_reason: content_filter`. That must look like analysis failure, not a green lane.
10. **Determinism is approximate.** `temperature: 0` plus a pinned gpt-4.1 version is the best Azure offers for this task; it is not a bit-identical guarantee across regions or silent platform changes. `NoAutoUpgrade` is what stops Azure from swapping the model under the demo.

---

## Sources

### Azure / OpenAI (primary)

- [How to use structured outputs with Azure OpenAI](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/structured-outputs) (ms.date 2026-08-06)
- [OpenAI Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
- [Azure OpenAI working with models / versionUpgradeOption](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/working-with-models)
- [Models sold directly by Azure (GPT-4.1 context / output limits)](https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/models-sold-directly-by-azure)
- [Azure reasoning models (unsupported sampling params, Chat Completions vs Responses)](https://learn.microsoft.com/en-us/azure/ai-foundry/openai/how-to/reasoning)
- [Azure Chat Completions (`finish_reason`)](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/chatgpt)
- [Azure content filtering](https://learn.microsoft.com/en-us/azure/ai-foundry/foundry-models/concepts/content-filter)
- [Azure OpenAI latest API (v1)](https://learn.microsoft.com/en-us/azure/foundry/openai/latest)

### Papers (primary)

- Gao et al., ALCE, EMNLP 2023: [ACL](https://aclanthology.org/2023.emnlp-main.398/) · [arXiv 2305.14627](https://arxiv.org/abs/2305.14627) · [code](https://github.com/princeton-nlp/ALCE/)
- Rashkin et al., AIS, CL 2023: [ACL](https://aclanthology.org/2023.cl-4.2/) · [arXiv 2112.12870](https://arxiv.org/abs/2112.12870)
- Zheng et al., LLM-as-a-Judge, [arXiv 2306.05685](https://arxiv.org/abs/2306.05685)
- Abreu et al., Meta DRS, [arXiv 2410.06351](https://arxiv.org/abs/2410.06351)
- Adams et al., Meta RADAR, [arXiv 2605.30208](https://arxiv.org/abs/2605.30208)

### Products (first-party)

- [GitHub Copilot code review (concepts)](https://docs.github.com/en/copilot/concepts/agents/code-review)
- [GitHub Copilot code review (how-to)](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/use-code-review)
- [Gemini Code Assist on GitHub](https://developers.google.com/gemini-code-assist/docs/review-github-code)
- [Gemini Code Assist config.yaml (severity threshold)](https://developers.google.com/gemini-code-assist/docs/customize-gemini-behavior-github)
- [Cursor Bugbot](https://cursor.com/docs/bugbot.md)

### GitHub API (primary)

- [Compare two commits](https://docs.github.com/en/rest/commits/commits?apiVersion=2022-11-28#compare-two-commits)
- [Pulls REST API / diff media type](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28)
- [List pull request files](https://docs.github.com/en/rest/pulls/pulls?apiVersion=2022-11-28#list-pull-requests-files)

### SafeLane (this repo)

- `CONTEXT.md` glossary
- [ADR 0001](../docs/adr/0001-bound-ai-to-risk-findings.md), [ADR 0002](../docs/adr/0002-separate-assessment-from-rollout-decision.md), [ADR 0004](../docs/adr/0004-resolve-ai-intents-through-trusted-probes.md)
- `schemas/ai-response-v2.schema.json`, `src/safelane/risk_finder.py`, `src/safelane/engine.py`, `src/safelane/pr_studio.py`
