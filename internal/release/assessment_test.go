package release_test

import (
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/release"
)

// An assessment records how far a change may ship per step. Attaching one
// to a release that may not ship at all would be a width decision on a
// change that never earned one, so NewRelease refuses it -- the same
// invariant, and the same reasoning, as the rendered bundle's.

func TestNewRelease_AssessmentOnIneligibleRelease_IsRefused(t *testing.T) {
	params := ineligibleParams(t)
	params.Assessment = assess.Combine(
		assess.Facts{MergeCommitSHA: strings.Repeat("a", 40)},
		assess.Verdict{Risk: assess.RiskLow, Available: true},
		assess.Verdict{},
		"fast",
	)

	_, err := release.NewRelease(params)
	if err == nil {
		t.Fatal("want an ineligible release carrying an assessment to be refused")
	}
	if !strings.Contains(err.Error(), "assessment_without_eligibility") {
		t.Fatalf("want assessment_without_eligibility, got %v", err)
	}
}

func TestNewRelease_IneligibleReleaseWithoutAssessment_IsAccepted(t *testing.T) {
	r, err := release.NewRelease(ineligibleParams(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.Assessment(); ok {
		t.Error("an ineligible release must carry no assessment")
	}
}

// An assessment that reached a risk but no lane is incomplete: the lane
// is the whole point of assessing, and recording a risk without one would
// leave the record unable to say what it authorised.
func TestNewRelease_AssessmentWithoutLane_IsRefused(t *testing.T) {
	params := ineligibleParams(t)
	params.Assessment = assess.Assessment{Risk: assess.RiskHigh}

	_, err := release.NewRelease(params)
	if err == nil {
		t.Fatal("want an assessment with no lane to be refused")
	}
}

// Combine is the only constructor, and it computes the risk itself: a
// caller cannot record a combined risk that is not the worse of the two.
func TestCombine_TakesTheWorseOfTheTwoVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name             string
		heuristic, model assess.Risk
		want             assess.Risk
	}{
		{"model raises the floor", assess.RiskLow, assess.RiskHigh, assess.RiskHigh},
		{"model cannot lower the floor", assess.RiskHigh, assess.RiskLow, assess.RiskHigh},
		{"an unavailable model leaves the floor", assess.RiskMedium, "", assess.RiskMedium},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := assess.Combine(assess.Facts{},
				assess.Verdict{Risk: tc.heuristic, Available: true},
				assess.Verdict{Risk: tc.model, Available: tc.model != ""},
				"guarded")
			if a.Risk != tc.want {
				t.Fatalf("want %s, got %s", tc.want, a.Risk)
			}
			if a.CombinedBy != "worse-of" {
				t.Fatalf("want the record to say how the risk was combined, got %q", a.CombinedBy)
			}
		})
	}
}

// The diff a model was shown is input, not a decision, and it is not kept
// on the record.
func TestCombine_DoesNotRecordTheDiff(t *testing.T) {
	a := assess.Combine(
		assess.Facts{UnifiedDiff: "diff --git a/x b/x"},
		assess.Verdict{Risk: assess.RiskLow, Available: true},
		assess.Verdict{},
		"fast",
	)
	if a.Facts.UnifiedDiff != "" {
		t.Fatalf("the unified diff must not reach the record, got %q", a.Facts.UnifiedDiff)
	}
}

func ineligibleParams(t *testing.T) release.ReleaseParams {
	t.Helper()
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	id, err := release.NewReleaseID(now, strings.NewReader("0123456789"))
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	elig, err := release.Ineligible("2", "pull_request_not_merged", "pull request #9 is open")
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return release.ReleaseParams{
		ID: id,
		Request: release.ReleaseRequest{
			SchemaVersion: release.RequestSchemaVersion,
			Target:        release.Target{Application: "podinfo", Environment: "production", Cluster: "safelane-demo", Namespace: "podinfo"},
			Source:        release.ClaimedSource{Repository: "AndrewMaged814/podinfo", BaseBranch: "master"},
			PullRequest:   release.ClaimedPullRequest{PullRequestNumber: 9},
			Caller:        release.CallerIdentity{Identity: "safelane-cli", Kind: release.CallerAgent},
			Metadata:      release.RequestMetadata{RequestID: "req-test", SubmittedAt: now},
		},
		Evidence: release.FailedEvidence(release.FailedEvidenceError(
			"pull_request_not_merged", "source.base_branch", "pull request #9 is not merged", "Merge it.")),
		Eligibility: elig,
		CreatedAt:   now,
	}
}
