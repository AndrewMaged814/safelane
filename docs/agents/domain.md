# Domain Docs

How engineering skills should consume this repository’s domain documentation.

## Before exploring, read these

- `CONTEXT.md` at the repository root.
- `CONTEXT-MAP.md` instead, if it exists, following it to relevant context files.
- Relevant ADRs under `docs/adr/`.

If these files do not exist, proceed silently. Domain-modeling skills create them lazily when terminology or decisions are resolved.

## File structure

This repository uses a single-context layout:

```text
/
├── CONTEXT.md
├── docs/adr/
└── src/
```

## Use the glossary’s vocabulary

Use terms as defined in `CONTEXT.md` in issues, proposals, hypotheses, and tests. Avoid synonyms that the glossary explicitly rejects.

If a required concept is absent, reconsider whether it belongs to the project or note the gap for domain modeling.

## Flag ADR conflicts

Explicitly identify any recommendation that contradicts an existing ADR rather than silently overriding the decision.
