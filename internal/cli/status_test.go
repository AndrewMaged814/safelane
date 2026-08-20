package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

func statusRuntime(t *testing.T, r *release.Release) (projectFile, storeDir string) {
	t.Helper()
	dir := t.TempDir()
	projectFile = filepath.Join(dir, "project.yml")
	if err := os.WriteFile(projectFile, project.DefaultYAML(
		"podinfo", "AndrewMaged814/podinfo", "master", "ghcr.io/andrewmaged814/podinfo",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	storeDir = filepath.Join(dir, "releases")
	if err := (&store.FileStore{Dir: storeDir}).Save(r); err != nil {
		t.Fatal(err)
	}
	return projectFile, storeDir
}

func TestStatusJSON_AllSevenExecutorStatesAreReachable(t *testing.T) {
	r := fastLaneStarted(t)
	projectFile, storeDir := statusRuntime(t, r)
	cases := map[execute.State]string{
		execute.StateNotStarted:  `{}`,
		execute.StateProgressing: `{"status":{"phase":"Progressing"}}`,
		execute.StateAnalysing: `{"status":{"phase":"Progressing","canary":` +
			`{"currentStepAnalysisRunStatus":{"status":"Running"}}}}`,
		execute.StateAtGate: `{"status":{"phase":"Paused","pauseConditions":[{}],"currentStepIndex":0},` +
			`"spec":{"strategy":{"canary":{"steps":[{"setWeight":5},{"pause":{}}]}}}}`,
		execute.StateComplete: `{"status":{"phase":"Healthy","stableRS":"abc","currentPodHash":"abc"}}`,
		execute.StateDegraded: `{ "status":{"phase":"Degraded"}}`,
		execute.StateAborted:  `{ "status":{"abort":true,"phase":"Progressing"}}`,
	}

	originalNewExecutor := newExecutor
	t.Cleanup(func() { newExecutor = originalNewExecutor })
	for wantState, raw := range cases {
		t.Run(string(wantState), func(t *testing.T) {
			q := &queueRunner{}
			q.enqueue(raw, nil)
			newExecutor = func(cfg execute.Config) *execute.Executor {
				ex := execute.New(cfg)
				ex.Run = q.run
				return ex
			}
			var stdout, stderr bytes.Buffer
			code := runStatus(context.Background(), []string{
				string(r.ID), "--json", "--project", projectFile, "--store-dir", storeDir,
			}, &stdout, &stderr, ".", "", time.Now)
			if code != ExitOK {
				t.Fatalf("status exit = %d, stderr: %s", code, stderr.String())
			}
			var got statusReport
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("JSON: %v\n%s", err, stdout.String())
			}
			if got.State != wantState {
				t.Fatalf("state = %q, want %q", got.State, wantState)
			}
			if len(q.calls) != 1 || strings.Join(q.calls[0], " ") != "get rollout podinfo -n podinfo -o json" {
				t.Fatalf("kubectl calls = %v", q.calls)
			}
		})
	}
}

func TestStatusJSONReportsLaneRiskEnvelopeAndGate(t *testing.T) {
	r := fastLaneStarted(t)
	report := buildStatusReport(r, execute.Status{State: execute.StateAtGate, CurrentWeight: 5, Gate: 1})
	if report.Lane != "fast" || report.Risk != "low" || report.Weight != 5 || report.Gate != 1 || report.GateCount != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.NextAllowedWeight == nil || *report.NextAllowedWeight != 100 {
		t.Fatalf("next_allowed_weight = %v, want 100", report.NextAllowedWeight)
	}
}

func TestStatusReportsWhetherLiveStateBelongsToTheRelease(t *testing.T) {
	r := fastLaneStarted(t)
	bundle, ok := r.Bundle()
	if !ok {
		t.Fatal("test release has no bundle")
	}

	matched := buildStatusReport(r, execute.Status{
		State: execute.StateAtGate, ImageDigest: bundle.PinnedDigest(),
		Generation: 8, ObservedGeneration: 8,
	})
	if matched.ReleaseMatch == nil || !*matched.ReleaseMatch {
		t.Fatalf("release_match = %v, want true", matched.ReleaseMatch)
	}

	stale := buildStatusReport(r, execute.Status{
		State: execute.StateAborted, ImageDigest: bundle.PinnedDigest(),
		Generation: 8, ObservedGeneration: 7,
	})
	if stale.ReleaseMatch == nil || *stale.ReleaseMatch {
		t.Fatalf("release_match = %v, want false for an unobserved generation", stale.ReleaseMatch)
	}
}

func TestStatusJSONIncludesFailedAnalysisMeasurement(t *testing.T) {
	r := fastLaneStarted(t)
	projectFile, storeDir := statusRuntime(t, r)
	q := &queueRunner{}
	q.enqueue(`{"status":{"phase":"Degraded","abort":true,"canary":`+
		`{"currentBackgroundAnalysisRunStatus":{"name":"podinfo-abc-2","status":"Failed"}}}}`, nil)
	q.enqueue(`{"status":{"phase":"Failed","metricResults":[{"name":"request-success-rate",`+
		`"count":2,"successful":0,"measurements":[{"value":"[0.74]"}]}]},`+
		`"spec":{"metrics":[{"name":"request-success-rate",`+
		`"successCondition":"len(result) > 0 && result[0] >= 0.99","failureLimit":1}]}}`, nil)

	originalNewExecutor := newExecutor
	t.Cleanup(func() { newExecutor = originalNewExecutor })
	newExecutor = func(cfg execute.Config) *execute.Executor {
		ex := execute.New(cfg)
		ex.Run = q.run
		return ex
	}

	var stdout, stderr bytes.Buffer
	code := runStatus(context.Background(), []string{
		string(r.ID), "--json", "--project", projectFile, "--store-dir", storeDir,
	}, &stdout, &stderr, ".", "", time.Now)
	if code != ExitOK {
		t.Fatalf("status exit = %d, stderr: %s", code, stderr.String())
	}
	var got statusReport
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AnalysisRun != "podinfo-abc-2" || got.AnalysisPhase != "Failed" ||
		got.AnalysisMetric != "request-success-rate" || got.AnalysisMeasured == nil || *got.AnalysisMeasured != 0.74 ||
		got.AnalysisCondition != ">= 0.99" || got.AnalysisCount != 2 || got.AnalysisFailureLimit != 1 {
		t.Fatalf("analysis diagnostics = %+v", got)
	}
}

func TestStatusListUsesStoredRecordOnlyAndFormatsStalledDuration(t *testing.T) {
	r := fastLaneStarted(t)
	projectFile, storeDir := statusRuntime(t, r)
	originalNewExecutor := newExecutor
	t.Cleanup(func() { newExecutor = originalNewExecutor })
	newExecutor = func(execute.Config) *execute.Executor {
		panic("the no-argument status listing must not read the cluster")
	}

	var stdout, stderr bytes.Buffer
	now := time.Date(2026, 8, 20, 15, 2, 59, 0, time.UTC)
	code := runStatus(context.Background(), []string{
		"--project", projectFile, "--store-dir", storeDir,
	}, &stdout, &stderr, ".", "", func() time.Time { return now })
	if code != ExitOK {
		t.Fatalf("status exit = %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"1 open release", string(r.ID), "podinfo/production", "fast", "at_gate", "weight 5", "stalled 41m"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "41m0s") {
		t.Fatalf("listing used time.Duration.String():\n%s", out)
	}
}

func TestStatusListMatchesN14GoldenFragment(t *testing.T) {
	guarded := guardedLaneStarted(t)
	var err error
	guarded, err = guarded.WithExecution(release.ExecutionEntry{
		At: time.Date(2026, 8, 20, 16, 52, 44, 0, time.UTC), Verb: release.VerbAdvance,
		RequestedWeight: 5, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatal(err)
	}
	fast := fastLaneStartedForEnvironment(t, "staging")
	now := time.Date(2026, 8, 20, 17, 33, 44, 0, time.UTC)
	assertGoldenFragment(t, "n14-open-statuses.txt", renderOpenStatuses([]*release.Release{guarded, fast}, now))
}

func TestStoredOpenStatusExcludesCompleteAndAbortedReleases(t *testing.T) {
	started := fastLaneStarted(t)
	complete, err := started.WithExecution(release.ExecutionEntry{
		At: time.Now(), Verb: release.VerbAdvance, RequestedWeight: 100, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := started.WithExecution(release.ExecutionEntry{
		At: time.Now(), Verb: release.VerbArgoAbort, Outcome: release.OutcomeAborted, ReasonCode: "analysis_failed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := storedOpenStatus(complete); ok {
		t.Error("complete release was listed as open")
	}
	if _, ok := storedOpenStatus(aborted); ok {
		t.Error("aborted release was listed as open")
	}
}

func TestFormatStalledTrimsZeroUnits(t *testing.T) {
	cases := map[time.Duration]string{
		41*time.Minute + 59*time.Second: "41m",
		3*time.Hour + 12*time.Minute:    "3h12m",
		3 * time.Hour:                   "3h",
	}
	for in, want := range cases {
		if got := formatStalled(in); got != want {
			t.Errorf("formatStalled(%s) = %q, want %q", in, got, want)
		}
	}
}
