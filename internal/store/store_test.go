package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func fixtureRequest() release.ReleaseRequest {
	return release.ReleaseRequest{
		SchemaVersion: release.RequestSchemaVersion,
		Target: release.Target{
			Application: "podinfo", Environment: "production", Cluster: "safelane-demo", Namespace: "podinfo",
		},
		Source: release.ClaimedSource{
			Repository: "acme/podinfo", BaseBranch: "main", MergeCommitSHA: "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91",
		},
		PullRequest: release.ClaimedPullRequest{
			PullRequestNumber: 1, Author: "andrew",
		},
		CI: release.ClaimedCI{Workflow: "publish", CheckName: "publish"},
		Artifact: release.ClaimedArtifact{
			ImageReference: "ghcr.io/acme/podinfo@sha256:" + repeatHex(),
		},
		Caller:   release.CallerIdentity{Identity: "test", Kind: release.CallerCI},
		Metadata: release.RequestMetadata{RequestID: "req-1", SubmittedAt: time.Now()},
	}
}

func TestFileStore_ListSkipsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"release_id":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a release"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != r.ID {
		t.Fatalf("List = %+v, want only %s", got, r.ID)
	}
}

func TestFileStore_ListMissingDirectoryIsEmpty(t *testing.T) {
	got, err := (&FileStore{Dir: filepath.Join(t.TempDir(), "missing")}).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List returned %d releases, want none", len(got))
	}
}

func repeatHex() string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func mustStoreEligibility(t *testing.T) release.Eligibility {
	t.Helper()
	elig, err := release.Ineligible("1", "requirement_failed", "A mandatory evidence requirement failed.")
	if err != nil {
		t.Fatalf("Ineligible: %v", err)
	}
	return elig
}

func fixtureRelease(t *testing.T) *release.Release {
	t.Helper()
	id, err := release.MintReleaseID()
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	r, err := release.NewRelease(release.ReleaseParams{
		ID:          id,
		Request:     fixtureRequest(),
		Evidence:    release.MissingEvidence(),
		Eligibility: mustStoreEligibility(t),
		CreatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return r
}

func TestFileStore_SaveAndLoad_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)

	if err := s.Save(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := s.Load(r.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.ID != r.ID {
		t.Fatalf("want ID %s, got %s", r.ID, loaded.ID)
	}
	if loaded.Evidence().Outcome() != r.Evidence().Outcome() {
		t.Fatalf("want outcome %s, got %s", r.Evidence().Outcome(), loaded.Evidence().Outcome())
	}
}

func TestFileStore_Save_RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)

	if err := s.Save(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := s.Save(r); err == nil {
		t.Fatal("want an error when saving a release id that already exists")
	}
}

func TestFileStore_SaveUpdateLoad_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)

	if err := s.Save(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	elig, err := release.Ineligible("2", "a_different_requirement_failed", "A later re-check found a different failure.")
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	updated, err := release.NewRelease(release.ReleaseParams{
		ID:          r.ID,
		Request:     fixtureRequest(),
		Evidence:    release.MissingEvidence(),
		Eligibility: elig,
		CreatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}

	if err := s.Update(updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded, err := s.Load(r.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loaded.ID != r.ID {
		t.Fatalf("want ID %s, got %s", r.ID, loaded.ID)
	}
	if got := loaded.Eligibility().ReasonCode(); got != "a_different_requirement_failed" {
		t.Fatalf("want the update to have replaced the eligibility, got reason code %q", got)
	}
}

func TestFileStore_Update_MissingRelease(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)

	if err := s.Update(r); err == nil {
		t.Fatal("want an error updating a release that was never saved")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := s.Load(r.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("want Update to leave no record behind when the release did not already exist")
	}
}

func TestFileStore_Load_MissingRelease(t *testing.T) {
	dir := t.TempDir()
	s := &FileStore{Dir: dir}
	id, _ := release.ParseReleaseID("rel_00000000000000000000000000")

	if _, err := s.Load(id); err == nil {
		t.Fatal("want an error loading a release that was never saved")
	} else if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFileStore_CreatesDirOnFirstSave(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "releases")
	s := &FileStore{Dir: dir}
	r := fixtureRelease(t)

	if err := s.Save(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
