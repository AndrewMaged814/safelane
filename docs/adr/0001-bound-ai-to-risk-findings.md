# Bound AI to risk findings, not rollout authority

SafeLane will use AI to read a code change and return structured risk findings with exact evidence, while fixed, versioned safety rules choose the final risk tier and rollout lane. Phase 1 will run the AI locally through Ollama. This gives the AI + DevOps hackathon a meaningful AI contribution without relying on an unexplained AI-generated score; AI can make the rollout more careful, never less careful, and missing or uncertain AI output falls back to the guarded lane.

## Considered options

- No AI: easier to test, but too weak for the hackathon theme.
- AI-generated risk score: visually impressive, but difficult to trust, test, or explain.
- Bounded AI findings plus fixed rules: chosen because the AI understands the change while the safety decision stays predictable.

## Consequences

- The AI response needs a small structured format containing findings, evidence, and confidence.
- Every AI finding shown to a user must point to the code that caused it.
- SafeLane must still work safely when the AI call fails, times out, or returns invalid output.
- The response format must not depend on one Ollama model, so the model can be replaced without changing the safety policy.
- The Phase 1 model must be downloaded and warmed up on the demo laptop before the presentation.
