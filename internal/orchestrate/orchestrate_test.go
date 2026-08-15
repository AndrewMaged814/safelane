package orchestrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// --- fakes ---

type fakeFetcher struct {
	facts github.Facts
	err   error
}

func (f fakeFetcher) FetchPullRequestFacts(ctx context.Context, owner, repo string, number int) (github.Facts, error) {
	if f.err != nil {
		return github.Facts{}, f.err
	}
	return f.facts, nil
}

type fakeResolver struct {
	digest string
	err    error
}

func (f fakeResolver) ResolveDigest(ctx context.Context, ref release.ImageReference) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.digest, nil
}

type fakeStore struct {
	saved []*release.Release
	err   error
}

func (s *fakeStore) Save(r *release.Release) error {
	if s.err != nil {
		return s.err
	}
	s.saved = append(s.saved, r)
	return nil
}

// --- fixtures ---

const fixtureDigest = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
const fixtureMergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"

func loadFixtureRaw(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "release-evidence.json"))
	if err != nil {
		t.Fatalf("could not read fixture: %v", err)
	}
	return raw
}

func loadFixtureObject(t *testing.T) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(loadFixtureRaw(t), &obj); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	return obj
}

func marshal(t *testing.T, obj map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("could not marshal test object: %v", err)
	}
	return raw
}

func loadTemplate(t *testing.T) render.Template {
	t.Helper()
	tmpl, err := render.LoadDir(filepath.Join("..", "render", "testdata", "release-template"))
	if err != nil {
		t.Fatalf("could not load template fixture: %v", err)
	}
	return tmpl
}

// verifiedFacts matches every claim in testdata/release-evidence.json, so a
// fixture request submitted against these fakes verifies end to end.
func verifiedFacts() github.Facts {
	return github.Facts{
		Repository:     "AndrewMaged814/podinfo",
		Number:         1,
		URL:            "https://github.com/AndrewMaged814/podinfo/pull/1",
		Merged:         true,
		MergedAt:       time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC),
		BaseRef:        "main",
		MergeCommitSHA: fixtureMergeSHA,
		AuthorLogin:    "AndrewMaged814",
		Approvals: []github.Approval{
			{Reviewer: "ahmed-placeholder", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)},
		},
		CheckRuns: []github.CheckRun{
			{
				Name: "publish / build-and-push", Conclusion: "success", HeadSHA: fixtureMergeSHA,
				RunID: 16453210987, URL: "https://github.com/AndrewMaged814/podinfo/actions/runs/16453210987",
				CompletedAt: time.Date(2026, 8, 15, 8, 30, 0, 0, time.UTC),
			},
		},
	}
}

func fixedReleaseID(t *testing.T) func() (release.ReleaseID, error) {
	t.Helper()
	id, err := release.ParseReleaseID("rel_" + strings.Repeat("0", 26))
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return func() (release.ReleaseID, error) { return id, nil }
}

func baseDeps(t *testing.T) (Deps, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	deps := Deps{
		GitHub:   fakeFetcher{facts: verifiedFacts()},
		GHCR:     fakeResolver{digest: fixtureDigest},
		Template: loadTemplate(t),
		Store:    store,
		Now:      func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) },
		NewID:    fixedReleaseID(t),
	}
	return deps, store
}

// --- happy path ---

func TestSubmitRelease_ValidFixture_ProducesVerifiedPersistedRelease(t *testing.T) {
	deps, store := baseDeps(t)

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Evidence().IsVerified() {
		t.Fatalf("want verified evidence, got %s", r.Evidence())
	}
	bundle, ok := r.Bundle()
	if !ok {
		t.Fatal("want a rendered bundle on a verified release")
	}
	if bundle.PinnedDigest() != fixtureDigest {
		t.Fatalf("want the bundle pinned to %s, got %s", fixtureDigest, bundle.PinnedDigest())
	}
	if len(r.BundleHashes()) == 0 {
		t.Fatal("want per-resource hashes recorded")
	}
	if r.SourceRevision() != fixtureMergeSHA {
		t.Fatalf("want source revision %s, got %s", fixtureMergeSHA, r.SourceRevision())
	}
	if r.ArtifactDigest() != fixtureDigest {
		t.Fatalf("want artifact digest %s, got %s", fixtureDigest, r.ArtifactDigest())
	}
	if len(store.saved) != 1 || store.saved[0].ID != r.ID {
		t.Fatalf("want exactly the returned release persisted, got %+v", store.saved)
	}

	// The Artifact-proof fields the ticket's "done when" criterion asks for.
	evidence, _ := r.Evidence().Verified()
	if evidence.PullRequest().MergedAt.IsZero() {
		t.Error("want a non-zero MergedAt on verified evidence")
	}
	if !evidence.IndependentApproval() {
		t.Error("want an independent approval recorded")
	}
	if evidence.RequiredCheck().Conclusion != "success" {
		t.Errorf("want a successful required check recorded, got %q", evidence.RequiredCheck().Conclusion)
	}
	if id, ok := r.TemplateIdentity(); !ok || id.ContentDigest == "" {
		t.Error("want a template identity with a content digest recorded")
	}
}

func TestSubmitRelease_RoundTripsThroughJSON(t *testing.T) {
	deps, store := baseDeps(t)
	if _, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := json.Marshal(store.saved[0])
	if err != nil {
		t.Fatalf("could not marshal persisted release: %v", err)
	}
	var reloaded release.Release
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("persisted release did not round-trip: %v", err)
	}
	if reloaded.ID != store.saved[0].ID {
		t.Fatalf("want ID to survive round-trip, got %s vs %s", reloaded.ID, store.saved[0].ID)
	}
}

// --- intake rejections never reach persistence ---

func TestSubmitRelease_MalformedRequest_NoReleaseCreated(t *testing.T) {
	deps, store := baseDeps(t)
	_, err := SubmitRelease(context.Background(), []byte("{not json"), deps)
	if err == nil {
		t.Fatal("want an error for malformed JSON")
	}
	if release.Categorize(err) != release.CategoryMalformedRequest {
		t.Fatalf("want CategoryMalformedRequest, got %v", release.Categorize(err))
	}
	if len(store.saved) != 0 {
		t.Fatalf("want nothing persisted for a malformed request, got %d", len(store.saved))
	}
}

func TestSubmitRelease_ForbiddenField_NoReleaseCreated(t *testing.T) {
	deps, store := baseDeps(t)
	obj := loadFixtureObject(t)
	obj["manifests"] = []any{"whatever"}

	_, err := SubmitRelease(context.Background(), marshal(t, obj), deps)
	if release.Categorize(err) != release.CategoryForbiddenField {
		t.Fatalf("want CategoryForbiddenField, got %v (%v)", release.Categorize(err), err)
	}
	if len(store.saved) != 0 {
		t.Fatalf("want nothing persisted for a forbidden field, got %d", len(store.saved))
	}
}

// --- non-verified evidence still persists a Release ---

func TestSubmitRelease_PullRequestNotMerged_PersistsFailedEvidenceWithoutBundle(t *testing.T) {
	deps, store := baseDeps(t)
	facts := verifiedFacts()
	facts.Merged = false
	deps.GitHub = fakeFetcher{facts: facts}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceFailed {
		t.Fatalf("want EvidenceFailed, got %s", r.Evidence().Outcome())
	}
	if _, ok := r.Bundle(); ok {
		t.Fatal("want no bundle when evidence did not verify")
	}
	if len(store.saved) != 1 {
		t.Fatalf("want the release still persisted (a denial needs an id and a record), got %d", len(store.saved))
	}
	if len(r.Evidence().Reasons()) == 0 {
		t.Fatal("want at least one actionable reason recorded")
	}
}

func TestSubmitRelease_RequiredCheckFailed_PersistsFailedEvidence(t *testing.T) {
	deps, store := baseDeps(t)
	facts := verifiedFacts()
	facts.CheckRuns = []github.CheckRun{
		{Name: "publish / build-and-push", Conclusion: "failure", HeadSHA: fixtureMergeSHA},
	}
	deps.GitHub = fakeFetcher{facts: facts}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceFailed {
		t.Fatalf("want EvidenceFailed for a failed required check, got %s", r.Evidence().Outcome())
	}
	if _, ok := r.Bundle(); ok {
		t.Fatal("want no bundle when the required check failed")
	}
	if len(store.saved) != 1 {
		t.Fatalf("want the release still persisted, got %d", len(store.saved))
	}
}

func TestSubmitRelease_ApproverIsAuthor_PersistsFailedEvidence(t *testing.T) {
	deps, _ := baseDeps(t)
	facts := verifiedFacts()
	facts.Approvals = []github.Approval{{Reviewer: facts.AuthorLogin, State: "APPROVED"}}
	deps.GitHub = fakeFetcher{facts: facts}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceFailed {
		t.Fatalf("want EvidenceFailed for self-approval, got %s", r.Evidence().Outcome())
	}
}

func TestSubmitRelease_NoApproval_PersistsMissingEvidence(t *testing.T) {
	deps, _ := baseDeps(t)
	facts := verifiedFacts()
	facts.Approvals = nil
	deps.GitHub = fakeFetcher{facts: facts}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceMissing {
		t.Fatalf("want EvidenceMissing when no approval exists, got %s", r.Evidence().Outcome())
	}
}

func TestSubmitRelease_GitHubUnreachable_PersistsUnknownEvidence_NeverPassing(t *testing.T) {
	deps, store := baseDeps(t)
	deps.GitHub = fakeFetcher{err: fmt.Errorf("connection reset")}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceUnknown {
		t.Fatalf("want EvidenceUnknown when GitHub is unreachable, got %s", r.Evidence().Outcome())
	}
	if r.Evidence().IsVerified() {
		t.Fatal("an unreachable GitHub must never verify")
	}
	if _, ok := r.Bundle(); ok {
		t.Fatal("want no bundle when evidence is unknown")
	}
	if len(store.saved) != 1 {
		t.Fatalf("want the release still persisted, got %d", len(store.saved))
	}
}

func TestSubmitRelease_DigestMismatch_PersistsFailedEvidence(t *testing.T) {
	deps, _ := baseDeps(t)
	deps.GHCR = fakeResolver{digest: "sha256:" + strings.Repeat("9", 64)}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceFailed {
		t.Fatalf("want EvidenceFailed for a digest mismatch, got %s", r.Evidence().Outcome())
	}
}

func TestSubmitRelease_RegistryUnreachable_PersistsUnknownEvidence(t *testing.T) {
	deps, _ := baseDeps(t)
	deps.GHCR = fakeResolver{err: fmt.Errorf("registry timeout")}

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceUnknown {
		t.Fatalf("want EvidenceUnknown when the registry is unreachable, got %s", r.Evidence().Outcome())
	}
}

func TestSubmitRelease_BothChecksFailIndependently_UnknownOutranksFailed(t *testing.T) {
	deps, _ := baseDeps(t)
	facts := verifiedFacts()
	facts.Merged = false // github: failed
	deps.GitHub = fakeFetcher{facts: facts}
	deps.GHCR = fakeResolver{err: fmt.Errorf("registry timeout")} // ghcr: unknown

	r, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Evidence().Outcome() != release.EvidenceUnknown {
		t.Fatalf("want unknown to outrank failed when both checks fail differently, got %s", r.Evidence().Outcome())
	}
}

func TestSubmitRelease_StoreFailure_ReturnsError(t *testing.T) {
	deps, store := baseDeps(t)
	store.err = fmt.Errorf("disk full")

	_, err := SubmitRelease(context.Background(), loadFixtureRaw(t), deps)
	if err == nil {
		t.Fatal("want an error when the store fails")
	}
	if release.Categorize(err) != release.CategoryInternal {
		t.Fatalf("want CategoryInternal, got %v (%v)", release.Categorize(err), err)
	}
}
