package cli

import (
	"bufio"
	"context"
	"errors"
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
// after one explicit approval. It never writes inside the application repo.
func SetupCommand(root string) Command {
	return setupCommand(root, setupDeps{
		recommend: func(ctx context.Context, snapshot setupengine.Snapshot) (setupengine.Proposal, error) {
			return setupengine.Recommend(ctx, snapshot, setupengine.RealRunner)
		},
		input: os.Stdin,
	})
}

type setupDeps struct {
	recommend func(context.Context, setupengine.Snapshot) (setupengine.Proposal, error)
	input     io.Reader
}

func setupCommand(root string, deps setupDeps) Command {
	return Command{
		Name:    "setup",
		Summary: "discover the repository and create approved operator configuration",
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
		agentProposal, recommendErr := deps.recommend(recommendCtx, snapshot)
		cancel()
		if recommendErr != nil {
			fmt.Fprintf(stdout, "Claude recommendation unavailable: %v\n", recommendErr)
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
	fmt.Fprintf(stdout, "  repository       %s\n", snapshot.Repository)
	fmt.Fprintf(stdout, "  default branch   %s\n", snapshot.DefaultBranch)
	fmt.Fprintf(stdout, "  image repository %s\n", snapshot.ImageRepository)
	fmt.Fprintf(stdout, "  required checks  %s\n", strings.Join(proposal.RequiredChecks, ", "))
	fmt.Fprintf(stdout, "  policy           recommended\n")
	fmt.Fprintf(stdout, "  release template %d files recommended\n", len(proposal.TemplateFiles))
	if proposal.Summary != "" {
		fmt.Fprintf(stdout, "\n%s\n", proposal.Summary)
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Apply this setup? [Y/n]")
	reader := bufio.NewReader(deps.input)
	answer, readErr := reader.ReadString('\n')
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		fmt.Fprintf(stderr, "safelane setup: read approval: %v\n", readErr)
		return ExitFail
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Fprintln(stderr, "safelane setup: cancelled; no operator files were written")
		return ExitFail
	}

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
