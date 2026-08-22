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
		fmt.Fprintln(stderr, "safelane release run: exactly one release id is required")
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
		fmt.Fprintf(stderr, "safelane release run: %v\n", err)
		return ExitFail
	}
	if err := refuseIfNotEligible(r); err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	if r.State() != release.StateReady {
		printRolloutRejection(stderr, release.Invalid("release_not_ready", "release_id",
			fmt.Sprintf("release %s is %s; rollout start only accepts ready attempts", r.ID, r.State()),
			"Run `safelane release plan --pr <n>` and follow its next_command."))
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
	pre, err := ex.GetStatus(ctx)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	bundle, _ := r.Bundle()
	binding := release.ExecutionBinding{ReleaseID: r.ID, Application: r.Target().Application,
		Environment: r.Target().Environment, Cluster: r.Target().Cluster, Namespace: r.Target().Namespace,
		Rollout: cfg.Target.Rollout, Digest: bundle.PinnedDigest(),
		PreGeneration: pre.Generation, PreDigest: pre.ImageDigest, PreArgoRevision: pre.Revision, PreAnalysisRun: pre.AnalysisRunName}
	r, err = r.WithState(release.StateStarting, binding)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	if err := st.Update(r); err != nil {
		fmt.Fprintf(stderr, "safelane release run: could not persist starting state: %v\n", err)
		return ExitFail
	}

	retryAbortedSameDigest := false
	if r.RetryOf() != "" && pre.ImageDigest == bundle.PinnedDigest() {
		if parent, loadErr := st.Load(r.RetryOf()); loadErr == nil && parent.State() == release.StateAborted {
			retryAbortedSameDigest = true
		}
	}
	result, err := startRolloutAttempt(ctx, r, ex, f.timeout, time.Now, retryAbortedSameDigest)
	if result.release != nil {
		finalBinding := binding
		if result.final.Generation != 0 {
			finalBinding.Generation, finalBinding.ArgoRevision = result.final.Generation, result.final.Revision
			finalBinding.AnalysisRunName = result.final.AnalysisRunName
		}
		state := release.StateAtGate
		if err != nil {
			state = release.StateFailed
		}
		if err != nil && result.final.Generation == 0 {
			state = release.StateUnknown
		}
		if result.final.State == execute.StateAborted {
			state = release.StateAborted
		}
		if result.final.ReleaseID != "" && result.final.ReleaseID != r.ID {
			state = release.StateUnknown
		}
		result.release, _ = result.release.WithState(state, finalBinding)
	}
	if err != nil && result.release != nil {
		if serr := st.Update(result.release); serr != nil {
			fmt.Fprintf(stderr, "safelane release run: the attempted start could not be persisted: %v\n", serr)
			return ExitFail
		}
	}
	if errors.Is(err, execute.ErrGateTimeout) {
		fmt.Fprintf(stdout, "\nThe bundle was applied, but the rollout did not reach its first gate within %s.\n"+
			"SafeLane does not know whether it succeeded. Read status. Do not retry.\n", f.timeout)
		return ExitTimeout
	}
	if err != nil {
		if len(result.applyRows) > 0 {
			fmt.Fprint(stdout, result.RenderFailure())
			printRolloutFailure(stderr, err)
		} else {
			printRolloutRejection(stderr, err)
		}
		return ExitFail
	}

	if err := st.Update(result.release); err != nil {
		fmt.Fprintf(stderr, "safelane release run: the rollout was granted but could not be persisted: %v\n", err)
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
		fmt.Fprintln(w, "safelane release run: rejected:")
		for _, e := range errs {
			printError(w, e)
		}
		return
	}
	var single *release.Error
	if errors.As(err, &single) {
		fmt.Fprintln(w, "safelane release run: rejected:")
		printError(w, single)
		return
	}
	fmt.Fprintf(w, "safelane release run: %v\n", err)
}

func printRolloutFailure(w io.Writer, err error) {
	fmt.Fprintln(w, "safelane release run: failed after applying the Rendered Manifest Bundle:")
	var single *release.Error
	if errors.As(err, &single) {
		printError(w, single)
		return
	}
	fmt.Fprintf(w, "  %v\n", err)
}

// startResult is the whole `rollout start` outcome, before any of it is a
// string: the release with its granted execution entry appended, plus the
// observed apply and wait detail Appendix A2.2/A3.2 print.
type startResult struct {
	release           *release.Release
	applyRows         []execute.ApplyRow
	progressingWeight int
	grantedAt         time.Time
	final             execute.Status
}

// RenderFailure makes the mutation boundary explicit. A start that reaches
// this report was not a pre-apply refusal: the exact bundle was applied and
// the terminal observation was persisted for proof.
func (s startResult) RenderFailure() string {
	var b strings.Builder
	bundle, _ := s.release.Bundle()
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Applied the Rendered Manifest Bundle before the rollout stopped:")
	renderApplyRows(&b, s.applyRows, bundle.PinnedDigest())
	fmt.Fprintf(&b, "Argo Rollouts: %s at generation %d (observed %d)\n",
		s.final.State, s.final.Generation, s.final.ObservedGeneration)
	if s.final.Message != "" {
		fmt.Fprintf(&b, "Argo message: %s\n", s.final.Message)
	}
	fmt.Fprintf(&b, "The failed start was recorded. Run: safelane release proof %s --details\n\n", s.release.ID)
	return b.String()
}

// startRollout applies r's already-hashed bundle -- never a re-render --
// and waits for Argo to reach the lane's first gate, per Appendix C5's
// blocking wait. now is injected so tests get a deterministic timestamp.
func startRollout(ctx context.Context, r *release.Release, ex *execute.Executor, timeout time.Duration, now func() time.Time) (startResult, error) {
	return startRolloutAttempt(ctx, r, ex, timeout, now, false)
}

func startRolloutAttempt(ctx context.Context, r *release.Release, ex *execute.Executor, timeout time.Duration, now func() time.Time, retryAbortedSameDigest bool) (startResult, error) {
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

	// The capability assertion sits immediately before the first mutating
	// kubectl call. Separate controller credentials are the signal that the
	// two-identity boundary is configured; legacy ambient-context setups have
	// no honest caller identity to assert and therefore record no boundary.
	if ex.ControllerKubeconfig != "" || ex.ControllerContext != "" {
		namespace := r.Target().Namespace
		boundary, err := ex.AssertBoundary(ctx,
			"system:serviceaccount:"+namespace+":safelane-controller",
			"system:serviceaccount:"+namespace+":safelane-caller")
		if err != nil {
			return startResult{}, err
		}
		r, err = r.WithBoundary(boundary)
		if err != nil {
			return startResult{}, err
		}
	}

	if _, bound := r.Binding(); bound {
		if err := ex.AnnotateRelease(ctx, r.ID); err != nil {
			return startResult{release: r}, err
		}
	}
	var rows []execute.ApplyRow
	var err error
	if retryAbortedSameDigest {
		err = ex.Retry(ctx)
	} else {
		rows, err = ex.Apply(ctx, bundle)
	}
	if err != nil {
		return startResult{release: r}, err
	}
	result := startResult{release: r, applyRows: rows}

	var progressingWeight int
	status, err := ex.WaitForGate(ctx, timeout, func(st execute.Status) {
		if st.State == execute.StateProgressing && progressingWeight == 0 {
			progressingWeight = st.CurrentWeight
		}
	})
	if err != nil {
		return result, err
	}
	result.final = status
	if _, bound := r.Binding(); bound && status.ReleaseID != r.ID {
		return result, release.Invalid("release_identity_mismatch", "safelane.dev/release-id",
			fmt.Sprintf("live Rollout is annotated for %q, expected %q", status.ReleaseID, r.ID),
			"Do not transition this attempt; inspect the Rollout identity and retry explicitly if safe.")
	}
	if status.State != execute.StateAtGate {
		outcome := release.OutcomeFailed
		reasonCode := "rollout_did_not_reach_a_gate"
		if status.State == execute.StateAborted || status.State == execute.StateDegraded {
			outcome = release.OutcomeAborted
			reasonCode = "rollout_" + string(status.State) + "_before_first_gate"
		}
		detail := fmt.Sprintf("Argo state %s at generation %d (observed %d)", status.State, status.Generation, status.ObservedGeneration)
		if status.Message != "" {
			detail += ": " + status.Message
		}
		updated, updateErr := r.WithExecution(release.ExecutionEntry{
			At: now(), Verb: release.VerbStart, RequestedWeight: weights[0],
			Outcome: outcome, ReasonCode: reasonCode, Detail: detail,
		})
		if updateErr != nil {
			return result, updateErr
		}
		result.release = updated
		return result, release.Invalid("rollout_did_not_reach_a_gate", "",
			fmt.Sprintf("the rollout reached state %q instead of pausing at its first gate", status.State),
			"Read `safelane release status <id>` and `safelane release proof <id> --details`. This release did not start cleanly.")
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
		applyRows:         result.applyRows,
		progressingWeight: progressingWeight,
		grantedAt:         grantedAt,
		final:             status,
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
	if a, ok := s.release.RecordedAssessment(); ok {
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
