// Package policy evaluates Release Eligibility from verified GitHub and GHCR
// evidence against the operator-owned Release Policy.
//
// Phase one does not treat evidence as risk and does not choose a rollout
// envelope from it. Eligible releases receive the policy's static envelope;
// ineligible and indeterminate releases receive none.
// See docs/adr/0002-eligibility-not-risk.md.
package policy

import (
	"github.com/AndrewMaged814/safelane/internal/release"
)

// Policy is the operator-owned phase-one Release Policy: which evidence is
// mandatory, whether independent PR approval is required, and the static
// rollout envelope attached to an eligible release.
type Policy struct {
	Version                       string
	IndependentPRApprovalRequired bool
	Stages                        []int
	NextAction                    string
}

// Default is the compiled phase-one Release Policy. It matches
// docs/policy/safelane-policy.yml: independent PR approval is required, and
// eligible releases receive the static 5 → 25 → 50 → 100 envelope.
func Default() Policy {
	return Policy{
		Version:                       "1",
		IndependentPRApprovalRequired: true,
		Stages:                        []int{5, 25, 50, 100},
		NextAction:                    "start",
	}
}

// Evaluate maps an evidence outcome onto Release Eligibility. It does not
// produce a risk tier and does not pick stages from evidence completeness.
func Evaluate(p Policy, evidence release.EvidenceResult) (release.Eligibility, error) {
	switch evidence.Outcome() {
	case release.EvidenceVerified:
		return eligible(p)
	case release.EvidenceUnknown:
		return fromReasons(p, evidence, release.Indeterminate)
	default:
		return fromReasons(p, evidence, release.Ineligible)
	}
}

func eligible(p Policy) (release.Eligibility, error) {
	env, err := release.NewRolloutEnvelope(p.Stages, p.NextAction)
	if err != nil {
		return release.Eligibility{}, err
	}
	return release.Eligible(p.Version, "all_mandatory_evidence_verified",
		"All configured mandatory evidence verified. The release may enter SafeLane on the operator's static rollout envelope.",
		env)
}

func fromReasons(p Policy, evidence release.EvidenceResult, build func(string, string, string) (release.Eligibility, error)) (release.Eligibility, error) {
	code, message := "requirement_failed", "A mandatory evidence requirement failed. The release may not enter SafeLane."
	if evidence.Outcome() == release.EvidenceUnknown {
		code, message = "verification_incomplete", "Verification could not be completed. The release may not enter SafeLane until GitHub and GHCR can be reached."
	}
	if reasons := evidence.Reasons(); len(reasons) > 0 {
		code = reasons[0].Code
		message = reasons[0].Message
		if reasons[0].Remedy != "" {
			message = message + " " + reasons[0].Remedy
		}
	}
	return build(p.Version, code, message)
}
