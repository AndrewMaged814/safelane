package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

const (
	proofFixtureDigest   = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	proofFixtureMergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	proofFixtureID       = "rel_00000000000000000000000000"
)

type proofFakeFetcher struct {
	facts github.Facts
	err   error
}

func (f proofFakeFetcher) FetchPullRequestFacts(ctx context.Context, owner, repo string, number int) (github.Facts, error) {
	if f.err != nil {
		return github.Facts{}, f.err
	}
	return f.facts, nil
}

type proofFakeResolver struct {
	digest string
}

func (f proofFakeResolver) ResolveDigest(ctx context.Context, ref release.ImageReference) (string, error) {
	return f.digest, nil
}

func (f proofFakeResolver) ResolveTag(ctx context.Context, repository, tag string) (string, error) {
	return f.digest, nil
}

func proofVerifiedFacts() github.Facts {
	return github.Facts{
		Repository:     "AndrewMaged814/podinfo",
		Number:         1,
		URL:            "https://github.com/AndrewMaged814/podinfo/pull/1",
		Merged:         true,
		MergedAt:       time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC),
		BaseRef:        "main",
		MergeCommitSHA: proofFixtureMergeSHA,
		AuthorLogin:    "AndrewMaged814",
		Approvals:      []github.Approval{{Reviewer: "ahmed-placeholder", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)}},
		CheckRuns: []github.CheckRun{{
			Name: "publish / build-and-push", Conclusion: "success", HeadSHA: proofFixtureMergeSHA,
			RunID: 16453210987, URL: "https://github.com/AndrewMaged814/podinfo/actions/runs/16453210987",
			CompletedAt: time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC),
		}},
	}
}

func persistProofRelease(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	tmpl, err := render.LoadDir(filepath.Join("..", "render", "testdata", "release-template"))
	if err != nil {
		t.Fatalf("load template: %v", err)
	}
	id, err := release.ParseReleaseID(proofFixtureID)
	if err != nil {
		t.Fatalf("ParseReleaseID: %v", err)
	}
	deps := orchestrate.Deps{
		GitHub:   proofFakeFetcher{facts: proofVerifiedFacts()},
		GHCR:     proofFakeResolver{digest: proofFixtureDigest},
		Template: tmpl,
		Store:    &store.FileStore{Dir: dir},
		Project: project.Config{
			Version:     1,
			Application: "podinfo",
			Repository:  project.Repository{Name: "AndrewMaged814/podinfo", DefaultBranch: "main"},
			Release: project.Release{
				Environment:     "production",
				ImageRepository: "ghcr.io/andrewmaged814/podinfo",
				ImageTag:        "sha-{{merge_sha_short8}}",
				RequiredCheck:   "publish / build-and-push",
				TemplatePath:    ".safelane/release-template",
			},
			Target: project.Target{Cluster: "safelane-demo", Namespace: "podinfo", Rollout: "podinfo"},
		},
		Now:   func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
		NewID: func() (release.ReleaseID, error) { return id, nil },
	}
	intent := release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    "AndrewMaged814/podinfo",
		PullRequest:   1,
		Environment:   "production",
	}
	if _, err := orchestrate.SubmitRelease(context.Background(), intent, deps); err != nil {
		t.Fatalf("SubmitRelease: %v", err)
	}
	return dir
}

func TestProofCommand_MissingID_ExitsUsage(t *testing.T) {
	cmd := ProofCommand("store-dir-unused")
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "release id is required") {
		t.Fatalf("want a missing-id message, got %q", stderr.String())
	}
}

func TestProofCommand_UnknownID_TypedActionableError(t *testing.T) {
	dir := t.TempDir()
	cmd := ProofCommand(dir)
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), []string{proofFixtureID}, &stdout, &stderr)
	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d (stderr: %s)", code, stderr.String())
	}
	out := stderr.String()
	if !strings.Contains(out, "release_not_found") {
		t.Errorf("want typed code release_not_found, got %q", out)
	}
	if !strings.Contains(out, "remedy:") {
		t.Errorf("want an actionable remedy, got %q", out)
	}
}

func TestProofCommand_MalformedID_ExitsUsage(t *testing.T) {
	cmd := ProofCommand(t.TempDir())
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), []string{"not-a-release-id"}, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "malformed_release_id") {
		t.Errorf("want malformed_release_id, got %q", stderr.String())
	}
}

func TestProofCommand_DetailsAndJSON_ExitsUsage(t *testing.T) {
	cmd := ProofCommand(t.TempDir())
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), []string{"--details", "--json", proofFixtureID}, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestProofCommand_JSON_FromPersistedEligibleRelease(t *testing.T) {
	dir := persistProofRelease(t)
	cmd := ProofCommand(dir)
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), []string{"--json", proofFixtureID}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}
	var obj map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &obj); err != nil {
		t.Fatalf("JSON output: %v\n%s", err, stdout.String())
	}
	decision, _ := obj["decision"].(map[string]any)
	if decision["eligibility"] != "eligible" {
		t.Errorf("eligibility = %v, want eligible", decision["eligibility"])
	}
	if strings.Contains(strings.ToLower(stdout.String()), "deploywhisper") {
		t.Error("JSON must not mention DeployWhisper")
	}
}

func TestProofCommand_ConciseAndDetails_FromPersistedEligibleRelease(t *testing.T) {
	dir := persistProofRelease(t)
	cmd := ProofCommand(dir)

	var concise bytes.Buffer
	if code := cmd.Run(context.Background(), []string{proofFixtureID}, &concise, ioDiscard()); code != ExitOK {
		t.Fatalf("concise: want ExitOK, got %d", code)
	}
	var details bytes.Buffer
	if code := cmd.Run(context.Background(), []string{"--details", proofFixtureID}, &details, ioDiscard()); code != ExitOK {
		t.Fatalf("details: want ExitOK, got %d", code)
	}
	shared := []string{
		"eligibility: eligible",
		"rollout_envelope: 1 → 5 → 25 → 50 → 100",
		"next_action: start",
		proofFixtureDigest,
	}
	for _, want := range shared {
		if !strings.Contains(concise.String(), want) {
			t.Errorf("concise missing %q\n%s", want, concise.String())
		}
		if !strings.Contains(details.String(), want) {
			t.Errorf("details missing %q\n%s", want, details.String())
		}
	}
	if !strings.Contains(concise.String(), "execution: pending") || !strings.Contains(concise.String(), "boundary: pending") {
		t.Errorf("concise missing pending sections\n%s", concise.String())
	}
	if !strings.Contains(details.String(), "Execution") || !strings.Contains(details.String(), "Boundary") || !strings.Contains(details.String(), "pending") {
		t.Errorf("details missing pending Execution/Boundary\n%s", details.String())
	}
}

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }
