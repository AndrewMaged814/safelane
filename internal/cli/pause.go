package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

// rollout pause and rollout abort share one row of Appendix A's "shape of
// it" table: both touch production, and neither is ever refused -- no
// eligibility check, no envelope check, only execute-and-record. That is
// what tells them apart from `start`/`advance`: there is no decision tree
// here at all, only a privileged kubectl call and a log entry.

type rolloutPauseFlags struct {
	storeDir             string
	projectFile          string
	controllerKubeconfig string
	controllerContext    string
}

func parseRolloutPauseFlags(args []string, stderr io.Writer, defaultStoreDir string) (rolloutPauseFlags, string, error) {
	var f rolloutPauseFlags
	fs := flag.NewFlagSet("rollout pause", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: .safelane/project.yml)")
	fs.StringVar(&f.controllerKubeconfig, "controller-kubeconfig", "",
		"kubeconfig for the privileged controller identity (optional; every privileged call runs unprivileged when unset)")
	fs.StringVar(&f.controllerContext, "controller-context", "", "kubeconfig context for the privileged controller identity (optional)")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "safelane rollout pause: exactly one release id is required")
		fs.Usage()
		return f, "", flag.ErrHelp
	}
	return f, rest[0], nil
}

func runRolloutPause(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseRolloutPauseFlags(args, stderr, defaultStoreDir)
	if err != nil {
		return ExitUsage
	}

	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitUsage
	}

	st := &store.FileStore{Dir: f.storeDir}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			printRolloutRejection(stderr, release.Invalid("release_not_found", "release_id",
				fmt.Sprintf("no Release record for %s", id),
				"Use the release id `safelane release` returned. rollout pause cannot invent a record."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane rollout pause: %v\n", err)
		return ExitFail
	}

	projPath := f.projectFile
	if projPath == "" {
		projPath = filepath.Join(root, filepath.FromSlash(project.RelPath))
	}
	cfg, err := project.Load(projPath)
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

	updated, err := pauseRollout(ctx, r, ex, time.Now)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}

	if err := st.Update(updated); err != nil {
		fmt.Fprintf(stderr, "safelane rollout pause: the pause was sent but could not be recorded: %v\n", err)
		return ExitFail
	}

	fmt.Fprint(stdout, renderPause(updated))
	return ExitOK
}

// pauseRollout runs `kubectl argo rollouts pause` and records who asked,
// narrowing the release to exactly where it is. It is never refused
// (Appendix A's "shape of it" table): there is no eligibility check and
// no envelope check, only the kubectl call and the record.
func pauseRollout(ctx context.Context, r *release.Release, ex *execute.Executor, now func() time.Time) (*release.Release, error) {
	if err := ex.Pause(ctx); err != nil {
		return nil, err
	}
	return r.WithExecution(release.ExecutionEntry{At: now(), Verb: release.VerbPause, Outcome: release.OutcomeGranted})
}

func renderPause(r *release.Release) string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Pausing rollout…")
	fmt.Fprintln(&b, "Argo Rollouts: pause requested")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "caller        %s (%s)\n", r.Caller().Identity, r.Caller().Kind)
	return b.String()
}
