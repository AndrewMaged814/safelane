package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/AndrewMaged814/safelane/internal/integrate"
)

// InitCommand builds `safelane init --adapter codex`: a local generator for
// SafeLane-owned discovery files. It never calls Codex, an LLM, the network,
// or MCP.
func InitCommand(root string) Command {
	return Command{
		Name:    "init",
		Summary: "generate SafeLane discovery guidance",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runInit(args, stdout, stderr, root)
		},
	}
}

func runInit(args []string, stdout, stderr io.Writer, root string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adapter := fs.String("adapter", "", "caller adapter to generate (required: codex)")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *adapter == "" {
		fmt.Fprintln(stderr, "safelane init: --adapter is required")
		fs.Usage()
		return ExitUsage
	}
	if *adapter != "codex" {
		fmt.Fprintf(stderr, "safelane init: unknown adapter %q (supported: codex)\n", *adapter)
		return ExitUsage
	}

	changes, err := integrate.Apply(root)
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	for _, change := range changes {
		fmt.Fprintln(stdout, change.String())
	}
	return ExitOK
}
