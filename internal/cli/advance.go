package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

// defaultAdvanceTimeout bounds how long `rollout advance` waits for the
// next gate before giving up with exit 3 (N12). It is deliberately
// shorter than start's five minutes: by the time a release is advancing,
// the operator is watching it live, and a lane's own per-gate analysis
// window (Appendix C3's AnalysisTemplate: three 30s intervals) is well
// under this.
const defaultAdvanceTimeout = 180 * time.Second

// analysisDetailIndent is the column Appendix A2.3's metric detail line
// starts at -- aligned under the arrow on the line above it, not under
// the label.
const analysisDetailIndent = 17

func runRolloutAdvance(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	f, idArg, err := parseRolloutAdvanceFlags(args, stderr, defaultStoreDir)
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
				"Use the release id `safelane release` returned. rollout advance cannot invent a record."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane release run: %v\n", err)
		return ExitFail
	}

	if err := refuseIfNotEligible(r); err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
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

	result, err := advanceRollout(ctx, r, ex, cfg.Application, f.to, f.timeout, time.Now)
	if errors.Is(err, execute.ErrGateTimeout) {
		fmt.Fprint(stdout, result.RenderTimeout())
		return ExitTimeout
	}
	if err != nil {
		// A refusal (and any catch-up grant it followed) still belongs in
		// the record even though the call itself failed: [advanceRollout]
		// already appended it to result.release when there was one.
		if result.release != nil {
			if serr := st.Update(result.release); serr != nil {
				fmt.Fprintf(stderr, "safelane release run: %v could not be recorded: %v\n", err, serr)
				return ExitFail
			}
		}
		printRolloutRejection(stderr, err)
		return ExitFail
	}

	if err := st.Update(result.release); err != nil {
		fmt.Fprintf(stderr, "safelane release run: the outcome was decided but could not be persisted: %v\n", err)
		return ExitFail
	}
	fmt.Fprint(stdout, result.Render())
	if result.outcome == outcomeArgoAborted {
		return ExitFail
	}
	return ExitOK
}

type rolloutAdvanceFlags struct {
	storeDir             string
	projectFile          string
	controllerKubeconfig string
	controllerContext    string
	timeout              time.Duration
	to                   *int
}

func parseRolloutAdvanceFlags(args []string, stderr io.Writer, defaultStoreDir string) (rolloutAdvanceFlags, string, error) {
	var f rolloutAdvanceFlags
	var toStr string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	fs := flag.NewFlagSet("rollout advance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.controllerKubeconfig, "controller-kubeconfig", "",
		"kubeconfig for the privileged controller identity (default: project.yml)")
	fs.StringVar(&f.controllerContext, "controller-context", "", "kubeconfig context for the privileged controller identity (default: project.yml)")
	fs.DurationVar(&f.timeout, "timeout", defaultAdvanceTimeout, "how long to wait for the rollout to reach the next gate")
	fs.StringVar(&toStr, "to", "",
		"the weight to request (default: the next weight the lane permits). SafeLane's own SKILL.md forbids an agent from passing this; it exists for an operator to demonstrate the boundary by hand.")
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	if toStr != "" {
		w, err := strconv.Atoi(toStr)
		if err != nil {
			fmt.Fprintf(stderr, "safelane release run: --to must be an integer weight, got %q\n", toStr)
			return f, "", flag.ErrHelp
		}
		f.to = &w
	}
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(stderr, "safelane release run: exactly one release id is required")
		fs.Usage()
		return f, "", flag.ErrHelp
	}
	return f, rest[0], nil
}

// advanceOutcome is what [advanceRollout] actually did.
type advanceOutcome int

const (
	outcomeNoChange advanceOutcome = iota
	outcomePromotedComplete
	outcomePromotedAtGate
	// outcomeArgoAborted is A3.4's payoff: WaitForGate stopped at
	// StateDegraded or StateAborted -- a deliberately failing analysis
	// tripped its failureLimit and Argo Rollouts aborted the rollout on
	// its own. This is not a SafeLane refusal (there is no *release.Error
	// for it); it is a fact to report and record, exit 1 regardless.
	outcomeArgoAborted
)

// advanceResult is the whole `rollout advance` outcome, before any of it
// is a string -- the same startResult/Render() split `rollout start` uses
// (internal/cli/rollout.go). It carries enough to render either a normal
// outcome (Render) or a timeout (RenderTimeout), since [advanceRollout]
// returns a populated result even when it returns [execute.ErrGateTimeout].
type advanceResult struct {
	outcome         advanceOutcome
	release         *release.Release
	weights         []int
	requestedWeight int
	observedWeight  int
	grantedAt       time.Time
	nextAllowed     int
	final           execute.Status
	analysisRun     execute.AnalysisRun
	friendlyName    string
	timeout         time.Duration
}

// advanceRollout runs Appendix C7's whole decision tree against the
// release's current, live state and -- when the decision is to promote --
// calls kubectl argo rollouts promote and waits for the next gate, the
// same blocking-wait contract [startRollout] uses. now is injected so
// tests get deterministic timestamps.
//
// On a timeout, the returned error is [execute.ErrGateTimeout] and the
// returned result is still populated enough for [advanceResult.RenderTimeout].
func advanceRollout(ctx context.Context, r *release.Release, ex *execute.Executor, application string, toFlag *int, timeout time.Duration, now func() time.Time) (advanceResult, error) {
	history := r.Execution()
	envelope, ok := r.Eligibility().Envelope()
	if !ok {
		return advanceResult{}, release.Internal("release_without_envelope",
			"an eligible release must carry a rollout envelope to advance")
	}
	weights := envelope.Stages()
	bundle, ok := r.Bundle()
	if !ok {
		return advanceResult{}, release.Internal("release_without_bundle",
			"an eligible release must carry a rendered bundle to advance")
	}
	assessment, _ := r.RecordedAssessment()

	// "no execution entries recorded" (Appendix C5's state table) is read
	// straight off the release's own record: a release that never started
	// is refused before any kubectl call is even considered, the same as
	// N10's ineligible-release refusal in `rollout start`.
	if _, _, hasGrant := highestGranted(history); !hasGrant {
		return advanceResult{}, release.Invalid("rollout_not_started", "",
			"this release has no execution record",
			"safelane release run <id>")
	}

	observed, err := ex.GetStatus(ctx)
	if err != nil {
		return advanceResult{}, err
	}

	plan, rerr := decideAdvance(advanceParams{
		Weights:  weights,
		Lane:     assessment.Lane,
		Digest:   shortDigest(bundle.Template().ContentDigest),
		ToFlag:   toFlag,
		Observed: observed,
		History:  history,
		Now:      now(),
	})

	// A catch-up grant belongs in the record regardless of what this call
	// goes on to decide with it -- a refusal that follows, an error that
	// follows, or an ordinary promotion. Apply it once, up front, and
	// thread the result through every return path below.
	updated := r
	if plan.catchUp != nil {
		updated, err = updated.WithExecution(*plan.catchUp)
		if err != nil {
			return advanceResult{}, err
		}
	}

	if rerr != nil {
		result := advanceResult{release: updated}
		// requestedWeight is 0 only for the two refusals decideAdvance
		// returns before it ever computes one (rollout_not_started,
		// rollout_closed) -- there is no transition to record against
		// those. Every other refusal names the weight it turned down,
		// and Appendix C2's own record shows a refusal belongs in the
		// log: A3.5's proof reads it back.
		if plan.requestedWeight != 0 {
			updated, err = updated.WithExecution(release.ExecutionEntry{
				At: now(), Verb: release.VerbAdvance, RequestedWeight: plan.requestedWeight,
				Outcome: release.OutcomeRefused, ReasonCode: rerr.Code,
			})
			if err != nil {
				return advanceResult{}, err
			}
			result.release = updated
		}
		return result, rerr
	}

	result := advanceResult{release: updated, weights: weights, requestedWeight: plan.requestedWeight,
		observedWeight: plan.observedWeight, grantedAt: plan.grantedAt, nextAllowed: plan.nextAllowed, timeout: timeout}

	if plan.kind == planNoChange {
		result.outcome = outcomeNoChange
		return result, nil
	}

	final := observed
	shouldPromote := true
	var waitErr error
	if plan.requestedWeight == weights[len(weights)-1] && observed.AnalysisRunPhase == "Running" {
		final, err = ex.WaitForBackgroundAnalysis(ctx, timeout)
		result.final = final
		if errors.Is(err, execute.ErrGateTimeout) {
			return result, err
		}
		if err != nil {
			return result, err
		}
		switch final.State {
		case execute.StateAborted, execute.StateDegraded:
			shouldPromote = false
		case execute.StateAtGate:
		default:
			return result, release.Invalid("rollout_did_not_reach_a_gate", "",
				fmt.Sprintf("the rollout reached state %q while waiting for final analysis", final.State),
				"Read `safelane release status` for detail.")
		}
	}

	if shouldPromote {
		if err := ex.Promote(ctx); err != nil {
			return result, err
		}

		final, waitErr = ex.WaitForGate(ctx, timeout, nil)
		result.final = final
		if errors.Is(waitErr, execute.ErrGateTimeout) {
			return result, waitErr
		}
		if waitErr != nil {
			return result, waitErr
		}
	}
	if final.State == execute.StateAtGate && final.AnalysisRunPhase == "Running" {
		final, waitErr = ex.WaitForBackgroundAnalysis(ctx, timeout)
		result.final = final
		if errors.Is(waitErr, execute.ErrGateTimeout) {
			return result, waitErr
		}
		if waitErr != nil {
			return result, waitErr
		}
	}

	// A3.4's payoff: a deliberately failing analysis tripped its own
	// failureLimit and Argo Rollouts aborted the rollout on its own. This
	// is not a SafeLane refusal -- there is no *release.Error for it --
	// but the record must still show it happened and why.
	if final.State == execute.StateDegraded || final.State == execute.StateAborted {
		entry := release.ExecutionEntry{At: now(), Verb: release.VerbArgoAbort, Outcome: release.OutcomeAborted, ReasonCode: "analysis_failed"}
		if realName := backgroundAnalysisRunName(ex.Rollout, final); realName != "" {
			run, err := ex.GetAnalysisRun(ctx, realName)
			if err != nil {
				return result, err
			}
			result.analysisRun = run
			result.friendlyName = analysisDisplayName(application, realName)
			entry.Analysis = fmt.Sprintf("%s %s", result.friendlyName, run.Phase)
			entry.Detail = fmt.Sprintf("%s measured %.2f, condition %s", run.Metric.Name, run.Metric.Measured, run.Metric.Condition)
		}
		updated, err = updated.WithExecution(entry)
		if err != nil {
			return result, err
		}
		result.release = updated
		result.outcome = outcomeArgoAborted
		return result, nil
	}

	if final.State != execute.StateAtGate && final.State != execute.StateComplete {
		return result, release.Invalid("rollout_did_not_reach_a_gate", "",
			fmt.Sprintf("the rollout reached state %q instead of a gate or completion", final.State),
			"Read `safelane release status` for detail.")
	}

	grantedAt := now()
	entry := release.ExecutionEntry{
		At:              grantedAt,
		Verb:            release.VerbAdvance,
		RequestedWeight: plan.requestedWeight,
		Outcome:         release.OutcomeGranted,
	}

	if final.State == execute.StateComplete {
		if realName := backgroundAnalysisRunName(ex.Rollout, final); realName != "" {
			run, err := ex.GetAnalysisRun(ctx, realName)
			if err != nil {
				// AnalysisRun is supplemental proof, not the commit point for
				// an already-applied promotion. Argo can garbage-collect a
				// completed run (or clear its transient status reference) before
				// this read. Preserve the truthful Healthy/completed result when
				// the only failure is that optional object no longer exists; keep
				// surfacing RBAC/cluster failures because those are not safe to
				// silently ignore.
				if !analysisRunNotFound(err) {
					return result, err
				}
			} else {
				result.analysisRun = run
				result.friendlyName = analysisDisplayName(application, realName)
				entry.Analysis = fmt.Sprintf("%s %s", result.friendlyName, run.Phase)
			}
		}
	}

	updated, err = updated.WithExecution(entry)
	if err != nil {
		return result, err
	}
	result.release = updated
	result.grantedAt = grantedAt
	if final.State == execute.StateComplete {
		result.outcome = outcomePromotedComplete
	} else {
		result.outcome = outcomePromotedAtGate
	}
	return result, nil
}

// planKind is what [decideAdvance] decided to do: the promotion itself, or
// nothing because the requested weight was already granted.
type planKind int

const (
	planPromote planKind = iota
	planNoChange
)

// advancePlan is [decideAdvance]'s answer -- what to do, and everything a
// caller needs to render either outcome without re-deriving it.
type advancePlan struct {
	kind            planKind
	requestedWeight int
	observedWeight  int
	nextAllowed     int
	// catchUp is non-nil when Argo's observed weight had already moved
	// past what SafeLane last recorded as granted (Appendix C7's first
	// branch) -- a granted entry for that weight belongs in the record
	// before this call's own outcome is appended.
	catchUp *release.ExecutionEntry
	// grantedAt is when the weight this plan compares against was
	// granted -- the existing grant for planNoChange, the pre-advance
	// grant for planPromote.
	grantedAt time.Time
}

// advanceParams is everything [decideAdvance] needs: the operator's static
// envelope, the caller's own request, and what was actually observed.
type advanceParams struct {
	Weights  []int
	Lane     string
	Digest   string
	ToFlag   *int
	Observed execute.Status
	History  []release.ExecutionEntry
	Now      time.Time
}

// decideAdvance is Appendix C7's whole idempotent-advance decision tree:
// catch-up, no-change, exceeds-envelope, backwards, not-at-gate, promote.
// It runs on every call, not only a repeat one -- there is no other path
// into a promotion. Ticket 10 exercises the exceeds-envelope, not-at-gate
// and backwards-refusal branches (N11, A3.3); the no-change and catch-up
// branches are ticket 11's idempotency contract (N12), built here because
// this is where the algorithm lives, not golden-tested here.
func decideAdvance(p advanceParams) (advancePlan, *release.Error) {
	granted, grantedAt, hasGrant := highestGranted(p.History)
	if !hasGrant {
		return advancePlan{}, release.Invalid("rollout_not_started", "",
			"this release has no execution record",
			"safelane release run <id>")
	}

	observed := p.Observed.CurrentWeight

	// If Argo is already ahead of the last grant this release's own
	// record knows about, that gap is caught up here, before anything
	// else is decided -- Appendix C7's first branch. It is recorded
	// regardless of what the rest of this call goes on to do, including a
	// refusal: the gap is real whether or not this specific request is.
	var catchUp *release.ExecutionEntry
	if observed > granted {
		entry := release.ExecutionEntry{At: p.Now, Verb: release.VerbAdvance, RequestedWeight: observed, Outcome: release.OutcomeGranted}
		catchUp = &entry
		granted, grantedAt = observed, p.Now
	}

	// nextAllowed is read off the *true* current position -- the
	// caught-up one, when a catch-up just happened -- since that is what
	// any further request, explicit or default, must be judged against.
	nextAllowed, hasNext := nextAllowedAfter(p.Weights, granted)

	var requested int
	switch {
	case p.ToFlag != nil:
		requested = *p.ToFlag
	case catchUp != nil:
		// No explicit target, and Argo already moved past what this
		// release's record had last granted: an agent that did not see
		// the response to whichever call got it there is, from its own
		// point of view, still only trying to reach *that* weight. It
		// has already arrived. This call's job is to notice and stop --
		// not to also push one step further in the same breath. A
		// further step needs its own call.
		requested = observed
	case hasNext:
		requested = nextAllowed
	default:
		return advancePlan{catchUp: catchUp}, release.Invalid("rollout_closed", "",
			"this release has already reached its final weight",
			"nothing further may advance")
	}

	if observed == requested {
		return advancePlan{kind: planNoChange, requestedWeight: requested, observedWeight: observed,
			nextAllowed: nextAllowed, grantedAt: grantedAt, catchUp: catchUp}, nil
	}
	if requested < observed {
		return advancePlan{requestedWeight: requested, observedWeight: observed, catchUp: catchUp},
			release.Invalid("transition_not_permitted", "to",
				fmt.Sprintf("weight %d is behind the current weight %d\n      the envelope moves forward only; use abort to withdraw",
					requested, observed),
				"")
	}
	if !hasNext || requested > nextAllowed {
		return advancePlan{requestedWeight: requested, observedWeight: observed, catchUp: catchUp},
			release.Invalid("transition_exceeds_envelope", "to",
				fmt.Sprintf("you requested weight %d; the envelope permits %d next\n"+
					"      current weight %d, granted %s\n"+
					"      envelope %s (lane %q,\n"+
					"               digest %s)",
					requested, nextAllowed,
					observed, grantedAt.UTC().Format("15:04:05")+"Z",
					weightLadder(p.Weights), p.Lane, p.Digest),
				fmt.Sprintf("request %d, or run advance with no --to flag", nextAllowed))
	}
	if p.Observed.State != execute.StateAtGate {
		return advancePlan{requestedWeight: requested, observedWeight: observed, catchUp: catchUp},
			release.Invalid("rollout_not_at_gate", "",
				fmt.Sprintf("the Rollout is %s toward weight %d; it is not at a gate", argoStateWord(p.Observed.State), requested),
				"wait for the gate, then retry")
	}

	return advancePlan{kind: planPromote, requestedWeight: requested, observedWeight: observed,
		nextAllowed: nextAllowed, catchUp: catchUp, grantedAt: grantedAt}, nil
}

// highestGranted returns the highest requested_weight among the release's
// granted execution entries -- Appendix C7's `granted`. ok is false only
// for a release with no execution history at all.
func highestGranted(history []release.ExecutionEntry) (weight int, at time.Time, ok bool) {
	for _, e := range history {
		if e.Outcome != release.OutcomeGranted {
			continue
		}
		if !ok || e.RequestedWeight > weight {
			weight, at, ok = e.RequestedWeight, e.At, true
		}
	}
	return weight, at, ok
}

// nextAllowedAfter is the envelope weight immediately after weight. ok is
// false when weight is the envelope's last one: there is nothing further
// to advance to.
func nextAllowedAfter(weights []int, weight int) (next int, ok bool) {
	for i, w := range weights {
		if w == weight && i+1 < len(weights) {
			return weights[i+1], true
		}
	}
	return 0, false
}

// argoStateWord is the capitalised word Argo's own phase reads as, for the
// two in-flight states a caller might be told to wait out.
func argoStateWord(s execute.State) string {
	switch s {
	case execute.StateProgressing:
		return "Progressing"
	case execute.StateAnalysing:
		return "Analysing"
	default:
		return string(s)
	}
}

// gateNumberForWeight is the 1-based gate a weight corresponds to
// (Appendix C5: gates are indices 0..len-2 of the envelope; the final
// weight is completion, not a gate of its own).
func gateNumberForWeight(weights []int, weight int) int {
	for i, w := range weights {
		if w == weight {
			return i + 1
		}
	}
	return 0
}

// backgroundAnalysisRunName returns only the real AnalysisRun name Argo
// reported. Pod hash and revision are insufficient evidence: a Rollout can
// have no analysis configured, and Argo can clear the transient status after
// a run settles. Guessing a name from those fields caused harmless 404s to be
// treated as a failed final promotion.
func backgroundAnalysisRunName(rollout string, st execute.Status) string {
	if st.AnalysisRunName != "" {
		return st.AnalysisRunName
	}
	// Do not manufacture an AnalysisRun name from pod metadata alone. A
	// healthy Rollout may legitimately have no analysis configured, and Argo
	// may clear both the name and phase after a run settles. Without an
	// explicit status name there is no evidence that this object ever existed.
	return ""
}

func analysisRunNotFound(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "analysisrun") && strings.Contains(message, "not found")
}

// analysisDisplayName is the friendly `<application>-success-rate-<N>`
// label Appendix A2.3 prints, built from data SafeLane already has (the
// AnalysisTemplate naming convention, Appendix C3) plus the ordinal Argo
// put on the real AnalysisRun's own name -- not the real name itself,
// which is an implementation detail (`<rollout>-<podHash>-<revision>`) no
// caller should have to read.
func analysisDisplayName(application, realName string) string {
	idx := strings.LastIndex(realName, "-")
	ordinal := realName
	if idx >= 0 {
		ordinal = realName[idx+1:]
	}
	return application + "-success-rate-" + ordinal
}

// Render writes the advance report: A2.3 when the promotion ran the
// release to completion, the non-terminal at-gate shape otherwise
// (Appendix A has no literal golden for that case -- A2.3 happens to run
// to completion in one call -- so it follows start's own established
// shape, Appendix A2.2/A3.2, rather than inventing a new one), or the
// no-change report (N12's idempotency half, ticket 11's to golden-test).
func (r advanceResult) Render() string {
	switch r.outcome {
	case outcomeNoChange:
		return r.renderNoChange()
	case outcomePromotedComplete:
		return r.renderComplete()
	case outcomeArgoAborted:
		return r.renderArgoAbort()
	default:
		return r.renderAtGate()
	}
}

func (r advanceResult) renderNoChange() string {
	var b strings.Builder
	fmt.Fprintln(&b, "safelane release run: no change.")
	fmt.Fprintf(&b, "  weight %d was already granted at %s\n", r.observedWeight, r.grantedAt.UTC().Format("15:04:05")+"Z")
	fmt.Fprintf(&b, "  current weight %d, next allowed %d\n", r.observedWeight, r.nextAllowed)
	return b.String()
}

// renderComplete is A2.3: the promotion reached the envelope's final
// weight and the Rollout is Healthy. It shows the AnalysisRun's own
// measurement detail rather than a bare "Progressing" line -- that detail
// is the evidence the demo's whole argument rests on ("SafeLane decided
// how wide each step is allowed to be, and that's all").
func (r advanceResult) renderComplete() string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Promoting %d → %d…\n", r.observedWeight, r.requestedWeight)
	if r.friendlyName != "" {
		fmt.Fprintf(&b, "Argo Rollouts: AnalysisRun %s → %s (%d/%d measurements)\n",
			r.friendlyName, r.analysisRun.Phase, r.analysisRun.Metric.Successful, r.analysisRun.Metric.Count)
		fmt.Fprintf(&b, "%s%s  measured %.2f, condition %s\n",
			strings.Repeat(" ", analysisDetailIndent), r.analysisRun.Metric.Name, r.analysisRun.Metric.Measured, r.analysisRun.Metric.Condition)
	}
	fmt.Fprintln(&b, "Argo Rollouts: Healthy. The canary is now the stable version.")
	fmt.Fprintln(&b)

	lane := ""
	if a, ok := r.release.RecordedAssessment(); ok {
		lane = a.Lane
	}
	gates := gateCount(r.weights)
	granted, refused := countOutcomes(r.release.Execution())
	fmt.Fprintf(&b, "Release complete.  lane %s, %d %s, %d granted %s, %d %s.\n",
		lane, gates, plural(gates, "gate", "gates"),
		granted, plural(granted, "transition", "transitions"),
		refused, plural(refused, "refusal", "refusals"))
	fmt.Fprintf(&b, "Release Proof: safelane release proof %s\n", r.release.ID)
	return b.String()
}

// renderAtGate is the non-terminal advance render: the promotion reached
// a gate short of completion.
func (r advanceResult) renderAtGate() string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Promoting %d → %d…\n", r.observedWeight, r.requestedWeight)
	gates := gateCount(r.weights)
	gate := gateNumberForWeight(r.weights, r.requestedWeight)
	fmt.Fprintf(&b, "Argo Rollouts: Progressing → weight %d\n", r.requestedWeight)
	fmt.Fprintf(&b, "Argo Rollouts: Paused at gate %d of %d\n", gate, gates)
	fmt.Fprintln(&b)

	grantedIndex := 0
	for i, w := range r.weights {
		if w == r.requestedWeight {
			grantedIndex = i
		}
	}
	renderWeightLadder(&b, r.weights, grantedIndex, r.grantedAt.UTC().Format("15:04:05")+"Z")
	fmt.Fprintln(&b)

	lane := ""
	if a, ok := r.release.RecordedAssessment(); ok {
		lane = a.Lane
	}
	fmt.Fprintf(&b, "lane          %s\n", lane)
	if next, ok := nextAllowedAfter(r.weights, r.requestedWeight); ok {
		fmt.Fprintf(&b, "next action   advance (%d)\n", next)
	} else {
		fmt.Fprintln(&b, "next action   none. This release is closed.")
	}
	return b.String()
}

// renderArgoAbort is A3.4's payoff, copied verbatim from Appendix A: the
// promotion that reached requestedWeight tripped a deliberately failing
// analysis and Argo Rollouts aborted the rollout on its own, not SafeLane.
//
// The stable digest printed here is this release's own verified, pinned
// artifact digest -- the only digest SafeLane's own record actually
// carries. Reading back the *previous* ReplicaSet's own image would need
// a kubectl call Appendix C5 never lists (there is no stable-image lookup
// in its exact invocations); this is the closest fact SafeLane can
// honestly stand behind rather than a literal claim about which exact
// image traffic reverted to.
func (r advanceResult) renderArgoAbort() string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Promoting %d → %d…\n", r.observedWeight, r.requestedWeight)
	fmt.Fprintf(&b, "Argo Rollouts: Progressing → weight %d\n", r.requestedWeight)
	if r.friendlyName != "" {
		fmt.Fprintf(&b, "Argo Rollouts: AnalysisRun %s → %s\n", r.friendlyName, r.analysisRun.Phase)
		fmt.Fprintf(&b, "%s%s  measured %.2f, condition %s\n",
			strings.Repeat(" ", analysisDetailIndent), r.analysisRun.Metric.Name, r.analysisRun.Metric.Measured, r.analysisRun.Metric.Condition)
		fmt.Fprintf(&b, "%s(%d of %d measurements below threshold, failureLimit %d)\n",
			strings.Repeat(" ", analysisDetailIndent),
			r.analysisRun.Metric.Count-r.analysisRun.Metric.Successful, r.analysisRun.Metric.Count, r.analysisRun.Metric.FailureLimit)
	}
	fmt.Fprintln(&b, "Argo Rollouts: Degraded → automatic abort → weight 0")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The rollout was aborted by Argo Rollouts, not by SafeLane.")
	digest := ""
	if bundle, ok := r.release.Bundle(); ok {
		digest = shortDigest(bundle.PinnedDigest())
	}
	fmt.Fprintf(&b, "Stable traffic is restored to %s.\n", digest)
	fmt.Fprintln(&b)

	lane := ""
	if a, ok := r.release.RecordedAssessment(); ok {
		lane = a.Lane
	}
	last := 0
	if len(r.weights) > 0 {
		last = r.weights[len(r.weights)-1]
	}
	fmt.Fprintf(&b, "lane          %s\n", lane)
	fmt.Fprintf(&b, "reached       %d of %d\n", r.requestedWeight, last)
	fmt.Fprintln(&b, "next action   none. This release is closed.")
	return b.String()
}

// RenderTimeout is N12's timeout block, copied verbatim: the promotion
// was sent, Argo's own last observed transition, then the deadline.
// Unlike Render's success shapes this carries no leading blank line --
// N12 shows none, and this is the literal contract.
func (r advanceResult) RenderTimeout() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Promoting %d → %d…\n", r.observedWeight, r.requestedWeight)
	fmt.Fprintf(&b, "Argo Rollouts: %s → weight %d\n", argoStateWord(r.final.State), r.requestedWeight)
	fmt.Fprintf(&b, "timeout after %s waiting for gate %d\n", r.timeout, gateNumberForWeight(r.weights, r.requestedWeight))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "The promotion was sent. The outcome is unknown.")
	fmt.Fprintf(&b, "Run: safelane release status %s\n", r.release.ID)
	fmt.Fprintln(&b, "Reconnect with safelane release run; it will reconcile before acting.")
	return b.String()
}

// countOutcomes tallies granted and refused entries across a release's
// whole execution history, for the "N granted transitions, M refusals"
// summary line.
func countOutcomes(history []release.ExecutionEntry) (granted, refused int) {
	for _, e := range history {
		switch e.Outcome {
		case release.OutcomeGranted:
			granted++
		case release.OutcomeRefused:
			refused++
		}
	}
	return granted, refused
}
