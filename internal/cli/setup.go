package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
	"github.com/spf13/pflag"
)

// SetupCommand discovers repository facts and activates SafeLane's conservative
// policy after one confirmation. It never starts Claude, Codex, or another agent.
func SetupCommand(root string) Command {
	return Command{Name: "setup", Summary: "create operator configuration from repository facts", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runDeterministicSetup(ctx, args, os.Stdin, stdout, stderr, root)
	}}
}

// SetupInspectCommand reads the repository and persists only a fingerprinted
// operator-side inspection for an already-active agent.
func SetupInspectCommand(root string) Command {
	return Command{Name: "inspect", Summary: "inspect repository facts", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runSetupInspect(ctx, args, stdout, stderr, root)
	}}
}

// SetupPlanCommand validates agent findings and persists SafeLane's exact setup.
func SetupPlanCommand(root string) Command {
	return Command{Name: "plan", Summary: "compile and persist an immutable setup plan", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runSetupPlan(ctx, args, os.Stdin, stdout, stderr, root)
	}}
}

// SetupApplyCommand applies one already-compiled setup plan by ID.
func SetupApplyCommand(root string) Command {
	return Command{Name: "apply", Summary: "apply an immutable setup plan", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runSetupApply(ctx, args, os.Stdin, stdout, stderr, root)
	}}
}

type setupFlags struct{ yes, jsonOut bool }

var setupIDPattern = regexp.MustCompile(`^setup_[a-f0-9]{20}$`)

const maxFindingsBytes = 64 * 1024

func parseSetupFlags(name string, args []string, stderr io.Writer) (setupFlags, error) {
	var f setupFlags
	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&f.yes, "yes", false, "approve the displayed setup")
	fs.BoolVar(&f.jsonOut, "json", false, "print a machine-readable final result")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() != 0 {
		return f, fmt.Errorf("no positional arguments are allowed")
	}
	return f, nil
}

func runDeterministicSetup(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	f, err := parseSetupFlags("setup", args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "safelane setup: %v\n", err)
		return ExitUsage
	}
	snapshot, err := setupengine.Discover(root)
	if err != nil {
		return writeResultError(stderr, "setup", err)
	}
	findings := setupengine.ConservativeFindings(snapshot)
	compiled, err := setupengine.CompileFindings(findings, snapshot, false)
	if err != nil {
		return writeResultError(stderr, "setup", err)
	}
	plan := setupengine.NewPlan(snapshot, findings, compiled, false)
	if err := persistSetupPlan(plan); err != nil {
		return writeResultError(stderr, "setup", err)
	}
	if !f.jsonOut {
		renderSetupPreview(stdout, plan)
	}
	if !f.yes && !confirmApply(stdin, stderr) {
		return ExitDecision
	}
	loc, err := setupLocation(snapshot.Application)
	if err != nil {
		return writeResultError(stderr, "setup", err)
	}
	if err := ensureSetupTargetAbsent(loc); err != nil {
		return writeResultError(stderr, "setup", err)
	}
	if err := activateSetup(loc, plan.Snapshot, plan.Compiled); err != nil {
		return writeResultError(stderr, "setup", err)
	}
	next := "safelane doctor"
	if f.jsonOut {
		if err := WriteResult(stdout, "setup", "configured", next, map[string]any{"application": snapshot.Application, "project_file": loc.ProjectFile, "inspection_fingerprint": snapshot.InspectionFingerprint, "setup_id": plan.ID}); err != nil {
			return writeResultError(stderr, "setup", err)
		}
	} else {
		fmt.Fprintf(stdout, "\nConfigured %s outside the repository.\nNext: %s\n", snapshot.Application, next)
	}
	return ExitOK
}

func runSetupInspect(_ context.Context, args []string, stdout, stderr io.Writer, root string) int {
	fs := pflag.NewFlagSet("setup inspect", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "print the repository inspection as JSON")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "safelane setup inspect: no positional arguments are allowed")
		return ExitUsage
	}
	snapshot, err := setupengine.Discover(root)
	if err != nil {
		return writeResultError(stderr, "setup inspect", err)
	}
	if err := persistSetupInspection(snapshot); err != nil {
		return writeResultError(stderr, "setup inspect", err)
	}
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(snapshot); err != nil {
			return writeResultError(stderr, "setup inspect", err)
		}
	} else {
		fmt.Fprintf(stdout, "Repository: %s\nApplication: %s\nFingerprint: %s\n", snapshot.Repository, snapshot.Application, snapshot.InspectionFingerprint)
	}
	return ExitOK
}

func runSetupPlan(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	fs := pflag.NewFlagSet("setup plan", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	findingsPath := fs.String("findings", "", "absolute path to findings JSON, or - for stdin")
	jsonOut := fs.Bool("json", false, "print a machine-readable final result")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 || *findingsPath == "" {
		fmt.Fprintln(stderr, "safelane setup plan: --findings <absolute-path|-> is required")
		return ExitUsage
	}
	if *findingsPath != "-" && !filepath.IsAbs(*findingsPath) {
		fmt.Fprintln(stderr, "safelane setup plan: --findings must be an absolute path or -")
		return ExitUsage
	}
	raw, err := readBoundedFindings(*findingsPath, stdin)
	if err != nil {
		return writeResultError(stderr, "setup plan", err)
	}
	var findings setupengine.Findings
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&findings); err != nil {
		return writeResultError(stderr, "setup plan", fmt.Errorf("invalid findings: %w", err))
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return writeResultError(stderr, "setup plan", fmt.Errorf("findings contain trailing JSON"))
	}
	snapshot, err := setupengine.Discover(root)
	if err != nil {
		return writeResultError(stderr, "setup plan", err)
	}
	compiled, err := setupengine.CompileFindings(findings, snapshot, true)
	if err != nil {
		return writeResultError(stderr, "setup plan", err)
	}
	plan := setupengine.NewPlan(snapshot, findings, compiled, true)
	if err := persistSetupPlan(plan); err != nil {
		return writeResultError(stderr, "setup plan", err)
	}
	next := fmt.Sprintf("safelane setup apply %s --yes --json", plan.ID)
	if !*jsonOut {
		renderSetupPreview(stdout, plan)
		fmt.Fprintf(stdout, "\nSetup plan: %s\nNext: %s\n", plan.ID, next)
	} else if err := WriteResult(stdout, "setup plan", "planned", next, map[string]any{"setup_id": plan.ID, "application": snapshot.Application, "inspection_fingerprint": snapshot.InspectionFingerprint}); err != nil {
		return writeResultError(stderr, "setup plan", err)
	}
	return ExitOK
}

func runSetupApply(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	fs := pflag.NewFlagSet("setup apply", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "approve this exact setup plan")
	jsonOut := fs.Bool("json", false, "print a machine-readable final result")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 || !setupIDPattern.MatchString(fs.Arg(0)) {
		fmt.Fprintln(stderr, "safelane setup apply: one setup-id is required")
		return ExitUsage
	}
	plan, err := loadSetupPlan(fs.Arg(0))
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := setupengine.ValidatePlan(plan); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	current, err := setupengine.Discover(root)
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if current.InspectionFingerprint != plan.Snapshot.InspectionFingerprint {
		return writeResultError(stderr, "setup apply", fmt.Errorf("setup plan is stale; run safelane setup inspect again"))
	}
	if !*jsonOut {
		renderSetupPreview(stdout, plan)
	}
	if !*yes && !confirmApply(stdin, stderr) {
		return ExitDecision
	}
	loc, err := setupLocation(plan.Snapshot.Application)
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := ensureSetupTargetAbsent(loc); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := activateSetup(loc, plan.Snapshot, plan.Compiled); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	next := "safelane doctor"
	if *jsonOut {
		if err := WriteResult(stdout, "setup apply", "configured", next, map[string]any{"application": plan.Snapshot.Application, "project_file": loc.ProjectFile, "inspection_fingerprint": plan.Snapshot.InspectionFingerprint, "setup_id": plan.ID}); err != nil {
			return writeResultError(stderr, "setup apply", err)
		}
	} else {
		fmt.Fprintf(stdout, "\nConfigured %s.\nNext: %s\n", plan.Snapshot.Application, next)
	}
	return ExitOK
}

func readBoundedFindings(path string, stdin io.Reader) ([]byte, error) {
	var reader io.Reader = stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxFindingsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxFindingsBytes {
		return nil, fmt.Errorf("findings exceed %d bytes", maxFindingsBytes)
	}
	return raw, nil
}

func setupArtifactPath(kind, name string) (string, error) {
	home, err := project.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, kind, name+".json"), nil
}

func persistSetupInspection(snapshot setupengine.Snapshot) error {
	name := strings.TrimPrefix(snapshot.InspectionFingerprint, "sha256:")
	path, err := setupArtifactPath("setup-inspections", name)
	if err != nil {
		return err
	}
	return writeImmutableJSON(path, snapshot)
}

func persistSetupPlan(plan setupengine.Plan) error {
	if err := setupengine.ValidatePlan(plan); err != nil {
		return err
	}
	path, err := setupArtifactPath("setup-plans", plan.ID)
	if err != nil {
		return err
	}
	return writeImmutableJSON(path, plan)
}

func writeImmutableJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Equal(existing, raw) {
			return nil
		}
		return fmt.Errorf("immutable setup artifact %s already exists with different content", path)
	}
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(raw); err != nil {
		return err
	}
	return file.Sync()
}

func loadSetupPlan(id string) (setupengine.Plan, error) {
	path, err := setupArtifactPath("setup-plans", id)
	if err != nil {
		return setupengine.Plan{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return setupengine.Plan{}, err
	}
	var plan setupengine.Plan
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return setupengine.Plan{}, fmt.Errorf("invalid setup plan: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return setupengine.Plan{}, errors.New("setup plan contains trailing JSON")
	}
	return plan, nil
}

func setupLocation(app string) (project.Locations, error) {
	home, err := project.Home()
	if err != nil {
		return project.Locations{}, err
	}
	return project.ForApp(home, app), nil
}

func ensureSetupTargetAbsent(loc project.Locations) error {
	if _, err := os.Stat(loc.ProjectFile); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite operator configuration", loc.ProjectFile)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func confirmApply(stdin io.Reader, stderr io.Writer) bool {
	fmt.Fprint(stderr, "Type APPLY to create operator configuration: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == "APPLY"
}

func renderSetupPreview(w io.Writer, plan setupengine.Plan) {
	fmt.Fprintf(w, "SafeLane setup plan\n\nRepository: %s\nApplication: %s\nImage: %s\nRequired CI: %s\nCompiled assertions:\n", plan.Snapshot.Repository, plan.Snapshot.Application, plan.Snapshot.ImageRepository, strings.Join(plan.Compiled.RequiredChecks, ", "))
	for _, assertion := range plan.Compiled.RuntimeAssertions {
		fmt.Fprintf(w, "  - %s: %s (%s)\n", assertion.Surface, assertion.Expectation, assertion.Covers)
	}
	fmt.Fprintln(w, "Application risk findings:")
	for _, rule := range plan.Findings.RiskPaths {
		fmt.Fprintf(w, "  - %s → %s: %s\n", rule.Glob, rule.Minimum, rule.Reason)
	}
	fmt.Fprintln(w, "SafeLane also enforces product-owned CI and container floors when present.")
}

func activateSetup(loc project.Locations, snapshot setupengine.Snapshot, compiled setupengine.CompiledSetup) error {
	appsDir := filepath.Dir(loc.AppDir)
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp(appsDir, ".safelane-setup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	stage := project.Locations{Home: loc.Home, AppDir: stageDir, ProjectFile: filepath.Join(stageDir, "project.yml"), PolicyFile: filepath.Join(stageDir, "policy.yml"), TemplateDir: filepath.Join(stageDir, "release-template"), ReleasesDir: filepath.Join(stageDir, "releases")}
	assertions := make([]project.RuntimeAssertion, 0, len(compiled.RuntimeAssertions))
	for _, assertion := range compiled.RuntimeAssertions {
		assertions = append(assertions, project.RuntimeAssertion{ID: assertion.ID, Surface: assertion.Surface, Expectation: assertion.Expectation, Covers: assertion.Covers})
	}
	projectYAML := project.YAML(snapshot.Application, snapshot.Repository, snapshot.DefaultBranch, snapshot.ImageRepository, compiled.RequiredChecks, assertions)
	if _, err := writeInitFile(stage.ProjectFile, projectYAML); err != nil {
		return err
	}
	if _, err := writeInitFile(stage.PolicyFile, []byte(compiled.PolicyYAML)); err != nil {
		return err
	}
	if err := os.MkdirAll(stage.TemplateDir, 0o755); err != nil {
		return err
	}
	for _, file := range compiled.TemplateFiles {
		if _, err := writeInitFile(filepath.Join(stage.TemplateDir, filepath.FromSlash(file.Path)), []byte(file.Content)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(stage.ReleasesDir, 0o755); err != nil {
		return err
	}
	if err := os.Rename(stageDir, loc.AppDir); err != nil {
		return fmt.Errorf("activate operator configuration atomically: %w", err)
	}
	if snapshot.Application == "safelane-demo-api" {
		for source, target := range map[string]string{
			filepath.Join(loc.Home, "demo", "safelane-controller.kubeconfig"): filepath.Join(loc.AppDir, "controller.kubeconfig"),
			filepath.Join(loc.Home, "demo", "safelane-caller.kubeconfig"):     filepath.Join(loc.AppDir, "caller.kubeconfig"),
		} {
			if _, err := os.Stat(source); err == nil {
				if err := copyFile(source, target); err != nil {
					return err
				}
			}
		}
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(userHome, ".claude", "skills", "safelane", "SKILL.md"), filepath.Join(userHome, ".agents", "skills", "safelane", "SKILL.md")} {
		if _, err := writeSkillFile(path); err != nil {
			return err
		}
	}
	return nil
}

// copyFile duplicates one file, creating the target's parent directory.
// It previously lived alongside the Kind demo; setup is now its only caller.
func copyFile(source, target string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o600)
}
