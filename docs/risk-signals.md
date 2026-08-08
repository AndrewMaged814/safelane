# Phase 1 risk signals

**Decision date:** 2026-08-01
**Decision owner:** Andrew

SafeLane Phase 1 uses a small set of facts plus four bounded AI finding types. AI finds exact dangers in the change. Fixed policy rules choose the final risk tier. A rule may only keep or raise the tier; no reassuring signal can cancel a danger.

## Failure propensity band

Normal code assigns one coarse failure-propensity band before applying safety floors:

- **Low** only when the change is small, exactly one known service is directly changed, and every changed path is recognized and mapped.
- **High** when the change is large or directly changes at least three services.
- **Medium** in every other case, including two changed services or a small change with an unknown path.

The bands project to the schema-v2 display values `20`, `50`, and `80`. These are fixed labels, not probabilities or continuous scores. They establish the baseline `safe`, `guarded`, or `risky` tier respectively; fast-lane eligibility and every safety floor are then applied, so the final tier can only stay the same or become more careful.

Incident candidates and connections, downstream impact, criticality, shipping support, evidence completeness, and AI findings do not inflate failure propensity. They remain separate facts because they constrain acceptable rollout behavior for different reasons.

## Fast lane requires positive proof

A change is `safe` and eligible for the fast lane only when all of these are true:

- no more than 2 files and 50 changed lines;
- exactly 1 known service is directly changed;
- that service is not critical and has no downstream dependents;
- every changed file is recognized;
- incident history was checked successfully and no connection was found;
- shipping is inside a supported shipping window;
- Ollama returned a valid response with verifiable evidence and found no danger.

The absence of an AI warning is not enough. Missing or uncertain evidence prevents the fast lane.

## Normal-code signals

### Change size

- **Small:** at most 2 files and at most 50 changed lines. It can contribute to low propensity but may be `safe` only if every other fast-lane check passes.
- **Medium:** larger than small but not large. It produces at least medium propensity.
- **Large:** at least 10 files or at least 500 changed lines. It produces high propensity.

Small never means safe by itself. These are configurable demo-policy defaults, not scientifically learned thresholds.

### Directly changed services

- 1 service adds no restriction.
- 2 services is at least `guarded`.
- 3 or more services is `risky`.
- Any changed file that cannot be mapped to a service is at least `guarded`.

### Downstream impact

- No downstream dependents adds no restriction.
- 1 or 2 downstream dependents is at least `guarded`.
- 3 or more downstream dependents is `risky`.
- A directly changed critical service is always `risky`.

### Shipping support

- Shipping inside a configured supported window adds no restriction.
- Late-night, weekend, or otherwise unsupported shipping is at least `guarded`.
- Timing alone never makes a change `risky`.

Supported windows belong in `policy.yaml`; they are not hardcoded.

### Evidence availability

- A successful incident search with no incidents adds no restriction.
- Unavailable or missing incident data is at least `guarded`.
- An Ollama timeout, failure, invalid response, or unverifiable quote is at least `guarded`.
- Normal rules still apply when AI fails. A large change or critical service therefore remains `risky`.
- If SafeLane cannot produce a valid decision file at all, the existing contract fails closed as `risky` with low confidence.

## AI finding types

Every finding must name its type and quote exact changed code. SafeLane verifies that the quoted added line exists in the diff before using the finding.

### Database or stored-data danger — `risky`

This finding applies when changed lines:

- add or alter a database migration or schema;
- delete, overwrite, or transform stored data; or
- change how stored data is encoded or interpreted.

A read-only query change is not automatically a stored-data danger. There is no separate vague rollback score: persisted-data evidence is the Phase 1 proof that a change may be hard to undo.

### Login, permission, or secret danger — `risky`

This finding applies when changed lines alter:

- who can sign in or access something;
- token, session, or permission checks; or
- how secrets are loaded, stored, or validated.

Documentation and clearly marked test fixtures do not count. A real secret committed in code also belongs to the separate security gate, which should block deployment.

### Breaking API danger — `risky`

This finding applies when changed lines:

- remove or rename an existing endpoint or field;
- change an existing field's type;
- make an optional field required; or
- change an existing endpoint's required permission or response meaning.

Adding a new endpoint or optional field is not breaking by itself.

### Retry, timeout, or backoff change — usually `guarded`

Changing retry counts, delays, or timeouts is at least `guarded`.

It becomes `risky` when it removes a retry limit, allows endless retries, disables backoff, or is combined with any of these facts:

- a critical service is changed;
- the changed service has at least 3 downstream dependents; or
- a connected incident shows the same retry danger.

## Incident history

Normal code selects at most 5 recent incident candidates in total from the affected services, all within the last 180 days. The limit and lookback are configurable defaults. A shared service makes an incident a candidate only; it does not change the tier.

Ollama checks each candidate against the new change and quotes exact evidence from both records. SafeLane verifies that both quotes exist.

- A meaningful connection to the same component or behavior is at least `guarded`.
- Repeating the incident's trigger or root cause is `risky`.
- Vague similarity or a shared service alone has no effect.

## Explicitly excluded from Phase 1

- author experience or past-mistake scoring;
- test results as a risk signal—failed CI stops the deployment before SafeLane;
- vulnerability or security-scanner ingestion;
- a generic config-versus-code weight;
- a vague rollback or reversibility score;
- fuzzy searches across every incident;
- a catch-all AI finding such as `other`;
- trained risk models or precise-looking continuous scores.

These exclusions keep SafeLane focused on choosing how carefully an already-approved change should roll out.
