package assess

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// PathRule raises the floor to Minimum when any changed file matches Glob.
type PathRule struct {
	Glob    string
	Minimum Risk
}

// SizeRule raises the floor to Minimum once a size threshold is met. Each
// rule sets exactly one of ChangedLinesAtLeast or FilesAtLeast; the zero
// value means "this threshold is unset" for that rule.
type SizeRule struct {
	ChangedLinesAtLeast int
	FilesAtLeast        int
	Minimum             Risk
}

// HeuristicConfig is the operator-owned configuration behind the
// heuristic assessor -- policy.yml's assessment.heuristic block (Appendix
// C3), held here as Go values. policy.yml itself is not read from disk
// until task 17; until then, whatever loads it is responsible for
// producing a HeuristicConfig.
type HeuristicConfig struct {
	AgentAuthoredMinimum Risk
	Paths                []PathRule
	Size                 []SizeRule
}

// Heuristic returns the deterministic, operator-owned assessor: the floor
// every change gets, always run, never optional. It never invokes a model
// and never reads anything but the Facts it is given.
func Heuristic(cfg HeuristicConfig) Assessor {
	return heuristicAssessor{cfg: cfg}
}

type heuristicAssessor struct {
	cfg HeuristicConfig
}

func (heuristicAssessor) Name() string { return "heuristic" }

// Assess raises a floor that starts at RiskLow, one rule at a time, and
// records every rule that fired by name so the output can say why. A rule
// can only ever raise the floor -- there is no rule that lowers it.
//
// An error here means the heuristic itself could not run: a malformed
// path glob, for instance. That is a configuration error, not a low
// verdict, and per Appendix C1 the release is refused rather than
// defaulting to anything -- the heuristic is not optional.
func (h heuristicAssessor) Assess(_ context.Context, f Facts) (Verdict, error) {
	if riskRank(h.cfg.AgentAuthoredMinimum) < 0 {
		return Verdict{}, fmt.Errorf("assess: heuristic: agent_authored_minimum %q is not a recognised risk level", h.cfg.AgentAuthoredMinimum)
	}

	compiled := make(map[string]*regexp.Regexp, len(h.cfg.Paths))
	for _, r := range h.cfg.Paths {
		if riskRank(r.Minimum) < 0 {
			return Verdict{}, fmt.Errorf("assess: heuristic: path rule %q: minimum %q is not a recognised risk level", r.Glob, r.Minimum)
		}
		re, err := compileGlob(r.Glob)
		if err != nil {
			return Verdict{}, fmt.Errorf("assess: heuristic: path rule %q: %w", r.Glob, err)
		}
		compiled[r.Glob] = re
	}
	for _, r := range h.cfg.Size {
		if riskRank(r.Minimum) < 0 {
			return Verdict{}, fmt.Errorf("assess: heuristic: size rule: minimum %q is not a recognised risk level", r.Minimum)
		}
	}

	floor := RiskLow
	var rules []string
	raise := func(name string, min Risk) {
		if riskRank(min) > riskRank(floor) {
			floor = min
		}
		rules = append(rules, name)
	}

	if f.AgentAuthored {
		raise(ruleAgentAuthored, h.cfg.AgentAuthoredMinimum)
	}

	for _, r := range h.cfg.Paths {
		re := compiled[r.Glob]
		for _, file := range f.Files {
			if re.MatchString(file.Path) {
				raise(rulePathPrefix+r.Glob, r.Minimum)
				break // one firing per rule, however many files match it
			}
		}
	}

	totalLines := f.TotalAdditions + f.TotalDeletions
	for _, r := range h.cfg.Size {
		switch {
		case r.ChangedLinesAtLeast > 0 && totalLines >= r.ChangedLinesAtLeast,
			r.FilesAtLeast > 0 && len(f.Files) >= r.FilesAtLeast:
			raise(sizeRuleName(r), r.Minimum)
		}
	}

	rationale := ""
	if len(rules) == 0 {
		rationale = "no rule raised the floor"
	}

	return Verdict{
		Risk:      floor,
		Rationale: rationale,
		Rules:     rules,
		Available: true,
	}, nil
}

// MinimumFor returns the floor the named rule raises to, for a rule name
// as it appears in [Verdict.Rules]. Verdict.Rules records names only --
// that is the shape the Release Record stores -- so anything that wants
// to show *what* a rule did, rather than just that it fired, resolves it
// back through the operator configuration that declared it.
func (c HeuristicConfig) MinimumFor(rule string) (Risk, bool) {
	if rule == ruleAgentAuthored {
		return c.AgentAuthoredMinimum, true
	}
	if glob, ok := strings.CutPrefix(rule, rulePathPrefix); ok {
		for _, r := range c.Paths {
			if r.Glob == glob {
				return r.Minimum, true
			}
		}
		return "", false
	}
	for _, r := range c.Size {
		if sizeRuleName(r) == rule {
			return r.Minimum, true
		}
	}
	return "", false
}

// Rule-name prefixes. They are the identifiers the Release Record
// stores, so they are constants here rather than inline format strings
// in two places that could drift apart.
const (
	ruleAgentAuthored = "agent_authored"
	rulePathPrefix    = "path:"
)

func sizeRuleName(r SizeRule) string {
	if r.ChangedLinesAtLeast > 0 {
		return fmt.Sprintf("size:changed_lines_at_least:%d", r.ChangedLinesAtLeast)
	}
	return fmt.Sprintf("size:files_at_least:%d", r.FilesAtLeast)
}
