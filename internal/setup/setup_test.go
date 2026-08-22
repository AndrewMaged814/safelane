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

func TestDiscoverReturnsFactsAndProductAssertionsWithoutAnAgentBaseline(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "remote", "add", "origin", "https://github.com/AndrewMaged814/safelane-demo-api.git")
	writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "jobs:\n  test:\n    name: Test\n")
	writeFile(t, filepath.Join(root, "Program.cs"), `app.MapGet("/api/demo", () => new { status = "ok" }); app.MapGet("/version", () => "abc");`)

	snapshot, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.MandatoryAssertions) != 3 {
		t.Fatalf("mandatory runtime assertions = %d, want availability, latency, and identity", len(snapshot.MandatoryAssertions))
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"\"proposal\"", "policy_yaml", "template_files", "risk_paths"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("inspection exposed configuration baseline field %q: %s", forbidden, raw)
		}
	}
}

func TestFingerprintChangesWithFileContentDigest(t *testing.T) {
	first := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:first"}}}
	second := Snapshot{Files: []File{{Path: "src/Program.cs", ContentSHA256: "sha256:second"}}}
	if Fingerprint(first) == Fingerprint(second) {
		t.Fatal("fingerprint did not bind compact file content digest")
	}
}

func TestConservativeFindingsUseTheSameCompilerAsAgentFindings(t *testing.T) {
	snapshot := Snapshot{
		Application:         "safelane-demo-api",
		Repository:          "AndrewMaged814/safelane-demo-api",
		RequiredChecks:      []string{"Publish image"},
		Files:               []File{{Path: "Dockerfile"}, {Path: ".github/workflows/ci.yml"}},
		CriticalSurfaces:    []string{"GET /api/demo", "GET /version"},
		MandatoryAssertions: []RuntimeAssertion{{ID: "identity", Surface: "GET /version", Expectation: "commit matches", Covers: "artifact-identity"}},
	}
	findings := ConservativeFindings(snapshot)
	if err := ValidateFindings(findings, snapshot, false); err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileFindings(findings, snapshot, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.PolicyYAML, `glob: "Dockerfile"`) {
		t.Fatal("fallback policy did not use repository-shaped Dockerfile rule")
	}
	if len(findings.RiskPaths) == 0 {
		t.Fatal("fallback findings did not include bounded risk-path decisions")
	}
	if len(compiled.TemplateFiles) != 4 {
		t.Fatalf("compiled template files = %d, want two services, analysis, and rollout", len(compiled.TemplateFiles))
	}
}

func TestCompileFindingsKeepsInfrastructureOperatorOwned(t *testing.T) {
	snapshot := validSnapshot()
	snapshot.Files = append(snapshot.Files, File{Path: "infra/bootstrap.ps1", Content: "selector:\n  app: stale-repository-contract\ntargetPort: 8080"})
	findings := ConservativeFindings(snapshot)
	compiled, err := CompileFindings(findings, snapshot, false)
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

func TestValidateFindingsRejectsInvalidRiskPath(t *testing.T) {
	snapshot := validSnapshot()
	findings := ConservativeFindings(snapshot)
	findings.RiskPaths = append(findings.RiskPaths, RiskPath{Glob: "../outside/**", Minimum: "high", Reason: "escape"})
	if err := ValidateFindings(findings, snapshot, false); err == nil || !strings.Contains(err.Error(), "risk path") {
		t.Fatalf("error = %v, want unsafe risk path rejection", err)
	}
}

func TestValidateFindingsRejectsMissingRuntimeAssertions(t *testing.T) {
	snapshot := validSnapshot()
	findings := ConservativeFindings(snapshot)
	findings.AssertionIntents = nil
	if err := ValidateFindings(findings, snapshot, false); err == nil || !strings.Contains(err.Error(), "assertion intents") {
		t.Fatalf("error = %v, want assertion intents rejection", err)
	}
}

func TestAgentFindingsRequireRealEvidenceAndCannotClaimProductCoverage(t *testing.T) {
	snapshot := validSnapshot()
	findings := ConservativeFindings(snapshot)
	if err := ValidateFindings(findings, snapshot, true); err == nil || !strings.Contains(err.Error(), "evidence") {
		t.Fatalf("error = %v, want missing evidence rejection", err)
	}
	findings.RiskPaths[0].Evidence = []Evidence{{File: "Program.cs", Line: 1}}
	findings.AssertionIntents[0].Evidence = []Evidence{{File: "Program.cs", Line: 1}}
	findings.AssertionIntents[0].Covers = "latency"
	if err := ValidateFindings(findings, snapshot, true); err == nil || !strings.Contains(err.Error(), "SafeLane-owned coverage") {
		t.Fatalf("error = %v, want product coverage rejection", err)
	}
	findings.AssertionIntents[0].Covers = "correctness"
	findings.AssertionIntents[0].Surface = "GET /invented"
	if err := ValidateFindings(findings, snapshot, true); err == nil || !strings.Contains(err.Error(), "not executable") {
		t.Fatalf("error = %v, want unsupported assertion rejection", err)
	}
}

func TestPlanIDBindsTheExactCompiledSetup(t *testing.T) {
	snapshot := validSnapshot()
	findings := ConservativeFindings(snapshot)
	compiled, err := CompileFindings(findings, snapshot, false)
	if err != nil {
		t.Fatal(err)
	}
	plan := NewPlan(snapshot, findings, compiled, false)
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.Compiled.PolicyYAML += "\n# changed"
	if err := ValidatePlan(plan); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want modified plan rejection", err)
	}
}

func validSnapshot() Snapshot {
	return Snapshot{
		Application: "app", RequiredChecks: []string{"Test"}, CriticalSurfaces: []string{"GET /api/demo"},
		MandatoryAssertions: []RuntimeAssertion{{ID: "availability", Surface: "GET /api/demo", Expectation: "success rate", Covers: "availability"}},
		Files:               []File{{Path: "Program.cs", Content: "route", ContentSHA256: "sha256:route"}},
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
