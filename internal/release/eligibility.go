package release

import (
	"encoding/json"
	"fmt"
)

// EligibilityStatus is whether one exact Release Request may enter SafeLane.
//
// The zero value is [EligibilityIndeterminate]: an unset field withholds
// authority and is never eligible. Evidence completeness is not risk and does
// not choose a rollout envelope; see docs/adr/0002-eligibility-not-risk.md.
type EligibilityStatus uint8

const (
	// EligibilityIndeterminate means verification could not be completed
	// because GitHub, GHCR, or another dependency was unavailable or returned
	// unusable data. It withholds authority. It is the zero value.
	EligibilityIndeterminate EligibilityStatus = iota
	// EligibilityEligible means every configured mandatory evidence check
	// verified. Only this status may carry the operator's static envelope.
	EligibilityEligible
	// EligibilityIneligible means verification completed but a requirement
	// failed. It withholds authority and carries no envelope.
	EligibilityIneligible
)

func (s EligibilityStatus) String() string {
	switch s {
	case EligibilityEligible:
		return "eligible"
	case EligibilityIneligible:
		return "ineligible"
	default:
		return "indeterminate"
	}
}

func (s EligibilityStatus) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

func (s *EligibilityStatus) UnmarshalJSON(data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch v {
	case "eligible":
		*s = EligibilityEligible
	case "ineligible":
		*s = EligibilityIneligible
	default:
		*s = EligibilityIndeterminate
	}
	return nil
}

// RolloutEnvelope is the operator-defined static canary ladder. It is not
// selected from evidence; it is attached only to an eligible release.
type RolloutEnvelope struct {
	stages     []int
	nextAction string
}

// NewRolloutEnvelope copies the operator's static stages and next action.
func NewRolloutEnvelope(stages []int, nextAction string) (RolloutEnvelope, error) {
	if len(stages) == 0 {
		return RolloutEnvelope{}, Internal("empty_rollout_envelope",
			"an eligible release needs the operator's static rollout stages")
	}
	if nextAction == "" {
		return RolloutEnvelope{}, Internal("missing_next_action",
			"an eligible release needs the operator's next action")
	}
	copied := make([]int, len(stages))
	copy(copied, stages)
	return RolloutEnvelope{stages: copied, nextAction: nextAction}, nil
}

// Stages returns a copy of the operator's static canary weights.
func (e RolloutEnvelope) Stages() []int {
	if len(e.stages) == 0 {
		return nil
	}
	out := make([]int, len(e.stages))
	copy(out, e.stages)
	return out
}

// NextAction is the first permitted transition, for example "start".
func (e RolloutEnvelope) NextAction() string { return e.nextAction }

// IsZero reports whether this envelope was never constructed.
func (e RolloutEnvelope) IsZero() bool { return len(e.stages) == 0 || e.nextAction == "" }

type rolloutEnvelopeJSON struct {
	Stages     []int  `json:"stages"`
	NextAction string `json:"next_action"`
}

func (e RolloutEnvelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(rolloutEnvelopeJSON{Stages: e.stages, NextAction: e.nextAction})
}

func (e *RolloutEnvelope) UnmarshalJSON(data []byte) error {
	var w rolloutEnvelopeJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	built, err := NewRolloutEnvelope(w.Stages, w.NextAction)
	if err != nil {
		return err
	}
	*e = built
	return nil
}

// Eligibility is the persisted determination of whether one exact Release
// Request may enter SafeLane. It is not a risk assessment.
type Eligibility struct {
	set           bool
	status        EligibilityStatus
	policyVersion string
	reasonCode    string
	message       string
	retryable     bool
	envelope      *RolloutEnvelope
}

// Eligible records that every configured mandatory check verified. The
// operator's static envelope is attached; retryable is false.
func Eligible(policyVersion, reasonCode, message string, envelope RolloutEnvelope) (Eligibility, error) {
	if envelope.IsZero() {
		return Eligibility{}, Internal("eligible_without_envelope",
			"an eligible release must carry the operator's static rollout envelope")
	}
	if err := requireEligibilityFields(policyVersion, reasonCode, message); err != nil {
		return Eligibility{}, err
	}
	env := envelope
	return Eligibility{
		set:           true,
		status:        EligibilityEligible,
		policyVersion: policyVersion,
		reasonCode:    reasonCode,
		message:       message,
		retryable:     false,
		envelope:      &env,
	}, nil
}

// Ineligible records that verification completed but a requirement failed.
// No envelope is attached; retryable is false.
func Ineligible(policyVersion, reasonCode, message string) (Eligibility, error) {
	if err := requireEligibilityFields(policyVersion, reasonCode, message); err != nil {
		return Eligibility{}, err
	}
	return Eligibility{
		set:           true,
		status:        EligibilityIneligible,
		policyVersion: policyVersion,
		reasonCode:    reasonCode,
		message:       message,
		retryable:     false,
	}, nil
}

// Indeterminate records that verification could not be completed. No
// envelope is attached; retryable is true so the caller can try again.
func Indeterminate(policyVersion, reasonCode, message string) (Eligibility, error) {
	if err := requireEligibilityFields(policyVersion, reasonCode, message); err != nil {
		return Eligibility{}, err
	}
	return Eligibility{
		set:           true,
		status:        EligibilityIndeterminate,
		policyVersion: policyVersion,
		reasonCode:    reasonCode,
		message:       message,
		retryable:     true,
	}, nil
}

func requireEligibilityFields(policyVersion, reasonCode, message string) error {
	if policyVersion == "" {
		return Internal("missing_policy_version", "eligibility must record the Release Policy version")
	}
	if reasonCode == "" {
		return Internal("missing_eligibility_reason", "eligibility must record a reason code")
	}
	if message == "" {
		return Internal("missing_eligibility_message", "eligibility must record an actionable message")
	}
	return nil
}

// Status is eligible, ineligible, or indeterminate.
func (e Eligibility) Status() EligibilityStatus {
	if !e.set {
		return EligibilityIndeterminate
	}
	return e.status
}

// PolicyVersion is the operator policy this determination was made against.
func (e Eligibility) PolicyVersion() string { return e.policyVersion }

// ReasonCode is a stable machine identifier for why this status was recorded.
func (e Eligibility) ReasonCode() string { return e.reasonCode }

// Message is the actionable explanation for a caller or agent.
func (e Eligibility) Message() string { return e.message }

// Retryable is true only for indeterminate: verification may succeed later.
func (e Eligibility) Retryable() bool { return e.set && e.retryable }

// Envelope is present only when the release is eligible.
func (e Eligibility) Envelope() (RolloutEnvelope, bool) {
	if e.status != EligibilityEligible || e.envelope == nil {
		return RolloutEnvelope{}, false
	}
	return *e.envelope, true
}

// IsZero reports whether this value was never constructed.
func (e Eligibility) IsZero() bool { return !e.set }

func (e Eligibility) String() string {
	if !e.set {
		return "indeterminate"
	}
	return fmt.Sprintf("%s (%s)", e.status, e.reasonCode)
}

type eligibilityJSON struct {
	Status        EligibilityStatus `json:"status"`
	PolicyVersion string            `json:"policy_version"`
	ReasonCode    string            `json:"reason_code"`
	Message       string            `json:"message"`
	Retryable     bool              `json:"retryable"`
	Envelope      *RolloutEnvelope  `json:"rollout_envelope,omitempty"`
}

func (e Eligibility) MarshalJSON() ([]byte, error) {
	w := eligibilityJSON{
		Status:        e.Status(),
		PolicyVersion: e.policyVersion,
		ReasonCode:    e.reasonCode,
		Message:       e.message,
		Retryable:     e.Retryable(),
		Envelope:      e.envelope,
	}
	return json.Marshal(w)
}

func (e *Eligibility) UnmarshalJSON(data []byte) error {
	var w eligibilityJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return Malformed("malformed_eligibility", "eligibility",
			"stored eligibility could not be decoded",
			"The Release record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	var (
		built Eligibility
		err   error
	)
	switch w.Status {
	case EligibilityEligible:
		if w.Envelope == nil {
			return Internal("eligible_without_envelope",
				"an eligible record must carry the operator's static rollout envelope")
		}
		built, err = Eligible(w.PolicyVersion, w.ReasonCode, w.Message, *w.Envelope)
	case EligibilityIneligible:
		built, err = Ineligible(w.PolicyVersion, w.ReasonCode, w.Message)
	default:
		built, err = Indeterminate(w.PolicyVersion, w.ReasonCode, w.Message)
	}
	if err != nil {
		return err
	}
	*e = built
	return nil
}
