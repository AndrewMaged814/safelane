package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/intake"
	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// ReleaseCommand builds `safelane release --pr <n>` (and `--file` for CI).
// root is the application directory used to find .safelane/project.yml.
func ReleaseCommand(root, defaultStoreDir string) Command {
	return Command{
		Name:    "release",
		Summary: "submit a Release Request and record a Release",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runRelease(ctx, args, stdout, stderr, root, defaultStoreDir)
		},
	}
}

func runRelease(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "path to a slim Release Request JSON file")
	pr := fs.Int("pr", 0, "merged pull request number")
	repo := fs.String("repo", "", "GitHub owner/name (default: git origin or project.yml)")
	environment := fs.String("environment", "", "environment selector (default: project.yml)")
	image := fs.String("image", "", "optional immutable digest pin to verify")
	jsonOut := fs.Bool("json", false, "print the full Release record as JSON instead of a human summary")
	templateDir := fs.String("template-dir", "", "override project.yml template_path")
	projectFile := fs.String("project", "", "path to project.yml (default: .safelane/project.yml)")
	storeDir := fs.String("store-dir", defaultStoreDir, "directory Release records are persisted under")
	githubToken := fs.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub API token (optional; unauthenticated calls work against public repos, rate-limited)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *file != "" && *pr != 0 {
		fmt.Fprintln(stderr, "safelane release: use --pr or --file, not both")
		fs.Usage()
		return ExitUsage
	}
	if *file == "" && *pr == 0 {
		fmt.Fprintln(stderr, "safelane release: --pr or --file is required")
		fs.Usage()
		return ExitUsage
	}

	intent, err := loadIntent(*file, *pr, *repo, *environment, *image)
	if err != nil {
		if *file != "" && strings.Contains(err.Error(), "could not read") {
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

	projPath := *projectFile
	if projPath == "" {
		projPath = filepath.Join(root, filepath.FromSlash(project.RelPath))
	}
	cfg, err := project.Load(projPath)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}

	tmplPath := *templateDir
	if tmplPath == "" {
		tmplPath = cfg.Release.TemplatePath
		if !filepath.IsAbs(tmplPath) {
			tmplPath = filepath.Join(root, tmplPath)
		}
	}
	tmpl, err := render.LoadDir(tmplPath)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release: could not load the Release Template at %s: %v\n", tmplPath, err)
		return ExitFail
	}

	deps := orchestrate.Deps{
		GitHub:   &github.Client{Token: *githubToken},
		GHCR:     &ghcr.Client{},
		Template: tmpl,
		Store:    &store.FileStore{Dir: *storeDir},
		Project:  cfg,
		Caller:   release.CallerIdentity{Identity: "safelane-cli", Kind: release.CallerAgent, Tool: "safelane"},
	}

	r, err := orchestrate.SubmitRelease(ctx, intent, deps)
	if err != nil {
		printRejection(stderr, err)
		return ExitFail
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			fmt.Fprintf(stderr, "safelane release: could not encode the result: %v\n", err)
			return ExitFail
		}
		return outcomeExitCode(r)
	}

	printSummary(stdout, r)
	return outcomeExitCode(r)
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

func printRejection(w io.Writer, err error) {
	var errs release.Errors
	if errors.As(err, &errs) {
		fmt.Fprintf(w, "safelane release: rejected (%d problem(s)):\n", len(errs))
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

func printError(w io.Writer, e *release.Error) {
	fmt.Fprintf(w, "  - [%s] %s", e.Category, e.Code)
	if e.Field != "" {
		fmt.Fprintf(w, " (%s)", e.Field)
	}
	fmt.Fprintf(w, ": %s\n", e.Message)
	if e.Remedy != "" {
		fmt.Fprintf(w, "    remedy: %s\n", e.Remedy)
	}
}

func printSummary(w io.Writer, r *release.Release) {
	fmt.Fprintf(w, "release_id: %s\n", r.ID)
	fmt.Fprintf(w, "application: %s  environment: %s\n", r.Target().Application, r.Target().Environment)
	fmt.Fprintf(w, "caller: %s (%s)\n", r.Caller().Identity, r.Caller().Kind)
	fmt.Fprintf(w, "evidence: %s\n", r.Evidence())

	if evidence, ok := r.Evidence().Verified(); ok {
		fmt.Fprintf(w, "  source revision: %s\n", evidence.MergeCommitSHA())
		fmt.Fprintf(w, "  pull request: #%d (approved by %s, independent of author: %v)\n",
			evidence.PullRequest().Number, evidence.Approval().Reviewer, evidence.IndependentApproval())
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
