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

func TestDiscoverIncludesAReadyValidBoundedAgentProposal(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "jobs:\n  test:\n    name: Test\n")
	writeFile(t, filepath.Join(root, "Program.cs"), `app.MapGet("/api/demo", () => new { status = "ok" }); app.MapGet("/version", () => "abc");`)

	snapshot, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Proposal == nil {
		t.Fatal("inspection omitted its ready agent proposal")
	}
	if snapshot.Proposal.InspectionFingerprint != snapshot.InspectionFingerprint {
		t.Fatalf("proposal fingerprint = %q, inspection = %q", snapshot.Proposal.InspectionFingerprint, snapshot.InspectionFingerprint)
	}
	if len(snapshot.Proposal.RuntimeAssertions) != 4 {
		t.Fatalf("proposal runtime assertions = %d, want 4", len(snapshot.Proposal.RuntimeAssertions))
	}
	raw, err := json.Marshal(snapshot.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"policy_yaml", "template_files", "required_checks", "template_highlights"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("bounded proposal exposed operator-owned field %q: %s", forbidden, raw)
		}
	}
	if len(raw) > 2500 {
		t.Fatalf("bounded proposal is %d bytes, want at most 2500", len(raw))
	}
	if err := ValidateProposal(*snapshot.Proposal, snapshot); err != nil {
		t.Fatalf("inspection proposal is not directly applicable: %v", err)
	}
}

func TestFingerprintChangesWithFileContentDigest(t *testing.T) {
	first := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:first"}}}
	second := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:second"}}}
	if Fingerprint(first) == Fingerprint(second) {
		t.Fatal("fingerprint did not bind compact file content digest")
	}
}

func TestFingerprintDoesNotDependOnEmbeddedProposal(t *testing.T) {
	snapshot := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:first"}}}
	want := Fingerprint(snapshot)
	snapshot.Proposal = &Proposal{Summary: "agent changed this draft"}
	if got := Fingerprint(snapshot); got != want {
		t.Fatalf("fingerprint changed from %q to %q when only the proposal changed", want, got)
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
	compiled, err := CompileProposal(proposal, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.PolicyYAML, `glob: "Dockerfile"`) {
		t.Fatal("fallback policy did not use repository-shaped Dockerfile rule")
	}
	if len(proposal.RiskPaths) == 0 {
		t.Fatal("fallback proposal did not include bounded risk-path decisions")
	}
	if len(compiled.TemplateFiles) != 4 {
		t.Fatalf("compiled template files = %d, want two services, analysis, and rollout", len(compiled.TemplateFiles))
	}
}

func TestCompileProposalKeepsInfrastructureOperatorOwned(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Files = append(snapshot.Files, File{Path: "infra/bootstrap.ps1", Content: "selector:\n  app: stale-repository-contract\ntargetPort: 8080"})
	proposal := ConservativeProposal(snapshot)
	compiled, err := CompileProposal(proposal, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, file := range compiled.TemplateFiles {
		all.WriteString(file.Content)
	}
	if strings.Contains(all.String(), "stale-repository-contract") || !strings.Contains(all.String(), "app.kubernetes.io/name: app") {
		t.Fatalf("compiled infrastructure followed repository prose instead of the operator contract:\n%s", all.String())
	}
}

func TestValidateProposalRejectsInvalidRiskPath(t *testing.T) {
	snapshot := validSnapshot()
	proposal := ConservativeProposal(snapshot)
	proposal.RiskPaths = append(proposal.RiskPaths, RiskPath{Glob: "../outside/**", Minimum: "high", Reason: "escape"})
	if err := ValidateProposal(proposal, snapshot); err == nil || !strings.Contains(err.Error(), "risk path") {
		t.Fatalf("error = %v, want unsafe risk path rejection", err)
	}
}

func TestValidateProposalRejectsMissingRuntimeAssertions(t *testing.T) {
	snapshot := validSnapshot()
	proposal := ConservativeProposal(snapshot)
	proposal.RuntimeAssertions = nil
	if err := ValidateProposal(proposal, snapshot); err == nil || !strings.Contains(err.Error(), "runtime assertions") {
		t.Fatalf("error = %v, want runtime assertions rejection", err)
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
