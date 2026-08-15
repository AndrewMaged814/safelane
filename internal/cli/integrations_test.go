package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationsSync_EmptyDir_CreatesGuidanceAndAgentsMd(t *testing.T) {
	root := t.TempDir()
	cmd := IntegrationsCommand(root)
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"sync"}, &stdout, &stderr)

	if code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".safelane", "agent-guidance.md")); err != nil {
		t.Fatalf("want created agent-guidance.md, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("want created AGENTS.md, got %v", err)
	}
	report := stdout.String()
	if !strings.Contains(report, "created .safelane/agent-guidance.md") {
		t.Fatalf("want created guidance report, got %q", report)
	}
	if !strings.Contains(report, "created AGENTS.md managed section") {
		t.Fatalf("want created AGENTS.md report, got %q", report)
	}
}

func TestIntegrationsSync_AfterInit_ReportsUnchanged(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := InitCommand(root).Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("init: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := IntegrationsCommand(root).Run(context.Background(), []string{"sync"}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("sync: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}
	report := stdout.String()
	if !strings.Contains(report, "unchanged .safelane/agent-guidance.md") {
		t.Fatalf("want unchanged guidance report, got %q", report)
	}
	if !strings.Contains(report, "unchanged AGENTS.md managed section") {
		t.Fatalf("want unchanged AGENTS.md report, got %q", report)
	}
}

func TestIntegrations_MissingSync_ExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := IntegrationsCommand(t.TempDir()).Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr.String(), "safelane integrations sync") {
		t.Fatalf("want sync usage, got %q", stderr.String())
	}
}
