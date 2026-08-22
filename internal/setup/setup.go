// Package setup implements SafeLane's deterministic, repository-aware setup.
// Agent-authored findings are an explicit inspect/plan/apply workflow and never
// execute a nested coding-agent process.
package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing/fstest"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/render"
	"gopkg.in/yaml.v3"
)

const (
	maxSnapshotBytes = 120_000
	maxFileBytes     = 24_000
)

// File is a bounded, read-only snapshot of one repository file.
type File struct {
	Path          string `json:"path"`
	Bytes         int    `json:"bytes"`
	ContentSHA256 string `json:"content_sha256"`
	Truncated     bool   `json:"truncated,omitempty"`
	// Content is retained only inside this SafeLane process for deterministic
	// discovery. The active agent already has repository tools; echoing source
	// through setup inspection wastes context and can expose irrelevant text.
	Content string `json:"-"`
}

// Snapshot contains facts SafeLane discovered without changing the app repo.
type Snapshot struct {
	SchemaVersion         string             `json:"schema_version"`
	Application           string             `json:"application"`
	Repository            string             `json:"repository"`
	DefaultBranch         string             `json:"default_branch"`
	ImageRepository       string             `json:"image_repository"`
	RequiredChecks        []string           `json:"required_checks"`
	KubernetesFiles       []string           `json:"kubernetes_files"`
	CriticalSurfaces      []string           `json:"critical_surfaces"`
	MandatoryAssertions   []RuntimeAssertion `json:"mandatory_runtime_assertions"`
	Uncertainties         []string           `json:"uncertainties"`
	Files                 []File             `json:"files"`
	InspectionFingerprint string             `json:"inspection_fingerprint"`
}

// Evidence ties one semantic finding to repository text the operator can review.
type Evidence struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

// RuntimeAssertion is a concrete black-box claim about a discovered route.
type RuntimeAssertion struct {
	ID          string     `json:"id"`
	Surface     string     `json:"surface"`
	Expectation string     `json:"expectation"`
	Covers      string     `json:"covers"`
	Evidence    []Evidence `json:"evidence,omitempty"`
}

// AssertionIntent identifies semantic coverage the analyst requires. SafeLane
// compiles it into an executable Runtime Assertion supported by the probe.
type AssertionIntent struct {
	Surface  string     `json:"surface"`
	Covers   string     `json:"covers"`
	Evidence []Evidence `json:"evidence"`
}

// TemplateFile is one operator-owned Release Template file compiled by SafeLane.
type TemplateFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// RiskPath is one bounded semantic decision the active agent may make. SafeLane
// compiles it into the operator-owned policy; the agent never authors policy YAML.
type RiskPath struct {
	Glob     string     `json:"glob"`
	Minimum  string     `json:"minimum"`
	Reason   string     `json:"reason"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Findings are the replaceable semantic analyst's bounded, evidence-backed
// input. SafeLane remains the sole setup authority and configuration compiler.
type Findings struct {
	SchemaVersion         string            `json:"schema_version"`
	InspectionFingerprint string            `json:"inspection_fingerprint"`
	Summary               string            `json:"summary"`
	RiskPaths             []RiskPath        `json:"risk_paths"`
	AssertionIntents      []AssertionIntent `json:"assertion_intents"`
}

// CompiledSetup is the operator-owned configuration produced from valid
// Semantic Findings and the exact repository Snapshot they cite.
type CompiledSetup struct {
	RequiredChecks    []string           `json:"required_checks"`
	RuntimeAssertions []RuntimeAssertion `json:"runtime_assertions"`
	PolicyYAML        string             `json:"policy_yaml"`
	TemplateFiles     []TemplateFile     `json:"template_files"`
}

// Plan is the immutable, content-addressed setup SafeLane presents and applies.
type Plan struct {
	SchemaVersion  string        `json:"schema_version"`
	ID             string        `json:"id"`
	FindingsSource string        `json:"findings_source"`
	Snapshot       Snapshot      `json:"snapshot"`
	Findings       Findings      `json:"findings"`
	Compiled       CompiledSetup `json:"compiled"`
}

// Runner is the seam for testing and for replacing the agent CLI later.
var workflowJobName = regexp.MustCompile(`(?m)^\s{4}name:\s*([^#\r\n]+)`)

// Discover reads only repository metadata and bounded, non-secret text files.
func Discover(root string) (Snapshot, error) {
	repo, err := project.DetectGitHubRepo(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("discover repository: %w", err)
	}
	parts := strings.Split(strings.Trim(repo, "/"), "/")
	if len(parts) != 2 {
		return Snapshot{}, fmt.Errorf("discover repository: %q is not owner/name", repo)
	}
	files, err := snapshotFiles(root)
	if err != nil {
		return Snapshot{}, err
	}
	checks := discoverChecks(files)
	snapshot := Snapshot{
		SchemaVersion:    "safelane.setup.inspection/v1",
		Application:      project.SanitizeApplication(parts[1]),
		Repository:       repo,
		DefaultBranch:    project.DetectDefaultBranch(root),
		ImageRepository:  discoverImageRepository(repo, files),
		RequiredChecks:   checks,
		KubernetesFiles:  discoverKubernetesFiles(files),
		CriticalSurfaces: discoverCriticalSurfaces(files),
		Files:            files,
	}
	snapshot.MandatoryAssertions = mandatoryAssertionsFor(snapshot.CriticalSurfaces)
	if len(semanticAssertionsFor(snapshot.CriticalSurfaces)) == 0 {
		snapshot.Uncertainties = append(snapshot.Uncertainties, "No concrete application behavior route was discovered; setup plan requires at least one semantic runtime assertion.")
	}
	snapshot.InspectionFingerprint = Fingerprint(snapshot)
	return snapshot, nil
}

// Fingerprint binds agent findings to the exact repository inspection.
func Fingerprint(snapshot Snapshot) string {
	snapshot.InspectionFingerprint = ""
	raw, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum)
}

func discoverKubernetesFiles(files []File) []string {
	var paths []string
	for _, file := range files {
		lower := strings.ToLower(file.Path)
		if strings.Contains(lower, "k8s") || strings.Contains(lower, "kubernetes") || strings.Contains(file.Content, "apiVersion: argoproj.io/") {
			paths = append(paths, file.Path)
		}
	}
	return paths
}

func discoverCriticalSurfaces(files []File) []string {
	wanted := []string{"/api/demo", "/version"}
	var surfaces []string
	for _, route := range wanted {
		for _, file := range files {
			if strings.Contains(file.Content, route) {
				surfaces = append(surfaces, "GET "+route)
				break
			}
		}
	}
	return surfaces
}

func semanticAssertionsFor(surfaces []string) []RuntimeAssertion {
	has := map[string]bool{}
	for _, surface := range surfaces {
		has[surface] = true
	}
	var assertions []RuntimeAssertion
	if has["GET /api/demo"] {
		assertions = append(assertions, RuntimeAssertion{ID: "demo-response", Surface: "GET /api/demo", Expectation: `HTTP 200 and JSON status equals "ok"`, Covers: "correctness"})
	}
	return assertions
}

func mandatoryAssertionsFor(surfaces []string) []RuntimeAssertion {
	has := map[string]bool{}
	for _, surface := range surfaces {
		has[surface] = true
	}
	var assertions []RuntimeAssertion
	if has["GET /api/demo"] {
		assertions = append(assertions,
			RuntimeAssertion{ID: "demo-success-rate", Surface: "GET /api/demo", Expectation: "success rate is at least 95 percent over 20 requests", Covers: "availability"},
			RuntimeAssertion{ID: "demo-latency", Surface: "GET /api/demo", Expectation: "p95 latency is at most 500ms over 20 requests", Covers: "latency"},
		)
	}
	if has["GET /version"] {
		assertions = append(assertions, RuntimeAssertion{ID: "canary-identity", Surface: "GET /version", Expectation: "commit equals the inspected merge commit", Covers: "artifact-identity"})
	}
	return assertions
}

func snapshotFiles(root string) ([]File, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (name == ".git" || name == "bin" || name == "obj" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if isSensitivePath(rel) {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover files: %w", err)
	}
	sort.Strings(paths)
	files := make([]File, 0, len(paths))
	used := 0
	for _, rel := range paths {
		if used >= maxSnapshotBytes {
			break
		}
		raw, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil || len(raw) == 0 || len(raw) > maxFileBytes || bytes.IndexByte(raw, 0) >= 0 {
			continue
		}
		remaining := maxSnapshotBytes - used
		truncated := false
		if len(raw) > remaining {
			raw = raw[:remaining]
			truncated = true
		}
		sum := sha256.Sum256(raw)
		files = append(files, File{Path: filepath.ToSlash(rel), Bytes: len(raw), ContentSHA256: fmt.Sprintf("sha256:%x", sum), Truncated: truncated, Content: string(raw)})
		used += len(raw)
	}
	return files, nil
}

func isSensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	for _, part := range strings.Split(lower, "/") {
		if part == ".git" || part == ".env" || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || strings.Contains(part, "secret") {
			return true
		}
	}
	return false
}

func discoverChecks(files []File) []string {
	seen := map[string]bool{}
	var checks []string
	for _, file := range files {
		if !strings.HasPrefix(file.Path, ".github/workflows/") {
			continue
		}
		for _, match := range workflowJobName.FindAllStringSubmatch(file.Content, -1) {
			name := strings.TrimSpace(match[1])
			// GitHub expands expressions in matrix job names before creating check
			// runs. The template itself is never a literal check name, and setup
			// cannot safely enumerate every matrix expansion from a text scan.
			if name != "" && !strings.Contains(name, "${{") && !seen[name] {
				seen[name] = true
				checks = append(checks, name)
			}
		}
	}
	sort.Strings(checks)
	return checks
}

func discoverImageRepository(repo string, files []File) string {
	ghcr := regexp.MustCompile(`ghcr\.io/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`)
	for _, file := range files {
		if !strings.HasPrefix(file.Path, ".github/workflows/") && file.Path != "Dockerfile" {
			continue
		}
		if match := ghcr.FindString(file.Content); match != "" {
			return strings.ToLower(match)
		}
	}
	return "ghcr.io/" + strings.ToLower(repo)
}

// ConservativeFindings are the semantic fallback used by non-agent setup.
func ConservativeFindings(s Snapshot) Findings {
	intents := make([]AssertionIntent, 0)
	for _, assertion := range semanticAssertionsFor(s.CriticalSurfaces) {
		intents = append(intents, AssertionIntent{Surface: assertion.Surface, Covers: assertion.Covers})
	}
	return Findings{
		SchemaVersion:         "safelane.setup.findings/v1",
		InspectionFingerprint: Fingerprint(s),
		Summary:               "SafeLane selected conservative semantic findings from repository facts.",
		RiskPaths:             []RiskPath{{Glob: "src/**", Minimum: "medium", Reason: "Runtime code changes require the standard lane."}},
		AssertionIntents:      intents,
	}
}

// ValidateFindings checks semantic input before SafeLane compiles a plan.
// Agent findings require citations; the trusted deterministic fallback does not.
func ValidateFindings(p Findings, s Snapshot, requireEvidence bool) error {
	if p.SchemaVersion != "safelane.setup.findings/v1" {
		return fmt.Errorf("setup: unsupported findings schema %q", p.SchemaVersion)
	}
	if p.InspectionFingerprint == "" || p.InspectionFingerprint != Fingerprint(s) {
		return errors.New("setup: findings inspection fingerprint is stale or does not match this repository")
	}
	if len(semanticAssertionsFor(s.CriticalSurfaces)) == 0 {
		return errors.New("setup: no semantic application surface was discovered")
	}
	if strings.TrimSpace(p.Summary) == "" {
		return errors.New("setup: findings have no summary")
	}
	if len(p.RiskPaths) == 0 || len(p.RiskPaths) > 20 {
		return errors.New("setup: findings must contain 1-20 application risk paths")
	}
	seenPaths := map[string]bool{}
	reservedPaths := map[string]bool{"Dockerfile": true, ".github/workflows/**": true}
	for _, rule := range p.RiskPaths {
		clean := filepath.ToSlash(filepath.Clean(rule.Glob))
		if clean != rule.Glob || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || strings.ContainsAny(rule.Glob, "\r\n") {
			return fmt.Errorf("setup: recommendation has an unsafe risk path %q", rule.Glob)
		}
		if seenPaths[rule.Glob] {
			return fmt.Errorf("setup: risk path %q is duplicated", rule.Glob)
		}
		seenPaths[rule.Glob] = true
		if reservedPaths[rule.Glob] {
			return fmt.Errorf("setup: risk path %q is owned by SafeLane's product floors", rule.Glob)
		}
		if rule.Minimum != "low" && rule.Minimum != "medium" && rule.Minimum != "high" {
			return fmt.Errorf("setup: risk path %q has invalid minimum %q", rule.Glob, rule.Minimum)
		}
		reason := strings.TrimSpace(rule.Reason)
		if reason == "" || strings.ContainsAny(reason, "\r\n") {
			return fmt.Errorf("setup: risk path %q requires a one-line reason", rule.Glob)
		}
		if err := validateEvidence(rule.Evidence, s, requireEvidence); err != nil {
			return fmt.Errorf("setup: risk path %q: %w", rule.Glob, err)
		}
	}
	if len(p.AssertionIntents) == 0 {
		return errors.New("setup: findings have no semantic assertion intents")
	}
	supportedAssertions := map[string]RuntimeAssertion{}
	for _, assertion := range semanticAssertionsFor(s.CriticalSurfaces) {
		supportedAssertions[assertion.Surface+"\x00"+assertion.Covers] = assertion
	}
	seenAssertions := map[string]bool{}
	coveredSurfaces := map[string]bool{}
	reservedCovers := map[string]bool{"availability": true, "latency": true, "artifact-identity": true}
	for _, intent := range p.AssertionIntents {
		if strings.TrimSpace(intent.Surface) == "" || strings.TrimSpace(intent.Covers) == "" {
			return errors.New("setup: assertion intents require surface and covers")
		}
		key := intent.Surface + "\x00" + intent.Covers
		if seenAssertions[key] {
			return fmt.Errorf("setup: assertion intent for %q and %q is duplicated", intent.Surface, intent.Covers)
		}
		seenAssertions[key] = true
		if reservedCovers[intent.Covers] {
			return fmt.Errorf("setup: assertion intent for %q uses SafeLane-owned coverage %q", intent.Surface, intent.Covers)
		}
		if _, ok := supportedAssertions[key]; !ok {
			return fmt.Errorf("setup: assertion intent for %q and %q is not executable by the configured probe", intent.Surface, intent.Covers)
		}
		if err := validateEvidence(intent.Evidence, s, requireEvidence); err != nil {
			return fmt.Errorf("setup: assertion intent for %q: %w", intent.Surface, err)
		}
		coveredSurfaces[intent.Surface] = true
	}
	for _, surface := range s.CriticalSurfaces {
		if surface == "GET /version" {
			continue
		}
		if !coveredSurfaces[surface] {
			return fmt.Errorf("setup: critical surface %q has no runtime assertion", surface)
		}
	}
	return nil
}

func compileAssertionIntents(findings Findings, s Snapshot) []RuntimeAssertion {
	supported := map[string]RuntimeAssertion{}
	for _, assertion := range semanticAssertionsFor(s.CriticalSurfaces) {
		supported[assertion.Surface+"\x00"+assertion.Covers] = assertion
	}
	assertions := make([]RuntimeAssertion, 0, len(findings.AssertionIntents)+len(s.MandatoryAssertions))
	for _, intent := range findings.AssertionIntents {
		assertions = append(assertions, supported[intent.Surface+"\x00"+intent.Covers])
	}
	return append(assertions, s.MandatoryAssertions...)
}

func validateEvidence(items []Evidence, s Snapshot, required bool) error {
	if required && len(items) == 0 {
		return errors.New("at least one file/line evidence citation is required")
	}
	files := map[string]File{}
	for _, file := range s.Files {
		files[file.Path] = file
	}
	for _, item := range items {
		file, ok := files[filepath.ToSlash(item.File)]
		if !ok || item.Line < 1 {
			return fmt.Errorf("evidence %s:%d does not identify an inspected file line", item.File, item.Line)
		}
		if file.Content != "" && item.Line > strings.Count(file.Content, "\n")+1 {
			return fmt.Errorf("evidence %s:%d is past the end of the inspected file", item.File, item.Line)
		}
	}
	return nil
}

func productRiskPaths(s Snapshot) []RiskPath {
	var paths []RiskPath
	seen := map[string]bool{}
	for _, file := range s.Files {
		if file.Path == "Dockerfile" && !seen["Dockerfile"] {
			paths = append(paths, RiskPath{Glob: "Dockerfile", Minimum: "high", Reason: "Container construction changes require the guarded lane."})
			seen["Dockerfile"] = true
		}
		if strings.HasPrefix(file.Path, ".github/workflows/") && !seen[".github/workflows/**"] {
			paths = append(paths, RiskPath{Glob: ".github/workflows/**", Minimum: "high", Reason: "Release evidence workflow changes require the guarded lane."})
			seen[".github/workflows/**"] = true
		}
	}
	return paths
}

// CompileFindings is the only setup path that authors policy or manifests.
func CompileFindings(p Findings, s Snapshot, requireEvidence bool) (CompiledSetup, error) {
	if err := ValidateFindings(p, s, requireEvidence); err != nil {
		return CompiledSetup{}, err
	}
	checks := append([]string(nil), s.RequiredChecks...)
	if len(checks) == 0 {
		checks = []string{"build-and-push"}
	}
	riskPaths := append(productRiskPaths(s), p.RiskPaths...)
	assertions := compileAssertionIntents(p, s)
	compiled := CompiledSetup{
		RequiredChecks:    checks,
		RuntimeAssertions: assertions,
		PolicyYAML:        conservativePolicy(riskPaths),
		TemplateFiles:     conservativeTemplate(s),
	}
	if err := validatePolicyYAML(compiled.PolicyYAML); err != nil {
		return CompiledSetup{}, fmt.Errorf("setup: SafeLane compiled an invalid policy: %w", err)
	}
	for _, file := range compiled.TemplateFiles {
		if !safeTemplatePath(file.Path) || strings.TrimSpace(file.Content) == "" {
			return CompiledSetup{}, fmt.Errorf("setup: SafeLane compiled an invalid Release Template file %q", file.Path)
		}
	}
	files := fstest.MapFS{}
	for _, file := range compiled.TemplateFiles {
		files[file.Path] = &fstest.MapFile{Data: []byte(file.Content)}
	}
	if _, err := render.LoadFS(files); err != nil {
		return CompiledSetup{}, fmt.Errorf("setup: SafeLane compiled an invalid Release Template: %w", err)
	}
	if err := validateMandatoryAnalysis(compiled.TemplateFiles); err != nil {
		return CompiledSetup{}, err
	}
	return compiled, nil
}

// NewPlan freezes the exact validated setup that can later be applied by ID.
func NewPlan(s Snapshot, findings Findings, compiled CompiledSetup, agentFindings bool) Plan {
	s.InspectionFingerprint = Fingerprint(s)
	source := "deterministic"
	if agentFindings {
		source = "agent"
	}
	plan := Plan{SchemaVersion: "safelane.setup.plan/v1", FindingsSource: source, Snapshot: s, Findings: findings, Compiled: compiled}
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	plan.ID = fmt.Sprintf("setup_%x", sum[:10])
	return plan
}

// ValidatePlan detects malformed or modified plan artifacts before mutation.
func ValidatePlan(plan Plan) error {
	if plan.SchemaVersion != "safelane.setup.plan/v1" || plan.ID == "" {
		return errors.New("setup: invalid setup plan identity")
	}
	want := plan.ID
	plan.ID = ""
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	if got := fmt.Sprintf("setup_%x", sum[:10]); got != want {
		return errors.New("setup: setup plan content does not match its ID")
	}
	if Fingerprint(plan.Snapshot) != plan.Snapshot.InspectionFingerprint {
		return errors.New("setup: setup plan contains an invalid inspection fingerprint")
	}
	if plan.FindingsSource != "agent" && plan.FindingsSource != "deterministic" {
		return errors.New("setup: setup plan contains an invalid findings source")
	}
	if err := ValidateFindings(plan.Findings, plan.Snapshot, plan.FindingsSource == "agent"); err != nil {
		return fmt.Errorf("setup: setup plan contains invalid findings: %w", err)
	}
	if err := validateCompiledSetup(plan.Compiled); err != nil {
		return fmt.Errorf("setup: setup plan contains invalid compiled configuration: %w", err)
	}
	return nil
}

func validateCompiledSetup(compiled CompiledSetup) error {
	if len(compiled.RequiredChecks) == 0 || len(compiled.RuntimeAssertions) == 0 {
		return errors.New("required checks and runtime assertions must be present")
	}
	if err := validatePolicyYAML(compiled.PolicyYAML); err != nil {
		return err
	}
	files := fstest.MapFS{}
	for _, file := range compiled.TemplateFiles {
		if !safeTemplatePath(file.Path) || strings.TrimSpace(file.Content) == "" {
			return fmt.Errorf("invalid Release Template file %q", file.Path)
		}
		files[file.Path] = &fstest.MapFile{Data: []byte(file.Content)}
	}
	if _, err := render.LoadFS(files); err != nil {
		return err
	}
	return validateMandatoryAnalysis(compiled.TemplateFiles)
}

func validateMandatoryAnalysis(files []TemplateFile) error {
	var all strings.Builder
	for _, file := range files {
		all.WriteString(file.Content)
		all.WriteByte('\n')
	}
	content := all.String()
	if strings.Contains(content, "/api/analysis") {
		return errors.New("setup: Release Template uses forbidden hard-coded /api/analysis endpoint")
	}
	for _, required := range []string{
		"kind: AnalysisTemplate", "{{ .ProbeImage }}", "{{ .CanaryServiceName }}", "{{ .SourceRevision }}",
		"automountServiceAccountToken: false", "REQUEST_COUNT", "MIN_SUCCESS_RATE", "MAX_P95_MS",
	} {
		if !strings.Contains(content, required) {
			return fmt.Errorf("setup: Release Template is missing mandatory canary analysis element %q", required)
		}
	}
	return nil
}

func safeTemplatePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == path && clean != "." && !strings.HasPrefix(clean, "../") && !strings.Contains(clean, "/../") && strings.HasSuffix(clean, ".yaml.tmpl")
}

func validatePolicyYAML(raw string) error {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return err
	}
	if len(doc) == 0 {
		return errors.New("empty YAML")
	}
	tmp, err := os.CreateTemp("", "safelane-policy-*.yml")
	if err != nil {
		return fmt.Errorf("create validation file: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.WriteString(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write validation file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close validation file: %w", err)
	}
	if _, err := policy.Load(name); err != nil {
		return err
	}
	return nil
}

func conservativePolicy(rules []RiskPath) string {
	paths := make([]string, 0, len(rules))
	for _, rule := range rules {
		paths = append(paths, fmt.Sprintf("    - { glob: %q, minimum: %s }", rule.Glob, rule.Minimum))
	}
	return fmt.Sprintf(`version: 2

mandatory_evidence:
  - merged_commit_on_default_branch
  - passing_publish_workflow
  - immutable_ghcr_digest

lanes:
  fast:
    weights: [50, 100]
  standard:
    weights: [25, 50, 100]
  guarded:
    weights: [25, 50, 75, 100]

risk_to_lane:
  low: fast
  medium: standard
  high: guarded

default_lane: guarded

assessment:
  heuristic:
    agent_authored_minimum: medium
    paths:
%s
    size:
      - { changed_lines_at_least: 200, minimum: medium }
      - { files_at_least: 15, minimum: medium }
  model:
    assessors: [claude, codex]
    timeout: 90s
    max_diff_bytes: 200000
`, strings.Join(paths, "\n"))
}

func conservativeTemplate(s Snapshot) []TemplateFile {
	app := s.Application
	return []TemplateFile{
		{Path: "10-service-stable.yaml.tmpl", Content: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: {{ .StableServiceName }}
  namespace: {{ .Namespace }}
spec:
  selector:
    app.kubernetes.io/name: %s
  ports:
    - name: http
      port: 80
      targetPort: http
`, app)},
		{Path: "15-service-canary.yaml.tmpl", Content: fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: {{ .CanaryServiceName }}
  namespace: {{ .Namespace }}
spec:
  selector:
    app.kubernetes.io/name: %s
  ports:
    - name: http
      port: 80
      targetPort: http
`, app)},
		{Path: "18-analysis-demo-behavior.yaml.tmpl", Content: `apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: {{ .AnalysisTemplateName }}
  namespace: {{ .Namespace }}
spec:
  metrics:
    - name: demo-behavior
      count: 1
      failureLimit: 1
      provider:
        job:
          spec:
            backoffLimit: 0
            template:
              spec:
                restartPolicy: Never
                automountServiceAccountToken: false
                containers:
                  - name: probe
                    image: {{ .ProbeImage }}
                    imagePullPolicy: IfNotPresent
                    env:
                      - name: TARGET_BASE_URL
                        value: http://{{ .CanaryServiceName }}.{{ .Namespace }}.svc.cluster.local
                      - name: EXPECTED_COMMIT
                        value: {{ .SourceRevision }}
                      - name: REQUEST_COUNT
                        value: "20"
                      - name: MIN_SUCCESS_RATE
                        value: "0.95"
                      - name: MAX_P95_MS
                        value: "500"
`},
		{Path: "20-rollout.yaml.tmpl", Content: fmt.Sprintf(`apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: {{ .RolloutName }}
  namespace: {{ .Namespace }}
  annotations:
    safelane.dev/source-repository: {{ .SourceRepository }}
    safelane.dev/source-revision: {{ .SourceRevision }}
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: %s
  strategy:
    canary:
      canaryService: {{ .CanaryServiceName }}
      stableService: {{ .StableServiceName }}
      steps:
{{- range .Steps }}
        - setWeight: {{ . }}
        - analysis:
            templates:
              - templateName: {{ $.AnalysisTemplateName }}
        - pause: {}
{{- end }}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: %s
    spec:
      containers:
        - name: %s
          image: {{ .ImageReference }}
          ports:
            - name: http
              containerPort: 8080
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
          readinessProbe:
            httpGet:
              path: /healthz
              port: http
`, app, app, app)},
	}
}
