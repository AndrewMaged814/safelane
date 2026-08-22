package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

// buildRelease runs the same Submit pass inspectCase.run uses, but returns
// the persisted Release rather than the `release plan` report -- the
// object `rollout start` actually operates on.
func (c inspectCase) buildRelease(t *testing.T) *release.Release {
	t.Helper()
	cfg := c.project
	if cfg.Application == "" {
		cfg = demoProject()
	}
	now := c.now
	if now.IsZero() {
		now = time.Date(2026, 8, 20, 14, 21, 44, 0, time.UTC)
	}

	deps := orchestrate.Deps{
		GitHub:      c.github,
		GHCR:        c.ghcr,
		ChangeFacts: c.facts,
		Model:       fakeModel{verdict: c.model},
		Template:    demoTemplate(t),
		Store:       discardStore{},
		Project:     cfg,
		Now:         func() time.Time { return now },
		NewID:       releaseID(t, c.id),
	}
	result, err := orchestrate.Submit(context.Background(), release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    "AndrewMaged814/safelane-demo-api",
		PullRequest:   c.pr,
		Environment:   cfg.Release.Environment,
	}, deps)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return result.Release
}

func persistAndReload(t *testing.T, rel *release.Release) *release.Release {
	t.Helper()
	s := &store.FileStore{Dir: t.TempDir()}
	if err := s.Save(rel); err != nil {
		t.Fatalf("persist release: %v", err)
	}
	reloaded, err := s.Load(rel.ID)
	if err != nil {
		t.Fatalf("reload release: %v", err)
	}
	return reloaded
}

// queueRunner is Appendix D's fake command factory: every kubectl call
// `rollout start` makes goes through it, so these tests touch no cluster.
type queueRunner struct {
	calls     [][]string
	responses []string
	errs      []error
	i         int
}

func (q *queueRunner) run(_ context.Context, args []string, _ []byte) ([]byte, error) {
	q.calls = append(q.calls, append([]string{}, args...))
	if q.i >= len(q.responses) {
		return nil, errors.New("queueRunner: no more canned responses")
	}
	out, err := []byte(q.responses[q.i]), q.errs[q.i]
	q.i++
	return out, err
}

func (q *queueRunner) enqueue(out string, err error) {
	q.responses = append(q.responses, out)
	q.errs = append(q.errs, err)
}

// safeApplyOutput and riskyApplyOutput are what `kubectl apply -f -`
// reports for the bundle's five resources, in bundle order: only the
// Rollout's spec is release-specific, so it is the only one that ever
// reports anything but unchanged.
const applyUnchangedFour = "service/safelane-demo-api-stable unchanged\n" +
	"service/safelane-demo-api-canary unchanged\n" +
	"analysistemplate.argoproj.io/safelane-demo-api-success-rate unchanged\n" +
	"ingress.networking.k8s.io/safelane-demo-api unchanged\n"

func progressingThenAtGate(steps string) (progressing, atGate string) {
	progressing = `{"status":{"phase":"Progressing"},"spec":{"strategy":{"canary":{"steps":[` + steps + `]}}}}`
	atGate = `{"status":{"phase":"Paused","pauseConditions":[{"reason":"CanaryPauseStep"}],"currentStepIndex":0},` +
		`"spec":{"strategy":{"canary":{"steps":[` + steps + `]}}}}`
	return progressing, atGate
}

func TestRolloutStart_SafeChange_MatchesA22(t *testing.T) {
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

	if lane, ok := rel.Assessment(); !ok || lane.Lane != "fast" {
		t.Fatalf("test setup: want lane fast, got %+v", lane)
	}

	q := &queueRunner{}
	q.enqueue(applyUnchangedFour+"rollout.argoproj.io/safelane-demo-api configured\n", nil)
	progressing, atGate := progressingThenAtGate(`{"setWeight":5},{"pause":{}}`)
	q.enqueue(progressing, nil)
	q.enqueue(atGate, nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}
	grantedAt := time.Date(2026, 8, 20, 14, 21, 44, 0, time.UTC)

	result, err := startRollout(context.Background(), rel, ex, time.Minute, func() time.Time { return grantedAt })
	if err != nil {
		t.Fatalf("startRollout: %v", err)
	}
	assertGolden(t, "a2-2-start-safe.txt", result.Render())

	entries := result.release.Execution()
	if len(entries) != 1 || entries[0].Verb != release.VerbStart || entries[0].RequestedWeight != 50 || entries[0].Outcome != release.OutcomeGranted {
		t.Errorf("execution history = %+v, want one granted start at weight 50", entries)
	}
	for _, call := range q.calls {
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

func TestRolloutStart_RiskyChange_MatchesA32(t *testing.T) {
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

	if a, ok := rel.Assessment(); !ok || a.Lane != "guarded" {
		t.Fatalf("test setup: want lane guarded, got %+v", a)
	}
	rel = persistAndReload(t, rel)

	q := &queueRunner{}
	q.enqueue(applyUnchangedFour+"rollout.argoproj.io/safelane-demo-api configured\n", nil)
	progressing, atGate := progressingThenAtGate(`{"setWeight":1},{"pause":{}},{"setWeight":5},{"pause":{}},{"setWeight":25},{"pause":{}},{"setWeight":50},{"pause":{}}`)
	q.enqueue(progressing, nil)
	q.enqueue(atGate, nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}
	grantedAt := time.Date(2026, 8, 20, 14, 26, 3, 0, time.UTC)

	result, err := startRollout(context.Background(), rel, ex, time.Minute, func() time.Time { return grantedAt })
	if err != nil {
		t.Fatalf("startRollout: %v", err)
	}
	assertGolden(t, "a3-2-start-risky.txt", result.Render())

	entries := result.release.Execution()
	if len(entries) != 1 || entries[0].RequestedWeight != 25 {
		t.Errorf("execution history = %+v, want one granted start at weight 25", entries)
	}
}

func TestRolloutStart_IgnoresAnAbortFromThePreviousObservedGeneration(t *testing.T) {
	rel := inspectCase{
		id:     "rel_01M0F3QD9NBV6JKC2WS8XA7TR4",
		pr:     4,
		github: fakeGitHub{facts: mergedFacts(4, riskyMergeSHA)},
		ghcr:   fakeGHCR{digest: riskyDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files:          []assess.FileChange{{Path: "pkg/api/http/info.go", Additions: 9, Deletions: 2}},
			TotalAdditions: 9, TotalDeletions: 2, MergeCommitSHA: riskyMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskHigh, Rationale: riskyRationale, Available: true, Assessor: "claude"},
	}.buildRelease(t)

	steps := `{"setWeight":1},{"pause":{}},{"setWeight":5},{"pause":{}}`
	q := &queueRunner{}
	q.enqueue(applyUnchangedFour+"rollout.argoproj.io/safelane-demo-api configured\n", nil)
	// The apply advanced metadata.generation to 8, but Argo's first read still
	// carries generation 7's abort. This is the live sequence that caused a new
	// release to inherit the preceding release's terminal state.
	q.enqueue(`{"metadata":{"generation":8},"status":{"observedGeneration":7,"phase":"Degraded","abort":true},`+
		`"spec":{"strategy":{"canary":{"steps":[`+steps+`]}}}}`, nil)
	q.enqueue(`{"metadata":{"generation":8},"status":{"observedGeneration":8,"phase":"Progressing"},`+
		`"spec":{"strategy":{"canary":{"steps":[`+steps+`]}}}}`, nil)
	q.enqueue(`{"metadata":{"generation":8},"status":{"observedGeneration":8,"phase":"Paused",`+
		`"pauseConditions":[{"reason":"CanaryPauseStep"}],"currentStepIndex":0},`+
		`"spec":{"strategy":{"canary":{"steps":[`+steps+`]}}}}`, nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	ex.Sleep = func(time.Duration) {}
	grantedAt := time.Date(2026, 8, 20, 14, 26, 3, 0, time.UTC)

	result, err := startRollout(context.Background(), rel, ex, time.Minute, func() time.Time { return grantedAt })
	if err != nil {
		t.Fatalf("startRollout treated the previous generation's abort as this release's outcome: %v", err)
	}
	entries := result.release.Execution()
	if len(entries) != 1 || entries[0].Verb != release.VerbStart || entries[0].Outcome != release.OutcomeGranted {
		t.Fatalf("execution history = %+v, want one granted start", entries)
	}
}

func TestRolloutStart_FreshAbortIsPersistedAndReportedAsPostApplyFailure(t *testing.T) {
	rel := inspectCase{
		id:     "rel_01M0F3QD9NBV6JKC2WS8XA7TR4",
		pr:     4,
		github: fakeGitHub{facts: mergedFacts(4, riskyMergeSHA)},
		ghcr:   fakeGHCR{digest: riskyDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files:          []assess.FileChange{{Path: "pkg/api/http/info.go", Additions: 9, Deletions: 2}},
			TotalAdditions: 9, TotalDeletions: 2, MergeCommitSHA: riskyMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskHigh, Rationale: riskyRationale, Available: true, Assessor: "claude"},
	}.buildRelease(t)
	projectFile, storeDir := statusRuntime(t, rel)

	q := &queueRunner{}
	q.enqueue(`{"metadata":{"generation":7},"status":{"observedGeneration":7,"phase":"Healthy"}}`, nil)
	q.enqueue(`{"status":{"userInfo":{"username":"system:serviceaccount:safelane-demo-api:safelane-controller"}}}`, nil)
	q.enqueue("yes\n", nil)
	q.enqueue(`{"status":{"userInfo":{"username":"system:serviceaccount:safelane-demo-api:safelane-caller"}}}`, nil)
	q.enqueue("yes\n", nil)
	q.enqueue("no\n", nil)
	q.enqueue("rollout.argoproj.io/safelane-demo-api annotated\n", nil)
	q.enqueue(applyUnchangedFour+"rollout.argoproj.io/safelane-demo-api configured\n", nil)
	q.enqueue(`{"metadata":{"generation":8,"annotations":{"safelane.dev/release-id":"`+string(rel.ID)+`"}},"spec":{"template":{"spec":{"containers":[{"image":"safelane-demo-api@`+riskyDigest+`"}]}}},"status":{"observedGeneration":8,"phase":"Degraded",`+
		`"abort":true,"message":"Rollout aborted update to revision 4"}}`, nil)
	originalNewExecutor := newExecutor
	t.Cleanup(func() { newExecutor = originalNewExecutor })
	newExecutor = func(cfg execute.Config) *execute.Executor {
		ex := execute.New(cfg)
		ex.Run = q.run
		ex.Sleep = func(time.Duration) {}
		return ex
	}

	var stdout, stderr bytes.Buffer
	code := runRolloutStart(context.Background(), []string{
		"--project", projectFile, "--store-dir", storeDir, string(rel.ID),
	}, &stdout, &stderr, ".", "")
	if code != ExitFail {
		t.Fatalf("exit = %d, want ExitFail\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{"Applied the Rendered Manifest Bundle", "Argo Rollouts: aborted", "failed start was recorded"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout.String())
		}
	}
	if !strings.Contains(stderr.String(), "failed after applying the Rendered Manifest Bundle") {
		t.Errorf("stderr described a post-apply failure as a refusal:\n%s", stderr.String())
	}

	stored, err := (&store.FileStore{Dir: storeDir}).Load(rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	entries := stored.Execution()
	if len(entries) != 1 || entries[0].Verb != release.VerbStart || entries[0].Outcome != release.OutcomeAborted ||
		entries[0].ReasonCode != "rollout_aborted_before_first_gate" {
		t.Fatalf("persisted execution = %+v", entries)
	}
}

// TestRolloutStart_Ineligible_MatchesN10 drives the whole command, not
// just startRollout: an ineligible release is refused before any kubectl
// call is even considered, so there is nothing to fake.
func TestRolloutStart_Ineligible_MatchesN10(t *testing.T) {
	facts := mergedFacts(3, safeMergeSHA)
	facts.CheckRuns[0].Conclusion = "failure"

	rel := inspectCase{
		id:     "rel_01M03FJT6BQ3SZ4ZRZZVQJ99T1",
		pr:     3,
		github: fakeGitHub{facts: facts},
		ghcr:   fakeGHCR{digest: safeDigest},
	}.buildRelease(t)

	if rel.Eligibility().Status() == release.EligibilityEligible {
		t.Fatal("test setup: want an ineligible release")
	}

	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	if err := (&store.FileStore{Dir: storeDir}).Save(rel); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runRolloutStart(context.Background(), []string{"--store-dir", storeDir, string(rel.ID)}, &stdout, &stderr, dir, storeDir)

	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d (stdout: %s, stderr: %s)", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("a refused start must apply nothing, got stdout:\n%s", stdout.String())
	}
	assertGolden(t, "n10-start-ineligible.txt", stderr.String())
}

func TestRolloutStart_MissingBinary_IsHumanReadableNotAStackTrace(t *testing.T) {
	rel := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files: []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}}, MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskLow, Available: true, Assessor: "claude"},
	}.buildRelease(t)

	q := &queueRunner{}
	q.enqueue("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})
	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run

	_, err := startRollout(context.Background(), rel, ex, time.Minute, time.Now)
	if err == nil {
		t.Fatal("want an error when kubectl is missing")
	}
	var rerr *release.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %v (%T), want a *release.Error, never a raw stack trace", err, err)
	}
	if rerr.Code != "kubectl_missing" {
		t.Errorf("code = %q, want kubectl_missing", rerr.Code)
	}
}

func TestRolloutStart_GateTimeout_NeverRetries(t *testing.T) {
	rel := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files: []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}}, MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskLow, Available: true, Assessor: "claude"},
	}.buildRelease(t)

	q := &queueRunner{}
	q.enqueue(applyUnchangedFour+"rollout.argoproj.io/safelane-demo-api configured\n", nil)
	progressing, _ := progressingThenAtGate(`{"setWeight":5},{"pause":{}}`)
	for i := 0; i < 5; i++ {
		q.enqueue(progressing, nil)
	}

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	current := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	ex.Now = func() time.Time { return current }
	ex.Sleep = func(d time.Duration) { current = current.Add(d) }
	ex.PollInterval = 2 * time.Second

	_, err := startRollout(context.Background(), rel, ex, 5*time.Second, func() time.Time { return current })
	if !errors.Is(err, execute.ErrGateTimeout) {
		t.Fatalf("err = %v, want ErrGateTimeout", err)
	}
	for _, call := range q.calls[1:] { // calls[0] is the one apply
		if call[0] != "get" {
			t.Errorf("a timed-out wait must never do anything but poll, got: %v", call)
		}
	}
}
