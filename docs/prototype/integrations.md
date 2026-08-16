# SafeLane guidance and integration bootstrap

## Commands

```text
safelane init
safelane integrations sync
```

`safelane init` bootstraps SafeLane project configuration and optional caller adapters. `safelane integrations sync` regenerates guidance and adapter files after the Release Policy or SafeLane tool contract changes.

## Canonical SafeLane-owned files

The project source of truth is:

```text
.safelane/project.yml
.safelane/policy.yml
.safelane/agent-guidance.md
```

Adapters for Codex, Claude Code, MCP, and CI refer back to these files rather than becoming independent policy copies. The live demo needs one working adapter; the interface remains caller-neutral.

## Existing agent instruction files

SafeLane must never overwrite an entire existing `AGENTS.md` or `CLAUDE.md`. It may add or update only a clearly marked managed section, for example:

```markdown
<!-- BEGIN SAFELANE MANAGED: guidance -->
See `.safelane/agent-guidance.md` for the release workflow. Identify the merged pull request and run `safelane release --pr <number>`. Do not author evidence claims. Do not call Kubernetes or Argo directly for the protected application.
<!-- END SAFELANE MANAGED: guidance -->
```

If the file is missing, malformed, duplicated, or otherwise ambiguous, leave it untouched and create a separate SafeLane integration file. The command must report what it changed.

## Generated guidance

Guidance teaches agents to:

- recognize when a change is ready for release;
- identify the merged pull request and never author evidence claims or Kubernetes configuration, which SafeLane renders from the operator-owned Release Template;
- call `safelane release --pr <number>`;
- interpret typed results and actionable rejections; and
- avoid direct Kubernetes or Argo mutations in the protected release path.

Guidance is a discovery and behavior adapter. It is not authorization.

## Optional MCP surface

When MCP is configured, expose typed tools such as:

- `request_release`;
- `get_status`;
- `abort_release`; and
- `get_proof`.

These tools call the same SafeLane API as the CLI. Authorization remains inside SafeLane; an MCP description or model decision cannot grant production authority.

## Prototype checks

Test two properties independently:

1. **Invocation reliability:** across representative natural-language release requests, does the selected agent discover and call SafeLane?
2. **Bypass resistance:** when guidance is ignored or a caller attempts direct Kubernetes deployment, does the action fail without changing the protected application?

The first tests the guidance/tool-shaping experience and belongs to this document. The second tests the enforcement layer, must not depend on the first passing, and is **not owned here** — it is a standalone issue in Ahmed's infrastructure workstream, deliberately separated so enforcement evidence is never gated behind guidance prototyping.

That issue covers the restricted caller identity, the before/after protected-state capture, the required `Forbidden` on direct `apply` and image patch, and the separation between the restricted caller and SafeLane's narrowly-permitted controller identity. A no-kubeconfig caller is an additional defense demonstration, not the enforcement proof.
