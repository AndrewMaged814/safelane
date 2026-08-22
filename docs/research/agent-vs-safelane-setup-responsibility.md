# Agent versus SafeLane setup responsibility

**Question:** Should an agent or SafeLane be responsible for configuring SafeLane?

**Research date:** 2026-08-22

## Verdict

If the choice must be stated as one owner, **SafeLane should own setup**. Claude or Codex should be an optional repository analyst inside that setup, not the author or executor of SafeLane configuration.

That does not mean a deterministic setup must pretend to understand application semantics. The useful split is:

- **The agent discovers meaning:** critical user-facing surfaces, risky code paths, and what behavior should be asserted, each tied to repository evidence.
- **SafeLane produces the configuration:** it discovers mechanical facts, supplies the schema and defaults, validates findings, compiles policy and infrastructure, previews the exact result, applies the approved result, and verifies it.
- **The user supplies intent and approval:** the user resolves genuinely ambiguous choices and approves one frozen setup plan.

The current “SafeLane baseline plus agent amendment” model exposes an implementation detail and makes the agent repeat data it does not own. The better product model is:

```text
repository facts + agent findings -> SafeLane setup plan -> one approval -> apply
```

SafeLane may use internal defaults while compiling the plan, but neither the user nor the agent should have to copy, edit, or understand a baseline document.

## What comparable systems establish

### Terraform: intent is input; the product compiles and applies

Terraform separates authored intent from executable change. `terraform plan` reads current state, compares it with configuration, and produces the change actions; a saved plan can later be applied so automation performs the exact reviewed changes ([Terraform plan reference](https://developer.hashicorp.com/terraform/cli/commands/plan), [plan and apply workflow](https://developer.hashicorp.com/terraform/tutorials/cli/plan)).

The author does not implement provider validation. Providers publish schemas describing field shape and constraints, and Terraform invokes those schemas during validate, plan, and apply ([provider schemas](https://developer.hashicorp.com/terraform/plugin/framework/handling-data/schemas)). Terraform performs syntax and schema validation itself and providers add domain-specific validators ([provider validation](https://developer.hashicorp.com/terraform/plugin/framework/validation)).

**SafeLane implication:** an agent can supply declarative intent, but SafeLane must own the canonical schema, domain validation, compiled setup plan, plan hash, and exact apply. Giving the agent Kubernetes templates or compiled policy is analogous to asking a Terraform author to construct an internal plan file.

### Kubernetes and Argo: controllers own reconciliation

Kubernetes resources express desired state in `spec`; controllers continuously observe actual state and move it toward that desired state ([Kubernetes controller pattern](https://kubernetes.io/docs/concepts/architecture/controller/)). This assigns operational correctness to deterministic software rather than to the actor that first described the desired outcome.

Argo Rollouts follows that model. A user updates the Rollout specification, then the Argo controller executes the configured rollout steps and exposes observed status ([Argo Rollouts getting started](https://github.com/argoproj/argo-rollouts/blob/master/docs/getting-started.md)). Argo describes itself as the Kubernetes controller responsible for canary mechanics, analysis, promotion, and rollback ([Argo Rollouts README](https://github.com/argoproj/argo-rollouts)).

**SafeLane implication:** setup may collect intent from an agent, but reconciliation with the actual repository, cluster, and Argo objects belongs to SafeLane and Argo. An agent should not manufacture selectors, ports, manifests, or rollout-control details from incomplete repository clues.

### Renovate: small user config over product defaults, with product validation

Renovate onboards repositories with a small recommended preset rather than requiring users to reproduce its complete effective configuration. It can detect repository characteristics and add needed overrides automatically ([Renovate installation and onboarding](https://docs.renovatebot.com/getting-started/installing-onboarding/), [configuration presets](https://docs.renovatebot.com/config-presets/)). Renovate then resolves defaults, global configuration, inherited configuration, presets, and repository configuration into an internal final configuration ([configuration overview](https://docs.renovatebot.com/config-overview/)).

Renovate explicitly says its JSON Schema is useful for early editor or agent feedback, but its own validator is more robust because some checks require product code ([Renovate JSON Schema](https://docs.renovatebot.com/json-schema/), [config validator](https://docs.renovatebot.com/config-validation/)).

**SafeLane implication:** the agent should provide a small semantic input, while SafeLane resolves the complete effective configuration. Schema-conforming agent output is not sufficient evidence that a setup is safe or applicable.

### Backstage: bounded parameters feed owned actions

Backstage Software Templates ask for typed input variables, show a review page, and then pass those variables into a predefined sequence of scaffolder actions ([Software Templates overview](https://backstage.io/docs/features/software-templates/), [writing templates](https://backstage.io/docs/features/software-templates/writing-templates/)). Backstage also places permissions around parameters, steps, actions, and tasks rather than treating template input as authority to execute arbitrary behavior ([scaffolder authorization](https://backstage.io/docs/features/software-templates/authorizing-scaffolder-template-details/)).

**SafeLane implication:** agent findings should be bounded parameters to SafeLane-owned compilation and actions. The agent should not define the action graph, credentials, permissions, or mutation target.

### Model-native structured output: shape is not truth

OpenAI Structured Outputs can constrain model output to a developer-supplied JSON Schema, but OpenAI explicitly notes that schema adherence does not prevent mistakes inside field values ([Structured Outputs](https://openai.com/index/introducing-structured-outputs-in-the-api/)).

**SafeLane implication:** a narrow schema improves the interface, but deterministic cross-checks still need to prove that cited files exist, surfaces are real, assertions are executable, templates match the target, and the resulting policy obeys SafeLane invariants.

## Concrete responsibility boundary

| Concern | Owner | Reason |
| --- | --- | --- |
| Repository remote, CI names, image workflow, Kubernetes objects, live selectors and ports | SafeLane | Discoverable facts must be gathered consistently and refreshed before apply. |
| Critical surfaces, failure modes, risky paths, desired behavioral assertions | Agent | These require semantic reading of application code and operational intent. |
| Evidence citations for semantic findings | Agent | Every judgment should be reviewable and mechanically checkable. |
| Default policy, lanes, authority ceilings, fallback behavior | SafeLane | These are the product's safety promise and cannot vary with model phrasing. |
| Kubernetes and Argo templates, RBAC, probe wiring | SafeLane | These require authoritative target knowledge and deterministic compatibility checks. |
| Schema, merge rules, normalization, validation, plan hash | SafeLane | The product must turn suggestions into one reproducible result. |
| Setup approval | User | Approval expresses intent; it should apply one frozen plan rather than a regenerated result. |
| File writes and operator configuration | SafeLane | Mutation must be atomic, auditable, and idempotent. |
| Post-apply reconciliation and `doctor` | SafeLane | The product must verify actual output, not trust the agent's description. |

## Recommended SafeLane flow

1. `safelane setup inspect --json` discovers facts once, persists a fingerprinted inspection, and returns a bounded semantic task—not a complete editable proposal.
2. Claude or Codex reads only the relevant repository files and submits findings such as `risk_paths`, `critical_surfaces`, and `assertion_intents`, with file/line evidence. It cannot submit policy YAML, manifests, lanes, credentials, or target wiring.
3. SafeLane verifies the evidence and compiles a complete immutable setup plan from the inspection, accepted findings, product rules, and target capabilities.
4. SafeLane presents the effective result and material choices. The user approves that exact plan once.
5. `setup apply` applies the frozen plan by ID/hash. It does not ask the agent to serialize or replay it.
6. `doctor` reconciles stored configuration with repository and live target state.

For a non-agent setup, SafeLane follows the same compiler path with conservative product-owned findings. There should not be two configuration formats or two apply implementations.

The agent-facing payload should remain small and semantic, for example:

```json
{
  "inspection_fingerprint": "sha256:...",
  "risk_paths": [
    {
      "glob": "src/Payments/**",
      "minimum_risk": "high",
      "reason": "Changes affect payout execution",
      "evidence": [{ "file": "src/Payments/Executor.cs", "line": 41 }]
    }
  ],
  "assertion_intents": [
    {
      "surface": "POST /api/payouts/quote",
      "covers": "correctness",
      "evidence": [{ "file": "src/Api/QuoteController.cs", "line": 18 }]
    }
  ]
}
```

SafeLane may reject, normalize, or supplement these findings. It must never silently weaken them.

## Rejected ownership models

### Agent owns all setup

This produces the most visibly “agentic” demo but undermines SafeLane's product claim. Output varies with model and context; infrastructure facts can be guessed; validation becomes reactive; and a model can accidentally author the safeguards meant to constrain it. The previous selector/port mismatch is the concrete failure mode.

### SafeLane owns all semantic decisions

This is reproducible but makes the agent unnecessary and forces generic heuristics to infer application meaning they cannot reliably know. It would recreate the weak deterministic setup that motivated agent assistance.

### Baseline plus amendment

This is internally reasonable but externally leaky. It makes the agent echo unchanged defaults, introduces merge semantics users must understand, and encourages direct editing of a representation that SafeLane should compile. Keep defaults internal; expose findings and the resulting plan.

## Bottom line

SafeLane is the setup authority. The agent is a replaceable semantic sensor.

The product should work if Claude is replaced by Codex, another model, or no model. The semantic quality may change, but the configuration schema, safety invariants, infrastructure compatibility, approval artifact, mutation behavior, and proof must not.
