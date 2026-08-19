# Eligibility is not risk

**Superseded by [0003-risk-selects-the-lane](0003-risk-selects-the-lane.md).** Risk-based
envelopes are no longer a future extension — they are built. This ADR's claim below
still holds and is restated there: evidence completeness is not risk.

Phase one treats GitHub and GHCR checks as Release Eligibility, not as a risk policy. Evidence does not produce low, medium, or high risk and does not select a rollout envelope. Eligible releases receive the operator's static envelope; ineligible and indeterminate releases receive none. Risk-based envelopes remain a future Release Policy extension.
