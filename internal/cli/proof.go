package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/AndrewMaged814/safelane/internal/proof"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

// ProofCommand builds `safelane release proof <release-id>`: a read of an already
// persisted Release. It never calls GitHub, GHCR, or policy.Evaluate.
func ProofCommand(defaultStoreDir string) Command {
	return Command{
		Name:    "proof",
		Summary: "retrieve Release Proof for a persisted release",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runProof(args, stdout, stderr, defaultStoreDir)
		},
	}
}

func runProof(args []string, stdout, stderr io.Writer, defaultStoreDir string) int {
	fs := flag.NewFlagSet("proof", flag.ContinueOnError)
	fs.SetOutput(stderr)
	details := fs.Bool("details", false, "print the complete human-readable proof record")
	jsonOut := fs.Bool("json", false, "print the machine-readable proof contract")
	storeDir := fs.String("store-dir", defaultStoreDir, "directory Release records are persisted under")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *details && *jsonOut {
		fmt.Fprintln(stderr, "safelane release proof: --details and --json cannot be used together")
		fs.Usage()
		return ExitUsage
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(stderr, "safelane release proof: release id is required")
		fs.Usage()
		return ExitUsage
	}
	if len(rest) > 1 {
		fmt.Fprintf(stderr, "safelane release proof: unexpected extra arguments %q\n", rest[1:])
		fs.Usage()
		return ExitUsage
	}

	id, err := release.ParseReleaseID(rest[0])
	if err != nil {
		printProofError(stderr, err)
		return ExitUsage
	}

	r, err := (&store.FileStore{Dir: *storeDir}).Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			printProofError(stderr, release.Invalid("release_not_found", "release_id",
				fmt.Sprintf("no Release record for %s", id),
				"Use the release id SafeLane returned from `safelane release`. Proof cannot invent a record."))
			return ExitFail
		}
		fmt.Fprintf(stderr, "safelane release proof: %v\n", err)
		return ExitFail
	}

	p := proof.From(r)
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(p); err != nil {
			fmt.Fprintf(stderr, "safelane release proof: could not encode the result: %v\n", err)
			return ExitFail
		}
		return ExitOK
	}
	if *details {
		fmt.Fprint(stdout, p.Details())
		return ExitOK
	}
	fmt.Fprint(stdout, p.Concise())
	return ExitOK
}

func printProofError(w io.Writer, err error) {
	var errs release.Errors
	if errors.As(err, &errs) {
		fmt.Fprintf(w, "safelane release proof: rejected (%d problem(s)):\n", len(errs))
		for _, e := range errs {
			printError(w, e)
		}
		return
	}
	var single *release.Error
	if errors.As(err, &single) {
		fmt.Fprintln(w, "safelane release proof:")
		printError(w, single)
		return
	}
	fmt.Fprintf(w, "safelane release proof: %v\n", err)
}
