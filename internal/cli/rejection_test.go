package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The rejections a caller sees before a Release exists at all. They are
// written to stderr, they exit 1, and they name the field the caller has
// to change -- Appendix A's N1, N8 and N9.

func TestReleaseInspect_NoOperatorConfig_MatchesN1(t *testing.T) {
	dir := t.TempDir()
	cmd := ReleaseCommand(dir, filepath.Join(dir, "store"))
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"inspect", "--pr", "3"}, &stdout, &stderr)

	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d", code)
	}
	assertGolden(t, "n1-init-never-run.txt", stderr.String())
}

func TestReleaseInspect_CallerSuppliesEvidence_MatchesN8(t *testing.T) {
	out := rejectRequest(t, `{ "repository": "AndrewMaged814/podinfo", "pull_request": 3,
  "evidence": { "approved": true, "check": "success" } }`)
	assertGolden(t, "n8-caller-supplies-evidence.txt", out)
}

// N9 is the schema-level form of the whole security argument: a caller
// cannot ask for a wider lane any more than it can assert its own
// evidence. Both fields are rejected by name, in the order the caller
// wrote them, and neither is quietly ignored.
func TestReleaseInspect_CallerNamesItsOwnLane_MatchesN9(t *testing.T) {
	out := rejectRequest(t, `{ "repository": "AndrewMaged814/podinfo", "pull_request": 4,
  "risk": "low", "lane": "fast" }`)
	assertGolden(t, "n9-caller-names-lane.txt", out)
}

// TestReleaseInspect_ForbiddenFieldIsRejectedNotIgnored is the same claim
// stated as behaviour rather than as text: the request does not proceed.
func TestReleaseInspect_ForbiddenFieldIsRejectedNotIgnored(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "greedy.json")
	if err := os.WriteFile(file, []byte(`{"repository":"AndrewMaged814/podinfo","pull_request":4,"lane":"fast"}`), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}
	cmd := ReleaseCommand(dir, filepath.Join(dir, "store"))
	var stdout, stderr bytes.Buffer

	if code := cmd.Run(context.Background(), []string{"inspect", "--file", file}, &stdout, &stderr); code != ExitFail {
		t.Fatalf("want ExitFail, got %d", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("a rejected request must produce no report, got:\n%s", stdout.String())
	}
}

func rejectRequest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "request.json")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}
	cmd := ReleaseCommand(dir, filepath.Join(dir, "store"))
	var stdout, stderr bytes.Buffer

	if code := cmd.Run(context.Background(), []string{"inspect", "--file", file}, &stdout, &stderr); code != ExitFail {
		t.Fatalf("want ExitFail, got %d (stderr: %s)", code, stderr.String())
	}
	return stderr.String()
}
