package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseCommand_MissingFile_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand("template-dir-unused", "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), nil, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--file is required") {
		t.Fatalf("want a message about the required --file flag, got %q", stderr.String())
	}
}

func TestReleaseCommand_UnreadableFile_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand("template-dir-unused", "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--file", filepath.Join(t.TempDir(), "does-not-exist.json")}, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "could not read") {
		t.Fatalf("want a file-read error message, got %q", stderr.String())
	}
}

func TestReleaseCommand_BadTemplateDir_ExitsFail(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "request.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	cmd := ReleaseCommand(filepath.Join(dir, "no-such-template-dir"), filepath.Join(dir, "store"))
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--file", file}, &stdout, &stderr)

	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "could not load the Release Template") {
		t.Fatalf("want a template-load error message, got %q", stderr.String())
	}
}

func TestReleaseCommand_UnknownFlag_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand("template-dir-unused", "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--not-a-real-flag"}, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage for an unrecognized flag, got %d", code)
	}
}
