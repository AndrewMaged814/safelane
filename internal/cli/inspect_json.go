package cli

import (
	"strings"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/release"
)

// inspectionJSON is the machine form of the report `--json` prints.
//
// It carries the same content as the rendered text, from the same values,
// so an agent branching on JSON and an operator reading the terminal are
// never looking at two different derivations of the same release. What it
// deliberately does not carry is layout: no padding, no ladders, no
// wrapped prose.
type inspectionJSON struct {
	ReleaseID   release.ReleaseID  `json:"release_id"`
	Target      release.Target     `json:"target"`
	Checks      []checkJSON        `json:"checks"`
	Assessment  *assess.Assessment `json:"assessment"`
	Bundle      *bundleJSON        `json:"bundle"`
	Decision    decisionJSON       `json:"decision"`
	NextCommand string             `json:"next_command,omitempty"`
}

type checkJSON struct {
	Name string `json:"name"`
	// Outcome is "detected", "failed" or "unavailable" -- the same three
	// the report's section headings name, never collapsed to a boolean.
	// "I could not look" is not "no".
	Outcome string `json:"outcome"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
	Remedy  string `json:"remedy,omitempty"`
}

type bundleJSON struct {
	TemplateDigest string                 `json:"template_digest"`
	Resources      []release.ResourceHash `json:"resources"`
}

type decisionJSON struct {
	Eligibility   string `json:"eligibility"`
	PolicyVersion string `json:"policy_version"`
	Reason        string `json:"reason"`
	Retryable     bool   `json:"retryable"`
	Lane          string `json:"lane,omitempty"`
	Weights       []int  `json:"weights,omitempty"`
	Gates         int    `json:"gates"`
	NextAction    string `json:"next_action,omitempty"`
}

// JSON builds the machine form of the report.
func (in inspection) JSON() inspectionJSON {
	r := in.release
	out := inspectionJSON{
		ReleaseID: r.ID,
		Target:    r.Target(),
		Decision: decisionJSON{
			Eligibility:   r.Eligibility().Status().String(),
			PolicyVersion: in.policy.Version,
			Reason:        r.Eligibility().ReasonCode(),
			Retryable:     r.Eligibility().Retryable(),
		},
	}
	for _, c := range in.checks {
		out.Checks = append(out.Checks, checkJSON{
			Name:    c.label,
			Outcome: c.outcome.String(),
			Summary: strings.TrimSpace(c.value + " " + c.tail),
			Detail:  c.detail,
			Remedy:  c.remedy,
		})
	}
	if a, ok := r.Assessment(); ok {
		out.Assessment = &a
		out.Decision.Lane = a.Lane
	}
	if bundle, ok := r.Bundle(); ok {
		out.Bundle = &bundleJSON{
			TemplateDigest: bundle.Template().ContentDigest,
			Resources:      bundle.Hashes(),
		}
	}
	if env, ok := r.Eligibility().Envelope(); ok {
		out.Decision.Weights = env.Stages()
		out.Decision.Gates = gateCount(env.Stages())
		out.Decision.NextAction = env.NextAction()
		out.NextCommand = "safelane rollout start " + string(r.ID)
	}
	return out
}

func (o checkOutcome) String() string {
	switch o {
	case checkDetected:
		return "detected"
	case checkFailed:
		return "failed"
	default:
		return "unavailable"
	}
}
