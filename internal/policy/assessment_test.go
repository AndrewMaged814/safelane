package policy_test

import (
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/policy"
)

const assessmentPolicy = `version: 2
lanes:
  fast:     { weights: [5, 100] }
  standard: { weights: [5, 25, 50, 100] }
  guarded:  { weights: [1, 5, 25, 50, 100] }
risk_to_lane:
  low:    fast
  medium: standard
  high:   guarded
default_lane: guarded
assessment:
  heuristic:
    agent_authored_minimum: medium
    paths:
      - { glob: "pkg/api/**",       minimum: medium }
      - { glob: "**/migrations/**", minimum: high }
    size:
      - { changed_lines_at_least: 200, minimum: medium }
      - { files_at_least: 15,          minimum: medium }
  model:
    assessors: [claude, codex]
    timeout: 90s
    max_diff_bytes: 200000
`

func TestLoad_AssessmentBlock_ReachesBothAssessors(t *testing.T) {
	p, err := policy.Load(writePolicy(t, assessmentPolicy))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h := p.Assessment.Heuristic
	if h.AgentAuthoredMinimum != assess.RiskMedium {
		t.Errorf("agent_authored_minimum = %q, want medium", h.AgentAuthoredMinimum)
	}
	if len(h.Paths) != 2 || h.Paths[0].Glob != "pkg/api/**" || h.Paths[1].Minimum != assess.RiskHigh {
		t.Errorf("path rules did not load: %+v", h.Paths)
	}
	if len(h.Size) != 2 || h.Size[0].ChangedLinesAtLeast != 200 || h.Size[1].FilesAtLeast != 15 {
		t.Errorf("size rules did not load: %+v", h.Size)
	}

	m := p.Assessment.Model
	if len(m.Assessors) != 2 || m.Assessors[0] != "claude" {
		t.Errorf("model assessors did not load: %v", m.Assessors)
	}
	if m.Timeout != 90*time.Second {
		t.Errorf("model timeout = %s, want 90s", m.Timeout)
	}
	if m.MaxDiffBytes != 200000 {
		t.Errorf("max_diff_bytes = %d, want 200000", m.MaxDiffBytes)
	}
}

// A policy.yml with no assessment: block falls back to the compiled
// default, not to an empty configuration. An empty HeuristicConfig would
// silently disable every floor rule, which is the one outcome a missing
// block must not produce: every change would assess low and take the
// widest lane.
func TestLoad_NoAssessmentBlock_KeepsTheDefaultFloorRules(t *testing.T) {
	p, err := policy.Load(writePolicy(t, `version: 2
lanes:
  guarded: { weights: [1, 5, 25, 50, 100] }
default_lane: guarded
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.Assessment.Heuristic.Paths) == 0 {
		t.Fatal("a policy with no assessment block must not lose the floor rules")
	}
	if p.Assessment.Heuristic.AgentAuthoredMinimum != assess.RiskMedium {
		t.Fatalf("want the default agent_authored floor, got %q", p.Assessment.Heuristic.AgentAuthoredMinimum)
	}
}

// There are three risk levels. A fourth is a configuration error, caught
// at load rather than at rollout time.
func TestLoad_UnrecognisedRiskLevel_IsRejected(t *testing.T) {
	_, err := policy.Load(writePolicy(t, `version: 2
lanes:
  guarded: { weights: [1, 100] }
default_lane: guarded
assessment:
  heuristic:
    agent_authored_minimum: catastrophic
`))
	if err == nil {
		t.Fatal("want a fourth risk level to be rejected at load")
	}
}

// MinimumFor is how the report says what a rule did, from the rule names
// the record stores. Every name the heuristic can emit has to resolve.
func TestHeuristicConfig_MinimumFor_ResolvesEveryRuleName(t *testing.T) {
	p, err := policy.Load(writePolicy(t, assessmentPolicy))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, want := range map[string]assess.Risk{
		"agent_authored":                  assess.RiskMedium,
		"path:pkg/api/**":                 assess.RiskMedium,
		"path:**/migrations/**":           assess.RiskHigh,
		"size:changed_lines_at_least:200": assess.RiskMedium,
		"size:files_at_least:15":          assess.RiskMedium,
	} {
		got, ok := p.Assessment.Heuristic.MinimumFor(name)
		if !ok || got != want {
			t.Errorf("MinimumFor(%q) = %q, %v; want %q, true", name, got, ok, want)
		}
	}
	if _, ok := p.Assessment.Heuristic.MinimumFor("path:charts/**"); ok {
		t.Error("a rule this policy never declared must not resolve")
	}
}
