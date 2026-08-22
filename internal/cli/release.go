package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/intake"
	"github.com/AndrewMaged814/safelane/internal/ledger"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
	"github.com/spf13/pflag"
)

// ReleaseCommand builds `safelane release plan --pr <n>` (and `--file`
// for CI). root is the application clone whose GitHub remote selects the
// operator-owned app under SAFELANE_HOME.
func ReleaseCommand(root, defaultStoreDir string) Command {
	return Command{
		Name:    "release",
		Summary: "investigate a change and record what it may do",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			if len(args) > 0 && args[0] == "retry" {
				return runReleaseRetry(ctx, args[1:], stdout, stderr, root, defaultStoreDir)
			}
			// `inspect` is the read-only report an agent runs first and an
			// operator reads on screen. The bare form is the same pass with a
			// terser summary, kept because scripts and the integration test
			// use it.
			if len(args) > 0 && args[0] == "inspect" {
				return runRelease(ctx, args[1:], stdout, stderr, root, defaultStoreDir, true, 0, "")
			}
			return runRelease(ctx, args, stdout, stderr, root, defaultStoreDir, false, 0, "")
		},
	}
}

// ReleasePlanCommand builds the public `safelane release plan` adapter.
func ReleasePlanCommand(root, defaultStoreDir string) Command {
	return Command{Name: "plan", Summary: "compile and persist a Safety Contract", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runPublicReleasePlan(ctx, args, stdout, stderr, root, defaultStoreDir)
	}}
}

// ReleaseRetryCommand creates a fresh, re-verified attempt from a terminal release.
func ReleaseRetryCommand(root, defaultStoreDir string) Command {
	return Command{Name: "retry", Summary: "create a re-verified release attempt", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runReleaseRetry(ctx, args, stdout, stderr, root, defaultStoreDir)
	}}
}

// releaseFlags is the flag set both forms of `safelane release` share.
type releaseFlags struct {
	file        string
	pr          int
	repo        string
	environment string
	image       string
	jsonOut     bool
	templateDir string
	projectFile string
	policyFile  string
	storeDir    string
	githubToken string
}

func parseReleaseFlags(args []string, stderr io.Writer, defaultStoreDir string) (releaseFlags, *flag.FlagSet, error) {
	var f releaseFlags
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.file, "file", "", "path to a slim Release Request JSON file")
	fs.IntVar(&f.pr, "pr", 0, "merged pull request number")
	fs.StringVar(&f.repo, "repo", "", "GitHub owner/name (default: git origin or project.yml)")
	fs.StringVar(&f.environment, "environment", "", "environment selector (default: project.yml)")
	fs.StringVar(&f.image, "image", "", "optional immutable digest pin to verify")
	fs.BoolVar(&f.jsonOut, "json", false, "print the report as JSON for an agent to branch on")
	fs.StringVar(&f.templateDir, "template-dir", "", "override project.yml template_path")
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.policyFile, "policy", "", "path to policy.yml (default: matched app under SAFELANE_HOME)")
	fs.StringVar(&f.storeDir, "store-dir", defaultStoreDir, "directory Release records are persisted under")
	f.githubToken = os.Getenv("GITHUB_TOKEN")
	if err := fs.Parse(args); err != nil {
		return f, fs, err
	}
	switch {
	case f.file != "" && f.pr != 0:
		fmt.Fprintln(stderr, "safelane release: use --pr or --file, not both")
		fs.Usage()
		return f, fs, flag.ErrHelp
	case f.file == "" && f.pr == 0:
		fmt.Fprintln(stderr, "safelane release: --pr or --file is required")
		fs.Usage()
		return f, fs, flag.ErrHelp
	}
	return f, fs, nil
}

func runRelease(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string, report bool, attempt int, retryOf release.ReleaseID) int {
	f, _, err := parseReleaseFlags(args, stderr, defaultStoreDir)
	if err != nil {
		return ExitUsage
	}

	intent, err := loadIntent(f.file, f.pr, f.repo, f.environment, f.image)
	if err != nil {
		if f.file != "" && strings.Contains(err.Error(), "could not read") {
			fmt.Fprintf(stderr, "safelane release: %v\n", err)
			return ExitUsage
		}
		printRejection(stderr, err)
		return ExitFail
	}
	if err := intent.Validate(); err != nil {
		printRejection(stderr, err)
		return ExitFail
	}

	if intent.Repository == "" {
		if detected, detErr := project.DetectGitHubRepo(root); detErr == nil {
			intent.Repository = detected
		}
	}

	paths, err := resolveRuntime(root, f.projectFile, f.policyFile, f.storeDir)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}
	f.storeDir = paths.storeDir
	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}
	pol := policy.Default()
	if paths.policyFile != "" {
		pol, err = policy.Load(paths.policyFile)
		if err != nil {
			printRejection(stderr, err)
			return ExitFail
		}
	}
	fileStore := &store.FileStore{Dir: f.storeDir}
	releaseLedger := ledger.ReleaseLedger{Store: fileStore}
	subject := ledger.Subject{Repository: intent.Repository, PullRequest: intent.PullRequest, Target: cfg.ReleaseTarget(intent.Environment)}
	if retryOf == "" {
		if existing, history, resolveErr := releaseLedger.Resolve(subject); resolveErr != nil {
			printRejection(stderr, resolveErr)
			return ExitFail
		} else if existing != nil {
			observed := orchestrate.Observe(ctx, existing.Request(), orchestrate.Deps{
				GitHub: &github.Client{Token: f.githubToken}, GHCR: &ghcr.Client{}, Project: cfg, Policy: pol,
				Caller: release.CallerIdentity{Identity: "safelane-cli", Kind: release.CallerAgent, Tool: "safelane"},
			})
			observed.Release = existing
			in := buildInspection(observed, cfg, pol, time.Now().UTC())
			if observed.GitHub.Facts == nil {
				in.checks = persistedEvidenceChecks(existing, cfg)
			}
			in.history = history
			// Reconcile every existing attempt, including terminal states. A
			// recorded "promoted" state is not live proof: the process may have
			// exited after Argo applied the final promotion but before SafeLane
			// persisted its last execution entry, or the cluster may have drifted
			// since the record was written.
			reconcileInspection(ctx, &in, cfg, paths, releaseLedger)
			return printInspection(stdout, stderr, in, f.jsonOut, report)
		}
	}

	tmplPath := f.templateDir
	if tmplPath == "" {
		tmplPath = cfg.Release.TemplatePath
		if !filepath.IsAbs(tmplPath) {
			base := paths.configDir
			if cfg.Version == 1 {
				base = root // compatibility for the former repo-relative schema.
			}
			tmplPath = filepath.Join(base, tmplPath)
		}
	}
	tmpl, err := render.LoadDir(tmplPath)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release: could not load the Release Template at %s: %v\n", tmplPath, err)
		return ExitFail
	}

	deps := orchestrate.Deps{
		GitHub:        &github.Client{Token: f.githubToken},
		GHCR:          &ghcr.Client{},
		ChangeFacts:   &assess.Client{Token: f.githubToken},
		Template:      tmpl,
		Store:         releaseLedger,
		Project:       cfg,
		Policy:        pol,
		Caller:        release.CallerIdentity{Identity: "safelane-cli", Kind: release.CallerAgent, Tool: "safelane"},
		AttemptNumber: attempt,
		RetryOf:       retryOf,
	}

	result, err := orchestrate.Submit(ctx, intent, deps)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}

	in := buildInspection(result, cfg, pol, time.Now().UTC())
	in.history, _ = releaseLedger.History(subject)
	return printInspection(stdout, stderr, in, f.jsonOut, report)
}

func reconcileInspection(ctx context.Context, in *inspection, cfg project.Config, paths runtimePaths, l ledger.ReleaseLedger) {
	r := in.release
	controllerKubeconfig, controllerContext := paths.controllerCredentials("", "")
	ex := newExecutor(execute.Config{Namespace: cfg.Target.Namespace, Rollout: cfg.Target.Rollout,
		ControllerKubeconfig: controllerKubeconfig, ControllerContext: controllerContext})
	live, err := ex.GetStatus(ctx)
	if err != nil {
		in.effectiveState, in.stateSource = release.StateUnknown, "live_reconciliation_failed"
		return
	}
	in.liveState = mapLiveState(live.State)
	bundle, hasBundle := r.Bundle()
	binding, bound := r.Binding()
	if !hasBundle || !bound || live.ReleaseID != r.ID || live.ImageDigest != bundle.PinnedDigest() ||
		!binding.Matches(r.ID, r.Target(), bundle.PinnedDigest()) || live.Generation < binding.Generation ||
		live.ObservedGeneration < live.Generation {
		in.effectiveState, in.stateSource = release.StateUnknown, "identity_mismatch"
		return
	}
	in.effectiveState, in.stateSource = in.liveState, "live"
	if in.liveState == release.StatePromoted || in.liveState == release.StateAborted || in.liveState == release.StateFailed {
		binding.Generation, binding.ArgoRevision, binding.AnalysisRunName = live.Generation, live.Revision, live.AnalysisRunName
		updated := r
		if live.State == execute.StateComplete {
			if envelope, ok := r.Eligibility().Envelope(); ok {
				stages := envelope.Stages()
				if len(stages) > 0 && !hasGrantedExecution(r, stages[len(stages)-1]) {
					// The live Rollout is the authoritative post-mutation fact.
					// Persist a catch-up grant so proof reflects what actually
					// happened even when the original advance command died while
					// reading optional AnalysisRun metadata.
					if caughtUp, execErr := updated.WithExecution(release.ExecutionEntry{
						At:              time.Now().UTC(),
						Verb:            release.VerbAdvance,
						RequestedWeight: stages[len(stages)-1],
						Outcome:         release.OutcomeGranted,
						Detail:          "reconciled from live Rollout completion",
					}); execErr == nil {
						updated = caughtUp
					}
				}
			}
		}
		if updated, updateErr := updated.WithState(in.liveState, binding); updateErr == nil && l.Update(updated) == nil {
			in.release = updated
			if len(in.history) > 0 {
				in.history[len(in.history)-1] = updated
			}
		}
	}
}

func hasGrantedExecution(r *release.Release, weight int) bool {
	for _, entry := range r.Execution() {
		if entry.Outcome == release.OutcomeGranted && entry.RequestedWeight == weight {
			return true
		}
	}
	return false
}

func mapLiveState(s execute.State) release.State {
	switch s {
	case execute.StateProgressing:
		return release.StateProgressing
	case execute.StateAnalysing:
		return release.StateAnalysing
	case execute.StateAtGate:
		return release.StateAtGate
	case execute.StateComplete:
		return release.StatePromoted
	case execute.StateAborted:
		return release.StateAborted
	case execute.StateDegraded:
		return release.StateFailed
	default:
		return release.StateStarting
	}
}

func printInspection(stdout, stderr io.Writer, in inspection, jsonOut, report bool) int {
	result := orchestrate.Inspection{Release: in.release}
	switch {
	case jsonOut && report:
		if err := writeJSON(stdout, in.JSON()); err != nil {
			fmt.Fprintf(stderr, "safelane release: could not encode the result: %v\n", err)
			return ExitFail
		}
	case jsonOut:
		if err := writeJSON(stdout, result.Release); err != nil {
			fmt.Fprintf(stderr, "safelane release: could not encode the result: %v\n", err)
			return ExitFail
		}
	case report:
		fmt.Fprint(stdout, in.Render())
	default:
		printSummary(stdout, result.Release)
	}
	return outcomeExitCode(result.Release)
}

func runReleaseRetry(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	fs := pflag.NewFlagSet("release retry", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	projectFile := fs.String("project", "", "path to operator project.yml")
	storeDir := fs.String("store-dir", defaultStoreDir, "release record directory")
	jsonOut := fs.Bool("json", false, "print a stable command result")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: safelane release retry <release-id>")
		return ExitUsage
	}
	id := release.ReleaseID(fs.Arg(0))
	if err := id.Validate(); err != nil {
		printRejection(stderr, err)
		return ExitUsage
	}
	paths, err := resolveRuntime(root, *projectFile, "", *storeDir)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}
	l := ledger.ReleaseLedger{Store: &store.FileStore{Dir: paths.storeDir}}
	parent, attempt, err := l.RetryParent(id)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}
	intent := parent.Request()
	callArgs := []string{"--pr", fmt.Sprint(intent.PullRequest), "--repo", intent.Repository, "--environment", intent.Environment}
	if intent.Image != "" {
		callArgs = append(callArgs, "--image", intent.Image)
	}
	callArgs = append(callArgs, "--project", paths.projectFile, "--store-dir", paths.storeDir)
	if !*jsonOut {
		return runRelease(ctx, callArgs, stdout, stderr, root, paths.storeDir, true, attempt, id)
	}
	callArgs = append(callArgs, "--json")
	var raw bytes.Buffer
	code := runRelease(ctx, callArgs, &raw, stderr, root, paths.storeDir, true, attempt, id)
	if raw.Len() == 0 {
		return code
	}
	var result map[string]any
	if err := json.Unmarshal(raw.Bytes(), &result); err != nil {
		return writeResultError(stderr, "release retry", err)
	}
	newID, _ := result["release_id"].(string)
	state, _ := result["effective_state"].(string)
	if state == "" {
		state, _ = result["recorded_state"].(string)
	}
	next := ""
	if code == ExitOK && newID != "" {
		next = fmt.Sprintf("safelane release run %s --yes --json", newID)
	}
	envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release retry", OK: code == ExitOK, ReleaseID: newID, State: state, NextCommand: next, Warnings: []string{}, Result: result}
	if err := jsonEncode(stdout, envelope); err != nil {
		return writeResultError(stderr, "release retry", err)
	}
	return code
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func loadIntent(file string, pr int, repo, environment, image string) (release.Intent, error) {
	if file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return release.Intent{}, fmt.Errorf("could not read %s: %v", file, err)
		}
		return intake.Parse(raw)
	}
	return release.Intent{
		SchemaVersion: release.RequestSchemaVersion,
		Repository:    repo,
		PullRequest:   pr,
		Environment:   environment,
		Image:         image,
	}, nil
}

// outcomeExitCode is ExitOK only when the release is eligible, so a caller
// scripting this command can branch on exit status alone.
func outcomeExitCode(r *release.Release) int {
	if r.Eligibility().Status() == release.EligibilityEligible {
		return ExitOK
	}
	return ExitFail
}

// printRejection writes the rejection block Appendix A specifies: one
// `safelane release: rejected:` line, then one entry per problem. Every
// problem the request has is reported in the same block, so an agent can
// correct all of them in one pass instead of one per round trip.
func printRejection(w io.Writer, err error) {
	var errs release.Errors
	if errors.As(err, &errs) {
		fmt.Fprintln(w, "safelane release: rejected:")
		for _, e := range errs {
			printError(w, e)
		}
		return
	}
	var single *release.Error
	if errors.As(err, &single) {
		fmt.Fprintln(w, "safelane release: rejected:")
		printError(w, single)
		return
	}
	fmt.Fprintf(w, "safelane release: %v\n", err)
}

// printError writes one rejection: the Appendix C4 tag and reason code on
// the first line with the field it is about, then what is wrong, then what
// to do. Three lines, because a caller acting on this needs the code to
// branch on and the remedy to act on, and mixing them into one line makes
// both harder to read.
func printError(w io.Writer, e *release.Error) {
	fmt.Fprintf(w, "  - [%s] %s", e.Tag(), e.Code)
	if e.Field != "" {
		fmt.Fprintf(w, " (%s)", e.Field)
	}
	fmt.Fprintln(w)
	if e.Message != "" {
		fmt.Fprintf(w, "      %s\n", e.Message)
	}
	if e.Remedy != "" {
		fmt.Fprintf(w, "      remedy: %s\n", e.Remedy)
	}
}

func printSummary(w io.Writer, r *release.Release) {
	fmt.Fprintf(w, "release_id: %s\n", r.ID)
	fmt.Fprintf(w, "application: %s  environment: %s\n", r.Target().Application, r.Target().Environment)
	fmt.Fprintf(w, "caller: %s (%s)\n", r.Caller().Identity, r.Caller().Kind)
	fmt.Fprintf(w, "evidence: %s\n", r.Evidence())

	if evidence, ok := r.Evidence().Verified(); ok {
		fmt.Fprintf(w, "  source revision: %s\n", evidence.MergeCommitSHA())
		fmt.Fprintf(w, "  pull request: #%d\n", evidence.PullRequest().Number)
		fmt.Fprintf(w, "  required check: %s (%s)\n", evidence.RequiredCheck().Name, evidence.RequiredCheck().Conclusion)
		fmt.Fprintf(w, "  artifact digest: %s\n", evidence.ArtifactDigest())
	} else {
		for _, reason := range r.Evidence().Reasons() {
			printError(w, reason)
		}
	}

	if bundle, ok := r.Bundle(); ok {
		fmt.Fprintf(w, "bundle: %d resource(s), template digest %s\n", bundle.Len(), bundle.Template().ContentDigest)
		for _, h := range bundle.Hashes() {
			fmt.Fprintf(w, "  - %s: %s\n", h.Ref, h.Hash)
		}
	}

	if a, ok := r.RecordedAssessment(); ok {
		fmt.Fprintf(w, "risk: %s (%s)\n", a.Risk, a.CombinedBy)
		fmt.Fprintf(w, "lane: %s\n", a.Lane)
	}

	elig := r.Eligibility()
	fmt.Fprintf(w, "eligibility: %s\n", elig.Status())
	fmt.Fprintf(w, "policy_version: %s\n", elig.PolicyVersion())
	fmt.Fprintf(w, "reason: %s\n", elig.ReasonCode())
	if elig.Message() != "" {
		fmt.Fprintf(w, "  %s\n", elig.Message())
	}
	fmt.Fprintf(w, "retryable: %v\n", elig.Retryable())
	if env, ok := elig.Envelope(); ok {
		fmt.Fprintf(w, "rollout_envelope: %s\n", joinStages(env.Stages()))
		fmt.Fprintf(w, "next_action: %s\n", env.NextAction())
	}
}

func joinStages(stages []int) string {
	if len(stages) == 0 {
		return ""
	}
	parts := make([]string, len(stages))
	for i, s := range stages {
		parts[i] = fmt.Sprintf("%d", s)
	}
	return strings.Join(parts, " → ")
}
