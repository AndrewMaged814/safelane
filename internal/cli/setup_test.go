package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
)

func setUserHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDeterministicSetupWritesOutsideRepositoryWithoutCallingAnAgent(t *testing.T) {
	root, home := setupRepository(t)
	var stdout, stderr bytes.Buffer
	code := runDeterministicSetup(context.Background(), []string{"--yes", "--json"}, strings.NewReader(""), &stdout, &stderr, root)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr: %s", code, stderr.String())
	}
	loc := project.ForApp(home, "safelane-demo-api")
	for _, path := range []string{loc.ProjectFile, loc.PolicyFile, filepath.Join(loc.TemplateDir, "20-rollout.yaml.tmpl")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".safelane")); !os.IsNotExist(err) {
		t.Fatalf("setup wrote inside repository: %v", err)
	}
	var result ResultEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if result.State != "configured" || result.NextCommand != "safelane doctor" {
		t.Fatalf("result = %+v", result)
	}
}

func TestSetupRequiresOneExplicitConfirmation(t *testing.T) {
	root, _ := setupRepository(t)
	var stdout, stderr bytes.Buffer
	if code := runDeterministicSetup(context.Background(), nil, strings.NewReader("no\n"), &stdout, &stderr, root); code != ExitDecision {
		t.Fatalf("exit = %d, want %d", code, ExitDecision)
	}
}

func TestSetupApplyRejectsStaleFingerprintBeforeWriting(t *testing.T) {
	root, home := setupRepository(t)
	snapshot, err := setupengine.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	proposal := setupengine.ConservativeProposal(snapshot)
	proposal.InspectionFingerprint = "sha256:stale"
	raw, _ := json.Marshal(proposal)
	path := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runSetupApply(context.Background(), []string{"--proposal", path, "--yes"}, strings.NewReader(""), &stdout, &stderr, root)
	if code != ExitFail || !strings.Contains(stderr.String(), "fingerprint is stale") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(project.ForApp(home, "safelane-demo-api").ProjectFile); !os.IsNotExist(err) {
		t.Fatalf("stale proposal wrote configuration: %v", err)
	}
}

func setupRepository(t *testing.T) (string, string) {
	t.Helper()
	root, home, userHome := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(project.HomeEnv, home)
	setUserHome(t, userHome)
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	writeSetupFixture(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "jobs:\n  publish:\n    name: Publish image\n")
	writeSetupFixture(t, filepath.Join(root, "Program.cs"), `app.MapGet("/api/demo", () => new { status = "ok" }); app.MapGet("/version", () => "abc");`)
	return root, home
}

func writeSetupFixture(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
