# Gate B — local Ollama repeatability

This directory contains the three frozen canonical diffs and their expected normalized results. The
gate runs each case twice, in `fast-copy`, `additive-route`, `quote-contract-break` order. Each
observation is one inference call: there is no truncation, chunking, retry, or best-of selection.
The generate request explicitly sets `truncate: false`, so a prompt that cannot fit the pinned
8,192-token context fails safer instead of silently dropping part of the diff.

With Ollama 0.32.1 running and the pinned model installed, warm it once outside the counted gate:

```powershell
ollama run qwen2.5-coder:7b "Return only the word ready."
```

Then record the six counted observations:

```powershell
uv run safelane evaluate-ollama --output demo/evaluation/ollama-observations.json
```

The command exits zero only for `6/6 fixture observations`. The report records the full model
manifest digest, prompt and response-schema hashes, model settings, wall latency, raw Ollama
envelope, raw model response, parsed normalized result, trusted-probe resolution, and pass/fail
reason for every call. This small authored gate is a repeatability check on this laptop, not an
accuracy estimate.
