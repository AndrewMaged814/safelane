# Phase 1 rollout profiles

**Decision date:** 2026-08-01
**Decision owner:** Andrew

SafeLane maps each risk tier to a configurable rollout profile. Profiles live in `policy.yaml`; application code contains no rollout constants. Every `decision.json` records the policy version, selected profile, and fully resolved settings used for that release.

## Built-in demo profiles

The Phase 1 demo service has 5 replicas and no traffic router. Pod counts are therefore the honest primary unit. SafeLane converts them to Argo weights and records both.

| Risk tier | Profile | Studio color | Exposure and health checkpoints |
|---|---|---|---|
| `safe` | `fast` | Green | `all` immediately; Kubernetes readiness only |
| `guarded` | `guarded` | Amber | 2 pods → health checkpoint → `all` |
| `risky` | `strict` | Red | 1 pod → checkpoint → 2 pods → checkpoint → 3 pods → checkpoint → `all` |

Profile names describe rollout behavior, not the danger of the change. Custom or AI-assisted profiles use purple in SafeLane Studio. Every color is accompanied by a name and icon so color is not the only signal.

## Demo health defaults

The guarded and strict built-ins share the service's normal health limit. Risk changes exposure and observation, not the definition of a broken service.

- maximum error rate: 5%;
- checkpoint duration: about 30 seconds;
- reading interval: 10 seconds;
- readings per checkpoint: 3;
- second unhealthy or empty reading during a checkpoint: rollback;
- first Prometheus connection or query error: retry;
- second consecutive connection or query error: rollback.

The 5% value is a configurable demo default, not a universal recommendation. The planned healthy fixture is near 0% errors and the broken fixture is around 35%, leaving a reliable visible gap.

Argo implements the unhealthy-reading rule with `failureLimit: 1`: it permits one failed measurement and fails on the second. It does not require those two unhealthy readings to be consecutive. Argo implements provider/query errors separately with `consecutiveErrorLimit: 2`. See Argo's [analysis behavior](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/) and [failure-versus-error explanation](https://argo-rollouts.readthedocs.io/en/latest/FAQ/#what-is-the-difference-between-failures-and-errors).

An empty Prometheus result is explicitly treated as a failed health reading, never as zero errors or success. Argo documents this result handling in its [empty-array examples](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/#empty-array).

## Replica count and resolved weights

Each service configuration is the single source of truth for replica count. For the five-replica demo, Studio offers pod choices 1 through 5 and SafeLane resolves them as follows:

| Profile value | Argo weight | Honest exposure |
|---|---:|---:|
| 1 pod | 20 | 1 of 5 pods |
| 2 pods | 40 | 2 of 5 pods |
| 3 pods | 60 | 3 of 5 pods |
| 4 pods | 80 | 4 of 5 pods |
| `all` | 100 | 5 of 5 pods |

The last profile step is always `all`, not a fixed number. If a service's replica count changes, SafeLane recalculates weights and rejects any earlier step that now exceeds the total.

## Custom profiles

A user creates a custom profile by cloning `fast`, `guarded`, or `strict`, then changing pod stages, checkpoint duration, reading interval, or the error-rate limit.

A custom profile must remain at least as careful as its base:

- let the base and candidate integer stages be the values before `all`; the candidate must have at least as many integer stages, and for every base-stage index its value must be less than or equal to the base value at that index;
- extra candidate stages are allowed only after those compared positions and remain below the service replica count;
- when the base has a checkpoint, candidate `checkpoint.seconds` is greater than or equal to the base, `interval_seconds` is less than or equal to the base, `failure_limit` and `consecutive_error_limit` are less than or equal to the base, and `max_error_rate` is less than or equal to both the base and service limits;
- `measurement_count` must equal `floor(checkpoint.seconds / interval_seconds)`, so longer or more frequent observation cannot be hidden by a lower sample count;
- a candidate with any integer stage must keep health analysis; and
- all integer stages must be positive, strictly increasing, below the service replica count, and end with `all`.

For example, a Strict-derived `[1, 3, 4, all]` profile is invalid because its second stage exposes more pods than Strict's second stage `[1, 2, 3, all]`. `[1, 2, 3, 4, all]` is valid because it preserves every Strict stage and adds another checkpoint before full exposure. A Fast-derived `[1, all]` profile is also valid when it supplies a valid checkpoint.

The exact persisted `base` and `source` fields are defined in [`docs/input-contracts.md`](input-contracts.md). `source` is `custom` for a manually approved profile and `ai_assisted` for an approved generated draft; this value is copied into `decision.json.profile_source`.

SafeLane automatically selects the minimum profile required by the risk tier. A developer may make a one-way profile override to something more careful, such as guarded to strict. A faster override is invalid.

Saving any profile change requires whole-policy validation, a preview of the YAML change, and human approval. An approved save updates `policy.yaml` and creates a new policy version. Phase 1 runs locally and relies on Git for review and history; it has no accounts or permission system.

## SafeLane Studio requirement

Phase 1 includes one small local SafeLane Studio. Its approved information architecture and interaction contract are defined in [`docs/safelane-studio.md`](safelane-studio.md). It must:

- show the risk tier, plain reasons, AI findings, and exact evidence;
- show the selected profile as pods and health checkpoints;
- offer the color-coded built-in profiles;
- create and edit custom profiles;
- validate and preview policy changes before saving; and
- include the required one-shot **Generate with AI** action.

Argo's existing dashboard remains responsible for live rollout controls. SafeLane Studio does not add deployment controls, accounts, a database, drag-and-drop editing, or historical analytics.

## Generate with AI

The user describes the rollout they want. Ollama reads only the description, current profiles, service replica count, service health settings, fixed validation rules, and a few built-in valid examples.

Ollama may draft:

- a profile name and description;
- pod stages;
- checkpoint duration;
- health-reading interval; and
- a health limit equal to or stricter than the service default.

It cannot change service replicas, risk-tier mappings, the service's normal health limit, validation rules, or the approval requirement. The result is one structured draft, not a chat or multiple alternatives. Normal code validates it, Studio shows a visual and YAML preview, and a person must approve it before saving. Invalid AI output changes nothing.

## Canonical policy shape

The exact `policy.yaml` structure, including service topology, supported shipping windows, thresholds, profiles, and validation rules, is frozen in [`docs/input-contracts.md`](input-contracts.md). Implementation must not refine that parser shape independently.

The lane generator resolves this configuration into exact Argo steps. No raw AI output is ever rendered or applied.
