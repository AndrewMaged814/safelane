package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/cli"
	"github.com/AndrewMaged814/safelane/internal/policy"
)

// TestReleaseInspect_RealServices_AgainstIntent runs the wired
// `safelane release inspect` path against testdata/release-request.json
// with real GitHub and GHCR.
//
// This is the wiring check the golden-file suite cannot make: every one of
// those runs against fakes, and what they cannot catch is a real client
// never being attached in the first place. Outcome depends on live
// PR/package state, so the assertions are about which path was taken --
// a report or a typed rejection -- not about what it concluded.
func TestReleaseInspect_RealServices_AgainstIntent(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, ".safelane", "policy.yml"), policy.DefaultYAML(), 0o644); err != nil {
		t.Fatal(err)
	}

	commands := []cli.Command{cli.ReleaseCommand(root, t.TempDir())}
	var stdout, stderr bytes.Buffer

	code := cli.Dispatch(t.Context(), []string{
		"release", "inspect",
		"--file", "../../testdata/release-request.json",
		"--project", filepath.Join(root, ".safelane", "project.yml"),
		"--template-dir", "../../internal/render/testdata/release-template",
	}, &stdout, &stderr, commands)

	if code != cli.ExitOK && code != cli.ExitFail {
		t.Fatalf("want a release attempt, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	out := stdout.String() + stderr.String()
	if strings.Contains(out, "could not load the Release Template") {
		t.Fatalf("must not fail at template load after a valid intent, got %q", out)
	}

	if strings.Contains(stderr.String(), "rejected") {
		return // a typed rejection is a legitimate outcome for live state
	}
	// Otherwise this is the report, and every section of it must be there:
	// a partially rendered investigation is worse than none, because the
	// missing section reads as "nothing to say" rather than "not printed".
	for _, section := range []string{
		"SafeLane investigation", "Target", "Detected", "Decision", "Nothing was changed.",
	} {
		if !strings.Contains(stdout.String(), section) {
			t.Errorf("the report is missing %q:\n%s", section, stdout.String())
		}
	}
}
