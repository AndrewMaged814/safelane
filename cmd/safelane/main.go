// Command safelane is the release-facing CLI entry point. It exposes no
// arbitrary Kubernetes mutation, Argo commands, or protected production
// credentials -- only the typed release-intake and proof-retrieval surface.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AndrewMaged814/safelane/internal/cli"
	"github.com/AndrewMaged814/safelane/internal/project"
)

// version is overridden at build time via:
//
//	go build -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	// Resolve the repository before command construction so every command
	// receives this application's operator-owned record directory.
	defaultStoreDir := ""
	if loc, err := project.Resolve("."); err == nil {
		defaultStoreDir = loc.ReleasesDir
	} else if home, homeErr := project.Home(); homeErr == nil {
		// Even without a matching app, proof must never fall back to the
		// application repository as its implicit record directory.
		defaultStoreDir = filepath.Join(home, "apps", ".unmatched", "releases")
	}
	commands := []cli.Command{
		versionCommand(),
		cli.ReleaseCommand(".", defaultStoreDir),
		cli.RolloutCommand(".", defaultStoreDir),
		cli.StatusCommand(".", defaultStoreDir),
		cli.DoctorCommand("."),
		cli.ProofCommand(defaultStoreDir),
		cli.SetupCommand("."),
		cli.InitCommand("."),
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
