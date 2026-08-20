package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

var fastWeights = []int{5, 100}
var guardedWeights = []int{1, 5, 25, 50, 100}

// fastLaneStarted builds the A2.1/A2.2 release (fast lane, weights 5, 100)
// and attaches the granted `start` entry A2.2 would have recorded, so
// tests here can begin partway through -- already at gate 1, weight 5 --
// without re-driving startRollout.
func fastLaneStarted(t *testing.T) *release.Release {
	return fastLaneStartedForEnvironment(t, "production")
}

func fastLaneStartedForEnvironment(t *testing.T, environment string) *release.Release {
	t.Helper()
	cfg := demoProject()
	cfg.Release.Environment = environment
	rel := inspectCase{
		id:      "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:      3,
		project: cfg,
		github:  fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:    fakeGHCR{digest: safeDigest},
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
	return persistAndReload(t, started)
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
	return persistAndReload(t, started)
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

func atGateWithBackgroundAnalysisRunning(weights []int, weight int) string {
	return fmt.Sprintf(`{"status":{"phase":"Paused","pauseConditions":[{"reason":"CanaryPauseStep"}],"currentStepIndex":%d,`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-4","status":"Running"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, stepIndexOf(weights, weight), stepsJSON(weights))
}

// progressingStatus is a Rollout still moving, mid-step.
func progressingStatus(weights []int) string {
	return fmt.Sprintf(`{"status":{"phase":"Progressing"},"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, stepsJSON(weights))
}

const analysisRunJSON = `{"status":{"phase":"Successful","metricResults":[{"name":"request-success-rate","count":3,` +
	`"successful":3,"measurements":[{"value":"[1]"},{"value":"[1]"},{"value":"[1]"}]}]},` +
	`"spec":{"metrics":[{"name":"request-success-rate","successCondition":"len(result) > 0 && result[0] >= 0.99"}]}}`

// failingAnalysisRunJSON is A3.4's own AnalysisRun: 2 of 3 measurements
// below threshold, failureLimit 1 tripped -- the deliberately failing
// analysis that ends a rollout with Argo's own abort, not SafeLane's.
const failingAnalysisRunJSON = `{"status":{"phase":"Failed","metricResults":[{"name":"request-success-rate","count":3,` +
	`"successful":1,"measurements":[{"value":"[1]"},{"value":"[0]"},{"value":"[0.71]"}]}]},` +
	`"spec":{"metrics":[{"name":"request-success-rate","successCondition":"len(result) > 0 && result[0] >= 0.99","failureLimit":1}]}}`

func TestRunRolloutAdvance_ZeroFlagsUsesControllerIdentityFromProject(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "podinfo")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", "https://github.com/AndrewMaged814/podinfo.git"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Setenv("SAFELANE_HOME", home)

	configDir := filepath.Join(home, "apps", "podinfo")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projectFile := filepath.Join(configDir, "project.yml")
	if err := os.WriteFile(projectFile, project.DefaultYAML(
		"podinfo", "AndrewMaged814/podinfo", "master", "ghcr.io/andrewmaged814/podinfo",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	rel := fastLaneStarted(t)
	storeDir := filepath.Join(configDir, "releases")
	if err := (&store.FileStore{Dir: storeDir}).Save(rel); err != nil {
		t.Fatal(err)
	}

	q := &queueRunner{}
	q.enqueue(atGateStatus(fastWeights, 5), nil)
	q.enqueue("", nil)
	q.enqueue(`{"status":{"phase":"Healthy","stableRS":"abc123","currentPodHash":"abc123",`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-2","status":"Successful"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil)
	q.enqueue(analysisRunJSON, nil)

	originalNewExecutor := newExecutor
	t.Cleanup(func() { newExecutor = originalNewExecutor })
	var executorConfig execute.Config
	newExecutor = func(cfg execute.Config) *execute.Executor {
		executorConfig = cfg
		ex := execute.New(cfg)
		ex.Run = q.run
		ex.Sleep = func(time.Duration) {}
		return ex
	}

	var stdout, stderr bytes.Buffer
	code := runRolloutAdvance(context.Background(), []string{string(rel.ID)}, &stdout, &stderr, root, "")
	if code != ExitOK {
		t.Fatalf("runRolloutAdvance exit = %d, want %d\nstderr: %s", code, ExitOK, stderr.String())
	}

	wantKubeconfig := filepath.Join(configDir, "controller.kubeconfig")
	if executorConfig.ControllerKubeconfig != wantKubeconfig || executorConfig.ControllerContext != "safelane-controller" {
		t.Fatalf("controller identity = %q / %q, want %q / safelane-controller",
			executorConfig.ControllerKubeconfig, executorConfig.ControllerContext, wantKubeconfig)
	}
	wantPromote := "argo rollouts promote podinfo -n podinfo --kubeconfig " + wantKubeconfig + " --context safelane-controller"
	if got := strings.Join(q.calls[1], " "); got != wantPromote {
		t.Fatalf("promote call = %q, want %q", got, wantPromote)
	}
}

// degradedAbortStatus is Argo's own status once a background analysis
// trips its failureLimit: Abort is set (classifyState reports this as
// StateAborted, ahead of Degraded, per its own priority order), and the
// background AnalysisRun's real name is still on the Rollout's status.
func degradedAbortStatus(weights []int, revision string) string {
	return fmt.Sprintf(`{"status":{"phase":"Degraded","abort":true,`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-%s","status":"Failed"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, revision, stepsJSON(weights))
}

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

func TestRolloutAdvance_FinalWeightWaitsForRunningBackgroundAnalysis(t *testing.T) {
	rel := fastLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateWithBackgroundAnalysisRunning(fastWeights, 5), nil) // initial GetStatus
	q.enqueue(atGateWithBackgroundAnalysisRunning(fastWeights, 5), nil) // analysis wait: still running
	q.enqueue(fmt.Sprintf(`{"status":{"phase":"Paused","pauseConditions":[{"reason":"CanaryPauseStep"}],"currentStepIndex":%d,`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-2","status":"Successful"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[%s]}}}}`, stepIndexOf(fastWeights, 5), stepsJSON(fastWeights)), nil)
	q.enqueue("", nil) // promote only after analysis settled
	q.enqueue(`{"status":{"phase":"Healthy","stableRS":"abc123","currentPodHash":"abc123",`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-2","status":"Successful"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil)
	q.enqueue(analysisRunJSON, nil)

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	if !strings.Contains(result.Render(), "(3/3 measurements)") {
		t.Fatalf("completion did not report the settled analysis:\n%s", result.Render())
	}
	if got := strings.Join(q.calls[3], " "); !strings.HasPrefix(got, "argo rollouts promote ") {
		t.Fatalf("call after analysis settled = %q, want promote", got)
	}
}

// TestRolloutAdvance_ToCompletion_DoesNotInventAnAnalysisRun verifies that a
// healthy Rollout with no analysis status is still a valid completion. Pod
// hash + revision are not proof that an AnalysisRun exists; manufacturing a
// name from them caused the final promotion to fail on a harmless 404.
func TestRolloutAdvance_ToCompletion_DoesNotInventAnAnalysisRun(t *testing.T) {
	rel := fastLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(fastWeights, 5), nil) // GetStatus before deciding
	q.enqueue("", nil)                           // promote
	q.enqueue(`{"metadata":{"annotations":{"rollout.argoproj.io/revision":"2"}},`+
		`"status":{"phase":"Healthy","stableRS":"5f9b48bf7c","currentPodHash":"5f9b48bf7c"},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil) // no currentBackgroundAnalysisRunStatus at all

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	if strings.Contains(result.Render(), "AnalysisRun") {
		t.Fatalf("completion invented analysis evidence:\n%s", result.Render())
	}
	if len(q.calls) != 3 {
		t.Fatalf("kubectl calls = %d, want status/promote/status only", len(q.calls))
	}
}

func TestRolloutAdvance_ToCompletion_ToleratesMissingAnalysisRun(t *testing.T) {
	rel := fastLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(fastWeights, 5), nil)
	q.enqueue("", nil)
	q.enqueue(`{"metadata":{"annotations":{"rollout.argoproj.io/revision":"2"}},`+
		`"status":{"phase":"Healthy","stableRS":"5f9b48bf7c","currentPodHash":"5f9b48bf7c",`+
		`"canary":{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-5f9b48bf7c-2","status":"Successful"}}},`+
		`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`, nil)
	q.enqueue("", errors.New(`analysisruns.argoproj.io "podinfo-5f9b48bf7c-2" not found`))

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	if result.outcome != outcomePromotedComplete {
		t.Fatalf("outcome = %v, want complete", result.outcome)
	}
	if strings.Contains(result.Render(), "AnalysisRun") {
		t.Fatalf("missing AnalysisRun was reported as present:\n%s", result.Render())
	}
}

func TestRolloutAdvance_OverWideRequest_MatchesA33(t *testing.T) {
	rel := guardedLaneStarted(t)
	to := 100

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 1), nil)

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", &to, time.Minute, time.Now)
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

	// A refusal belongs in the record too (Appendix C2's own example
	// shows one): result.release carries it even though the call itself
	// returned an error, so the caller can still persist it.
	if result.release == nil {
		t.Fatal("a refusal with a named requested weight must still return a release to persist")
	}
	entries := result.release.Execution()
	last := entries[len(entries)-1]
	if last.Verb != release.VerbAdvance || last.RequestedWeight != 100 ||
		last.Outcome != release.OutcomeRefused || last.ReasonCode != "transition_exceeds_envelope" {
		t.Errorf("last entry = %+v, want a refused advance to weight 100 (transition_exceeds_envelope)", last)
	}
}

func TestParseRolloutAdvanceFlagsAcceptsAppendixIDFirstTo(t *testing.T) {
	var stderr bytes.Buffer
	f, id, err := parseRolloutAdvanceFlags(
		[]string{"rel_01M0F3QD9NBV6JKC2WS8XA7TR4", "--to", "100"},
		&stderr,
		".safelane/releases",
	)
	if err != nil {
		t.Fatalf("parse documented A3.3 command: %v\n%s", err, stderr.String())
	}
	if id != "rel_01M0F3QD9NBV6JKC2WS8XA7TR4" {
		t.Fatalf("id = %q", id)
	}
	if f.to == nil || *f.to != 100 {
		t.Fatalf("to = %v, want 100", f.to)
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

// TestRolloutAdvance_Idempotent_MatchesN12 is ticket 11's idempotency
// contract: an agent that promoted 1 -> 5 successfully never saw the
// response and retries bare `advance` with no idea it already worked.
// This release's own record still shows only the start grant (weight 1);
// Argo, live, is already at gate 2 (weight 5) -- exactly the transition
// the earlier call must have made. The retry must catch up to that
// reality and stop there: one recorded grant, no second promotion.
func TestRolloutAdvance_Idempotent_MatchesN12(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 5), nil) // GetStatus before deciding: Argo is already at 5

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	now := time.Date(2026, 8, 20, 14, 26, 48, 0, time.UTC)

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	if result.outcome != outcomeNoChange {
		t.Fatalf("outcome = %v, want outcomeNoChange", result.outcome)
	}
	assertGolden(t, "n12-advance-idempotent.txt", result.Render())

	entries := result.release.Execution()
	if len(entries) != 2 {
		t.Fatalf("execution history = %+v, want 2 entries (start, catch-up advance)", entries)
	}
	last := entries[1]
	if last.Verb != release.VerbAdvance || last.RequestedWeight != 5 || last.Outcome != release.OutcomeGranted {
		t.Errorf("last entry = %+v, want a granted catch-up advance to weight 5", last)
	}

	for _, call := range q.calls {
		if call[0] != "get" {
			t.Errorf("a no-change retry must never do anything but read status, got: %v", call)
		}
	}
}

// TestRolloutAdvance_ArgoAborts_MatchesA34 is A3.4's payoff: a
// deliberately failing analysis trips its own failureLimit and Argo
// Rollouts aborts the rollout on its own, mid-promotion. This is not a
// SafeLane refusal -- the output must say plainly that Argo did this, not
// SafeLane -- and the execution log must still name the AnalysisRun and
// the measurement that failed.
func TestRolloutAdvance_ArgoAborts_MatchesA34(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue(atGateStatus(guardedWeights, 1), nil) // GetStatus before deciding
	q.enqueue("", nil)                              // promote
	q.enqueue(atGateWithBackgroundAnalysisRunning(guardedWeights, 5), nil)
	q.enqueue(degradedAbortStatus(guardedWeights, "4"), nil)
	q.enqueue(failingAnalysisRunJSON, nil) // GetAnalysisRun

	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}

	result, err := advanceRollout(context.Background(), rel, ex, "podinfo", nil, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("advanceRollout: %v", err)
	}
	if result.outcome != outcomeArgoAborted {
		t.Fatalf("outcome = %v, want outcomeArgoAborted", result.outcome)
	}
	assertGolden(t, "a3-4-argo-abort.txt", result.Render())

	entries := result.release.Execution()
	last := entries[len(entries)-1]
	if last.Verb != release.VerbArgoAbort || last.Outcome != release.OutcomeAborted || last.ReasonCode != "analysis_failed" {
		t.Errorf("last entry = %+v, want a recorded argo_abort/aborted/analysis_failed", last)
	}
	if last.Analysis == "" {
		t.Error("an Argo-own abort must name the AnalysisRun it observed")
	}
	if last.Detail == "" {
		t.Error("an Argo-own abort must record the measurement that failed")
	}

	for _, call := range q.calls {
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

func TestBackgroundAnalysisRunName_UsesOnlyTheLiveField(t *testing.T) {
	live := execute.Status{AnalysisRunName: "podinfo-5f9b48bf7c-2"}
	if got := backgroundAnalysisRunName("podinfo", live); got != "podinfo-5f9b48bf7c-2" {
		t.Errorf("got %q, want the live field verbatim", got)
	}

	cleared := execute.Status{CurrentPodHash: "5f9b48bf7c", Revision: "2"}
	if got := backgroundAnalysisRunName("podinfo", cleared); got != "" {
		t.Errorf("got %q, want empty when Argo cleared the status reference", got)
	}

	if got := backgroundAnalysisRunName("podinfo", execute.Status{}); got != "" {
		t.Errorf("got %q, want empty when nothing is known", got)
	}
}
