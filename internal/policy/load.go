package policy

import (
	"fmt"
	"os"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"

	"github.com/AndrewMaged814/safelane/internal/release"
	"gopkg.in/yaml.v3"
)

// yamlPolicy is policy.yml's on-disk shape (Appendix C3). mandatory_evidence
// is read but not yet enforced here -- nothing in this ticket consumes it,
// so it is not carried onto Policy, and yaml.Unmarshal ignores unknown
// fields on Policy's side without complaint either way.
type yamlPolicy struct {
	Version string `yaml:"version"`
	Lanes   map[string]struct {
		Weights []int `yaml:"weights"`
	} `yaml:"lanes"`
	RiskToLane  map[string]string `yaml:"risk_to_lane"`
	DefaultLane string            `yaml:"default_lane"`
	Assessment  struct {
		Heuristic struct {
			AgentAuthoredMinimum string `yaml:"agent_authored_minimum"`
			Paths                []struct {
				Glob    string `yaml:"glob"`
				Minimum string `yaml:"minimum"`
			} `yaml:"paths"`
			Size []struct {
				ChangedLinesAtLeast int    `yaml:"changed_lines_at_least"`
				FilesAtLeast        int    `yaml:"files_at_least"`
				Minimum             string `yaml:"minimum"`
			} `yaml:"size"`
		} `yaml:"heuristic"`
		Model struct {
			Assessors    []string `yaml:"assessors"`
			Timeout      string   `yaml:"timeout"`
			MaxDiffBytes int      `yaml:"max_diff_bytes"`
		} `yaml:"model"`
	} `yaml:"assessment"`
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
				"Run safelane setup from the application repository.")
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
		Version:     y.Version,
		Lanes:       make(map[string]Lane, len(y.Lanes)),
		RiskToLane:  y.RiskToLane,
		DefaultLane: y.DefaultLane,
	}
	for name, l := range y.Lanes {
		p.Lanes[name] = Lane{Weights: l.Weights}
	}

	assessment, err := readAssessment(y)
	if err != nil {
		return Policy{}, err
	}
	p.Assessment = assessment

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

// readAssessment converts policy.yml's assessment: block into the Go
// values the two assessors take.
//
// An omitted block falls back to [Default]'s assessment configuration
// rather than to an empty one. An empty HeuristicConfig would silently
// disable every floor rule -- every change would assess low and take the
// widest lane -- which is exactly the failure mode a missing block must
// not produce. The heuristic is not optional (Appendix C1's third rule),
// so "not configured" means "the compiled default", never "no rules".
func readAssessment(y yamlPolicy) (AssessmentConfig, error) {
	a := y.Assessment
	if a.Heuristic.AgentAuthoredMinimum == "" && len(a.Heuristic.Paths) == 0 &&
		len(a.Heuristic.Size) == 0 && len(a.Model.Assessors) == 0 {
		return Default().Assessment, nil
	}

	var errs release.Errors
	risk := func(field, value string, fallback assess.Risk) assess.Risk {
		if value == "" {
			return fallback
		}
		switch assess.Risk(value) {
		case assess.RiskLow, assess.RiskMedium, assess.RiskHigh:
			return assess.Risk(value)
		}
		errs = append(errs, release.Invalid("invalid_risk_level", field,
			fmt.Sprintf("%q is not one of low, medium, high", value),
			"Use one of the three risk levels. There is no fourth."))
		return fallback
	}

	cfg := AssessmentConfig{}
	cfg.Heuristic.AgentAuthoredMinimum = risk(
		"assessment.heuristic.agent_authored_minimum", a.Heuristic.AgentAuthoredMinimum, assess.RiskLow)

	for i, r := range a.Heuristic.Paths {
		field := fmt.Sprintf("assessment.heuristic.paths[%d]", i)
		if r.Glob == "" {
			errs = append(errs, release.Invalid("missing_policy_field", field+".glob",
				"a path rule declares no glob",
				"Give every path rule a glob, for example pkg/api/**."))
			continue
		}
		cfg.Heuristic.Paths = append(cfg.Heuristic.Paths, assess.PathRule{
			Glob:    r.Glob,
			Minimum: risk(field+".minimum", r.Minimum, assess.RiskLow),
		})
	}

	for i, r := range a.Heuristic.Size {
		field := fmt.Sprintf("assessment.heuristic.size[%d]", i)
		if r.ChangedLinesAtLeast <= 0 && r.FilesAtLeast <= 0 {
			errs = append(errs, release.Invalid("missing_policy_field", field,
				"a size rule sets neither changed_lines_at_least nor files_at_least",
				"Set exactly one threshold on every size rule."))
			continue
		}
		cfg.Heuristic.Size = append(cfg.Heuristic.Size, assess.SizeRule{
			ChangedLinesAtLeast: r.ChangedLinesAtLeast,
			FilesAtLeast:        r.FilesAtLeast,
			Minimum:             risk(field+".minimum", r.Minimum, assess.RiskLow),
		})
	}

	cfg.Model.Assessors = a.Model.Assessors
	cfg.Model.MaxDiffBytes = a.Model.MaxDiffBytes
	if a.Model.Timeout != "" {
		d, err := time.ParseDuration(a.Model.Timeout)
		if err != nil {
			errs = append(errs, release.Invalid("invalid_duration", "assessment.model.timeout",
				fmt.Sprintf("%q is not a duration", a.Model.Timeout),
				`Use a Go duration, for example "90s".`))
		} else {
			cfg.Model.Timeout = d
		}
	}

	return cfg, errs.OrNil()
}
