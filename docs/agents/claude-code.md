# Claude Code production permissions

SafeLane asks for approval with a fully expanded release ID and production target.
Claude Code auto mode may still classify production deployment commands as soft-denied.

If an operator wants those exact SafeLane transitions to reach Claude's normal approval
prompt, they may add these narrow project-local allow patterns themselves:

```text
Bash(safelane rollout start rel_*)
Bash(safelane rollout advance rel_*)
```

These rules permit only SafeLane's start and single-gate advance entry points. They do
not authorize direct `kubectl`, other rollout verbs, wildcard shell commands, or a
release that SafeLane reports as terminal or unknown. SafeLane never edits Claude
settings automatically.
