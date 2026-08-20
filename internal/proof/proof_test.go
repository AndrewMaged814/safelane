package proof

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

const (
	fixtureDigest    = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	fixtureMergeSHA  = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	fixtureReleaseID = "rel_00000000000000000000000000"
)

type fakeFetcher struct{ facts github.Facts }

func (f fakeFetcher) FetchPullRequestFacts(context.Context, string, string, int) (github.Facts, error) {
	return f.facts, nil
}

type fakeResolver struct{ digest string }

func (f fakeResolver) ResolveDigest(context.Context, release.ImageReference) (string, error) {
	return f.digest, nil
}
func (f fakeResolver) ResolveTag(context.Context, string, string) (string, error) {
	return f.digest, nil
}

type fakeFacts struct{ value assess.Facts }

func (f fakeFacts) FetchChangeFacts(context.Context, string, string, int) (assess.Facts, error) {
	return f.value, nil
}

type fakeAssessor struct{ verdict assess.Verdict }

func (f fakeAssessor) Name() string { return f.verdict.Assessor }

func (f fakeAssessor) Assess(context.Context, assess.Facts) (assess.Verdict, error) {
	return f.verdict, nil
}

func fromJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("fixture JSON: %v", err)
	}
	return value
}

func completeRelease(t *testing.T) *release.Release {
	t.Helper()
	tmpl, err := render.LoadDir(filepath.Join("..", "render", "testdata", "release-template"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := release.ParseReleaseID(fixtureReleaseID)
	facts := github.Facts{
		Repository: "AndrewMaged814/podinfo", Number: 4, URL: "https://github.com/AndrewMaged814/podinfo/pull/4",
		Merged: true, MergedAt: time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC), BaseRef: "master",
		MergeCommitSHA: fixtureMergeSHA, AuthorLogin: "AndrewMaged814",
		CheckRuns: []github.CheckRun{{Name: "build-and-push", Conclusion: "success", HeadSHA: fixtureMergeSHA}},
	}
	dir := t.TempDir()
	fs := &store.FileStore{Dir: dir}
	d := orchestrate.Deps{
		GitHub: fakeFetcher{facts}, GHCR: fakeResolver{fixtureDigest}, Template: tmpl, Store: fs,
		Project: project.Config{Version: 1, Application: "podinfo", Repository: project.Repository{Name: "AndrewMaged814/podinfo", DefaultBranch: "master"},
			Release: project.Release{Environment: "production", ImageRepository: "ghcr.io/andrewmaged814/podinfo", ImageTag: "sha-{{merge_sha_short8}}", RequiredCheck: "build-and-push", TemplatePath: ".safelane/release-template"},
			Target:  project.Target{Cluster: "safelane-demo", Namespace: "podinfo", Rollout: "podinfo"}},
		ChangeFacts: fakeFacts{fromJSON[assess.Facts](t, `{"files":[{"path":"one"},{"path":"two"},{"path":"three"}],"additions":64,"deletions":12,"agent_authored":true,"agent_evidence":"Co-authored-by: Claude <noreply@anthropic.com>"}`)},
		Heuristic:   fakeAssessor{fromJSON[assess.Verdict](t, `{"risk":"medium","rules":["agent_authored","path:pkg/api/**"],"available":true}`)},
		Model:       fakeAssessor{fromJSON[assess.Verdict](t, `{"assessor":"claude","risk":"high","rationale":"error path returns before writing a status","available":true}`)},
		Now:         func() time.Time { return time.Date(2026, 8, 20, 14, 20, 0, 0, time.UTC) },
		NewID:       func() (release.ReleaseID, error) { return id, nil },
	}
	r, err := orchestrate.SubmitRelease(context.Background(), release.Intent{SchemaVersion: release.RequestSchemaVersion, Repository: "AndrewMaged814/podinfo", PullRequest: 4, Environment: "production"}, d)
	if err != nil {
		t.Fatalf("SubmitRelease: %v", err)
	}
	r, err = r.WithBoundary(release.Boundary{
		ControllerIdentity: "system:serviceaccount:podinfo:safelane-controller",
		CallerIdentity:     "system:serviceaccount:podinfo:safelane-caller",
		CallerCapability:   release.CallerCapability{AssertedAt: time.Date(2026, 8, 20, 14, 26, 0, 0, time.UTC), Method: "SubjectAccessReview", GetRollouts: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := []release.ExecutionEntry{
		{At: time.Date(2026, 8, 20, 14, 26, 3, 0, time.UTC), Verb: release.VerbStart, RequestedWeight: 1, Outcome: release.OutcomeGranted},
		{At: time.Date(2026, 8, 20, 14, 26, 41, 0, time.UTC), Verb: release.VerbAdvance, RequestedWeight: 100, Outcome: release.OutcomeRefused, ReasonCode: "transition_exceeds_envelope"},
		{At: time.Date(2026, 8, 20, 14, 26, 48, 0, time.UTC), Verb: release.VerbAdvance, RequestedWeight: 5, Outcome: release.OutcomeGranted},
		{At: time.Date(2026, 8, 20, 14, 29, 8, 0, time.UTC), Verb: release.VerbArgoAbort, Outcome: release.OutcomeAborted, ReasonCode: "analysis_failed", Analysis: "podinfo-success-rate-4", Detail: "request-success-rate 0.71 < 0.99"},
	}
	for _, entry := range entries {
		r, err = r.WithExecution(entry)
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestReleaseRecordV2PrunesRequestAndRoundTripsEverySection(t *testing.T) {
	r := completeRelease(t)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"schema_version":"safelane.release.record/v2"`, `"assessment"`, `"envelope"`, `"execution"`, `"boundary"`, `"outcome":"aborted"`} {
		if !strings.Contains(text, want) {
			t.Errorf("record missing %s\n%s", want, text)
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	request := obj["request"].(map[string]any)
	if len(request) != 4 {
		t.Fatalf("request = %v, want four pruned fields", request)
	}
	for _, forbidden := range []string{"review", "ci", "artifact", "metadata", "risk", "lane"} {
		if _, ok := request[forbidden]; ok {
			t.Errorf("persisted request contains %q", forbidden)
		}
	}
	var loaded release.Release
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if len(loaded.Execution()) != 4 {
		t.Fatalf("execution entries = %d", len(loaded.Execution()))
	}
	if _, ok := loaded.Boundary(); !ok {
		t.Fatal("boundary lost on load")
	}
}

func TestReleaseRecordLoadRejectsTamperedDerivedProof(t *testing.T) {
	raw, _ := json.Marshal(completeRelease(t))
	for name, mutate := range map[string]func(map[string]any){
		"outcome":  func(obj map[string]any) { obj["outcome"] = "pending" },
		"envelope": func(obj map[string]any) { obj["envelope"].(map[string]any)["lane"] = "standard" },
		"old request field": func(obj map[string]any) {
			obj["request"].(map[string]any)["review"] = map[string]any{"approver": "claim"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var obj map[string]any
			_ = json.Unmarshal(raw, &obj)
			mutate(obj)
			damaged, _ := json.Marshal(obj)
			var loaded release.Release
			if err := json.Unmarshal(damaged, &loaded); err == nil {
				t.Fatal("tampered record loaded")
			}
		})
	}
}

func TestDetailsMatchesA35SectionsAndRecordedOrder(t *testing.T) {
	out := From(completeRelease(t)).Details()
	for _, want := range []string{
		"ARTIFACT", "ASSESSMENT", "DECISION", "EXECUTION", "BOUNDARY", "OUTCOME  aborted",
		"change            3 files, +64 −12",
		"heuristic         medium", "model (claude)    high", "combined by       worse-of", "risk              high", "lane              guarded",
		"14:26:41Z  advance    weight 100", "REFUSED  transition_exceeds_envelope",
		"14:29:08Z  argo_abort", "aborted  analysis_failed", "podinfo-success-rate-4: request-success-rate 0.71 < 0.99",
		"caller capability     get rollouts: yes | patch rollouts: no", "asserted by SubjectAccessReview at 14:26:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("proof missing %q\n%s", want, out)
		}
	}
	if strings.Index(out, "transition_exceeds_envelope") > strings.Index(out, "analysis_failed") {
		t.Fatal("refusal must precede Argo abort")
	}
	if strings.Contains(strings.ToLower(out), "bypass attempt") {
		t.Fatal("proof must not claim an unobservable bypass attempt")
	}
}

func TestDetailsMatchesA35Golden(t *testing.T) {
	got := From(completeRelease(t)).Details()
	want, err := os.ReadFile(filepath.Join("testdata", "a3-5-proof.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("proof differs from a3-5-proof.txt\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestProofJSONCarriesRecordedAssessmentExecutionBoundaryAndOutcome(t *testing.T) {
	raw, err := json.Marshal(From(completeRelease(t)))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"schema_version":"safelane.release.proof/v2"`, `"assessment"`, `"execution"`, `"boundary"`, `"outcome":"aborted"`} {
		if !strings.Contains(text, want) {
			t.Errorf("proof JSON missing %s", want)
		}
	}
}

func TestOutcomeIsCompleteWhenFinalEnvelopeWeightWasGranted(t *testing.T) {
	envelope, err := release.NewRolloutEnvelope([]int{5, 100}, "start")
	if err != nil {
		t.Fatal(err)
	}
	entries := []release.ExecutionEntry{
		{At: time.Now(), Verb: release.VerbStart, RequestedWeight: 5, Outcome: release.OutcomeGranted},
		{At: time.Now(), Verb: release.VerbAdvance, RequestedWeight: 100, Outcome: release.OutcomeGranted},
	}
	if got := outcome(entries, &envelope); got != "complete" {
		t.Fatalf("outcome = %q, want complete", got)
	}
}

func TestDetailsLabelsTemplateAndBundleDigestsAccurately(t *testing.T) {
	r := completeRelease(t)
	tmpl, ok := r.TemplateIdentity()
	if !ok {
		t.Fatal("fixture has no template identity")
	}
	bundle, ok := r.Bundle()
	if !ok {
		t.Fatal("fixture has no bundle")
	}
	out := From(r).Details()
	if !strings.Contains(out, "template digest "+tmpl.ContentDigest) {
		t.Fatalf("proof does not print the template content digest under its label\n%s", out)
	}
	if !strings.Contains(out, "hashed bundle, digest "+bundle.Digest()) {
		t.Fatalf("proof does not print the bundle digest under its label\n%s", out)
	}
}
