package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/cli"
)

// TestRelease_RealServices_AgainstIntent runs the wired `safelane release`
// path against testdata/release-request.json with real GitHub and GHCR.
// Outcome depends on live PR/package state; the test only asserts that
// intake and orchestration produce a Release record rather than a template
// or usage error.
func TestRelease_RealServices_AgainstIntent(t *testing.T) {
	if testing.Short() {
		t.Skip("requires network access to api.github.com and ghcr.io")
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".safelane"), 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := os.ReadFile(filepath.Join("..", "..", "testdata", "project.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".safelane", "project.yml"), project, 0o644); err != nil {
		t.Fatal(err)
	}

	commands := []cli.Command{cli.ReleaseCommand(root, t.TempDir())}
	var stdout, stderr bytes.Buffer

	code := cli.Dispatch(t.Context(), []string{
		"release",
		"--file", "../../testdata/release-request.json",
		"--template-dir", "../../internal/render/testdata/release-template",
	}, &stdout, &stderr, commands)

	if code != cli.ExitOK && code != cli.ExitFail {
		t.Fatalf("want a release attempt, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	out := stdout.String() + stderr.String()
	if strings.Contains(out, "could not load the Release Template") {
		t.Fatalf("must not fail at template load after a valid intent, got %q", out)
	}
	if !strings.Contains(stdout.String(), "release_id: rel_") && !strings.Contains(stderr.String(), "rejected") {
		t.Fatalf("want a release id or a typed rejection, got stdout %q stderr %q", stdout.String(), stderr.String())
	}
}
