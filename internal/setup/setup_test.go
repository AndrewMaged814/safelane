package setup

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverDerivesRepositoryAndWorkflowChecksWithoutSecrets(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: CI\n\njobs:\n  test:\n    name: Test\n  publish:\n    name: Publish image\n")
	writeFile(t, filepath.Join(root, "Dockerfile"), "FROM mcr.microsoft.com/dotnet/aspnet:10.0\n")
	writeFile(t, filepath.Join(root, ".env"), "TOKEN=must-not-be-sent")

	snapshot, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Application != "safelane-demo-api" {
		t.Fatalf("application = %q", snapshot.Application)
	}
	if snapshot.Repository != "AndrewMaged814/safelane-demo-api" {
		t.Fatalf("repository = %q", snapshot.Repository)
	}
	if snapshot.ImageRepository != "ghcr.io/andrewmaged814/safelane-demo-api" {
		t.Fatalf("image repository = %q", snapshot.ImageRepository)
	}
	if strings.Join(snapshot.RequiredChecks, ",") != "Publish image,Test" {
		t.Fatalf("checks = %v", snapshot.RequiredChecks)
	}
	for _, file := range snapshot.Files {
		if file.Path == ".env" || strings.Contains(file.Content, "must-not-be-sent") {
			t.Fatalf("sensitive file entered snapshot: %+v", file)
		}
	}
}

func TestConservativeProposalIsProjectShapedAndValid(t *testing.T) {
	proposal := ConservativeProposal(Snapshot{
		Application:    "safelane-demo-api",
		Repository:     "AndrewMaged814/safelane-demo-api",
		RequiredChecks: []string{"Publish image"},
		Files:          []File{{Path: "Dockerfile"}, {Path: ".github/workflows/ci.yml"}},
	})
	if err := ValidateProposal(proposal, Snapshot{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proposal.PolicyYAML, `glob: "Dockerfile"`) {
		t.Fatal("fallback policy did not use repository-shaped Dockerfile rule")
	}
}

func TestRecommendExtractsStructuredResultNestedInStreamEvent(t *testing.T) {
	want := ConservativeProposal(Snapshot{Application: "app", RequiredChecks: []string{"Test"}})
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	stream := `{"type":"result","structured_output":` + string(payload) + "}\n"
	got, err := Recommend(context.Background(), Snapshot{}, func(context.Context, string) ([]byte, error) {
		return []byte(stream), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Summary != want.Summary || len(got.TemplateFiles) != len(want.TemplateFiles) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRecommendationPromptRequiresCompletePolicyShape(t *testing.T) {
	prompt := recommendationPrompt(Snapshot{})
	for _, required := range []string{"COMPLETE policy.yml", "lanes", "risk_to_lane", "default_lane", "assessment.heuristic", "assessment.model", "changed_lines_at_least", "timeout: 90s", "Do not turn paths or size into maps", ".yaml.tmpl", "Do not return .yaml", "exactly low, medium, or high", "standard is a lane name"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing policy contract %q", required)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
