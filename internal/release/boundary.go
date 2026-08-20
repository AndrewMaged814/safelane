package release

import (
	"fmt"
	"time"
)

const subjectAccessReviewMethod = "SubjectAccessReview"

// Boundary is the live Kubernetes identity and capability evidence recorded
// immediately before rollout start.
//
// A direct RBAC bypass attempt is deliberately absent. SafeLane has no admission
// webhook and no audit stream, so it cannot observe one. Recording such an entry
// would record a human claim, while an absent entry would falsely imply nobody tried.
// Boundary therefore proves capability, not an unobservable anecdote.
type Boundary struct {
	ControllerIdentity string           `json:"controller_identity"`
	CallerIdentity     string           `json:"caller_identity"`
	CallerCapability   CallerCapability `json:"caller_capability"`
}

// CallerCapability is the timestamped authorization result asserted under the
// restricted caller identity.
type CallerCapability struct {
	AssertedAt    time.Time `json:"asserted_at"`
	Method        string    `json:"method"`
	GetRollouts   bool      `json:"get_rollouts"`
	PatchRollouts bool      `json:"patch_rollouts"`
}

func (b Boundary) IsZero() bool { return b == (Boundary{}) }

// Validate enforces the phase-one boundary: the caller can observe Rollouts but
// cannot mutate them, and the assertion came from Kubernetes authorization.
func (b Boundary) Validate() error {
	var errs Errors
	if b.ControllerIdentity == "" {
		errs = append(errs, Internal("missing_controller_identity", "boundary must name the controller identity"))
	}
	if b.CallerIdentity == "" {
		errs = append(errs, Internal("missing_caller_identity", "boundary must name the restricted caller identity"))
	}
	if b.CallerCapability.AssertedAt.IsZero() {
		errs = append(errs, Internal("missing_capability_timestamp", "caller capability must record when it was asserted"))
	}
	if b.CallerCapability.Method != subjectAccessReviewMethod {
		errs = append(errs, Internal("invalid_capability_method",
			fmt.Sprintf("caller capability method must be %q", subjectAccessReviewMethod)))
	}
	if !b.CallerCapability.GetRollouts {
		errs = append(errs, Internal("caller_cannot_get_rollouts", "restricted caller must be able to get rollouts"))
	}
	if b.CallerCapability.PatchRollouts {
		errs = append(errs, Internal("caller_can_patch_rollouts", "restricted caller must not be able to patch rollouts"))
	}
	return errs.OrNil()
}

// EnvelopeRecord is Appendix C2's persisted envelope proof.
type EnvelopeRecord struct {
	Lane           string `json:"lane"`
	Weights        []int  `json:"weights"`
	Gates          int    `json:"gates"`
	Source         string `json:"source"`
	TemplateDigest string `json:"template_digest"`
}

func NewEnvelopeRecord(lane string, weights []int, digest string) EnvelopeRecord {
	return EnvelopeRecord{
		Lane: lane, Weights: append([]int{}, weights...), Gates: len(weights) - 1,
		Source: "rendered_rollout", TemplateDigest: digest,
	}
}

func outcomeFrom(execution []ExecutionEntry) string {
	if len(execution) == 0 {
		return "pending"
	}
	last := execution[len(execution)-1]
	if last.Outcome == OutcomeAborted || last.Verb == VerbAbort || last.Verb == VerbArgoAbort {
		return "aborted"
	}
	if last.Outcome == OutcomeFailed {
		return "failed"
	}
	if last.Verb == VerbPause {
		return "paused"
	}
	return "in_progress"
}
