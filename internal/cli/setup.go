package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
)

// SetupCommand discovers repository facts and activates SafeLane's conservative
// policy after one confirmation. It never starts Claude, Codex, or another agent.
func SetupCommand(root string) Command {
	return Command{Name: "setup", Summary: "create operator configuration from repository facts", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runDeterministicSetup(ctx, args, os.Stdin, stdout, stderr, root)
	}}
}

// SetupInspectCommand is the read-only primitive used by an already-active agent.
func SetupInspectCommand(root string) Command {
	return Command{Name: "inspect", Summary: "inspect repository facts", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runSetupInspect(ctx, args, stdout, stderr, root)
	}}
}

// SetupApplyCommand validates and applies a proposal produced from setup inspect.
func SetupApplyCommand(root string) Command {
	return Command{Name: "apply", Summary: "apply a validated setup proposal", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runSetupApply(ctx, args, os.Stdin, stdout, stderr, root)
	}}
}

type setupFlags struct{ yes, jsonOut bool }

func parseSetupFlags(name string, args []string, stderr io.Writer) (setupFlags, error) {
	var f setupFlags
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
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
	proposal := setupengine.ConservativeProposal(snapshot)
	if err := setupengine.ValidateProposal(proposal, snapshot); err != nil {
		return writeResultError(stderr, "setup", err)
	}
	if !f.jsonOut {
		renderSetupPreview(stdout, snapshot, proposal)
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
	if err := activateSetup(loc, snapshot, proposal); err != nil {
		return writeResultError(stderr, "setup", err)
	}
	next := "safelane doctor"
	if f.jsonOut {
		if err := WriteResult(stdout, "setup", "configured", next, map[string]any{"application": snapshot.Application, "project_file": loc.ProjectFile, "inspection_fingerprint": snapshot.InspectionFingerprint}); err != nil {
			return writeResultError(stderr, "setup", err)
		}
	} else {
		fmt.Fprintf(stdout, "\nConfigured %s outside the repository.\nNext: %s\n", snapshot.Application, next)
	}
	return ExitOK
}

func runSetupInspect(_ context.Context, args []string, stdout, stderr io.Writer, root string) int {
	fs := flag.NewFlagSet("setup inspect", flag.ContinueOnError)
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
	if *jsonOut {
		if err := json.NewEncoder(stdout).Encode(snapshot); err != nil {
			return writeResultError(stderr, "setup inspect", err)
		}
	} else {
		fmt.Fprintf(stdout, "Repository: %s\nApplication: %s\nFingerprint: %s\n", snapshot.Repository, snapshot.Application, snapshot.InspectionFingerprint)
	}
	return ExitOK
}

func runSetupApply(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root string) int {
	fs := flag.NewFlagSet("setup apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	proposalPath := fs.String("proposal", "", "absolute path to an agent-authored proposal JSON")
	yes := fs.Bool("yes", false, "approve this exact proposal")
	jsonOut := fs.Bool("json", false, "print a machine-readable final result")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 || *proposalPath == "" {
		fmt.Fprintln(stderr, "safelane setup apply: --proposal <absolute-path> is required")
		return ExitUsage
	}
	if !filepath.IsAbs(*proposalPath) {
		fmt.Fprintln(stderr, "safelane setup apply: --proposal must be an absolute path")
		return ExitUsage
	}
	raw, err := os.ReadFile(*proposalPath)
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	var proposal setupengine.Proposal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal); err != nil {
		return writeResultError(stderr, "setup apply", fmt.Errorf("invalid proposal: %w", err))
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return writeResultError(stderr, "setup apply", fmt.Errorf("proposal contains trailing JSON"))
	}
	snapshot, err := setupengine.Discover(root)
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := setupengine.ValidateProposal(proposal, snapshot); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if !*jsonOut {
		renderSetupPreview(stdout, snapshot, proposal)
	}
	if !*yes && !confirmApply(stdin, stderr) {
		return ExitDecision
	}
	loc, err := setupLocation(snapshot.Application)
	if err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := ensureSetupTargetAbsent(loc); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	if err := activateSetup(loc, snapshot, proposal); err != nil {
		return writeResultError(stderr, "setup apply", err)
	}
	next := "safelane doctor"
	if *jsonOut {
		if err := WriteResult(stdout, "setup apply", "configured", next, map[string]any{"application": snapshot.Application, "project_file": loc.ProjectFile, "inspection_fingerprint": snapshot.InspectionFingerprint}); err != nil {
			return writeResultError(stderr, "setup apply", err)
		}
	} else {
		fmt.Fprintf(stdout, "\nConfigured %s.\nNext: %s\n", snapshot.Application, next)
	}
	return ExitOK
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

func renderSetupPreview(w io.Writer, snapshot setupengine.Snapshot, proposal setupengine.Proposal) {
	fmt.Fprintf(w, "SafeLane setup\n\nRepository: %s\nApplication: %s\nImage: %s\nRequired CI: %s\nAssertions:\n", snapshot.Repository, snapshot.Application, snapshot.ImageRepository, strings.Join(proposal.RequiredChecks, ", "))
	for _, assertion := range proposal.RuntimeAssertions {
		fmt.Fprintf(w, "  - %s: %s (%s)\n", assertion.Surface, assertion.Expectation, assertion.Covers)
	}
	fmt.Fprintln(w, "Policy:")
	for _, item := range proposal.PolicyHighlights {
		fmt.Fprintf(w, "  - %s\n", item)
	}
}

func activateSetup(loc project.Locations, snapshot setupengine.Snapshot, proposal setupengine.Proposal) error {
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
	assertions := make([]project.RuntimeAssertion, 0, len(proposal.RuntimeAssertions))
	for _, assertion := range proposal.RuntimeAssertions {
		assertions = append(assertions, project.RuntimeAssertion{ID: assertion.ID, Surface: assertion.Surface, Expectation: assertion.Expectation, Covers: assertion.Covers})
	}
	projectYAML := project.YAML(snapshot.Application, snapshot.Repository, snapshot.DefaultBranch, snapshot.ImageRepository, proposal.RequiredChecks, assertions)
	if _, err := writeInitFile(stage.ProjectFile, projectYAML); err != nil {
		return err
	}
	if _, err := writeInitFile(stage.PolicyFile, []byte(proposal.PolicyYAML)); err != nil {
		return err
	}
	if err := os.MkdirAll(stage.TemplateDir, 0o755); err != nil {
		return err
	}
	for _, file := range proposal.TemplateFiles {
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
