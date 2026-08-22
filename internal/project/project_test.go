package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestImageTag_ShortSHA(t *testing.T) {
	got := ImageTag("sha-{{merge_sha_short8}}", "def142b97b099bb7550ac9f4cb1ac32d16162740")
	if got != "sha-def142b9" {
		t.Fatalf("got %q, want sha-def142b9", got)
	}
}

func TestImageTag_FullSHA(t *testing.T) {
	sha := "def142b97b099bb7550ac9f4cb1ac32d16162740"
	got := ImageTag("sha-{{merge_sha}}", sha)
	if got != "sha-"+sha {
		t.Fatalf("got %q", got)
	}
}

func TestImageTag_DefaultMatchesPublishWorkflow(t *testing.T) {
	sha := "def142b97b099bb7550ac9f4cb1ac32d16162740"
	got := ImageTag("", sha)
	if got != "sha-"+sha {
		t.Fatalf("default tag = %q, want sha-<full merge SHA> to match docker tags: sha-${{ github.sha }}", got)
	}
}

func TestLoad_ValidFixture(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "testdata", "project.yml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Application != "safelane-demo-api" || cfg.Release.RequiredCheck != "build-and-push" {
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
	if got := SanitizeApplication("SafeLane Demo API"); got != "safelanedemoapi" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeApplication("!!!"); got != "app" {
		t.Fatalf("got %q, want app", got)
	}
}

func TestParseGitHubRemote(t *testing.T) {
	got, err := parseGitHubRemote("https://github.com/AndrewMaged814/safelane-demo-api.git")
	if err != nil || got != "AndrewMaged814/safelane-demo-api" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

func TestDefaultYAML_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "project.yml")
	if err := os.WriteFile(path, DefaultYAML("safelane-demo-api", "AndrewMaged814/safelane-demo-api", "master", "ghcr.io/andrewmaged814/safelane-demo-api"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load default YAML: %v", err)
	}
	if cfg.Repository.DefaultBranch != "master" || len(cfg.Release.RequiredChecks) != 1 || cfg.Release.RequiredChecks[0] != "build-and-push" {
		t.Fatalf("unexpected default: %+v", cfg)
	}
	if cfg.Release.ImageTag != DefaultImageTag {
		t.Fatalf("image_tag = %q, want %q", cfg.Release.ImageTag, DefaultImageTag)
	}
	if cfg.ControllerKubeconfig != "controller.kubeconfig" || cfg.ControllerContext != "safelane-controller" {
		t.Fatalf("controller identity = %q / %q, want default kubeconfig and context", cfg.ControllerKubeconfig, cfg.ControllerContext)
	}
}

func TestYAMLUsesAgentRuntimeAssertions(t *testing.T) {
	raw := YAML("app", "owner/app", "main", "ghcr.io/owner/app", []string{"Test"}, []RuntimeAssertion{{
		ID: "behavior", Surface: "GET /api/demo", Expectation: `HTTP 200 and JSON status equals "ok"`, Covers: "correctness",
	}})
	path := filepath.Join(t.TempDir(), "project.yml")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Analysis.Assertions) != 1 || cfg.Analysis.Assertions[0].ID != "behavior" {
		t.Fatalf("assertions = %+v", cfg.Analysis.Assertions)
	}
}

func TestResolveIn_MatchesGitHubRemoteNotDirectoryName(t *testing.T) {
	clone := filepath.Join(t.TempDir(), "renamed-working-copy")
	if err := os.Mkdir(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	runProjectGit(t, clone, "init")
	runProjectGit(t, clone, "remote", "add", "origin", "git@github.com:AndrewMaged814/safelane-demo-api.git")

	home := t.TempDir()
	wanted := ForApp(home, "safelane-demo-api")
	other := ForApp(home, "other")
	for path, body := range map[string][]byte{
		wanted.ProjectFile: DefaultYAML("safelane-demo-api", "AndrewMaged814/safelane-demo-api", "master", "ghcr.io/andrewmaged814/safelane-demo-api"),
		other.ProjectFile:  DefaultYAML("other", "someone/other", "master", "ghcr.io/someone/other"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ResolveIn(clone, home)
	if err != nil {
		t.Fatalf("ResolveIn: %v", err)
	}
	if got.AppDir != wanted.AppDir || got.ReleasesDir != wanted.ReleasesDir {
		t.Fatalf("resolved %+v, want app dir %s", got, wanted.AppDir)
	}
	if _, err := os.Stat(filepath.Join(clone, ".safelane")); !os.IsNotExist(err) {
		t.Fatalf("resolver wrote into clone: %v", err)
	}
}

func runProjectGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
