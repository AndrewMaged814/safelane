// Package cli is the safelane binary's command dispatch layer. It knows
// nothing about release semantics; it only routes argv to a named
// subcommand and reports usage/exit-code conventions consistently, so every
// subcommand (release today; proof and others later) behaves the same way
// for an agent or operator driving the CLI.
package cli

import (
	"context"
	"fmt"
	"io"
)

// Exit codes follow the Unix convention an agent can branch on without
// parsing output: 0 success, 1 the subcommand ran and reported a failure
// (e.g. a rejected release request), 2 a usage error (unknown command,
// missing/malformed flags).
const (
	ExitOK    = 0
	ExitFail  = 1
	ExitUsage = 2
)

// Command is one top-level safelane subcommand.
type Command struct {
	Name    string
	Summary string
	Run     func(ctx context.Context, args []string, stdout, stderr io.Writer) int
}

// Dispatch routes args[0] to the matching Command's Run, passing the
// remaining args through. An empty argv or an unknown command name prints
// usage to stderr and returns ExitUsage rather than panicking or silently
// doing nothing -- this is the CLI's entire behavior when the caller (human
// or agent) gets the invocation wrong.
func Dispatch(ctx context.Context, args []string, stdout, stderr io.Writer, commands []Command) int {
	if len(args) == 0 {
		printUsage(stderr, commands)
		return ExitUsage
	}
	name := args[0]
	for _, c := range commands {
		if c.Name == name {
			return c.Run(ctx, args[1:], stdout, stderr)
		}
	}
	fmt.Fprintf(stderr, "safelane: unknown command %q\n\n", name)
	printUsage(stderr, commands)
	return ExitUsage
}

func printUsage(w io.Writer, commands []Command) {
	fmt.Fprintln(w, "usage: safelane <command> [flags]")
	fmt.Fprintln(w, "\ncommands:")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-10s %s\n", c.Name, c.Summary)
	}
}
