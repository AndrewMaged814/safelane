# SafeLane

SafeLane examines one exact software change and chooses how carefully that revision may be released.

## Language

**Change evidence**:
Facts SafeLane can point to in the submitted code change and its declared release context.
_Avoid_: AI opinion, intuition

**Change assessment**:
The evidence-backed evaluation of one exact change under one policy version. It may recommend release
behavior, but it is not permission to deploy.
_Avoid_: Deployment decision, release status

**AI safety case**:
An AI-proposed, source-backed account of an endangered contract, its possible impact, and an intended
safeguard. It does not choose or execute rollout behavior.
_Avoid_: AI verdict, AI risk score, generated deployment

**Failure hypothesis**:
The bounded behavior that an AI safety case predicts could fail because of the cited change.
_Avoid_: Root cause, verified prediction

**Verification intent**:
A closed, non-executable description of what contract should be preserved during rollout.
_Avoid_: Generated test, probe command

**Source reference verified**:
Confirmation that cited code text exists at the claimed location and side of the exact change. It
does not confirm that the AI interpretation is correct.
_Avoid_: AI reasoning verified, risk verified

**Trusted probe**:
A pre-approved runtime check that SafeLane may bind to a validated verification intent. Its execution
contract is not authored by AI.
_Avoid_: AI-generated test, dynamic probe

**Approval question**:
A source-grounded prompt that calls attention to the unresolved assumption a human should consider
before authorizing a careful rollout. Answering it is not part of the release contract.
_Avoid_: Approval gate answer, questionnaire

**Bounded remediation**:
A source-grounded, advisory description of how the identified contract might be preserved. It never
changes code and never guarantees a faster rollout.
_Avoid_: Generated patch, automatic fix

**Change-scope band**:
A coarse policy baseline derived from the supported change's size and mapping completeness. It is not
a probability.
_Avoid_: Failure propensity, risk score

**Safety floor**:
A policy rule that prevents SafeLane from choosing a faster rollout when danger or uncertainty is
present.
_Avoid_: Risk points, score bonus

**Evidence confidence**:
Whether SafeLane received and validated all evidence required by the bounded policy. It does not
measure whether an AI prediction is probably correct.
_Avoid_: Model confidence, probability

**Risk tier**:
The final `safe`, `guarded`, or `risky` policy result after the change-scope band and every safety
floor are applied.
_Avoid_: AI verdict

**Fast-lane eligibility**:
Positive proof that every bounded Fast precondition passed and no accepted danger exists. An empty AI
response alone is insufficient.
_Avoid_: No risks found, AI says safe

**Assessment status**:
Whether a change assessment still requires rollout authorization or has been resolved. A new change
revision invalidates an earlier resolution.
_Avoid_: Deployment status, rollout status

**Rollout decision**:
The automatic or human-approved release behavior for one exact change assessment. Recording it does
not deploy anything.
_Avoid_: Assessment, deployment result

**Rollout profile**:
A named, versioned release pattern containing exposure stages and trusted analysis steps. A risk tier
sets its minimum required care.
_Avoid_: AI-generated rollout, deployment preset

**Verification receipt**:
A post-run evidence record that binds the predicted contract check, exact release identities, trusted
probe outcome, and rollout result.
_Avoid_: AI explanation, deployment decision
