package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
)

func TestSetupDiscoversRepoAndActivatesProposalOutsideAppRepo(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	userHome := t.TempDir()
	t.Setenv(project.HomeEnv, home)
	setUserHome(t, userHome)
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	writeSetupFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "jobs:\n  publish:\n    name: Publish image\n")
	writeSetupFile(t, filepath.Join(root, "Dockerfile"), "FROM mcr.microsoft.com/dotnet/aspnet:10.0\n")

	proposal := setupengine.ConservativeProposal(setupengine.Snapshot{
		Application:     "safelane-demo-api",
		Repository:      "AndrewMaged814/safelane-demo-api",
		DefaultBranch:   "main",
		ImageRepository: "ghcr.io/andrewmaged814/safelane-demo-api",
		RequiredChecks:  []string{"Publish image"},
		Files:           []setupengine.File{{Path: "Dockerfile"}},
	})
	var stdout, stderr bytes.Buffer
	code := setupCommand(root, setupDeps{
		recommend: func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error) {
			return proposal, nil
		},
	}).Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("setup exit = %d, stderr: %s\nstdout: %s", code, stderr.String(), stdout.String())
	}

	loc := project.ForApp(home, "safelane-demo-api")
	if _, err := os.Stat(loc.ProjectFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(loc.PolicyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(loc.TemplateDir, "20-rollout.yaml.tmpl")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".safelane")); !os.IsNotExist(err) {
		t.Fatalf("setup wrote into application repository: %v", err)
	}
	if !strings.Contains(stdout.String(), "setup ready") {
		t.Fatalf("setup output missing completion: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Apply this setup?") {
		t.Fatalf("setup still asked for approval: %s", stdout.String())
	}
	for _, section := range []string{"Recommendation", "Policy:", "Release Template", "- Default lane"} {
		if !strings.Contains(stdout.String(), section) {
			t.Fatalf("setup output missing structured section %q: %s", section, stdout.String())
		}
	}
}

func TestSetupAutomaticallyActivatesWithoutApproval(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv(project.HomeEnv, home)
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	proposal := setupengine.ConservativeProposal(setupengine.Snapshot{Application: "safelane-demo-api", RequiredChecks: []string{"Test"}})
	var stdout, stderr bytes.Buffer
	code := setupCommand(root, setupDeps{
		recommend: func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error) { return proposal, nil },
	}).Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("setup auto-apply exit = %d, stderr: %s\nstdout: %s", code, stderr.String(), stdout.String())
	}
	loc := project.ForApp(home, "safelane-demo-api")
	if _, err := os.Stat(loc.ProjectFile); err != nil {
		t.Fatalf("auto-apply did not write operator project: %v", err)
	}
	if strings.Contains(stdout.String(), "Apply this setup?") {
		t.Fatalf("setup still asked for approval: %s", stdout.String())
	}
}

func TestSetupRecommendationProgressUsesChangingStatuses(t *testing.T) {
	var progress bytes.Buffer
	want := setupengine.ConservativeProposal(setupengine.Snapshot{Application: "app"})
	_, err := recommendWithProgress(context.Background(), setupengine.Snapshot{}, func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error) {
		time.Sleep(1100 * time.Millisecond)
		return want, nil
	}, &progress)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(progress.String(), "Preparing SafeLane setup") || !strings.Contains(progress.String(), "SafeLane recommendation ready") {
		t.Fatalf("progress output did not show the calm setup lifecycle: %q", progress.String())
	}
	if strings.Contains(progress.String(), "\r") || strings.Contains(progress.String(), "⠋") {
		t.Fatalf("non-terminal progress should not contain animation control sequences: %q", progress.String())
	}
}

func writeSetupFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
