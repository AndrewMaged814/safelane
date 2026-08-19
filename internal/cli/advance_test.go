package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/release"
)

var fastWeights = []int{5, 100}
var guardedWeights = []int{1, 5, 25, 50, 100}

// fastLaneStarted builds the A2.1/A2.2 release (fast lane, weights 5, 100)
// and attaches the granted `start` entry A2.2 would have recorded, so
// tests here can begin partway through -- already at gate 1, weight 5 --
// without re-driving startRollout.
func fastLaneStarted(t *testing.T) *release.Release {
	t.Helper()
	rel := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files:          []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
			TotalAdditions: 1, TotalDeletions: 1, MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskLow, Rationale: safeRationale, Available: true, Assessor: "claude"},
	}.buildRelease(t)

	started, err := rel.WithExecution(release.ExecutionEntry{
		At: time.Date(2026, 8, 20, 14, 21, 44, 0, time.UTC), Verb: release.VerbStart,
		RequestedWeight: 5, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return started
}

// guardedLaneStarted builds the A3.1/A3.2 release (guarded lane, weights
// 1,5,25,50,100) already at gate 1, weight 1.
func guardedLaneStarted(t *testing.T) *release.Release {
	t.Helper()
	rel := inspectCase{
		id:     "rel_01M0F3QD9NBV6JKC2WS8XA7TR4",
		pr:     4,
		github: fakeGitHub{facts: mergedFacts(4, riskyMergeSHA)},
		ghcr:   fakeGHCR{digest: riskyDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files: []assess.FileChange{
				{Path: "pkg/api/echo.go", Additions: 41, Deletions: 6},
				{Path: "pkg/api/handlers.go", Additions: 22, Deletions: 5},
				{Path: "pkg/version/version.go", Additions: 1, Deletions: 1},
			},
			TotalAdditions: 64, TotalDeletions: 12,
			AgentAuthored: true, AgentEvidence: riskyTrailer, MergeCommitSHA: riskyMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskHigh, Rationale: riskyRationale, Available: true, Assessor: "claude"},
	}.buildRelease(t)

	started, err := rel.WithExecution(release.ExecutionEntry{
		At: time.Date(2026, 8, 20, 14, 26, 3, 0, time.UTC), Verb: release.VerbStart,
		RequestedWeight: 1, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return started
}

// stepsJSON is the Rollout's own `spec.strategy.canary.steps` shape for a
// lane's weights: setWeight, pause, setWeight, pause, ... -- the same
// shape the fixed 40-rollout.yaml.tmpl renders (task 09's fork fix).
func stepsJSON(weights []int) string {
	var b strings.Builder
	for i, w := range weights {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"setWeight":%d},{"pause":{}}`, w)
	}
	return b.String()
}

// stepIndexOf is the currentStepIndex a Rollout reports once weights[i]'s
// setWeight step has run: two entries (setWeight, pause) per earlier
// weight, landing on the setWeight entry itself.
func stepIndexOf(weights []int, weight int) int {
	for i, w := range weights {
		if w == weight {
			return i * 2
		}
	}
	return 0
}

// atGateStatus is a Rollout paused at a gate, having reached weight within
// the given lane.
func atGateStatus(weights []int, weight int) string {
	return fmt.Sprintf(`{"status":{"phase":"Paused","pauseConditions":[{"reason":"CanaryPauseStep"}],"currentStepIndex":%d},`+
		`"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, stepIndexOf(weights, weight), stepsJSON(weights))
}

// progressingStatus is a Rollout still moving, mid-step.
func progressingStatus(weights []int) string {
	return fmt.Sprintf(`{"status":{"phase":"Progressing"},"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, stepsJSON(weights))
}

const analysisRunJSON = `{"status":{"phase":"Successful","metricResults":[{"name":"request-success-rate","count":3,` +
	`"successful":3,"measurements":[{"value":"[1]"},{"value":"[1]"},{"value":"[1]"}]}]},` +
	`"spec":{"metrics":[{"name":"request-success-rate","successCondition":"len(result) > 0 && result[0] >= 0.99"}]}}`

func TestRolloutAdvance_ToCompletion_MatchesA23(t *testing.T) {
	rel := fastLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(fastWeights, 5), nil) // GetStatus before deciding
	q.enqueue("", nil)                           // promote
	q.enqueue(`{"status":{"phase":"Healthy","stableRS":"abc123","currentPodHash":"abc123",`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-2","status":"Successful"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil) // WaitForGate's first poll
	q.enqueue(analysisRunJSON, nil) // GetAnalysisRun

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	assertGolden(t, "a2-3-advance-complete.txt", result.Render())

	entries := result.release.Execution()
	if len(entries) != 2 {
		t.Fatalf("execution history = %+v, want 2 entries (start, advance)", entries)
	}
	last := entries[1]
	if last.Verb != release.VerbAdvance || last.RequestedWeight != 100 || last.Outcome != release.OutcomeGranted {
		t.Errorf("last entry = %+v, want a granted advance to weight 100", last)
	}
	if last.Analysis == "" {
		t.Errorf("a completed advance must record the AnalysisRun it observed")
	}

	for _, call := range q.calls {
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

// TestRolloutAdvance_ToCompletion_SurvivesArgoClearingTheTransientField is a
// regression test for a race this build hit in its own live rehearsal: Argo
// clears `.status.canary.currentBackgroundAnalysisRunStatus` from the
// Rollout once it settles Healthy, so the *very first* poll to observe
// Complete can already show that field empty. The name must still be
// reconstructed from CurrentPodHash + the revision annotation (both of
// which persist), or the whole AnalysisRun measurement line silently
// vanishes -- exactly what happened against the real cluster.
func TestRolloutAdvance_ToCompletion_SurvivesArgoClearingTheTransientField(t *testing.T) {
	rel := fastLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(fastWeights, 5), nil) // GetStatus before deciding
	q.enqueue("", nil)                           // promote
	q.enqueue(`{"metadata":{"annotations":{"rollout.argoproj.io/revision":"2"}},`+
		`"status":{"phase":"Healthy","stableRS":"5f9b48bf7c","currentPodHash":"5f9b48bf7c"},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil) // no currentBackgroundAnalysisRunStatus at all
	q.enqueue(analysisRunJSON, nil) // GetAnalysisRun

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	assertGolden(t, "a2-3-advance-complete.txt", result.Render())

	queried := q.calls[3]
	if strings.Join(queried, " ") != "get analysisrun podinfo-5f9b48bf7c-2 -n podinfo -o json" {
		t.Errorf("GetAnalysisRun call = %v, want the reconstructed name podinfo-5f9b48bf7c-2", queried)
	}
}

func TestRolloutAdvance_OverWideRequest_MatchesA33(t *testing.T) {
	rel := guardedLaneStarted(t)
	to := 100

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 1), nil)

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run

	_, err := advanceRollout(context.Background(), rel, ex, "podinfo", &to, time.Minute, time.Now)
	if err == nil {
		t.Fatal("want a refusal, got nil error")
	}
	var rerr *release.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %v (%T), want a *release.Error", err, err)
	}
	if rerr.Code != "transition_exceeds_envelope" {
		t.Errorf("code = %q, want transition_exceeds_envelope", rerr.Code)
	}

	var stderr, stdout bytes.Buffer
	printRolloutRejection(&stderr, err)
	assertGolden(t, "a3-3-refusal.txt", stderr.String())
	if stdout.String() != "" {
		t.Errorf("a refusal must apply nothing, got stdout %q", stdout.String())
	}
	for _, call := range q.calls {
		if call[0] != "get" {
			t.Errorf("a refused advance must never do anything but read status, got: %v", call)
		}
	}
}

func TestRolloutAdvance_NotStarted_MatchesN11(t *testing.T) {
	rel := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files: []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}}, MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskLow, Available: true, Assessor: "claude"},
	}.buildRelease(t) // never started: no execution entry attached

	q := &queueRunner{}
	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run

	_, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	var rerr *release.Error
	if !errors.As(err, &rerr) || rerr.Code != "rollout_not_started" {
		t.Fatalf("err = %v, want a rollout_not_started *release.Error", err)
	}
	var stderr bytes.Buffer
	printRolloutRejection(&stderr, err)
	assertGolden(t, "n11-rollout-not-started.txt", stderr.String())
	if len(q.calls) != 0 {
		t.Errorf("a release with no execution record must be refused before any kubectl call, got: %v", q.calls)
	}
}

func TestRolloutAdvance_NotAtGate_MatchesN11(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	// Progressing, not paused: not a gate.
	q.enqueue(progressingStatus(guardedWeights), nil)

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run

	_, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	var rerr *release.Error
	if !errors.As(err, &rerr) || rerr.Code != "rollout_not_at_gate" {
		t.Fatalf("err = %v, want a rollout_not_at_gate *release.Error", err)
	}
	var stderr bytes.Buffer
	printRolloutRejection(&stderr, err)
	assertGolden(t, "n11-rollout-not-at-gate.txt", stderr.String())
}

func TestRolloutAdvance_Backwards_MatchesN11(t *testing.T) {
	rel := guardedLaneStarted(t)
	// This release was granted weight 1 at start. Advance it for real once
	// (to weight 5) so "current weight 5" matches the golden, then ask to
	// go back to weight 1.
	rel, err := rel.WithExecution(release.ExecutionEntry{
		At: time.Date(2026, 8, 20, 14, 26, 48, 0, time.UTC), Verb: release.VerbAdvance,
		RequestedWeight: 5, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	back := 1

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 5), nil)

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run

	_, aerr := advanceRollout(context.Background(), rel, ex, "podinfo", &back, time.Minute, time.Now)
	var rerr *release.Error
	if !errors.As(aerr, &rerr) || rerr.Code != "transition_not_permitted" {
		t.Fatalf("err = %v, want a transition_not_permitted *release.Error", aerr)
	}
	var stderr bytes.Buffer
	printRolloutRejection(&stderr, aerr)
	assertGolden(t, "n11-transition-backwards.txt", stderr.String())
}

func TestRolloutAdvance_Timeout_MatchesN12(t *testing.T) {
	rel := guardedLaneStarted(t)
	rel, err := rel.WithExecution(release.ExecutionEntry{
		At: time.Date(2026, 8, 20, 14, 26, 48, 0, time.UTC), Verb: release.VerbAdvance,
		RequestedWeight: 5, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 5), nil)
	q.enqueue("", nil) // promote
	progressingTo25 := progressingStatus(guardedWeights)
	for i := 0; i < 5; i++ {
		q.enqueue(progressingTo25, nil)
	}

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	current := time.Date(2026, 8, 20, 14, 26, 50, 0, time.UTC)
	ex.Now = func() time.Time { return current }
	ex.Sleep = func(d time.Duration) { current = current.Add(d) }
	ex.PollInterval = 2 * time.Second

	result, aerr := advanceRollout(context.Background(), rel, ex, "podinfo", nil, 5*time.Second, func() time.Time { return current })
	if !errors.Is(aerr, execute.ErrGateTimeout) {
		t.Fatalf("err = %v, want ErrGateTimeout", aerr)
	}
	assertGolden(t, "n12-advance-timeout.txt", result.RenderTimeout())

	for _, call := range q.calls[1:] { // calls[0] is the pre-decision GetStatus, calls[1] is promote
		if call[0] == "get" {
			continue
		}
		if len(call) >= 3 && call[0] == "argo" && call[1] == "rollouts" && call[2] == "promote" {
			continue
		}
		t.Errorf("a timed-out wait must never do anything but poll (after one promote), got: %v", call)
	}
}

func TestBackgroundAnalysisRunName_PrefersTheLiveFieldFallsBackToReconstructing(t *testing.T) {
	live := execute.Status{AnalysisRunName: "podinfo-5f9b48bf7c-2"}
	if got := backgroundAnalysisRunName("podinfo", live); got != "podinfo-5f9b48bf7c-2" {
		t.Errorf("got %q, want the live field verbatim", got)
	}

	cleared := execute.Status{CurrentPodHash: "5f9b48bf7c", Revision: "2"}
	if got := backgroundAnalysisRunName("podinfo", cleared); got != "podinfo-5f9b48bf7c-2" {
		t.Errorf("got %q, want the reconstructed name when the transient field is gone", got)
	}

	if got := backgroundAnalysisRunName("podinfo", execute.Status{}); got != "" {
		t.Errorf("got %q, want empty when nothing is known", got)
	}
}
