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
