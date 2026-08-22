package setup

import (
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
	writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: CI\n\njobs:\n  test:\n    name: Test\n  publish:\n    name: Publish image\n  fixtures:\n    name: Publish fixture (${{ matrix.name }})\n")
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

func TestSnapshotJSONContainsCompactFileEvidenceNotSourceContent(t *testing.T) {
	snapshot := Snapshot{Files: []File{{Path: "src/Program.cs", Bytes: 14, ContentSHA256: "sha256:abc", Content: "TOP SECRET CODE"}}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "TOP SECRET CODE") {
		t.Fatalf("inspection leaked source content: %s", text)
	}
	if !strings.Contains(text, `"content_sha256":"sha256:abc"`) || !strings.Contains(text, `"bytes":14`) {
		t.Fatalf("inspection omitted compact file evidence: %s", text)
	}
}

func TestFingerprintChangesWithFileContentDigest(t *testing.T) {
	first := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:first"}}}
	second := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:second"}}}
	if Fingerprint(first) == Fingerprint(second) {
		t.Fatal("fingerprint did not bind compact file content digest")
	}
}

func TestConservativeProposalIsProjectShapedAndValid(t *testing.T) {
	snapshot := Snapshot{
		Application:       "safelane-demo-api",
		Repository:        "AndrewMaged814/safelane-demo-api",
		RequiredChecks:    []string{"Publish image"},
		Files:             []File{{Path: "Dockerfile"}, {Path: ".github/workflows/ci.yml"}},
		RuntimeAssertions: []RuntimeAssertion{{ID: "response", Surface: "GET /api/demo", Expectation: "status ok", Covers: "correctness"}},
	}
	proposal := ConservativeProposal(snapshot)
	if err := ValidateProposal(proposal, snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(proposal.PolicyYAML, `glob: "Dockerfile"`) {
		t.Fatal("fallback policy did not use repository-shaped Dockerfile rule")
	}
	if len(proposal.PolicyHighlights) == 0 || len(proposal.TemplateHighlights) == 0 {
		t.Fatal("fallback proposal did not include structured recommendation highlights")
	}
	if len(proposal.TemplateFiles) != 4 {
		t.Fatalf("fallback template files = %d, want two services, analysis, and rollout", len(proposal.TemplateFiles))
	}
}

func TestValidateProposalRejectsMultiDocumentTemplateFile(t *testing.T) {
	snapshot := validSnapshot()
	proposal := ConservativeProposal(snapshot)
	proposal.TemplateFiles[0].Content += "---\napiVersion: v1\nkind: Service\nmetadata:\n  name: second\n"

	if err := ValidateProposal(proposal, snapshot); err == nil || !strings.Contains(err.Error(), "multi_document_template") {
		t.Fatalf("error = %v, want multi_document_template", err)
	}
}

func TestValidateProposalRejectsTemplateThatDropsExternalCanaryProbe(t *testing.T) {
	snapshot := validSnapshot()
	proposal := ConservativeProposal(snapshot)
	for i := range proposal.TemplateFiles {
		proposal.TemplateFiles[i].Content = strings.ReplaceAll(proposal.TemplateFiles[i].Content, "{{ .ProbeImage }}", "busybox:latest")
	}
	if err := ValidateProposal(proposal, snapshot); err == nil || !strings.Contains(err.Error(), "ProbeImage") {
		t.Fatalf("error = %v, want mandatory ProbeImage rejection", err)
	}
}

func TestValidateProposalRejectsMissingRequiredChecks(t *testing.T) {
	snapshot := validSnapshot()
	proposal := ConservativeProposal(snapshot)
	proposal.RequiredChecks = nil
	if err := ValidateProposal(proposal, snapshot); err == nil || !strings.Contains(err.Error(), "required CI checks") {
		t.Fatalf("error = %v, want required CI checks rejection", err)
	}
}

func validSnapshot() Snapshot {
	return Snapshot{Application: "app", RequiredChecks: []string{"Test"}, RuntimeAssertions: []RuntimeAssertion{{ID: "response", Surface: "GET /api/demo", Expectation: "status ok", Covers: "correctness"}}}
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
