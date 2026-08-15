package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/AndrewMaged814/safelane/internal/orchestrate"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// ReleaseCommand builds the `safelane release --file release-evidence.json`
// subcommand: the CLI's one release-facing entry point (#47). Everything
// past argument parsing and file I/O is orchestrate.SubmitRelease -- this
// file's only job is to wire real dependencies (the real GitHub/GHCR
// clients, the operator's loaded Release Template, a real file-backed
// Store) and format the result for a human or an agent.
//
// defaultTemplateDir and defaultStoreDir give the current demo working
// defaults (the fixture Release Template, and ./.safelane/releases); both
// are overridable with --template-dir and --store-dir so this points at
// Ahmed's real operator-owned template with no code change once it exists.
func ReleaseCommand(defaultTemplateDir, defaultStoreDir string) Command {
	return Command{
		Name:    "release",
		Summary: "submit a Release Request and record a Release",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runRelease(ctx, args, stdout, stderr, defaultTemplateDir, defaultStoreDir)
		},
	}
}

func runRelease(ctx context.Context, args []string, stdout, stderr io.Writer, defaultTemplateDir, defaultStoreDir string) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "path to the Release Request JSON file (required)")
	jsonOut := fs.Bool("json", false, "print the full Release record as JSON instead of a human summary")
	templateDir := fs.String("template-dir", defaultTemplateDir, "path to the operator-owned Release Template directory")
	storeDir := fs.String("store-dir", defaultStoreDir, "directory Release records are persisted under")
	githubToken := fs.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub API token (optional; unauthenticated calls work against public repos, rate-limited)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *file == "" {
		fmt.Fprintln(stderr, "safelane release: --file is required")
		fs.Usage()
		return ExitUsage
	}

	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release: could not read %s: %v\n", *file, err)
		return ExitUsage
	}

	tmpl, err := render.LoadDir(*templateDir)
	if err != nil {
		fmt.Fprintf(stderr, "safelane release: could not load the Release Template at %s: %v\n", *templateDir, err)
		return ExitFail
	}

	deps := orchestrate.Deps{
		GitHub:   &github.Client{Token: *githubToken},
		GHCR:     &ghcr.Client{},
		Template: tmpl,
		Store:    &store.FileStore{Dir: *storeDir},
	}

	r, err := orchestrate.SubmitRelease(ctx, raw, deps)
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

// outcomeExitCode is ExitOK only when evidence verified, so a caller
// scripting this command can branch on exit status alone: a withheld or
// failed release is still successfully recorded (a plain ExitFail, not a
// crash), but its exit code says "do not proceed" either way.
func outcomeExitCode(r *release.Release) int {
	if r.Evidence().IsVerified() {
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

// printSummary prints the concise, human-readable result #47 asks for:
// release identity, caller, evidence outcome, and - only for a verified
// release - the bundle it rendered. Structured detail remains available via
// --json for automation.
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
}
