package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/spf13/pflag"
)

func ReleasePauseCommand(root, storeDir string) Command {
	return releaseControlCommand("pause", root, storeDir)
}
func ReleaseResumeCommand(root, storeDir string) Command {
	return releaseControlCommand("resume", root, storeDir)
}
func ReleaseAbortCommand(root, storeDir string) Command {
	return releaseControlCommand("abort", root, storeDir)
}

func releaseControlCommand(action, root, defaultStoreDir string) Command {
	return Command{Name: action, Summary: "emergency " + action, Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runReleaseControl(ctx, action, args, os.Stdin, stdout, stderr, root, defaultStoreDir)
	}}
}

type releaseControlFlags struct {
	reason, projectFile, storeDir string
	yes, jsonOut                  bool
}

func parseReleaseControl(action string, args []string, stderr io.Writer, defaultStoreDir string) (releaseControlFlags, string, error) {
	var f releaseControlFlags
	fs := pflag.NewFlagSet("release "+action, pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.reason, "reason", "", "durable reason for this emergency control")
	fs.BoolVar(&f.yes, "yes", false, "confirm this emergency control")
	fs.BoolVar(&f.jsonOut, "json", false, "print only the final JSON result to stdout")
	fs.StringVar(&f.projectFile, "project", "", "operator project file")
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "release record directory")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	if fs.NArg() != 1 {
		return f, "", fmt.Errorf("exactly one release id is required")
	}
	if strings.TrimSpace(f.reason) == "" {
		return f, "", fmt.Errorf("--reason is required")
	}
	return f, fs.Arg(0), nil
}

func runReleaseControl(ctx context.Context, action string, args []string, stdin io.Reader, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseReleaseControl(action, args, stderr, defaultStoreDir)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release %s: %v\n", action, err)
		return ExitUsage
	}
	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release %s: %v\n", action, err)
		return ExitUsage
	}
	if !f.yes && !confirmControl(stdin, stderr, action, id, f.reason) {
		return ExitDecision
	}
	paths, err := resolveRuntime(root, f.projectFile, "", f.storeDir)
	if err != nil {
		return writeResultError(stderr, "release "+action, err)
	}
	ordered := []string{"--project", paths.projectFile, "--store-dir", paths.storeDir, "--reason", f.reason, string(id)}
	var code int
	switch action {
	case "pause":
		code = runRolloutPause(ctx, ordered, stderr, stderr, root, paths.storeDir)
	case "abort":
		code = runRolloutAbort(ctx, ordered, stderr, stderr, root, paths.storeDir)
	case "resume":
		code = runRolloutResume(ctx, id, f.reason, stderr, root, paths)
	default:
		return ExitUsage
	}
	if code != ExitOK {
		return code
	}
	r, err := (&store.FileStore{Dir: paths.storeDir}).Load(id)
	if err != nil {
		return writeResultError(stderr, "release "+action, err)
	}
	next := fmt.Sprintf("safelane release status %s", id)
	if action == "resume" {
		next = fmt.Sprintf("safelane release run %s --yes", id)
	}
	if action == "abort" {
		next = fmt.Sprintf("safelane release proof %s", id)
	}
	if f.jsonOut {
		return encodeControlResult(stdout, stderr, action, r, next)
	}
	fmt.Fprintf(stdout, "Release %s is %s.\nNext: %s\n", id, r.State(), next)
	return ExitOK
}

func confirmControl(stdin io.Reader, stderr io.Writer, action string, id release.ReleaseID, reason string) bool {
	want := strings.ToUpper(action)
	fmt.Fprintf(stderr, "%s %s? Reason: %s\nType %s: ", strings.Title(action), id, reason, want)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == want
}

func encodeControlResult(stdout, stderr io.Writer, action string, r *release.Release, next string) int {
	envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release " + action, OK: true, ReleaseID: string(r.ID), State: string(r.State()), NextCommand: next, Warnings: []string{}, Result: map[string]any{}}
	if err := jsonEncode(stdout, envelope); err != nil {
		return writeResultError(stderr, "release "+action, err)
	}
	return ExitOK
}

func runRolloutResume(ctx context.Context, id release.ReleaseID, reason string, stderr io.Writer, root string, paths runtimePaths) int {
	st := &store.FileStore{Dir: paths.storeDir}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return writeResultError(stderr, "release resume", fmt.Errorf("release %s was not found", id))
		}
		return writeResultError(stderr, "release resume", err)
	}
	if r.State() != release.StatePaused {
		return writeResultError(stderr, "release resume", fmt.Errorf("release %s is %s, not emergency-paused", id, r.State()))
	}
	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		return writeResultError(stderr, "release resume", err)
	}
	kubeconfig, kubeContext := paths.controllerCredentials("", "")
	ex := newExecutor(execute.Config{Namespace: cfg.Target.Namespace, Rollout: cfg.Target.Rollout, ControllerKubeconfig: kubeconfig, ControllerContext: kubeContext})
	if err := ex.Promote(ctx); err != nil {
		return writeResultError(stderr, "release resume", err)
	}
	updated, err := r.WithExecution(release.ExecutionEntry{At: time.Now().UTC(), Verb: release.VerbResume, Outcome: release.OutcomeGranted, Detail: reason})
	if err != nil {
		return writeResultError(stderr, "release resume", err)
	}
	binding, _ := updated.Binding()
	updated, err = updated.WithState(release.StateProgressing, binding)
	if err != nil {
		return writeResultError(stderr, "release resume", err)
	}
	if err := st.Update(updated); err != nil {
		return writeResultError(stderr, "release resume", fmt.Errorf("resume was sent but could not be recorded: %w", err))
	}
	fmt.Fprintf(stderr, "Argo Rollouts: resume requested for %s.\n", id)
	_ = root
	return ExitOK
}
