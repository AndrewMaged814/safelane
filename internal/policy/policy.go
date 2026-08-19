// Package policy evaluates Release Eligibility from verified GitHub and GHCR
// evidence against the operator-owned Release Policy, and declares the
// several rollout lanes an eligible release may be assigned to.
//
// Eligibility and lane selection are two separate questions with two
// separate owners: evidence decides whether a release may enter SafeLane
// at all; a lane, resolved from risk, decides how far it may ship per
// step. This package only declares what lanes exist and how risk maps to
// them -- resolving an actual risk from a real change is
// internal/assess's job. See docs/adr/0003-risk-selects-the-lane.md.
package policy

import (
	"fmt"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// Lane is one operator-declared rollout envelope: an ordered list of
// traffic weights. Weights[:len(Weights)-1] become explicit canary
// steps; the final weight is reached automatically once the rollout
// runs out of steps. N weights make N-1 gates, never N.
type Lane struct {
	Weights []int
}

// Policy is the operator-owned Release Policy: which evidence is
// mandatory, whether independent PR approval is required, and the
// several lanes an eligible release may be assigned to.
//
// No assessor may invent weights, and no caller may name a lane: Lanes
// is the entire declared configuration surface, and RiskToLane is the
// only mapping from a risk level to one of them.
type Policy struct {
	Version                       string
	IndependentPRApprovalRequired bool

	// Lanes are every rollout envelope the operator has declared, keyed
	// by name (e.g. "fast", "standard", "guarded").
	Lanes map[string]Lane
	// RiskToLane maps a risk level ("low", "medium", "high") to a
	// declared lane name.
	RiskToLane map[string]string
	// DefaultLane is used when no risk is available at all -- a missing,
	// malformed, or failed assessment. Always the operator's most
	// cautious configured lane, never the widest, and this is not
	// itself an error: risk decides width, not entry.
	DefaultLane string
}

// LaneFor resolves a risk level to the lane name and weights an eligible
// release should render and enforce. An empty or unrecognised risk
// resolves to DefaultLane -- "no assessment available" is an expected,
// legitimate case, not a defect, and it never picks the widest lane.
//
// The one error this returns is a genuine configuration defect: the
// resolved name (from RiskToLane or DefaultLane) does not name a
// declared lane. [Load] already rejects that shape at load time for
// every entry in RiskToLane; this is the same check applied again at
// the point of use, so a Policy built by hand (as every test in this
// package does) cannot skip it.
func (p Policy) LaneFor(risk string) (name string, lane Lane, err error) {
	name = p.RiskToLane[risk]
	if name == "" {
		name = p.DefaultLane
	}
	lane, ok := p.Lanes[name]
	if !ok {
		return "", Lane{}, release.Internal("undeclared_lane",
			fmt.Sprintf("lane %q is not declared under policy.yml's lanes", name))
	}
	return name, lane, nil
}

// Default is the compiled phase-one Release Policy, matching Appendix
// C3's example: independent PR approval is not required, and risk maps
// to fast/standard/guarded with guarded as the default (most cautious)
// lane.
func Default() Policy {
	return Policy{
		Version:                       "2",
		IndependentPRApprovalRequired: false,
		Lanes: map[string]Lane{
			"fast":     {Weights: []int{5, 100}},
			"standard": {Weights: []int{5, 25, 50, 100}},
			"guarded":  {Weights: []int{1, 5, 25, 50, 100}},
		},
		RiskToLane: map[string]string{
			"low":    "fast",
			"medium": "standard",
			"high":   "guarded",
		},
		DefaultLane: "guarded",
	}
}

// Evaluate maps an evidence outcome onto Release Eligibility. It does
// not produce a risk tier and does not itself pick a lane: envelope is
// the rollout envelope the caller has already resolved (typically via
// Policy.LaneFor, rendered, and read back from the rendered bytes by
// [release.DeriveEnvelope]) and it is attached only when the release is
// eligible. Resolving the lane once, in the caller, and passing the
// same weights to both rendering and this function is what makes the
// enforced envelope provably the one in the manifest that was hashed
// and applied, rather than two independent resolutions that could
// silently disagree.
func Evaluate(p Policy, evidence release.EvidenceResult, envelope release.RolloutEnvelope) (release.Eligibility, error) {
	switch evidence.Outcome() {
	case release.EvidenceVerified:
		return release.Eligible(p.Version, "all_mandatory_evidence_verified",
			"All configured mandatory evidence verified. The release may enter SafeLane on the resolved rollout lane.",
			envelope)
	case release.EvidenceUnknown:
		return fromReasons(p, evidence, release.Indeterminate)
	default:
		return fromReasons(p, evidence, release.Ineligible)
	}
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
