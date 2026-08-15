package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func fixtureCommands(calls *[]string) []Command {
	return []Command{
		{
			Name:    "echo",
			Summary: "echo the given args",
			Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				*calls = append(*calls, strings.Join(args, ","))
				fmt.Fprintln(stdout, "echoed")
				return ExitOK
			},
		},
		{
			Name:    "boom",
			Summary: "always fails",
			Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
				fmt.Fprintln(stderr, "boom failed")
				return ExitFail
			},
		},
	}
}

func TestDispatch_RoutesToNamedCommand(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer

	code := Dispatch(context.Background(), []string{"echo", "a", "b"}, &stdout, &stderr, fixtureCommands(&calls))

	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}
	if len(calls) != 1 || calls[0] != "a,b" {
		t.Fatalf("want echo called with [a b], got %v", calls)
	}
	if !strings.Contains(stdout.String(), "echoed") {
		t.Fatalf("want stdout to contain command output, got %q", stdout.String())
	}
}

func TestDispatch_PropagatesSubcommandExitCode(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer

	code := Dispatch(context.Background(), []string{"boom"}, &stdout, &stderr, fixtureCommands(&calls))

	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d", code)
	}
}

func TestDispatch_NoArgs_PrintsUsageAndExitsUsage(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer

	code := Dispatch(context.Background(), []string{}, &stdout, &stderr, fixtureCommands(&calls))

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("want usage text on stderr, got %q", stderr.String())
	}
}

func TestDispatch_UnknownCommand_PrintsUsageAndExitsUsage(t *testing.T) {
	var calls []string
	var stdout, stderr bytes.Buffer

	code := Dispatch(context.Background(), []string{"does-not-exist"}, &stdout, &stderr, fixtureCommands(&calls))

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "does-not-exist"`) {
		t.Fatalf("want unknown-command message on stderr, got %q", stderr.String())
	}
}
