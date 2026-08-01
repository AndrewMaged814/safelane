# Phase 1 Ollama model and context decision

**Decision date:** 2026-08-01
**Scope:** local analysis of bounded git diffs; AI returns evidence-backed findings, while deterministic SafeLane policy chooses the rollout.

## Decision

Use **`qwen2.5-coder:7b`** as the Phase 1 default. The model installed and tested on Andrew's PC is Ollama artifact `dae161e27b0e`: 7.6B parameters using the Q4_K_M format.

Start it with:

- `num_ctx: 8192`
- at most **6,000 diff tokens** inside a prompt of at most **7,000 input tokens**
- `num_predict: 768`
- `temperature: 0`
- `seed: 42` for repeatable demo runs
- `stream: false`
- the response JSON Schema supplied through Ollama's `format` field

This is a practical 8K operating window, not the model's maximum. Ollama lists the 7B Q4_K_M artifact as a 4.7 GB download with a 32K model limit, and says larger context allocations use more memory. On Andrew's RTX 5060 with 8 GB VRAM and 15.7 GB system RAM, the model used 5.0 GB, stayed `100% GPU` at an 8,192-token context, and returned the warm test result in about 2.5 seconds. If another demo laptop spills to CPU or becomes unstable, reduce the context to 4,096 and use smaller chunks before changing models. [Ollama 7B artifact](https://ollama.com/library/qwen2.5-coder%3A7b-instruct-q4_K_M) [Ollama context guidance](https://docs.ollama.com/context-length)

The official Qwen card describes the model as instruction-tuned and code-focused. Its normal configuration is 32,768 tokens; Qwen's longer 131K mode requires separate YaRN configuration and is irrelevant to SafeLane's bounded-diff use case. Nothing in the model card proves deployment-risk accuracy, so the model still needs to pass SafeLane's own small fixture suite before the demo. [Official Qwen 7B model card](https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct) [Official model configuration](https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct/blob/main/config.json)

## Why the installed 3B model is not the default

The installed `qwen2.5-coder:3b` is the instruction-tuned Q4_K_M build: 3.09B parameters, 1.9 GB, and a 32,768-token model limit. At `num_ctx: 8192`, Ollama loaded it as 2.4 GB and `100% GPU` on this PC. It is therefore an excellent hardware fit. [Ollama 3B artifact](https://ollama.com/library/qwen2.5-coder%3A3b) [Official Qwen 3B model card](https://huggingface.co/Qwen/Qwen2.5-Coder-3B-Instruct)

It is not accurate enough for SafeLane's central demo, however. In local focused checks it missed a clearly defined retry increase twice. In a separate schema-constrained trial it returned valid JSON but mislabeled the retry change, cited the wrong line, and missed an obvious database migration. This shows the important distinction: constrained JSON can make the shape reliable without making the analysis correct.

Keep **`qwen2.5-coder:3b-instruct-q4_K_M`** only as an explicit low-resource fallback. Findings from the fallback may make a rollout more conservative, but a run using it must report low confidence and must never permit the `safe` lane.

## Structured response

Ollama accepts either `json` or a full JSON Schema in the `format` field of `/api/chat`. Its official guidance also recommends putting the schema in the prompt, using temperature zero, and validating the response in application code. SafeLane should use the full schema and reject unknown fields. [Ollama structured outputs](https://docs.ollama.com/capabilities/structured-outputs) [Ollama chat API](https://docs.ollama.com/api/chat)

Keep the response small:

```json
{
  "confidence": "high",
  "findings": [
    {
      "kind": "database_migration",
      "file": "db/migrations/042_users.sql",
      "added_line": "ALTER TABLE users ... NOT NULL",
      "reason": "Adds a required persisted field without a default."
    }
  ]
}
```

Use a closed enum for the few Phase 1 hazard kinds, require `file` and an exact added line for every finding, cap findings at five, and disallow severity or a rollout recommendation in the AI response. Application code must verify that the file and line really exist in the supplied diff. It may remove one optional leading `+` before comparing the text because the tested model returned both forms. Invalid evidence makes the whole AI result low-confidence; it is not silently repaired.

## Oversized diffs

Do not silently truncate a diff.

1. Split at file boundaries, then hunk boundaries if needed, preserving file names and new-file line numbers.
2. Keep each request within the 6,000-token diff budget and analyze at most three chunks in Phase 1.
3. Validate every chunk response, merge and deduplicate findings, and expose that the analysis was chunked.
4. If the diff exceeds three chunks, or any chunk times out, is invalid, or lacks verifiable evidence, set confidence to low and let the fixed policy apply at least the guarded lane.

This keeps the AI bounded while preventing a partial analysis from producing an optimistic decision.

## Licensing

The 7B model is licensed under Apache-2.0. SafeLane's source can remain MIT because the model is a separately downloaded runtime dependency and the repository does not redistribute its weights. If weights are ever bundled or redistributed, preserve the model's Apache license and notices. [Official 7B license](https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct/blob/main/LICENSE)

The 3B model is different: its official Qwen Research License permits non-commercial research or evaluation and requires a separate license for commercial use. Local use for this non-commercial hackathon fits that stated grant, but SafeLane must not imply that its MIT license relicenses the weights or makes the 3B model commercially usable. Redistribution adds license and attribution obligations. [Official 3B license](https://huggingface.co/Qwen/Qwen2.5-Coder-3B-Instruct/blob/main/LICENSE)

This is a project compatibility reading, not legal advice.

## Local acceptance result

The 7B model passed the focused SafeLane fixture twice with `temperature: 0`, `seed: 42`, and an 8,192-token context. Both runs found the database migration and retry increase, returned valid structured data, and quoted evidence that matched real added lines after allowing an optional leading `+`. Warm runs took about 2.5 seconds and the model stayed fully on the GPU.

This proves that the chosen setup is suitable for the two tested risk types on this laptop. It does not prove that the model will find every kind of risky change. Phase 1 still needs a small golden test set covering every allowed finding kind. Model failure, timeout, invalid evidence, or incomplete analysis must continue to fall back to the guarded lane.
