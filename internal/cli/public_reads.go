package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
	"github.com/spf13/pflag"
)

func runPublicReleasePlan(ctx context.Context, args []string, stdout, stderr io.Writer, root, defaultStoreDir string) int {
	if !hasArg(args, "--json") {
		return runRelease(ctx, args, stdout, stderr, root, defaultStoreDir, true, 0, "")
	}
	var raw bytes.Buffer
	code := runRelease(ctx, args, &raw, stderr, root, defaultStoreDir, true, 0, "")
	if raw.Len() == 0 {
		return code
	}
	var result map[string]any
	if err := json.Unmarshal(raw.Bytes(), &result); err != nil {
		return writeResultError(stderr, "release plan", err)
	}
	id, _ := result["release_id"].(string)
	state, _ := result["effective_state"].(string)
	if state == "" {
		state, _ = result["recorded_state"].(string)
	}
	next := ""
	if id != "" && state == string(release.StateReady) {
		next = fmt.Sprintf("safelane release run %s --yes --json", id)
	}
	envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release plan", OK: code == ExitOK, ReleaseID: id, State: state, NextCommand: next, Warnings: []string{}, Result: result}
	if err := jsonEncode(stdout, envelope); err != nil {
		return writeResultError(stderr, "release plan", err)
	}
	return code
}

// ReleaseProofCommand exposes proof under the release hierarchy and wraps JSON.
func ReleaseProofCommand(defaultStoreDir string) Command {
	return Command{Name: "proof", Summary: "show durable release proof", Run: func(_ context.Context, args []string, stdout, stderr io.Writer) int {
		fs := pflag.NewFlagSet("release proof", pflag.ContinueOnError)
		fs.SetOutput(stderr)
		details := fs.Bool("details", false, "print complete human proof")
		jsonOut := fs.Bool("json", false, "print stable JSON proof")
		storeDir := fs.String("store-dir", defaultStoreDir, "release record directory")
		if err := fs.Parse(args); err != nil {
			return ExitUsage
		}
		if fs.NArg() != 1 || (*details && *jsonOut) {
			return ExitUsage
		}
		ordered := []string{"--store-dir", *storeDir}
		if *details {
			ordered = append(ordered, "--details")
		}
		if *jsonOut {
			ordered = append(ordered, "--json")
		}
		ordered = append(ordered, fs.Arg(0))
		if !*jsonOut {
			return runProof(ordered, stdout, stderr, *storeDir)
		}
		var raw bytes.Buffer
		code := runProof(ordered, &raw, stderr, *storeDir)
		if code != ExitOK {
			return code
		}
		var proof map[string]any
		if err := json.Unmarshal(raw.Bytes(), &proof); err != nil {
			return writeResultError(stderr, "release proof", err)
		}
		state := "recorded"
		if outcome, ok := proof["outcome"].(string); ok && outcome != "" {
			state = outcome
		}
		envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release proof", OK: true, ReleaseID: fs.Arg(0), State: state, NextCommand: "", Warnings: []string{}, Result: proof}
		if err := jsonEncode(stdout, envelope); err != nil {
			return writeResultError(stderr, "release proof", err)
		}
		return ExitOK
	}}
}

// ReleaseStatusCommand supports one reconciled release or a machine list.
func ReleaseStatusCommand(root, defaultStoreDir string) Command {
	return Command{Name: "status", Summary: "reconcile and show release state", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		fs := pflag.NewFlagSet("release status", pflag.ContinueOnError)
		fs.SetOutput(stderr)
		jsonOut := fs.Bool("json", false, "print stable JSON status")
		projectFile := fs.String("project", "", "operator project file")
		storeDir := fs.String("store-dir", defaultStoreDir, "release record directory")
		if err := fs.Parse(args); err != nil {
			return ExitUsage
		}
		if fs.NArg() > 1 {
			return ExitUsage
		}
		if !*jsonOut {
			ordered := []string{"--project", *projectFile, "--store-dir", *storeDir}
			if fs.NArg() == 1 {
				ordered = append(ordered, fs.Arg(0))
			}
			return runStatus(ctx, ordered, stdout, stderr, root, *storeDir, time.Now)
		}
		if fs.NArg() == 0 {
			return writeOpenReleaseList(stdout, stderr, root, *projectFile, *storeDir)
		}
		ordered := []string{"--json", "--project", *projectFile, "--store-dir", *storeDir, fs.Arg(0)}
		var raw bytes.Buffer
		code := runStatus(ctx, ordered, &raw, stderr, root, *storeDir, time.Now)
		if code != ExitOK {
			return code
		}
		var status map[string]any
		if err := json.Unmarshal(raw.Bytes(), &status); err != nil {
			return writeResultError(stderr, "release status", err)
		}
		state, _ := status["state"].(string)
		next := fmt.Sprintf("safelane release run %s --yes --json", fs.Arg(0))
		if state == "complete" || state == "aborted" || state == "degraded" {
			next = fmt.Sprintf("safelane release proof %s --json", fs.Arg(0))
		}
		envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release status", OK: true, ReleaseID: fs.Arg(0), State: state, NextCommand: next, Warnings: []string{}, Result: status}
		if err := jsonEncode(stdout, envelope); err != nil {
			return writeResultError(stderr, "release status", err)
		}
		return ExitOK
	}}
}

func writeOpenReleaseList(stdout, stderr io.Writer, root, projectFile, storeDir string) int {
	paths, err := resolveRuntime(root, projectFile, "", storeDir)
	if err != nil {
		return writeResultError(stderr, "release status", err)
	}
	releases, err := (&store.FileStore{Dir: paths.storeDir}).List()
	if err != nil {
		return writeResultError(stderr, "release status", err)
	}
	items := make([]map[string]any, 0)
	for _, item := range releases {
		if item.State() == release.StateReady || item.State().Active() || item.State() == release.StateBlocked {
			items = append(items, map[string]any{"release_id": item.ID, "state": item.State(), "pull_request": item.Request().PullRequest, "environment": item.Target().Environment})
		}
	}
	return writeEnvelopeCode(stdout, stderr, ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "release status", OK: true, State: "listed", NextCommand: "", Warnings: []string{}, Result: map[string]any{"releases": items}})
}

func writeEnvelopeCode(stdout, stderr io.Writer, envelope ResultEnvelope) int {
	if err := jsonEncode(stdout, envelope); err != nil {
		return writeResultError(stderr, envelope.Command, err)
	}
	return ExitOK
}

func hasArg(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}
