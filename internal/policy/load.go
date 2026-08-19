package policy

import (
	"fmt"
	"os"

	"github.com/AndrewMaged814/safelane/internal/release"
	"gopkg.in/yaml.v3"
)

// yamlPolicy is policy.yml's on-disk shape (Appendix C3). mandatory_evidence
// is read but not yet enforced here -- nothing in this ticket consumes it,
// so it is not carried onto Policy, and yaml.Unmarshal ignores unknown
// fields on Policy's side without complaint either way.
type yamlPolicy struct {
	Version               string `yaml:"version"`
	IndependentPRApproval struct {
		Required bool `yaml:"required"`
	} `yaml:"independent_pr_approval"`
	Lanes map[string]struct {
		Weights []int `yaml:"weights"`
	} `yaml:"lanes"`
	RiskToLane  map[string]string `yaml:"risk_to_lane"`
	DefaultLane string            `yaml:"default_lane"`
}

// Load reads and validates the operator's policy.yml. A risk_to_lane
// entry naming a lane that was never declared under lanes: is rejected
// here, at load, not at rollout time -- along with every other lane
// shape a rollout later depends on being correct.
func Load(path string) (Policy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Policy{}, release.Invalid("missing_policy_config", "policy",
				fmt.Sprintf("no release policy at %s", path),
				"Run safelane init, or pass --policy.")
		}
		return Policy{}, release.Invalid("unreadable_policy_config", "policy",
			fmt.Sprintf("could not read %s: %v", path, err),
			"Fix the policy file path and retry.")
	}

	var y yamlPolicy
	if err := yaml.Unmarshal(raw, &y); err != nil {
		return Policy{}, release.Malformed("invalid_policy_config", "policy",
			"policy.yml is not valid YAML",
			"Match the SafeLane policy.yml schema.").WithCause(err)
	}

	p := Policy{
		Version:                       y.Version,
		IndependentPRApprovalRequired: y.IndependentPRApproval.Required,
		Lanes:                         make(map[string]Lane, len(y.Lanes)),
		RiskToLane:                    y.RiskToLane,
		DefaultLane:                   y.DefaultLane,
	}
	for name, l := range y.Lanes {
		p.Lanes[name] = Lane{Weights: l.Weights}
	}

	if err := p.validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// validate reports every lane-shape defect at once: an empty lane set,
// an unset or undeclared default_lane, a risk_to_lane entry naming a
// lane that was never declared, and a declared lane with no weights.
func (p Policy) validate() error {
	var errs release.Errors

	if len(p.Lanes) == 0 {
		errs = append(errs, release.Invalid("missing_policy_field", "lanes",
			"no lanes were declared",
			"Declare at least one lane under policy.yml's lanes:."))
	}
	for name, lane := range p.Lanes {
		if len(lane.Weights) == 0 {
			errs = append(errs, release.Invalid("empty_lane", "lanes."+name+".weights",
				fmt.Sprintf("lane %q declares no weights", name),
				"Give every lane at least one weight."))
		}
	}

	if p.DefaultLane == "" {
		errs = append(errs, release.Invalid("missing_policy_field", "default_lane",
			"no default_lane was configured",
			"Set default_lane to the operator's most cautious declared lane."))
	} else if _, ok := p.Lanes[p.DefaultLane]; !ok {
		errs = append(errs, release.Invalid("undeclared_lane", "default_lane",
			fmt.Sprintf("default_lane %q is not declared under lanes", p.DefaultLane),
			"Set default_lane to one of the lanes declared under lanes:."))
	}

	for risk, lane := range p.RiskToLane {
		if _, ok := p.Lanes[lane]; !ok {
			errs = append(errs, release.Invalid("undeclared_lane", "risk_to_lane."+risk,
				fmt.Sprintf("risk_to_lane %q names undeclared lane %q", risk, lane),
				"Set risk_to_lane entries only to lanes declared under lanes:."))
		}
	}

	return errs.OrNil()
}
