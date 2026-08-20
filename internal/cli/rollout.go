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

// defaultGateTimeout bounds how long `rollout start` waits for the
// Rollout to reach the lane's first gate before giving up with exit 3
// (Appendix C6). The demo's own wall-clock estimate is under a minute;
// this leaves headroom for a slower AnalysisRun without hanging forever
// on a cluster that will never answer.
const defaultGateTimeout = 5 * time.Minute

// RolloutCommand builds `safelane rollout start <release-id>`. root is the
// application clone whose GitHub remote selects the operator-owned app.
func RolloutCommand(root, defaultStoreDir string) Command {
	return Command{
		Name:    "rollout",
		Summary: "start, advance, pause, or abort a release's canary rollout",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			if len(args) == 0 {
				fmt.Fprintln(stderr, "safelane rollout: a subcommand is required (start, advance, pause, abort)")
				return ExitUsage
			}
			switch args[0] {
			case "start":
				return runRolloutStart(ctx, args[1:], stdout, stderr, root, defaultStoreDir)
			case "advance":
				return runRolloutAdvance(ctx, args[1:], stdout, stderr, root, defaultStoreDir)
			case "pause":
				return runRolloutPause(ctx, args[1:], stdout, stderr, root, defaultStoreDir)
			case "abort":
				return runRolloutAbort(ctx, args[1:], stdout, stderr, root, defaultStoreDir)
			default:
				fmt.Fprintf(stderr, "safelane rollout: unknown subcommand %q (supported: start, advance, pause, abort)\n", args[0])
				return ExitUsage
			}
		},
	}
}

type rolloutStartFlags struct {
	storeDir             string
	projectFile          string
	controllerKubeconfig string
	controllerContext    string
	timeout              time.Duration
}

func parseRolloutStartFlags(args []string, stderr io.Writer, defaultStoreDir string) (rolloutStartFlags, string, error) {
	var f rolloutStartFlags
	fs := flag.NewFlagSet("rollout start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.controllerKubeconfig, "controller-kubeconfig", "",
		"kubeconfig for the privileged controller identity (default: project.yml)")
	fs.StringVar(&f.controllerContext, "controller-context", "", "kubeconfig context for the privileged controller identity (default: project.yml)")
	fs.DurationVar(&f.timeout, "timeout", defaultGateTimeout, "how long to wait for the rollout to reach its first gate")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "safelane rollout start: exactly one release id is required")
		fs.Usage()
		return f, "", flag.ErrHelp
	}
	return f, rest[0], nil
}

func runRolloutStart(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseRolloutStartFlags(args, stderr, defaultStoreDir)
	if err != nil {
		return ExitUsage
	}

	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitUsage
	}

	var paths runtimePaths
	if f.storeDir == "" {
		paths, err = resolveRuntime(root, f.projectFile, "", f.storeDir)
		if err != nil {
			printRolloutRejection(stderr, err)
			return ExitFail
		}
		f.storeDir = paths.storeDir
	}
	st := &store.FileStore{Dir: f.storeDir}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			printRolloutRejection(stderr, release.Invalid("release_not_found", "release_id",
				fmt.Sprintf("no Release record for %s", id),
				"Use the release id `safelane release` returned. rollout start cannot invent a record."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane rollout start: %v\n", err)
		return ExitFail
	}
	if err := refuseIfNotEligible(r); err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	if paths.projectFile == "" {
		paths, err = resolveRuntime(root, f.projectFile, "", f.storeDir)
		if err != nil {
			printRolloutRejection(stderr, err)
			return ExitFail
		}
	}
	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	f.controllerKubeconfig, f.controllerContext = paths.controllerCredentials(f.controllerKubeconfig, f.controllerContext)

	ex := newExecutor(execute.Config{
		Namespace:            cfg.Target.Namespace,
		Rollout:              cfg.Target.Rollout,
		ControllerKubeconfig: f.controllerKubeconfig,
		ControllerContext:    f.controllerContext,
	})

	result, err := startRollout(ctx, r, ex, f.timeout, time.Now)
	if errors.Is(err, execute.ErrGateTimeout) {
		fmt.Fprintf(stdout, "\nThe promotion was sent, but the rollout did not reach its first gate within %s.\n"+
			"SafeLane does not know whether it succeeded. Read status. Do not retry.\n", f.timeout)
		return ExitTimeout
	}
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}

	if err := st.Update(result.release); err != nil {
		fmt.Fprintf(stderr, "safelane rollout start: the rollout was granted but could not be persisted: %v\n", err)
		return ExitFail
	}

	fmt.Fprint(stdout, result.Render())
	return ExitOK
}

// refuseIfNotEligible is N10: starting an ineligible release is refused
// outright, before any kubectl call, because an ineligible release never
// earned a lane or an envelope to start.
func refuseIfNotEligible(r *release.Release) error {
	elig := r.Eligibility()
	if elig.Status() == release.EligibilityEligible {
		return nil
	}
	return release.Invalid("release_not_eligible", "",
		fmt.Sprintf("eligibility is %s (%s)\n      no lane and no envelope were attached; no rollout may start",
			elig.Status(), elig.ReasonCode()),
		"")
}

// printRolloutRejection is [printRejection] under the `rollout` command
// name, so a caller reading stderr sees which subcommand refused rather
// than always "safelane release".
func printRolloutRejection(w io.Writer, err error) {
	var errs release.Errors
	if errors.As(err, &errs) {
		fmt.Fprintln(w, "safelane rollout: rejected:")
		for _, e := range errs {
			printError(w, e)
		}
		return
	}
	var single *release.Error
	if errors.As(err, &single) {
		fmt.Fprintln(w, "safelane rollout: rejected:")
		printError(w, single)
		return
	}
	fmt.Fprintf(w, "safelane rollout: %v\n", err)
}

// startResult is the whole `rollout start` outcome, before any of it is a
// string: the release with its granted execution entry appended, plus the
// observed apply and wait detail Appendix A2.2/A3.2 print.
type startResult struct {
	release           *release.Release
	applyRows         []execute.ApplyRow
	progressingWeight int
	grantedAt         time.Time
}

// startRollout applies r's already-hashed bundle -- never a re-render --
// and waits for Argo to reach the lane's first gate, per Appendix C5's
// blocking wait. now is injected so tests get a deterministic timestamp.
func startRollout(ctx context.Context, r *release.Release, ex *execute.Executor, timeout time.Duration, now func() time.Time) (startResult, error) {
	bundle, ok := r.Bundle()
	if !ok {
		return startResult{}, release.Internal("release_without_bundle",
			"an eligible release must carry a rendered bundle to start")
	}
	envelope, ok := r.Eligibility().Envelope()
	if !ok {
		return startResult{}, release.Internal("release_without_envelope",
			"an eligible release must carry a rollout envelope to start")
	}
	weights := envelope.Stages()
	if len(weights) == 0 {
		return startResult{}, release.Internal("empty_rollout_envelope", "the release's envelope has no weights")
	}

	rows, err := ex.Apply(ctx, bundle)
	if err != nil {
		return startResult{}, err
	}

	var progressingWeight int
	status, err := ex.WaitForGate(ctx, timeout, func(st execute.Status) {
		if st.State == execute.StateProgressing && progressingWeight == 0 {
			progressingWeight = st.CurrentWeight
		}
	})
	if err != nil {
		return startResult{}, err
	}
	if status.State != execute.StateAtGate {
		return startResult{}, release.Invalid("rollout_did_not_reach_a_gate", "",
			fmt.Sprintf("the rollout reached state %q instead of pausing at its first gate", status.State),
			"Read `safelane status` for detail. This release did not start cleanly.")
	}
	if progressingWeight == 0 {
		// The very first observed status was already at_gate -- a fast
		// reconciler, or a fake in a test that skips straight there.
		// The weight is still known: it is the lane's first one.
		progressingWeight = weights[0]
	}

	grantedAt := now()
	updated, err := r.WithExecution(release.ExecutionEntry{
		At:              grantedAt,
		Verb:            release.VerbStart,
		RequestedWeight: weights[0],
		Outcome:         release.OutcomeGranted,
	})
	if err != nil {
		return startResult{}, err
	}

	return startResult{
		release:           updated,
		applyRows:         rows,
		progressingWeight: progressingWeight,
		grantedAt:         grantedAt,
	}, nil
}

// Render writes the A2.2/A3.2 report: what was applied, Argo's own
// progress toward the first gate, the weight ladder, and what may happen
// next.
func (s startResult) Render() string {
	var b strings.Builder
	envelope, _ := s.release.Eligibility().Envelope()
	weights := envelope.Stages()
	gates := gateCount(weights)
	bundle, _ := s.release.Bundle()

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Applying the Rendered Manifest Bundle…")
	renderApplyRows(&b, s.applyRows, bundle.PinnedDigest())
	fmt.Fprintln(&b)

	fmt.Fprintf(&b, "Argo Rollouts: Progressing → weight %d\n", s.progressingWeight)
	fmt.Fprintf(&b, "Argo Rollouts: Paused at gate 1 of %d\n", gates)
	fmt.Fprintln(&b)

	renderWeightLadder(&b, weights, 0, s.grantedAt.UTC().Format("15:04:05")+"Z")
	fmt.Fprintln(&b)

	lane := ""
	if a, ok := s.release.Assessment(); ok {
		lane = a.Lane
	}
	fmt.Fprintf(&b, "lane          %s\n", lane)
	if len(weights) > 1 {
		fmt.Fprintf(&b, "next action   advance (%d)\n", weights[1])
	} else {
		fmt.Fprintln(&b, "next action   none. This release is closed.")
	}
	return b.String()
}

// renderApplyRows writes one row per applied resource, in bundle order:
// its name, its kind, and what kubectl did to it. Only the Rollout ever
// reports anything other than "unchanged" in the demo, since it is the
// only resource whose spec is release-specific -- and when it changes,
// the row names the image digest it was patched to rather than kubectl's
// bare verb, because the digest is the fact worth reading, not the verb.
func renderApplyRows(b *strings.Builder, rows []execute.ApplyRow, pinnedDigest string) {
	nameWidth, kindWidth := 0, 0
	for _, row := range rows {
		if n := runeLen(row.Ref.Name); n > nameWidth {
			nameWidth = n
		}
		if n := runeLen(row.Ref.Kind); n > kindWidth {
			kindWidth = n
		}
	}
	for _, row := range rows {
		outcome := row.Verb
		if row.Ref.Kind == "Rollout" && row.Verb != "unchanged" {
			outcome = "patched → " + shortDigest(pinnedDigest)
		}
		fmt.Fprintf(b, "  %s%s%s\n", pad(row.Ref.Name, nameWidth+2), pad(row.Ref.Kind, kindWidth+2), outcome)
	}
}

// renderWeightLadder writes the weight ladder widget: one row per
// envelope weight, the currently granted one marked "granted" with its
// timestamp and a filled bar, the row right after it marked "next
// allowed", and every other row bare.
func renderWeightLadder(b *strings.Builder, weights []int, grantedIndex int, grantedAt string) {
	for i, w := range weights {
		label := "  "
		if i == 0 {
			label = "  weight"
		}
		left := fmt.Sprintf("%-8s%5d", label, w)
		bar := ladderBar(w, i == grantedIndex)
		switch {
		case i == grantedIndex:
			fmt.Fprintf(b, "%s %s  granted   %s\n", left, bar, grantedAt)
		case i == grantedIndex+1:
			fmt.Fprintf(b, "%s %s  next allowed\n", left, bar)
		default:
			fmt.Fprintf(b, "%s %s\n", left, bar)
		}
	}
}

// ladderBar is the 20-character █/░ bar: each block is 5% of the way to
// 100. Only the granted row's weight is ever drawn filled -- a row not
// yet reached shows the shape of the scale, not a value it has not
// earned.
func ladderBar(weight int, filled bool) string {
	const n = 20
	blocks := 0
	if filled {
		blocks = weight / 5
		if blocks > n {
			blocks = n
		}
		if blocks < 0 {
			blocks = 0
		}
	}
	return strings.Repeat("█", blocks) + strings.Repeat("░", n-blocks)
}
