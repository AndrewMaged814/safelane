package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

type rolloutAbortFlags struct {
	storeDir             string
	projectFile          string
	controllerKubeconfig string
	controllerContext    string
	reason               string
}

func parseRolloutAbortFlags(args []string, stderr io.Writer, defaultStoreDir string) (rolloutAbortFlags, string, error) {
	var f rolloutAbortFlags
	fs := flag.NewFlagSet("rollout abort", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.controllerKubeconfig, "controller-kubeconfig", "",
		"kubeconfig for the privileged controller identity (optional; every privileged call runs unprivileged when unset)")
	fs.StringVar(&f.controllerContext, "controller-context", "", "kubeconfig context for the privileged controller identity (optional)")
	fs.StringVar(&f.reason, "reason", "", "why this rollout is being aborted (required)")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "safelane rollout abort: exactly one release id is required")
		fs.Usage()
		return f, "", flag.ErrHelp
	}
	if strings.TrimSpace(f.reason) == "" {
		fmt.Fprintln(stderr, "safelane rollout abort: --reason is required")
		fs.Usage()
		return f, "", flag.ErrHelp
	}
	return f, rest[0], nil
}

func runRolloutAbort(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseRolloutAbortFlags(args, stderr, defaultStoreDir)
	if err != nil {
		return ExitUsage
	}

	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitUsage
	}

	paths, err := resolveRuntime(root, f.projectFile, "", f.storeDir)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	f.storeDir = paths.storeDir
	st := &store.FileStore{Dir: f.storeDir}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			printRolloutRejection(stderr, release.Invalid("release_not_found", "release_id",
				fmt.Sprintf("no Release record for %s", id),
				"Use the release id `safelane release` returned. rollout abort cannot invent a record."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane rollout abort: %v\n", err)
		return ExitFail
	}

	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}

	ex := execute.New(execute.Config{
		Namespace:            cfg.Target.Namespace,
		Rollout:              cfg.Target.Rollout,
		ControllerKubeconfig: f.controllerKubeconfig,
		ControllerContext:    f.controllerContext,
	})

	updated, err := abortRollout(ctx, r, ex, f.reason, time.Now)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}

	if err := st.Update(updated); err != nil {
		fmt.Fprintf(stderr, "safelane rollout abort: the abort was sent but could not be recorded: %v\n", err)
		return ExitFail
	}

	fmt.Fprint(stdout, renderAbort(updated, f.reason))
	return ExitOK
}

// abortRollout runs `kubectl argo rollouts abort` and records who asked
// and why, restoring stable traffic. It is never refused (Appendix A's
// "shape of it" table): there is no eligibility check and no envelope
// check, only the kubectl call and the record. This is the caller's own
// abort ([release.VerbAbort]) -- distinct from Argo Rollouts deciding to
// abort on its own after a failed analysis ([release.VerbArgoAbort],
// advance.go), which SafeLane only ever observes.
func abortRollout(ctx context.Context, r *release.Release, ex *execute.Executor, reason string, now func() time.Time) (*release.Release, error) {
	if err := ex.Abort(ctx); err != nil {
		return nil, err
	}
	return r.WithExecution(release.ExecutionEntry{
		At: now(), Verb: release.VerbAbort, Outcome: release.OutcomeAborted, Detail: reason,
	})
}

func renderAbort(r *release.Release, reason string) string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Aborting rollout…")
	fmt.Fprintln(&b, "Argo Rollouts: abort requested")
	fmt.Fprintln(&b, "Stable traffic is restored.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "reason        %s\n", reason)
	fmt.Fprintf(&b, "caller        %s (%s)\n", r.Caller().Identity, r.Caller().Kind)
	return b.String()
}
