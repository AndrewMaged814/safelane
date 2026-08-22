package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// The two changes the demonstration runs, with the exact commits,
// digests, file lists and rationales Appendix A prints. They are here as
// constants because the golden files quote them: an abbreviated commit
// like "c9ac0363" survives normalisation and is compared literally, so
// the fixture has to be the appendix's own commit and not an arbitrary
// one.
const (
	safeMergeSHA  = "c9ac0363ba20589b3534bc8ae9629ed82e30c9e2"
	riskyMergeSHA = "7a19c4dbe0f38512a4c76b9e2d05fa1c3e8b7460"
	queuedSHA     = "3c5d8e1f9a2b7c4e6d0f8a1b3c5d7e9f0a2b4c6d"

	safeDigest  = "sha256:1f4827c4a9b3d5e7091c2f4a6b8d0e2f4a6c8e0b2d4f6a8c0e2b4d6f830f5faa"
	riskyDigest = "sha256:c30fb712a4d6e8091b3c5d7e9f0a2b4c6d8e0f2a4b6c8d0e2f4a6b8c0d28ea1b"

	safeRationale = "single-line version constant; no request path, no configuration, " +
		"no error handling touched"
	riskyRationale = "echo handler returns on the error path before writing a status code; " +
		"under load this produces empty 200s, not 5xx, so readiness will not catch it"
	riskyTrailer = "Co-authored-by: Claude <noreply@anthropic.com>"
)

// --- fakes ---

type fakeGitHub struct {
	facts github.Facts
	err   error
}

func (f fakeGitHub) FetchPullRequestFacts(context.Context, string, string, int) (github.Facts, error) {
	if f.err != nil {
		return github.Facts{}, f.err
	}
	return f.facts, nil
}

type fakeGHCR struct {
	digest string
	err    error
}

func (f fakeGHCR) ResolveDigest(context.Context, release.ImageReference) (string, error) {
	return f.resolve()
}

func (f fakeGHCR) ResolveTag(context.Context, string, string) (string, error) {
	return f.resolve()
}

func (f fakeGHCR) resolve() (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.digest, nil
}

type fakeChangeFacts struct {
	facts assess.Facts
	err   error
}

func (f fakeChangeFacts) FetchChangeFacts(context.Context, string, string, int) (assess.Facts, error) {
	if f.err != nil {
		return assess.Facts{}, f.err
	}
	return f.facts, nil
}

// fakeModel is the model assessor with the network taken out: it answers
// with whatever verdict the case under test needs. The real assessor's
// own behaviour is tested in internal/assess.
type fakeModel struct{ verdict assess.Verdict }

func (fakeModel) Name() string { return "model" }

func (f fakeModel) Assess(context.Context, assess.Facts) (assess.Verdict, error) {
	return f.verdict, nil
}

type discardStore struct{}

func (discardStore) Save(*release.Release) error { return nil }

// --- fixtures ---

func demoProject() project.Config {
	return project.Config{
		Version:     1,
		Application: "safelane-demo-api",
		Repository:  project.Repository{Name: "AndrewMaged814/safelane-demo-api", DefaultBranch: "master"},
		Release: project.Release{
			Environment:     "production",
			ImageRepository: "ghcr.io/andrewmaged814/safelane-demo-api",
			ImageTag:        "sha-{{merge_sha}}",
			RequiredCheck:   "build-and-push",
			TemplatePath:    ".safelane/release-template",
		},
		Target: project.Target{Cluster: "safelane-demo", Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"},
	}
}

// mergedFacts is a pull request that merged cleanly with its publish
// check green -- the shape every positive case starts from.
func mergedFacts(number int, sha string) github.Facts {
	return github.Facts{
		Repository:     "AndrewMaged814/safelane-demo-api",
		Number:         number,
		URL:            fmt.Sprintf("https://github.com/AndrewMaged814/safelane-demo-api/pull/%d", number),
		Merged:         true,
		MergedAt:       time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC),
		BaseRef:        "master",
		MergeCommitSHA: sha,
		AuthorLogin:    "AndrewMaged814",
		CheckRuns: []github.CheckRun{{
			Name: "build-and-push", Status: "completed", Conclusion: "success",
			HeadSHA: sha, RunID: 16453210987,
			CompletedAt: time.Date(2026, 8, 20, 14, 5, 0, 0, time.UTC),
		}},
	}
}

func releaseID(t *testing.T, id string) func() (release.ReleaseID, error) {
	t.Helper()
	parsed, err := release.ParseReleaseID(id)
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return func() (release.ReleaseID, error) { return parsed, nil }
}

func demoTemplate(t *testing.T) render.Template {
	t.Helper()
	tmpl, err := render.LoadDir(filepath.Join("..", "render", "testdata", "release-template"))
	if err != nil {
		t.Fatalf("could not load the template fixture: %v", err)
	}
	return tmpl
}

// inspectCase is one whole `release plan` run with every external
// dependency replaced.
type inspectCase struct {
	id      string
	pr      int
	project project.Config
	github  fakeGitHub
	ghcr    fakeGHCR
	facts   fakeChangeFacts
	model   assess.Verdict
	now     time.Time
}

func (c inspectCase) run(t *testing.T) string {
	t.Helper()
	cfg := c.project
	if cfg.Application == "" {
		cfg = demoProject()
	}
	pol := policy.Default()
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
		Policy:      pol,
		Now:         func() time.Time { return now },
		NewID:       releaseID(t, c.id),
	}

	result, err := orchestrate.Submit(context.Background(), release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    "AndrewMaged814/safelane-demo-api",
		PullRequest:   c.pr,
		Environment:   "production",
	}, deps)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return buildInspection(result, cfg, pol, now).Render()
}

// --- A2.1 and A3.1: the two changes the demonstration runs ---

func TestInspect_SafeChange_MatchesA21(t *testing.T) {
	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files:          []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
			TotalAdditions: 1,
			TotalDeletions: 1,
			MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{
			Risk: assess.RiskLow, Rationale: safeRationale, Available: true, Assessor: "claude",
		},
	}.run(t)

	assertGolden(t, "a2-1-inspect-safe.txt", out)
}

func TestInspect_RiskyChange_MatchesA31(t *testing.T) {
	out := inspectCase{
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
			TotalAdditions: 64,
			TotalDeletions: 12,
			AgentAuthored:  true,
			AgentEvidence:  riskyTrailer,
			MergeCommitSHA: riskyMergeSHA,
		}},
		model: assess.Verdict{
			Risk: assess.RiskHigh, Rationale: riskyRationale, Available: true, Assessor: "claude",
		},
	}.run(t)

	assertGolden(t, "a3-1-inspect-risky.txt", out)
}

// TestInspect_LaneReachesTheManifest is the claim the demonstration makes
// out loud: the same command, the same operator configuration and a
// different change produce a different Rollout and nothing else. Only the
// Rollout carries steps, so exactly one of the five resource hashes may
// differ between the two lanes.
func TestInspect_LaneReachesTheManifest(t *testing.T) {
	fast, err := render.Render(demoTemplate(t), demoTarget(), demoEvidence(t, safeMergeSHA, safeDigest), []int{50, 100})
	if err != nil {
		t.Fatalf("render fast lane: %v", err)
	}
	guarded, err := render.Render(demoTemplate(t), demoTarget(), demoEvidence(t, safeMergeSHA, safeDigest), []int{25, 50, 75, 100})
	if err != nil {
		t.Fatalf("render guarded lane: %v", err)
	}

	fastHashes, guardedHashes := fast.Hashes(), guarded.Hashes()
	if len(fastHashes) != len(guardedHashes) {
		t.Fatalf("the two lanes rendered different resource counts: %d and %d", len(fastHashes), len(guardedHashes))
	}
	var changed []string
	for i := range fastHashes {
		if fastHashes[i].Hash != guardedHashes[i].Hash {
			changed = append(changed, fastHashes[i].Ref.Kind)
		}
	}
	if len(changed) != 1 || changed[0] != "Rollout" {
		t.Fatalf("want exactly the Rollout to differ between lanes, got %v", changed)
	}
}

func demoTarget() release.Target {
	return release.Target{Application: "safelane-demo-api", Environment: "production", Cluster: "safelane-demo", Namespace: "safelane-demo-api"}
}

func demoEvidence(t *testing.T, sha, digest string) release.ReleaseEvidence {
	t.Helper()
	now := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	ev, err := release.NewReleaseEvidence(release.EvidenceInput{
		Repository:     release.RepositoryRef{Owner: "AndrewMaged814", Name: "safelane-demo-api"},
		PullRequest:    release.VerifiedPullRequest{Number: 3, URL: "https://github.com/AndrewMaged814/safelane-demo-api/pull/3", Author: "AndrewMaged814", BaseBranch: "master", MergedAt: now},
		MergeCommitSHA: sha,
		RequiredCheck: release.VerifiedCheckRun{
			Name: "build-and-push", HeadSHA: sha, Conclusion: release.CheckConclusionSuccess, CompletedAt: now,
		},
		Artifact: release.VerifiedArtifact{
			Reference:      release.ImageReference{Registry: "ghcr.io", Repository: "andrewmaged814/safelane-demo-api", Digest: digest},
			ObservedDigest: digest, ResolvedAt: now,
		},
		VerifiedAt: now,
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return ev
}

// --- N4 to N7: the four ways an investigation stops ---

func TestInspect_PullRequestOpen_MatchesN4(t *testing.T) {
	facts := mergedFacts(9, "")
	facts.Merged = false
	facts.MergeCommitSHA = ""
	facts.CheckRuns = nil

	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     9,
		github: fakeGitHub{facts: facts},
		ghcr:   fakeGHCR{err: errors.New("no manifest")},
	}.run(t)

	assertGoldenFragment(t, "n4-pull-request-open.txt", out)
}

func TestInspect_RequiredCheckFailed_MatchesN5(t *testing.T) {
	facts := mergedFacts(3, safeMergeSHA)
	facts.CheckRuns[0].Conclusion = "failure"

	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: facts},
		ghcr:   fakeGHCR{digest: safeDigest},
	}.run(t)

	assertGoldenFragment(t, "n5-required-check-failed.txt", out)
}

func TestInspect_ImageNotPublished_MatchesN6(t *testing.T) {
	now := time.Date(2026, 8, 20, 14, 21, 44, 0, time.UTC)
	facts := mergedFacts(5, queuedSHA)
	facts.CheckRuns[0].Status = "in_progress"
	facts.CheckRuns[0].Conclusion = ""
	facts.CheckRuns[0].StartedAt = now.Add(-40 * time.Second)
	facts.CheckRuns[0].CompletedAt = time.Time{}

	// The tag pattern is the operator's; this one publishes the short
	// form, which is what N6's "no manifest yet for tag sha-3c5d8e1f"
	// shows.
	cfg := demoProject()
	cfg.Release.ImageTag = "sha-{{merge_sha_short8}}"

	out := inspectCase{
		id:      "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:      5,
		project: cfg,
		github:  fakeGitHub{facts: facts},
		ghcr:    fakeGHCR{err: errors.New("no manifest for tag")},
		now:     now,
	}.run(t)

	assertGoldenFragment(t, "n6-image-not-published.txt", out)
}

func TestInspect_GitHubRateLimited_MatchesN7(t *testing.T) {
	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{err: errors.New("github: 403 rate limit exceeded, resets in 11m")},
		ghcr:   fakeGHCR{err: errors.New("no merge commit")},
	}.run(t)

	assertGoldenFragment(t, "n7-github-rate-limited.txt", out)
}

// --- the rules the report exists to enforce ---

// An ineligible release is not assessed at all. This is the invariant
// behind N4's "an ineligible release receives no lane": a change that may
// not ship has no business carrying a width decision.
func TestInspect_IneligibleRelease_CarriesNoRiskOrLane(t *testing.T) {
	facts := mergedFacts(9, "")
	facts.Merged = false
	facts.MergeCommitSHA = ""
	facts.CheckRuns = nil

	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     9,
		github: fakeGitHub{facts: facts},
		ghcr:   fakeGHCR{err: errors.New("no manifest")},
	}.run(t)

	for _, forbidden := range []string{"  risk ", "  lane ", "  heuristic ", "  model "} {
		if strings.Contains(out, forbidden) {
			t.Errorf("an ineligible release must carry no assessment, found %q in:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, "an ineligible release receives no lane") {
		t.Errorf("want the report to say why it did not assess:\n%s", out)
	}
}

// A check that could not run reports unavailable, never failed.
func TestInspect_UnrunChecksAreUnavailableNotFailed(t *testing.T) {
	facts := mergedFacts(9, "")
	facts.Merged = false
	facts.MergeCommitSHA = ""
	facts.CheckRuns = nil

	out := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     9,
		github: fakeGitHub{facts: facts},
		ghcr:   fakeGHCR{err: errors.New("no manifest")},
	}.run(t)

	failed := out[strings.Index(out, "Failed"):strings.Index(out, "Unavailable")]
	if strings.Contains(failed, "Required publish check") || strings.Contains(failed, "Immutable GHCR digest") {
		t.Errorf("a check that never ran must not be reported as failed:\n%s", failed)
	}
}

// An unavailable model is not a low verdict: the heuristic's floor stands
// alone and the lane is the one that floor bought, not a wider one.
func TestInspect_ModelUnavailable_KeepsTheHeuristicFloor(t *testing.T) {
	out := inspectCase{
		id:     "rel_01M0F3QD9NBV6JKC2WS8XA7TR4",
		pr:     4,
		github: fakeGitHub{facts: mergedFacts(4, riskyMergeSHA)},
		ghcr:   fakeGHCR{digest: riskyDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files: []assess.FileChange{
				{Path: "pkg/api/echo.go", Additions: 41, Deletions: 6},
				{Path: "pkg/api/handlers.go", Additions: 22, Deletions: 5},
			},
			TotalAdditions: 63, TotalDeletions: 11,
			AgentAuthored: true, AgentEvidence: riskyTrailer, MergeCommitSHA: riskyMergeSHA,
		}},
		model: assess.Verdict{
			Available: false,
			Reason:    "claude: exit 1 after 2 retries (api overloaded); codex: not found on PATH",
		},
	}.run(t)

	for _, want := range []string{
		"  model             unavailable",
		"claude: exit 1 after 2 retries (api overloaded)",
		"codex: not found on PATH",
		"  risk              medium  (heuristic only)",
		"  lane              guarded",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
}

// The JSON form carries the same decision as the text, so an agent
// branching on it and an operator reading the terminal never disagree.
func TestInspectJSON_CarriesTheSameDecision(t *testing.T) {
	c := inspectCase{
		id:     "rel_01M0F2K7RXQW3HDN8YT4B1MPZE",
		pr:     3,
		github: fakeGitHub{facts: mergedFacts(3, safeMergeSHA)},
		ghcr:   fakeGHCR{digest: safeDigest},
		facts: fakeChangeFacts{facts: assess.Facts{
			Files:          []assess.FileChange{{Path: "pkg/version/version.go", Additions: 1, Deletions: 1}},
			TotalAdditions: 1, TotalDeletions: 1, MergeCommitSHA: safeMergeSHA,
		}},
		model: assess.Verdict{Risk: assess.RiskLow, Rationale: safeRationale, Available: true, Assessor: "claude"},
	}
	// Re-run the same case and read the machine form rather than the text.
	in := c.inspection(t)
	j := in.JSON()

	if j.Decision.Eligibility != "eligible" || j.Decision.Lane != "fast" {
		t.Fatalf("want eligible/fast, got %s/%s", j.Decision.Eligibility, j.Decision.Lane)
	}
	if got := j.Decision.Weights; len(got) != 2 || got[0] != 50 || got[1] != 100 {
		t.Fatalf("want weights 50,100, got %v", got)
	}
	if j.Decision.Gates != 1 {
		t.Fatalf("want 1 gate, got %d", j.Decision.Gates)
	}
	if j.Assessment == nil || j.Assessment.Risk != assess.RiskLow {
		t.Fatalf("want a low risk assessment, got %+v", j.Assessment)
	}
	if len(j.Checks) != 3 {
		t.Fatalf("want all three checks reported, got %d", len(j.Checks))
	}
	if j.NextCommand == "" {
		t.Fatal("want the next command an agent should run")
	}
}

// inspection runs the case and returns the report as values.
func (c inspectCase) inspection(t *testing.T) inspection {
	t.Helper()
	cfg := c.project
	if cfg.Application == "" {
		cfg = demoProject()
	}
	pol := policy.Default()
	now := time.Date(2026, 8, 20, 14, 21, 44, 0, time.UTC)
	result, err := orchestrate.Submit(context.Background(), release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    "AndrewMaged814/safelane-demo-api",
		PullRequest:   c.pr,
		Environment:   "production",
	}, orchestrate.Deps{
		GitHub:      c.github,
		GHCR:        c.ghcr,
		ChangeFacts: c.facts,
		Model:       fakeModel{verdict: c.model},
		Template:    demoTemplate(t),
		Store:       discardStore{},
		Project:     cfg,
		Policy:      pol,
		Now:         func() time.Time { return now },
		NewID:       releaseID(t, c.id),
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return buildInspection(result, cfg, pol, now)
}
