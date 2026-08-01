# SafeLane

SafeLane examines a software change and chooses how carefully it should be released.

## Language

**Change evidence**:
Facts SafeLane can point to in the code change, repository history, service map, or incident records.
_Avoid_: AI opinion

**AI risk finding**:
A warning found by AI while reading a change, backed by the exact file or code that caused it. An AI risk finding does not choose the rollout by itself.
_Avoid_: AI score, AI verdict

**Failure propensity**:
A rough low, medium, or high estimate of how likely a change is to fail. It is not an exact probability.
_Avoid_: Failure probability, precise risk score

**Safety floor**:
A rule that prevents SafeLane from choosing a faster rollout when there is serious impact, difficult recovery, poor support coverage, or missing evidence.
_Avoid_: Risk points, score bonus

**Confidence**:
Whether SafeLane received and understood all evidence required by the policy. It does not mean that an AI prediction is probably correct.
_Avoid_: Model confidence, probability

**Risk tier**:
The final `safe`, `guarded`, or `risky` policy result after failure propensity and every safety floor are considered.
_Avoid_: AI verdict

**Rollout lane**:
The release behavior selected from the risk tier, such as a fast release or a slower guarded release.
_Avoid_: Risk score
