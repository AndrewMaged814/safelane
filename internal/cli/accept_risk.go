package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/spf13/pflag"
)

func ReleaseAcceptRiskCommand(root, defaultStoreDir string) Command {
	return Command{Name: "accept-risk", Summary: "accept one uncovered hazard", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runAcceptRisk(ctx, args, os.Stdin, stdout, stderr, root, defaultStoreDir)
	}}
}

func runAcceptRisk(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	fs := pflag.NewFlagSet("release accept-risk", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	hazardID := fs.String("hazard", "", "exact uncovered hazard ID")
	reason := fs.String("reason", "", "durable reason for accepting this hazard")
	yes := fs.Bool("yes", false, "confirm this hazard-specific decision")
	jsonOut := fs.Bool("json", false, "print only the final JSON result")
	projectFile := fs.String("project", "", "operator project file")
	storeDir := fs.String("store-dir", defaultStoreDir, "release record directory")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 || *hazardID == "" || strings.TrimSpace(*reason) == "" {
		fmt.Fprintln(stderr, "safelane release accept-risk: release id, --hazard, and --reason are required")
		return ExitUsage
	}
	id, err := release.ParseReleaseID(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "safelane release accept-risk: %v\n", err)
		return ExitUsage
	}
	paths, err := resolveRuntime(root, *projectFile, "", *storeDir)
	if err != nil {
		return writeResultError(stderr, "release accept-risk", err)
	}
	st := &store.FileStore{Dir: paths.storeDir}
	r, err := st.Load(id)
	if err != nil {
		return writeResultError(stderr, "release accept-risk", err)
	}
	hazard, ok := findHazard(r, *hazardID)
	if !ok {
		return writeResultError(stderr, "release accept-risk", fmt.Errorf("uncovered hazard %q was not found", *hazardID))
	}
	if hazard.Covered {
		return writeResultError(stderr, "release accept-risk", fmt.Errorf("hazard %q is already covered; acceptance would falsify proof", *hazardID))
	}
	if hazard.Severity == assess.RiskHigh || hazard.Reversibility == "hard" || hazard.Reversibility == "unknown" {
		return writeResultError(stderr, "release accept-risk", fmt.Errorf("policy forbids accepting %s or %s-reversibility hazard %q", hazard.Severity, hazard.Reversibility, hazard.ID))
	}
	fmt.Fprintf(stderr, "Hazard %s (%s): %s\nAffected surface: %s\nRequired assertion: %s (not configured)\n", hazard.ID, hazard.Severity, hazard.FailureMode, hazard.AffectedSurface, hazard.RequiredAssertion)
	if !*yes {
		fmt.Fprintf(stderr, "Type %s to accept this exact hazard: ", hazard.ID)
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.TrimSpace(line) != hazard.ID {
			return ExitDecision
		}
	}
	updated, err := r.WithExecution(release.ExecutionEntry{At: time.Now().UTC(), Verb: release.VerbAcceptRisk, Outcome: release.OutcomeGranted, HazardID: hazard.ID, Detail: strings.TrimSpace(*reason)})
	if err != nil {
		return writeResultError(stderr, "release accept-risk", err)
	}
	if err := st.Update(updated); err != nil {
		return writeResultError(stderr, "release accept-risk", err)
	}
	next := fmt.Sprintf("safelane release run %s --yes", id)
	if *jsonOut {
		return encodeControlResult(stdout, stderr, "accept-risk", updated, next)
	}
	fmt.Fprintf(stdout, "Accepted hazard %s without marking it covered.\nNext: %s\n", hazard.ID, next)
	return ExitOK
}

func findHazard(r *release.Release, id string) (assess.Hazard, bool) {
	a, ok := r.RecordedAssessment()
	if !ok {
		return assess.Hazard{}, false
	}
	for _, hazard := range a.Model.Hazards {
		if hazard.ID == id {
			return hazard, true
		}
	}
	return assess.Hazard{}, false
}

func hazardAccepted(r *release.Release, id string) bool {
	for _, entry := range r.Execution() {
		if entry.Verb == release.VerbAcceptRisk && entry.HazardID == id && entry.Outcome == release.OutcomeGranted {
			return true
		}
	}
	return false
}

func effectiveAuthority(r *release.Release) (int, *assess.Hazard) {
	a, ok := r.RecordedAssessment()
	if !ok {
		return 0, nil
	}
	authority := 100
	var limiting *assess.Hazard
	for i := range a.Model.Hazards {
		hazard := a.Model.Hazards[i]
		if hazard.Covered || hazardAccepted(r, hazard.ID) {
			continue
		}
		limit := 100
		switch hazard.Severity {
		case assess.RiskHigh:
			limit = 0
		case assess.RiskMedium:
			limit = 25
		}
		if limit < authority {
			authority, limiting = limit, &hazard
		}
	}
	return authority, limiting
}
