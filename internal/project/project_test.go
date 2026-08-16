package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImageTag_ShortSHA(t *testing.T) {
	got := ImageTag("sha-{{merge_sha_short8}}", "def142b97b099bb7550ac9f4cb1ac32d16162740")
	if got != "sha-def142b9" {
		t.Fatalf("got %q, want sha-def142b9", got)
	}
}

func TestLoad_ValidFixture(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "testdata", "project.yml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Application != "podinfo" || cfg.Release.RequiredCheck != "publish / build-and-push" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err == nil {
		t.Fatal("want an error for a missing project.yml")
	}
}

func TestSanitizeApplication(t *testing.T) {
	if got := SanitizeApplication("Podinfo"); got != "podinfo" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeApplication("!!!"); got != "app" {
		t.Fatalf("got %q, want app", got)
	}
}

func TestParseGitHubRemote(t *testing.T) {
	got, err := parseGitHubRemote("https://github.com/AndrewMaged814/podinfo.git")
	if err != nil || got != "AndrewMaged814/podinfo" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestDefaultYAML_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yml")
	if err := os.WriteFile(path, DefaultYAML("podinfo", "AndrewMaged814/podinfo", "master", "ghcr.io/andrewmaged814/podinfo"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load default YAML: %v", err)
	}
	if cfg.Repository.DefaultBranch != "master" || cfg.Release.RequiredCheck != "build-and-push" {
		t.Fatalf("unexpected default: %+v", cfg)
	}
}
