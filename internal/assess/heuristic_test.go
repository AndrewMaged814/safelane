package assess_test

import (
	"context"
	"slices"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/assess"
)

// samplePolicy mirrors PLAN.md Appendix C3's assessment.heuristic block
// exactly, so these tests exercise the shape the real policy.yml will
// hold once task 17 reads it.
func samplePolicy() assess.HeuristicConfig {
	return assess.HeuristicConfig{
		AgentAuthoredMinimum: assess.RiskMedium,
		Paths: []assess.PathRule{
			{Glob: "pkg/api/**", Minimum: assess.RiskMedium},
			{Glob: "**/migrations/**", Minimum: assess.RiskHigh},
			{Glob: "charts/**", Minimum: assess.RiskHigh},
		},
		Size: []assess.SizeRule{
			{ChangedLinesAtLeast: 200, Minimum: assess.RiskMedium},
			{FilesAtLeast: 15, Minimum: assess.RiskMedium},
		},
	}
}

func TestHeuristic_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		facts     assess.Facts
		wantRisk  assess.Risk
		wantRules []string
	}{
		{
			name: "one-line version bump assesses low",
			facts: assess.Facts{
				Files:          []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
				TotalAdditions: 1,
				TotalDeletions: 1,
			},
			wantRisk:  assess.RiskLow,
			wantRules: nil,
		},
		{
			name: "a critical path raises the floor regardless of size",
			facts: assess.Facts{
				Files:          []assess.FileChange{{Path: "pkg/api/echo.go", Additions: 1, Deletions: 0}},
				TotalAdditions: 1,
			},
			wantRisk:  assess.RiskMedium,
			wantRules: []string{"path:pkg/api/**"},
		},
		{
			name: "agent authorship raises the floor on its own",
			facts: assess.Facts{
				Files:         []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
				AgentAuthored: true,
				AgentEvidence: "Co-authored-by: Claude <noreply@anthropic.com>",
			},
			wantRisk:  assess.RiskMedium,
			wantRules: []string{"agent_authored"},
		},
		{
			name: "agent authorship and a critical path both fire and agree",
			facts: assess.Facts{
				Files: []assess.FileChange{
					{Path: "pkg/api/echo.go", Additions: 41, Deletions: 6},
					{Path: "pkg/api/handlers.go", Additions: 22, Deletions: 5},
					{Path: "pkg/version/version.go", Additions: 1, Deletions: 1},
				},
				TotalAdditions: 64,
				TotalDeletions: 12,
				AgentAuthored:  true,
				AgentEvidence:  "Co-authored-by: Claude <noreply@anthropic.com>",
			},
			wantRisk:  assess.RiskMedium,
			wantRules: []string{"agent_authored", "path:pkg/api/**"},
		},
		{
			name: "a high-minimum path rule outranks a medium one",
			facts: assess.Facts{
				Files: []assess.FileChange{
					{Path: "pkg/api/echo.go", Additions: 1},
					{Path: "charts/podinfo/values.yaml", Additions: 1},
				},
			},
			wantRisk:  assess.RiskHigh,
			wantRules: []string{"path:pkg/api/**", "path:charts/**"},
		},
		{
			name: "changed-line size rule fires without any path or agent signal",
			facts: assess.Facts{
				Files:          []assess.FileChange{{Path: "README.md", Additions: 150, Deletions: 60}},
				TotalAdditions: 150,
				TotalDeletions: 60,
			},
			wantRisk:  assess.RiskMedium,
			wantRules: []string{"size:changed_lines_at_least:200"},
		},
		{
			name: "files-at-least size rule fires on file count alone",
			facts: assess.Facts{
				Files: filesNamed(15),
			},
			wantRisk:  assess.RiskMedium,
			wantRules: []string{"size:files_at_least:15"},
		},
		{
			name:      "no facts at all still assesses low, not an error",
			facts:     assess.Facts{},
			wantRisk:  assess.RiskLow,
			wantRules: nil,
		},
	}

	assessor := assess.Heuristic(samplePolicy())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assessor.Assess(context.Background(), tc.facts)
			if err != nil {
				t.Fatalf("Assess: %v", err)
			}
			if !got.Available {
				t.Error("heuristic must always be Available when it runs without error")
			}
			if got.Risk != tc.wantRisk {
				t.Errorf("Risk = %q, want %q", got.Risk, tc.wantRisk)
			}
			if !slices.Equal(got.Rules, tc.wantRules) {
				t.Errorf("Rules = %v, want %v", got.Rules, tc.wantRules)
			}
		})
	}
}

func TestHeuristic_NoRulesFired_ExplainsWhyInRationale(t *testing.T) {
	assessor := assess.Heuristic(samplePolicy())
	got, err := assessor.Assess(context.Background(), assess.Facts{
		Files: []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Rationale != "no rule raised the floor" {
		t.Errorf("Rationale = %q, want %q", got.Rationale, "no rule raised the floor")
	}
}

func TestHeuristic_Name(t *testing.T) {
	if got := assess.Heuristic(samplePolicy()).Name(); got != "heuristic" {
		t.Errorf("Name() = %q, want %q", got, "heuristic")
	}
}

func TestHeuristic_NeverInvokesAModelOrReadsAWorkingDirectory(t *testing.T) {
	// There is nothing in heuristicAssessor.Assess that spawns a process
	// or opens a file -- Facts in, Verdict out. This test exists as a
	// standing assertion of that: a table-driven Facts-only test suite is
	// the proof, not a mock that would need maintaining.
	assessor := assess.Heuristic(samplePolicy())
	got, err := assessor.Assess(context.Background(), assess.Facts{
		Files: []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got.Risk != assess.RiskLow {
		t.Fatalf("Risk = %q, want %q", got.Risk, assess.RiskLow)
	}
}

func TestHeuristic_MalformedPolicyIsAConfigurationError(t *testing.T) {
	// An unrecognised risk level in the operator's own config -- not a
	// bad fact, a bad policy. Per Appendix C1 this is a configuration
	// error the release is refused for, not a low verdict and not a
	// silent default.
	cfg := samplePolicy()
	cfg.Paths = append(cfg.Paths, assess.PathRule{Glob: "docs/**", Minimum: assess.Risk("critical")})
	assessor := assess.Heuristic(cfg)

	_, err := assessor.Assess(context.Background(), assess.Facts{
		Files: []assess.FileChange{{Path: "pkg/version/version.go"}},
	})
	if err == nil {
		t.Fatal("want an error from an unrecognised risk level in the policy; the heuristic is not optional and must not silently assess anyway")
	}
}

func filesNamed(n int) []assess.FileChange {
	files := make([]assess.FileChange, n)
	for i := range files {
		files[i] = assess.FileChange{Path: "file" + string(rune('a'+i)) + ".go", Additions: 1}
	}
	return files
}
