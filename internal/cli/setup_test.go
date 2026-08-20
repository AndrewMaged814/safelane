package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
)

func TestSetupDiscoversRepoAndActivatesApprovedProposalOutsideAppRepo(t *testing.T) {
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
		input: strings.NewReader("y\n"),
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
}

func TestSetupDeclinesWithoutWritingOperatorFiles(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv(project.HomeEnv, home)
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	proposal := setupengine.ConservativeProposal(setupengine.Snapshot{Application: "safelane-demo-api", RequiredChecks: []string{"Test"}})
	var stdout, stderr bytes.Buffer
	code := setupCommand(root, setupDeps{
		recommend: func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error) { return proposal, nil },
		input:     strings.NewReader("n\n"),
	}).Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitFail {
		t.Fatalf("setup decline exit = %d, want %d", code, ExitFail)
	}
	if _, err := os.Stat(filepath.Join(home, "apps")); !os.IsNotExist(err) {
		t.Fatalf("declined setup wrote operator files: %v", err)
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
