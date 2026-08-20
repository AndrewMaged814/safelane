package cli

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
)

func TestInit_CreatesOperatorFilesOutsideRepository_MatchesA01(t *testing.T) {
	repoRoot := t.TempDir()
	home := t.TempDir()
	t.Setenv(project.HomeEnv, home)
	var stdout, stderr bytes.Buffer

	code := InitCommand(repoRoot).Run(context.Background(), []string{
		"--app", "podinfo", "--repo", "AndrewMaged814/podinfo",
	}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	loc := project.ForApp(home, "podinfo")
	for _, path := range []string{loc.ProjectFile, loc.PolicyFile, loc.TemplateDir, loc.ReleasesDir} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("want %s: %v", path, err)
		}
	}
	entries, err := os.ReadDir(loc.TemplateDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := countTemplateFiles(entries); got != 5 {
		t.Fatalf("template contains %d files, want 5", got)
	}
	if _, err := policy.Load(loc.PolicyFile); err != nil {
		t.Fatalf("generated policy does not load: %v", err)
	}
	if _, err := project.Load(loc.ProjectFile); err != nil {
		t.Fatalf("generated project does not load: %v", err)
	}

	got := strings.ReplaceAll(filepath.ToSlash(stdout.String()), filepath.ToSlash(home), "~/.safelane")
	assertGoldenFragment(t, "a0-1-init.txt", got)
	assertNoSafeLaneContent(t, repoRoot)
}

func TestInit_ThenInspect_FromBareCloneLeavesNoSafeLaneContent(t *testing.T) {
	clone := t.TempDir()
	home := t.TempDir()
	t.Setenv(project.HomeEnv, home)
	runGit(t, clone, "init")
	runGit(t, clone, "remote", "add", "origin", "https://github.com/AndrewMaged814/podinfo.git")

	var stdout, stderr bytes.Buffer
	if code := InitCommand(clone).Run(context.Background(), []string{
		"--app", "podinfo", "--repo", "AndrewMaged814/podinfo",
	}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: code %d: %s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	missingTemplate := filepath.Join(t.TempDir(), "missing-template")
	code := ReleaseCommand(clone, "").Run(context.Background(), []string{
		"inspect", "--pr", "3", "--template-dir", missingTemplate,
	}, &stdout, &stderr)
	if code != ExitFail {
		t.Fatalf("inspect: want ExitFail from deliberate template stop, got %d: %s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "missing_project_config") {
		t.Fatalf("inspect did not resolve the operator config: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "could not load the Release Template") {
		t.Fatalf("inspect did not get past config resolution: %s", stderr.String())
	}
	assertNoSafeLaneContent(t, clone)
}

func TestInit_RequiresAppAndRepoAndRejectsAdapter(t *testing.T) {
	t.Setenv(project.HomeEnv, t.TempDir())
	for _, args := range [][]string{nil, {"--adapter", "codex"}} {
		var stdout, stderr bytes.Buffer
		if code := InitCommand(t.TempDir()).Run(context.Background(), args, &stdout, &stderr); code != ExitUsage {
			t.Fatalf("args %v: want ExitUsage, got %d", args, code)
		}
	}
}

func assertNoSafeLaneContent(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(filepath.ToSlash(rel)), "safelane") {
			t.Errorf("application repository contains SafeLane path %s", rel)
		}
		if entry.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(bytes.ToLower(body), []byte("safelane")) {
			t.Errorf("application repository contains SafeLane text in %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
