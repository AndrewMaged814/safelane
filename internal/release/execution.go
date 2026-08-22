package release

import (
	"fmt"
	"time"
)

// ExecutionVerb is the action one execution entry records.
type ExecutionVerb string

const (
	// VerbStart is the first apply-and-wait-for-a-gate pass (#55).
	VerbStart ExecutionVerb = "start"
	// VerbAdvance is a later promotion past a reached gate (#56).
	VerbAdvance ExecutionVerb = "advance"
	// VerbArgoAbort records Argo Rollouts aborting the rollout on its own
	// -- a failed AnalysisRun, most often -- which SafeLane observes but
	// never causes (#57).
	VerbArgoAbort ExecutionVerb = "argo_abort"
	// VerbPause is a caller's own `rollout pause` -- narrowing a release
	// by stopping it exactly where it is. Never refused (ticket 11).
	VerbPause ExecutionVerb = "pause"
	// VerbResume records the explicit exit from an emergency pause.
	VerbResume ExecutionVerb = "resume"
	// VerbAcceptRisk records a hazard-specific human decision without
	// pretending the hazard became covered by runtime analysis.
	VerbAcceptRisk ExecutionVerb = "accept_risk"
	// VerbAbort is a caller's own `rollout abort --reason`, distinct from
	// [VerbArgoAbort]: this one SafeLane performed at the caller's own
	// request, not one Argo Rollouts decided on its own. Never refused
	// (ticket 11).
	VerbAbort ExecutionVerb = "abort"
)

// ExecutionOutcome is what came of one execution verb.
type ExecutionOutcome string

const (
	OutcomeGranted ExecutionOutcome = "granted"
	OutcomeRefused ExecutionOutcome = "refused"
	OutcomeAborted ExecutionOutcome = "aborted"
	OutcomeFailed  ExecutionOutcome = "failed"
)

// ExecutionEntry is one row of Appendix C2's execution[] array: one verb
// SafeLane or Argo Rollouts performed against a started rollout, and what
// came of it. Every transition a release goes through after eligibility
// is recorded this way, in order, so proof can read back exactly what
// happened without re-deriving it from the cluster.
type ExecutionEntry struct {
	At              time.Time        `json:"at"`
	Verb            ExecutionVerb    `json:"verb"`
	RequestedWeight int              `json:"requested_weight,omitempty"`
	Outcome         ExecutionOutcome `json:"outcome"`
	// ReasonCode is set for a refusal or an abort -- an Appendix C4 code
	// naming why. Empty for a plain grant.
	ReasonCode string `json:"reason_code,omitempty"`
	// Analysis names the AnalysisRun this entry observed, when one ran.
	Analysis string `json:"analysis,omitempty"`
	// Detail is the measured evidence behind a refusal or an abort, for
	// example the metric value that failed its condition.
	Detail   string `json:"detail,omitempty"`
	HazardID string `json:"hazard_id,omitempty"`
}

// Validate reports whether the entry is well formed. It does not check the
// entry against a release's envelope -- that is the caller's job, since
// only the caller knows which weights this release's lane declared.
func (e ExecutionEntry) Validate() error {
	var errs Errors
	if e.At.IsZero() {
		errs = append(errs, Internal("missing_execution_timestamp",
			"an execution entry must record when it happened"))
	}
	switch e.Verb {
	case VerbStart, VerbAdvance, VerbArgoAbort, VerbPause, VerbResume, VerbAcceptRisk, VerbAbort:
	default:
		errs = append(errs, Internal("invalid_execution_verb",
			fmt.Sprintf("%q is not a recognised execution verb", e.Verb)))
	}
	switch e.Outcome {
	case OutcomeGranted, OutcomeRefused, OutcomeAborted, OutcomeFailed:
	default:
		errs = append(errs, Internal("invalid_execution_outcome",
			fmt.Sprintf("%q is not a recognised execution outcome", e.Outcome)))
	}
	if e.Outcome == OutcomeRefused && e.ReasonCode == "" {
		errs = append(errs, Internal("refusal_without_reason_code", "a refused transition must record its reason code"))
	}
	if e.Outcome == OutcomeFailed && e.ReasonCode == "" {
		errs = append(errs, Internal("failed_execution_without_reason_code", "a failed transition must record its reason code"))
	}
	if e.Verb == VerbArgoAbort && e.ReasonCode == "" {
		errs = append(errs, Internal("argo_abort_without_reason_code", "an Argo abort must record its reason code"))
	}
	return errs.OrNil()
}
