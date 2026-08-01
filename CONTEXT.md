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

**Fast-lane eligibility**:
Positive proof that a change is small, fully understood, and has no verified danger. The absence of an AI risk finding alone is not enough.
_Avoid_: No risks found, AI says safe

**Incident candidate**:
A recent incident from an affected service that may be compared with a new change. Being from the same service does not make it connected and does not change the risk tier by itself.
_Avoid_: Incident match, related incident

**Incident connection**:
A verified relationship between a new change and a past incident, backed by exact evidence from both. A shared service alone is not a connection.
_Avoid_: Similar incident, AI memory, same-service match

**Supported shipping window**:
A time when the team has explicitly planned enough support coverage to watch and respond to a release.
_Avoid_: Business hours, safe time
