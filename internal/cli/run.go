package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/spf13/pflag"
)

// ReleaseRunCommand builds the public continuous release coordinator.
func ReleaseRunCommand(root, defaultStoreDir string) Command {
	return Command{Name: "run", Summary: "coordinate a release to a terminal outcome", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runReleaseToOutcome(ctx, args, os.Stdin, stdout, stderr, root, defaultStoreDir)
	}}
}

type releaseRunFlags struct {
	yes, step, jsonOut    bool
	timeout               time.Duration
	projectFile, storeDir string
}

func parseReleaseRunFlags(args []string, stderr io.Writer, defaultStoreDir string) (releaseRunFlags, string, error) {
	var f releaseRunFlags
	fs := pflag.NewFlagSet("release run", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&f.yes, "yes", false, "approve the frozen Safety Contract")
	fs.BoolVar(&f.step, "step", false, "perform at most one authorized progression")
	fs.BoolVar(&f.jsonOut, "json", false, "print only the final result to stdout")
	fs.DurationVar(&f.timeout, "timeout", 20*time.Minute, "maximum time to remain attached")
	fs.StringVar(&f.projectFile, "project", "", "operator project file")
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "release record directory")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	if fs.NArg() != 1 {
		return f, "", fmt.Errorf("exactly one release id is required")
	}
	if f.timeout <= 0 {
		return f, "", fmt.Errorf("--timeout must be positive")
	}
	return f, fs.Arg(0), nil
}

func runReleaseToOutcome(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseReleaseRunFlags(args, stderr, defaultStoreDir)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release run: %v\n", err)
		return ExitUsage
	}
	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release run: %v\n", err)
		return ExitUsage
	}
	paths, err := resolveRuntime(root, f.projectFile, "", f.storeDir)
	if err != nil {
		return writeResultError(stderr, "release run", err)
	}
	f.projectFile, f.storeDir = paths.projectFile, paths.storeDir
	st := &store.FileStore{Dir: f.storeDir}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return writeResultError(stderr, "release run", fmt.Errorf("release %s was not found", id))
		}
		return writeResultError(stderr, "release run", err)
	}
	if r.State() == release.StatePaused {
		return releaseRunDecision(stderr, id, "release is emergency-paused; use safelane release resume")
	}
	if !f.yes {
		renderSafetyContract(stderr, r)
		if !confirmRelease(stdin, stderr) {
			return ExitDecision
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	mutations := 0
	for {
		r, err = st.Load(id)
		if err != nil {
			return writeResultError(stderr, "release run", err)
		}
		switch r.State() {
		case release.StatePromoted:
			return writeReleaseRunResult(stdout, stderr, f.jsonOut, r, true)
		case release.StateAborted, release.StateFailed, release.StateBlocked, release.StateIneligible, release.StateIndeterminate:
			_ = writeReleaseRunResult(stdout, stderr, f.jsonOut, r, false)
			return ExitFail
		case release.StatePaused:
			return releaseRunDecision(stderr, id, "release is emergency-paused; only release resume may continue it")
		}
		if f.step && mutations == 1 {
			return writeReleaseRunResult(stdout, stderr, f.jsonOut, r, true)
		}
		if authority, hazard := effectiveAuthority(r); hazard != nil {
			nextExposure := 0
			if r.State() == release.StateReady {
				if envelope, ok := r.Eligibility().Envelope(); ok && len(envelope.Stages()) > 0 {
					nextExposure = envelope.Stages()[0]
				}
			} else if envelope, ok := r.Eligibility().Envelope(); ok {
				granted, _, _ := highestGranted(r.Execution())
				nextExposure, _ = nextAllowedAfter(envelope.Stages(), granted)
			}
			if nextExposure > authority {
				fmt.Fprintf(stderr, "Human decision required: hazard %s (%s) limits authority to %d%%; next exposure is %d%%.\n", hazard.ID, hazard.Severity, authority, nextExposure)
				fmt.Fprintf(stderr, "Use safelane release accept-risk %s --hazard %s --reason <reason>.\n", id, hazard.ID)
				return ExitDecision
			}
		}
		if err := runCtx.Err(); err != nil {
			fmt.Fprintf(stderr, "safelane release run: timed out after %s; mutation outcome may be unknown\n", f.timeout)
			return ExitTimeout
		}
		remaining := time.Until(deadline(runCtx, time.Now().Add(f.timeout)))
		if remaining < time.Second {
			remaining = time.Second
		}
		common := []string{"--project", f.projectFile, "--store-dir", f.storeDir, "--timeout", remaining.String(), string(id)}
		var code int
		if r.State() == release.StateReady {
			fmt.Fprintf(stderr, "Starting release %s through Argo…\n", id)
			code = runRolloutStart(runCtx, common, stderr, stderr, root, f.storeDir)
		} else {
			fmt.Fprintf(stderr, "Reconciling release %s and requesting the next authorized progression…\n", id)
			code = runRolloutAdvance(runCtx, common, stderr, stderr, root, f.storeDir)
		}
		mutations++
		if code == ExitTimeout {
			return ExitTimeout
		}
		if code != ExitOK {
			latest, loadErr := st.Load(id)
			if loadErr == nil && latest.State() == release.StatePaused {
				return ExitDecision
			}
			if loadErr == nil && latest.State() == release.StateAborted {
				_ = writeReleaseRunResult(stdout, stderr, f.jsonOut, latest, false)
				return ExitFail
			}
			return code
		}
	}
}

func deadline(ctx context.Context, fallback time.Time) time.Time {
	if value, ok := ctx.Deadline(); ok {
		return value
	}
	return fallback
}

func confirmRelease(stdin io.Reader, stderr io.Writer) bool {
	fmt.Fprint(stderr, "Approve this Safety Contract? Type RELEASE: ")
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	return strings.TrimSpace(line) == "RELEASE"
}

func renderSafetyContract(w io.Writer, r *release.Release) {
	fmt.Fprintf(w, "Safety Contract %s\n  PR: %s#%d\n  artifact: %s\n  target: %s/%s\n", r.ID, r.Request().Repository, r.Request().PullRequest, r.ArtifactDigest(), r.Target().Cluster, r.Target().Environment)
	if a, ok := r.RecordedAssessment(); ok {
		fmt.Fprintf(w, "  assessment mode: %s\n  risk: %s\n  lane: %s\n  authority ceiling: %d%%\n", a.AssessmentMode, a.Risk, a.Lane, a.AuthorizedUntil)
		if len(a.Facts.RuntimeAssertions) > 0 {
			fmt.Fprintf(w, "  runtime assertions: %s\n", strings.Join(a.Facts.RuntimeAssertions, ", "))
		}
		if !a.Model.Available {
			fmt.Fprintf(w, "  semantic assessment: unavailable (%s); guarded fallback\n", a.Model.Reason)
		}
		for _, hazard := range a.Model.Hazards {
			coverage := "uncovered"
			if hazard.Covered {
				coverage = "covered"
			}
			fmt.Fprintf(w, "  hazard %s: %s (%s, %s); requires %s\n", hazard.ID, hazard.FailureMode, hazard.Severity, coverage, hazard.RequiredAssertion)
		}
	}
	if envelope, ok := r.Eligibility().Envelope(); ok {
		fmt.Fprintf(w, "  authorized progression: %v\n", envelope.Stages())
	}
}

func releaseRunDecision(stderr io.Writer, id release.ReleaseID, message string) int {
	fmt.Fprintf(stderr, "safelane release run: %s (%s)\n", message, id)
	return ExitDecision
}

func writeReleaseRunResult(stdout, stderr io.Writer, jsonOut bool, r *release.Release, positive bool) int {
	next := fmt.Sprintf("safelane release proof %s --json", r.ID)
	if jsonOut {
		envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release run", OK: positive, ReleaseID: string(r.ID), State: string(r.State()), NextCommand: next, Warnings: []string{}, Result: map[string]any{}}
		if err := jsonEncode(stdout, envelope); err != nil {
			return writeResultError(stderr, "release run", err)
		}
	} else {
		fmt.Fprintf(stdout, "Release %s is %s.\nNext: %s\n", r.ID, r.State(), next)
	}
	return ExitOK
}

func jsonEncode(w io.Writer, value any) error {
	return json.NewEncoder(w).Encode(value)
}
