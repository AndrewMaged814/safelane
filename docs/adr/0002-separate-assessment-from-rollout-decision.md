# Separate change assessment from rollout decision

SafeLane will keep the full evidence, AI risk findings, incident connections, policy reasoning, and review state in a SHA-bound change assessment, then emit a separate rollout decision only after that assessment resolves automatically or receives human approval. Keeping one file would be simpler, but it would couple Ahmed's deployment consumer to Studio and Ollama details that do not affect rendering; the split preserves `decision.json` as the stable cross-workstream handoff while allowing assessment evidence to evolve independently.

## Consequences

- SafeLane Studio reads the change assessment; the release workstream reads only the rollout decision.
- Both artifacts identify the same repository, pull request, full head SHA, and policy version.
- A new push invalidates the earlier decision, and an unresolved guarded or risky assessment cannot emit a normal decision.
- The release consumer still fails closed to its local Strict profile when a decision is missing or invalid, without treating that fallback as human approval.
