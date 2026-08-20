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

// StatusCommand builds the top-level, read-only `safelane status` command.
// A named release is read live from Argo. The no-argument recovery listing is
// deliberately record-only so it remains cheap even with many open releases.
func StatusCommand(root, defaultStoreDir string) Command {
	return Command{
		Name:    "status",
		Summary: "show one live rollout or list open releases",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runStatus(ctx, args, stdout, stderr, root, defaultStoreDir, time.Now)
		},
	}
}

type statusFlags struct {
	jsonOut     bool
	projectFile string
	storeDir    string
}

func parseStatusFlags(args []string, stderr io.Writer, defaultStoreDir string) (statusFlags, string, error) {
	var f statusFlags
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&f.jsonOut, "json", false, "print the status as JSON for an agent to branch on")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")

	// Appendix A spells the machine form as `status <id> --json`. The standard
	// flag package stops at the first positional argument, so peel that one off
	// before parsing while still accepting the conventional flags-first form.
	idArg := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idArg, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return f, "", err
	}
	positional := fs.Args()
	if idArg == "" && len(positional) > 0 {
		idArg, positional = positional[0], positional[1:]
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "safelane status: at most one release id is allowed")
		return f, "", flag.ErrHelp
	}
	if idArg == "" && f.jsonOut {
		fmt.Fprintln(stderr, "safelane status: --json requires a release id")
		return f, "", flag.ErrHelp
	}
	return f, idArg, nil
}

func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string, now func() time.Time) int {
	f, idArg, err := parseStatusFlags(args, stderr, defaultStoreDir)
	if err != nil {
		return ExitUsage
	}
	paths, err := resolveRuntime(root, f.projectFile, "", f.storeDir)
	if err != nil {
		fmt.Fprintf(stderr, "safelane status: %v\n", err)
		return ExitFail
	}
	st := &store.FileStore{Dir: paths.storeDir}

	if idArg == "" {
		releases, err := st.List()
		if err != nil {
			fmt.Fprintf(stderr, "safelane status: %v\n", err)
			return ExitFail
		}
		fmt.Fprint(stdout, renderOpenStatuses(releases, now()))
		return ExitOK
	}

	id, err := release.ParseReleaseID(idArg)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	r, err := st.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			printRolloutRejection(stderr, release.Invalid("release_not_found", "release_id",
				fmt.Sprintf("no Release record for %s", id),
				"Use the release id `safelane release` returned."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane status: %v\n", err)
		return ExitFail
	}
	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		fmt.Fprintf(stderr, "safelane status: %v\n", err)
		return ExitFail
	}
	ex := newExecutor(execute.Config{Namespace: cfg.Target.Namespace, Rollout: cfg.Target.Rollout})
	live, err := ex.GetStatus(ctx)
	if err != nil {
		printRolloutRejection(stderr, err)
		return ExitFail
	}
	report := buildStatusReport(r, live)
	if f.jsonOut {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "safelane status: could not encode the result: %v\n", err)
			return ExitFail
		}
	} else {
		fmt.Fprint(stdout, report.Render())
	}
	return ExitOK
}

type statusReport struct {
	ReleaseID         release.ReleaseID `json:"release_id"`
	State             execute.State     `json:"state"`
	Lane              string            `json:"lane"`
	Risk              string            `json:"risk"`
	Weight            int               `json:"weight"`
	NextAllowedWeight *int              `json:"next_allowed_weight"`
	Gate              int               `json:"gate"`
	GateCount         int               `json:"gate_count"`
}

func buildStatusReport(r *release.Release, live execute.Status) statusReport {
	report := statusReport{ReleaseID: r.ID, State: live.State, Weight: live.CurrentWeight, Gate: live.Gate}
	if a, ok := r.RecordedAssessment(); ok {
		report.Lane, report.Risk = a.Lane, fmt.Sprint(a.Risk)
	}
	if env, ok := r.Eligibility().Envelope(); ok {
		stages := env.Stages()
		if len(stages) > 0 {
			report.GateCount = len(stages) - 1
		}
		for _, weight := range stages {
			if weight > live.CurrentWeight {
				next := weight
				report.NextAllowedWeight = &next
				break
			}
		}
	}
	return report
}

func (r statusReport) Render() string {
	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Reading rollout…")
	fmt.Fprintf(&b, "Argo Rollouts: %s\n", statusPhrase(r.State))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "release       %s\n", r.ReleaseID)
	fmt.Fprintf(&b, "lane          %s\n", r.Lane)
	fmt.Fprintf(&b, "risk          %s\n", r.Risk)
	fmt.Fprintf(&b, "state         %s\n", r.State)
	fmt.Fprintf(&b, "weight        %d%%\n", r.Weight)
	if r.NextAllowedWeight == nil {
		fmt.Fprintln(&b, "next          —")
	} else {
		fmt.Fprintf(&b, "next          %d%%\n", *r.NextAllowedWeight)
	}
	fmt.Fprintf(&b, "gate          %d of %d\n", r.Gate, r.GateCount)
	return b.String()
}

func statusPhrase(state execute.State) string {
	switch state {
	case execute.StateNotStarted:
		return "not started"
	case execute.StateProgressing:
		return "progressing"
	case execute.StateAnalysing:
		return "analysing"
	case execute.StateAtGate:
		return "waiting at gate"
	case execute.StateComplete:
		return "complete"
	case execute.StateDegraded:
		return "degraded"
	case execute.StateAborted:
		return "aborted"
	default:
		return string(state)
	}
}

type openStatus struct {
	releaseID release.ReleaseID
	target    string
	lane      string
	state     execute.State
	weight    int
	stalledAt time.Time
}

func storedOpenStatus(r *release.Release) (openStatus, bool) {
	if r.Eligibility().Status() != release.EligibilityEligible {
		return openStatus{}, false
	}
	target := r.Target()
	view := openStatus{
		releaseID: r.ID,
		target:    target.Application + "/" + target.Environment,
		state:     execute.StateNotStarted,
		stalledAt: r.CreatedAt,
	}
	if a, ok := r.RecordedAssessment(); ok {
		view.lane = a.Lane
	}
	history := r.Execution()
	if len(history) == 0 {
		return view, true
	}
	last := history[len(history)-1]
	view.stalledAt = last.At
	if last.Outcome == release.OutcomeAborted || last.Verb == release.VerbAbort || last.Verb == release.VerbArgoAbort {
		return openStatus{}, false
	}
	for _, entry := range history {
		if entry.Outcome == release.OutcomeGranted && entry.RequestedWeight > view.weight {
			view.weight = entry.RequestedWeight
		}
	}
	if env, ok := r.Eligibility().Envelope(); ok {
		stages := env.Stages()
		if len(stages) > 0 && view.weight >= stages[len(stages)-1] {
			return openStatus{}, false
		}
	}
	if view.weight > 0 {
		view.state = execute.StateAtGate
	}
	return view, true
}

func renderOpenStatuses(releases []*release.Release, now time.Time) string {
	var rows []openStatus
	for _, r := range releases {
		if row, ok := storedOpenStatus(r); ok {
			rows = append(rows, row)
		}
	}
	var b strings.Builder
	noun := "releases"
	if len(rows) == 1 {
		noun = "release"
	}
	fmt.Fprintf(&b, "%d open %s\n\n", len(rows), noun)
	for _, row := range rows {
		fmt.Fprintf(&b, "  %s  %-18s  %-8s  %s  weight %-3d stalled %s\n",
			row.releaseID, row.target, row.lane, row.state, row.weight, formatStalled(now.Sub(row.stalledAt)))
	}
	return b.String()
}

func formatStalled(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalMinutes := int64(d / time.Minute)
	hours, minutes := totalMinutes/60, totalMinutes%60
	if hours == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}
