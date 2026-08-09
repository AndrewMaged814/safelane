# Separate change assessment from rollout decision

SafeLane will keep the full evidence, AI safety case, policy reasoning, and review state in a
SHA-bound change assessment, then emit a separate rollout decision only after that assessment
resolves automatically or receives human approval. Keeping one file would be simpler, but it would
couple the deployment consumer to Studio and Ollama details that do not affect rendering. The split
preserves `decision.json` as the stable cross-workstream handoff while allowing assessment evidence
to evolve independently.

## Consequences

- SafeLane Studio reads the change assessment; the release workstream reads only the rollout decision.
- Both artifacts identify the same repository, pull request, full base and head SHAs, policy version,
  assessment-input hash, and assessment-result hash.
- A new push invalidates the earlier decision, and an unresolved guarded or risky assessment cannot emit a normal decision.
- AI-only fields remain in the assessment. The decision contains only normal-code-resolved profile
  stages and trusted analysis identity.
- A missing, invalid, stale, identity-mismatched, or unapproved decision rejects release. A local
  Strict preview is diagnostic only and cannot substitute for authorization.
