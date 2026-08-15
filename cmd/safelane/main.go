// Command safelane is the release-facing CLI entry point. It exposes no
// arbitrary Kubernetes mutation, Argo commands, or protected production
// credentials -- only the typed release-intake surface described in #47/#48.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/AndrewMaged814/safelane/internal/cli"
)

// version is overridden at build time via:
//
//	go build -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	commands := []cli.Command{
		versionCommand(),
		// "release" is registered once the release-intake orchestration
		// (evidence verification, rendering, Release record persistence) is
		// wired up -- see #48.
	}
	return cli.Dispatch(context.Background(), args, stdout, stderr, commands)
}

func versionCommand() cli.Command {
	return cli.Command{
		Name:    "version",
		Summary: "print the safelane build version",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			fmt.Fprintln(stdout, version)
			return cli.ExitOK
		},
	}
}
