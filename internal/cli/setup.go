package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/project"
	setupengine "github.com/AndrewMaged814/safelane/internal/setup"
)

// SetupCommand is the one-command, repository-aware entry point for humans.
// It discovers facts, asks a bounded agent for a proposal, and activates only
// after validating the recommendation. It never writes inside the application repo.
func SetupCommand(root string) Command {
	return setupCommand(root, setupDeps{
		recommend: func(ctx context.Context, snapshot setupengine.Snapshot) (setupengine.Proposal, error) {
			return setupengine.Recommend(ctx, snapshot, setupengine.RealRunner)
		},
	})
}

type setupDeps struct {
	recommend func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error)
}

func setupCommand(root string, deps setupDeps) Command {
	return Command{
		Name:    "setup",
		Summary: "discover the repository and create operator configuration",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runSetup(ctx, args, stdout, stderr, root, deps)
		},
	}
}

func runSetup(ctx context.Context, args []string, stdout, stderr io.Writer, root string, deps setupDeps) int {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noAgent := flags.Bool("no-agent", false, "skip Claude and use the conservative repository-derived proposal")
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "safelane setup: no positional arguments are allowed")
		return ExitUsage
	}

	snapshot, err := setupengine.Discover(root)
	if err != nil {
		fmt.Fprintf(stderr, "safelane setup: %v\n", err)
		return ExitFail
	}
	proposal := setupengine.ConservativeProposal(snapshot)
	if !*noAgent && deps.recommend != nil {
		recommendCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		agentProposal, recommendErr := recommendWithProgress(recommendCtx, snapshot, deps.recommend, stderr)
		cancel()
		if recommendErr != nil {
			fmt.Fprintf(stdout, "Claude recommendation could not be used: %v\n", recommendErr)
			fmt.Fprintln(stdout, "Using a conservative proposal from repository facts.")
		} else {
			proposal = agentProposal
			if len(proposal.RequiredChecks) == 0 {
				proposal.RequiredChecks = append([]string(nil), snapshot.RequiredChecks...)
			}
		}
	}
	if err := setupengine.ValidateProposal(proposal, snapshot); err != nil {
		fmt.Fprintf(stdout, "Recommendation was rejected by SafeLane validation: %v\n", err)
		proposal = setupengine.ConservativeProposal(snapshot)
		if fallbackErr := setupengine.ValidateProposal(proposal, snapshot); fallbackErr != nil {
			fmt.Fprintf(stderr, "safelane setup: conservative proposal is invalid: %v\n", fallbackErr)
			return ExitFail
		}
		fmt.Fprintln(stdout, "Using a conservative proposal from repository facts.")
	}

	fmt.Fprintln(stdout, "SafeLane setup")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Repository")
	fmt.Fprintf(stdout, "  name:          %s\n", snapshot.Repository)
	fmt.Fprintf(stdout, "  default branch: %s\n", snapshot.DefaultBranch)
	fmt.Fprintf(stdout, "  image:         %s\n", snapshot.ImageRepository)
	fmt.Fprintf(stdout, "  required CI:   %s\n", strings.Join(proposal.RequiredChecks, ", "))
	fmt.Fprintln(stdout, "\nRecommendation")
	fmt.Fprintf(stdout, "  Summary: %s\n", proposal.Summary)
	fmt.Fprintln(stdout, "  Policy:")
	printSetupBullets(stdout, proposal.PolicyHighlights)
	fmt.Fprintf(stdout, "  Release Template (%d files):\n", len(proposal.TemplateFiles))
	printSetupBullets(stdout, proposal.TemplateHighlights)
	fmt.Fprintln(stdout, "\nApplying validated setup...")

	home, err := project.Home()
	if err != nil {
		fmt.Fprintf(stderr, "safelane setup: %v\n", err)
		return ExitFail
	}
	loc := project.ForApp(home, snapshot.Application)
	if _, statErr := os.Stat(loc.ProjectFile); statErr == nil {
		fmt.Fprintf(stderr, "safelane setup: %s already exists; refusing to overwrite operator configuration\n", loc.ProjectFile)
		return ExitFail
	} else if !os.IsNotExist(statErr) {
		fmt.Fprintf(stderr, "safelane setup: inspect %s: %v\n", loc.ProjectFile, statErr)
		return ExitFail
	}
	if err := activateSetup(loc, snapshot, proposal); err != nil {
		fmt.Fprintf(stderr, "safelane setup: %v\n", err)
		return ExitFail
	}
	fmt.Fprintf(stdout, "\nsetup ready: %s\n", displayInitPath(home, loc.ProjectFile, false))
	fmt.Fprintln(stdout, "Run `safelane doctor` to validate the target and identities.")
	return ExitOK
}

func printSetupBullets(stdout io.Writer, items []string) {
	for _, item := range items {
		fmt.Fprintf(stdout, "    - %s\n", item)
	}
}

func recommendWithProgress(ctx context.Context, snapshot setupengine.Snapshot, recommend func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error), progress io.Writer) (setupengine.Proposal, error) {
	interactive := progressIsTerminal(progress)
	if !interactive {
		fmt.Fprintln(progress, "Preparing SafeLane setup...")
		proposal, err := recommend(ctx, snapshot)
		if err != nil {
			fmt.Fprintln(progress, "SafeLane recommendation unavailable.")
		} else {
			fmt.Fprintln(progress, "SafeLane recommendation ready.")
		}
		return proposal, err
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	statuses := []string{
		"Preparing SafeLane setup",
		"Reading repository shape",
		"Comparing CI checks",
		"Drafting policy and rollout template",
		"Waiting for Claude's recommendation",
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		frameIndex := 0
		statusIndex := 0
		lastStatus := time.Now()
		for {
			select {
			case <-ticker.C:
				if time.Since(lastStatus) >= 2500*time.Millisecond {
					statusIndex = (statusIndex + 1) % len(statuses)
					lastStatus = time.Now()
				}
				fmt.Fprintf(progress, "\r%s %s", frames[frameIndex%len(frames)], statuses[statusIndex])
				frameIndex++
			case <-done:
				return
			}
		}
	}()
	proposal, err := recommend(ctx, snapshot)
	close(done)
	<-stopped
	if err != nil {
		fmt.Fprintln(progress, "\r✖ SafeLane recommendation unavailable.                         ")
	} else {
		fmt.Fprintln(progress, "\r✔ SafeLane recommendation ready.                              ")
	}
	return proposal, err
}

func progressIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func activateSetup(loc project.Locations, snapshot setupengine.Snapshot, proposal setupengine.Proposal) error {
	if err := os.MkdirAll(loc.AppDir, 0o755); err != nil {
		return err
	}
	projectYAML := project.YAML(snapshot.Application, snapshot.Repository, snapshot.DefaultBranch, snapshot.ImageRepository, proposal.RequiredChecks)
	if _, err := writeInitFile(loc.ProjectFile, projectYAML); err != nil {
		return err
	}
	if _, err := writeInitFile(loc.PolicyFile, []byte(proposal.PolicyYAML)); err != nil {
		return err
	}
	if err := os.MkdirAll(loc.TemplateDir, 0o755); err != nil {
		return err
	}
	for _, file := range proposal.TemplateFiles {
		if _, err := writeInitFile(filepath.Join(loc.TemplateDir, filepath.FromSlash(file.Path)), []byte(file.Content)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(loc.ReleasesDir, 0o755); err != nil {
		return err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if _, err := writeSkillFile(filepath.Join(userHome, ".claude", "skills", "safelane", "SKILL.md")); err != nil {
		return err
	}
	if _, err := writeSkillFile(filepath.Join(userHome, ".agents", "skills", "safelane", "SKILL.md")); err != nil {
		return err
	}
	return nil
}
