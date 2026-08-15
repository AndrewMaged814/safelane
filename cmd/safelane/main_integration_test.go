package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/cli"
)

// TestRelease_RealFixture_AgainstRealGitHubAndGHCR runs the whole wired
// `safelane release` path -- real GitHub API, real GHCR anonymous flow, the
// fixture Release Template -- against testdata/release-evidence.json. No
// mocks anywhere in this test.
//
// The fixture's PR and GHCR digest are placeholders (see testdata/README.md):
// the Podinfo fork does not exist yet. So this asserts the *shape* of a real
// failure, not a green verification: GitHub 404s (the repository does not
// exist) and GHCR's token endpoint 403s (the package is not public), and
// both must land as unknown, never as a pass. Once #46 publishes real
// evidence and the fixture is updated, this test should be revisited to
// assert a verified outcome instead.
//
// Skipped in -short runs since it requires network access.
func TestRelease_RealFixture_AgainstRealGitHubAndGHCR(t *testing.T) {
	if testing.Short() {
		t.Skip("requires network access to api.github.com and ghcr.io")
	}

	commands := []cli.Command{cli.ReleaseCommand("../../internal/render/testdata/release-template", t.TempDir())}
	var stdout, stderr bytes.Buffer

	code := cli.Dispatch(t.Context(), []string{"release", "--file", "../../testdata/release-evidence.json"}, &stdout, &stderr, commands)

	if code != cli.ExitFail {
		t.Fatalf("want ExitFail against placeholder fixture evidence, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "release_id: rel_") {
		t.Fatalf("want a release id even for withheld evidence, got %q", out)
	}
	if !strings.Contains(out, "evidence: unknown") {
		t.Fatalf("want the placeholder PR/digest to land as unknown, not a pass, got %q", out)
	}
	if !strings.Contains(out, "eligibility: indeterminate") {
		t.Fatalf("want placeholder evidence to be indeterminate, got %q", out)
	}
	if !strings.Contains(out, "retryable: true") {
		t.Fatalf("want indeterminate to be retryable, got %q", out)
	}
	if strings.Contains(out, "rollout_envelope:") {
		t.Fatalf("indeterminate must not attach an envelope, got %q", out)
	}
}
