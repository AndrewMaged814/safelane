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
