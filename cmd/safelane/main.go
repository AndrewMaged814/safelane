// Command safelane coordinates evidence-shaped releases through Argo Rollouts.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/cli"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/spf13/cobra"
)

// version is overridden at build time via:
//
//	go build -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

type commandRuntime struct {
	root     string
	app      string
	stdout   io.Writer
	stderr   io.Writer
	storeDir string
	project  string
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	prependManagedDemoTools()
	app, args, err := extractGlobalApp(args)
	if err != nil {
		fmt.Fprintf(stderr, "safelane: %v\n", err)
		return cli.ExitUsage
	}
	rt := resolveCommandRuntime(".", app, stdout, stderr)
	restoreCaller := activateDemoCaller(rt.project)
	defer restoreCaller()
	root := newRootCommand(rt)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var coded exitError
		if errors.As(err, &coded) {
			return coded.code
		}
		fmt.Fprintf(stderr, "safelane: %v\n", err)
		return cli.ExitUsage
	}
	return cli.ExitOK
}

func activateDemoCaller(projectFile string) func() {
	if projectFile == "" {
		return func() {}
	}
	cfg, err := project.Load(projectFile)
	if err != nil || cfg.Application != "safelane-demo-api" {
		return func() {}
	}
	caller := filepath.Join(filepath.Dir(projectFile), "caller.kubeconfig")
	if _, err := os.Stat(caller); err != nil {
		return func() {}
	}
	previous, existed := os.LookupEnv("KUBECONFIG")
	_ = os.Setenv("KUBECONFIG", caller)
	return func() {
		if existed {
			_ = os.Setenv("KUBECONFIG", previous)
		} else {
			_ = os.Unsetenv("KUBECONFIG")
		}
	}
}

// prependManagedDemoTools makes demo-owned kubectl and its Argo plugin visible
// only to this SafeLane process. It never changes the user's ambient PATH.
func prependManagedDemoTools() {
	home, err := project.Home()
	if err != nil {
		return
	}
	bin := filepath.Join(home, "demo", "bin")
	if info, err := os.Stat(bin); err == nil && info.IsDir() {
		_ = os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

func resolveCommandRuntime(root, app string, stdout, stderr io.Writer) commandRuntime {
	rt := commandRuntime{root: root, app: app, stdout: stdout, stderr: stderr}
	if app != "" {
		if home, err := project.Home(); err == nil {
			loc := project.ForApp(home, app)
			rt.storeDir, rt.project = loc.ReleasesDir, loc.ProjectFile
		}
		return rt
	}
	if loc, err := project.Resolve(root); err == nil {
		rt.storeDir, rt.project = loc.ReleasesDir, loc.ProjectFile
	} else if home, homeErr := project.Home(); homeErr == nil {
		rt.storeDir = filepath.Join(home, "apps", ".unmatched", "releases")
	}
	return rt
}

func extractGlobalApp(args []string) (string, []string, error) {
	clean := make([]string, 0, len(args))
	app := ""
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--app":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", nil, fmt.Errorf("--app requires a value")
			}
			app, i = args[i+1], i+1
		case strings.HasPrefix(a, "--app="):
			app = strings.TrimPrefix(a, "--app=")
		default:
			clean = append(clean, a)
		}
	}
	if app != "" && project.SanitizeApplication(app) != app {
		return "", nil, fmt.Errorf("--app %q must be a lowercase DNS label", app)
	}
	return app, clean, nil
}

func newRootCommand(rt commandRuntime) *cobra.Command {
	root := &cobra.Command{
		Use:           "safelane",
		Short:         "Risk-shaped release coordination for coding agents",
		Long:          "SafeLane turns code evidence into a bounded rollout and coordinates Argo through promotion or rollback.",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.SetOut(rt.stdout)
	root.SetErr(rt.stderr)
	root.PersistentFlags().String("app", rt.app, "select an application outside its repository")
	root.AddCommand(setupGroup(rt), legacyLeaf(rt, "doctor [--json]", "Check whether SafeLane can release right now", cli.DoctorCommand(rt.root), injectProject))
	root.AddCommand(releaseGroup(rt), demoGroup(rt), completionCommand(root), versionCommand())
	return root
}

type argInjector func(commandRuntime, []string) []string

func injectProject(rt commandRuntime, args []string) []string {
	if rt.project == "" {
		return args
	}
	return append([]string{"--project", rt.project}, args...)
}

func injectProjectAndStore(rt commandRuntime, args []string) []string {
	if rt.project != "" {
		args = append([]string{"--project", rt.project}, args...)
	}
	if rt.storeDir != "" {
		args = append([]string{"--store-dir", rt.storeDir}, args...)
	}
	return args
}

func injectStore(rt commandRuntime, args []string) []string {
	if rt.storeDir == "" {
		return args
	}
	return append([]string{"--store-dir", rt.storeDir}, args...)
}

func legacyLeaf(rt commandRuntime, use, short string, command cli.Command, inject argInjector, prefix ...string) *cobra.Command {
	return &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if arg == "-h" || arg == "--help" {
					return cmd.Help()
				}
			}
			callArgs := append(append([]string(nil), prefix...), inject(rt, args)...)
			if code := command.Run(cmd.Context(), callArgs, rt.stdout, rt.stderr); code != cli.ExitOK {
				return exitError{code: code}
			}
			return nil
		},
	}
}

func releaseGroup(rt commandRuntime) *cobra.Command {
	releaseCmd := &cobra.Command{Use: "release", Short: "Plan, run, and prove a release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	releaseCmd.AddCommand(
		legacyLeaf(rt, "plan --pr <number> [--repo <owner/name>] [--environment <name>] [--json]", "Compile and persist a Safety Contract without mutating production", cli.ReleasePlanCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "run <release-id> [--yes] [--step] [--timeout 20m] [--json]", "Coordinate an approved release to a terminal outcome", cli.ReleaseRunCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "status [release-id] [--json]", "Reconcile and show release state", cli.ReleaseStatusCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "proof <release-id> [--details | --json]", "Show durable release proof", cli.ReleaseProofCommand(rt.storeDir), injectStore),
		legacyLeaf(rt, "retry <release-id> [--json]", "Create a new attempt after re-verifying evidence", cli.ReleaseRetryCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "accept-risk <release-id> --hazard <id> --reason <reason> [--yes] [--json]", "Accept one explicitly identified uncovered hazard", cli.ReleaseAcceptRiskCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "pause <release-id> --reason <reason> [--yes] [--json]", "Emergency-pause a release", cli.ReleasePauseCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "resume <release-id> --reason <reason> [--yes] [--json]", "Resume an explicitly emergency-paused release", cli.ReleaseResumeCommand(rt.root, rt.storeDir), injectProjectAndStore),
		legacyLeaf(rt, "abort <release-id> --reason <reason> [--yes] [--json]", "Emergency-abort a release", cli.ReleaseAbortCommand(rt.root, rt.storeDir), injectProjectAndStore),
	)
	return releaseCmd
}

func setupGroup(rt commandRuntime) *cobra.Command {
	setup := legacyLeaf(rt, "setup [--yes] [--json]", "Create operator-owned configuration from repository facts", cli.SetupCommand(rt.root), func(_ commandRuntime, args []string) []string { return args })
	setup.AddCommand(
		legacyLeaf(rt, "inspect [--json]", "Inspect repository facts as JSON without writing", cli.SetupInspectCommand(rt.root), func(_ commandRuntime, args []string) []string { return args }),
		legacyLeaf(rt, "apply --proposal <absolute-path> [--yes] [--json]", "Validate and apply an agent-authored setup proposal", cli.SetupApplyCommand(rt.root), func(_ commandRuntime, args []string) []string { return args }),
	)
	return setup
}

func demoGroup(rt commandRuntime) *cobra.Command {
	demo := &cobra.Command{Use: "demo", Short: "Manage the isolated SafeLane Kind demo", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	demo.AddCommand(
		legacyLeaf(rt, "up [--yes] [--json]", "Create or reconcile the demo", cli.DemoCommand("up"), func(_ commandRuntime, args []string) []string { return args }),
		legacyLeaf(rt, "reset [--yes] [--json]", "Archive records and restore demo fixtures", cli.DemoCommand("reset"), func(_ commandRuntime, args []string) []string { return args }),
		legacyLeaf(rt, "down [--yes] [--json]", "Delete only the owned demo cluster", cli.DemoCommand("down"), func(_ commandRuntime, args []string) []string { return args }),
	)
	return demo
}

func completionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate a shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return exitError{code: cli.ExitUsage}
			}
		},
	}
}

func versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print the SafeLane build version", Args: cobra.NoArgs, Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), version)
	}}
}
