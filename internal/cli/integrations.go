package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/AndrewMaged814/safelane/internal/integrate"
)

// IntegrationsCommand builds `safelane integrations sync`: the same local
// generator as init, used after the tool contract changes. It never calls
// Codex, an LLM, the network, or MCP.
func IntegrationsCommand(root string) Command {
	return Command{
		Name:    "integrations",
		Summary: "regenerate SafeLane discovery files",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runIntegrations(args, stdout, stderr, root)
		},
	}
}

func runIntegrations(args []string, stdout, stderr io.Writer, root string) int {
	if len(args) != 1 || args[0] != "sync" {
		fmt.Fprintln(stderr, "usage: safelane integrations sync")
		return ExitUsage
	}
	return reportApply(root, stdout, stderr, "integrations")
}

func reportApply(root string, stdout, stderr io.Writer, cmdName string) int {
	changes, err := integrate.Apply(root)
	if err != nil {
		fmt.Fprintf(stderr, "safelane %s: %v\n", cmdName, err)
		return ExitFail
	}
	for _, change := range changes {
		fmt.Fprintln(stdout, change.String())
	}
	return ExitOK
}
