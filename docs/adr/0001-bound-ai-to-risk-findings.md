# Bound AI to evidence-backed safety cases, not rollout authority

SafeLane will use AI to read a code change and propose one bounded safety case: a typed danger,
exact source references, failure-hypothesis kind, verification intent, and advisory remediation kind.
Normal code verifies the references, renders all prose, resolves only an allowlisted trusted probe,
and applies fixed policy rules to choose the final risk tier and rollout profile. This makes AI central
to understanding the change without allowing model output to become deployment authority.

## Considered options

- No AI: easier to test, but too weak for the hackathon theme.
- AI-generated risk score: visually impressive, but difficult to trust, test, or explain.
- Bounded AI safety case plus trusted-probe resolution and fixed rules: chosen because the AI
  understands the change while execution and rollout behavior stay predictable.

## Consequences

- The AI response contains closed enums, exact source-span candidates, and one bounded finding index,
  never free-form executable values.
- Every AI danger, hypothesis, question, and remediation shown to a user is derived from verified
  source references and fixed templates.
- The model cannot return a probe ID, URL, host, image, command, credential, tier, profile, approval,
  or rollout setting.
- A rejected safeguard proposal cannot erase an independently verified dangerous finding or lower
  the required rollout care.
- The engine uses duplicate-key rejection plus shallow-envelope, finding-component, and
  proposal-component validation; it does not validate the response as one all-or-nothing object.
- SafeLane must still work safely when the AI call fails, times out, or returns invalid output.
- The response format must not depend on one Ollama model, so the model can be replaced without changing the safety policy.
- The Phase 1 model must be downloaded and warmed up on the demo laptop before the presentation.
