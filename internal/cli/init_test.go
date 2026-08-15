package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_CodexAdapter_EmptyDir_CreatesAgentGuidance(t *testing.T) {
	root := t.TempDir()
	cmd := InitCommand(root)
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	guidance := filepath.Join(root, ".safelane", "agent-guidance.md")
	if _, err := os.Stat(guidance); err != nil {
		t.Fatalf("want created %s, got %v", guidance, err)
	}
	if !strings.Contains(stdout.String(), "created .safelane/agent-guidance.md") {
		t.Fatalf("want created report for agent-guidance.md, got %q", stdout.String())
	}
}

func TestInit_MissingAgentsMd_CreatesManagedSection(t *testing.T) {
	root := t.TempDir()
	cmd := InitCommand(root)
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("want created AGENTS.md, got %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "<!-- BEGIN SAFELANE MANAGED: guidance -->") ||
		!strings.Contains(text, "<!-- END SAFELANE MANAGED: guidance -->") {
		t.Fatalf("want a well-formed managed section, got %q", text)
	}
	if !strings.Contains(text, ".safelane/agent-guidance.md") {
		t.Fatalf("want the managed section to point at agent-guidance.md, got %q", text)
	}
	if !strings.Contains(stdout.String(), "created AGENTS.md managed section") {
		t.Fatalf("want created report for AGENTS.md, got %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".safelane", "integrations", "codex.md")); err == nil {
		t.Fatal("missing AGENTS.md must not write the undiscoverable Codex fallback")
	}
}

func TestInit_UnmarkedAgentsMd_AppendsManagedSection(t *testing.T) {
	root := t.TempDir()
	original := "# Project notes\r\n\r\nUse go test ./...\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := InitCommand(root)
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.HasPrefix(got, original) {
		t.Fatalf("want original bytes preserved, got %q", got)
	}
	if !strings.Contains(got, "<!-- BEGIN SAFELANE MANAGED: guidance -->") ||
		!strings.Contains(got, "<!-- END SAFELANE MANAGED: guidance -->") {
		t.Fatalf("want an appended managed section, got %q", got)
	}
	if !strings.Contains(stdout.String(), "updated AGENTS.md managed section") {
		t.Fatalf("want updated report for AGENTS.md, got %q", stdout.String())
	}
}
