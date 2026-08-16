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
	if _, err := os.Stat(filepath.Join(root, ".safelane", "project.yml")); err != nil {
		t.Fatalf("want created project.yml, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".safelane", "release-template")); err != nil {
		t.Fatalf("want created release-template, got %v", err)
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

func TestInit_OneManagedBlock_ReplacesOnlyThatBlock(t *testing.T) {
	root := t.TempDir()
	prefix := "# Team rules\n\nRun go test.\n\n"
	suffix := "\n## Local notes\nDo not rewrite this.\n"
	original := prefix +
		"<!-- BEGIN SAFELANE MANAGED: guidance -->\nOLD POINTER\n<!-- END SAFELANE MANAGED: guidance -->\n" +
		suffix
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
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("want prefix preserved, got %q", got)
	}
	if !strings.HasSuffix(got, suffix) {
		t.Fatalf("want suffix preserved, got %q", got)
	}
	if strings.Contains(got, "OLD POINTER") {
		t.Fatalf("want the stale managed body replaced, got %q", got)
	}
	if !strings.Contains(got, ".safelane/agent-guidance.md") {
		t.Fatalf("want the canonical pointer, got %q", got)
	}
	if !strings.Contains(stdout.String(), "updated AGENTS.md managed section") {
		t.Fatalf("want updated report for AGENTS.md, got %q", stdout.String())
	}
}

func TestInit_AmbiguousAgentsMd_LeavesFileAndWritesFallback(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  string
	}{
		{
			name: "incomplete begin only",
			body: "# Notes\n<!-- BEGIN SAFELANE MANAGED: guidance -->\nno end\n",
			why:  "incomplete",
		},
		{
			name: "incomplete end only",
			body: "# Notes\n<!-- END SAFELANE MANAGED: guidance -->\n",
			why:  "incomplete",
		},
		{
			name: "nested markers",
			body: "<!-- BEGIN SAFELANE MANAGED: guidance -->\n<!-- BEGIN SAFELANE MANAGED: guidance -->\ninner\n<!-- END SAFELANE MANAGED: guidance -->\n<!-- END SAFELANE MANAGED: guidance -->\n",
			why:  "nested",
		},
		{
			name: "duplicated markers",
			body: "<!-- BEGIN SAFELANE MANAGED: guidance -->\nfirst\n<!-- END SAFELANE MANAGED: guidance -->\n<!-- BEGIN SAFELANE MANAGED: guidance -->\nsecond\n<!-- END SAFELANE MANAGED: guidance -->\n",
			why:  "duplicated",
		},
		{
			name: "end before begin",
			body: "<!-- END SAFELANE MANAGED: guidance -->\n<!-- BEGIN SAFELANE MANAGED: guidance -->\n",
			why:  "malformed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			agentsPath := filepath.Join(root, "AGENTS.md")
			if err := os.WriteFile(agentsPath, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := InitCommand(root)
			var stdout, stderr bytes.Buffer
			code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr)
			if code != ExitOK {
				t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
			}

			got, err := os.ReadFile(agentsPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.body {
				t.Fatalf("want AGENTS.md left byte-for-byte unchanged, got %q", got)
			}

			report := stdout.String()
			if !strings.Contains(report, "skipped AGENTS.md") || !strings.Contains(report, tc.why) {
				t.Fatalf("want skipped AGENTS.md mentioning %q, got %q", tc.why, report)
			}
			if !strings.Contains(report, "created .safelane/integrations/codex.md") {
				t.Fatalf("want created fallback report, got %q", report)
			}

			fallback, err := os.ReadFile(filepath.Join(root, ".safelane", "integrations", "codex.md"))
			if err != nil {
				t.Fatalf("want fallback file, got %v", err)
			}
			text := string(fallback)
			if !strings.Contains(text, "does not auto-load") {
				t.Fatalf("want the fallback to say Codex will not auto-load it, got %q", text)
			}
			if !strings.Contains(text, "<!-- BEGIN SAFELANE MANAGED: guidance -->") {
				t.Fatalf("want a copyable managed section in the fallback, got %q", text)
			}
		})
	}
}

func TestInit_RepeatedWithoutChange_ReportsUnchanged(t *testing.T) {
	root := t.TempDir()
	cmd := InitCommand(root)
	var stdout, stderr bytes.Buffer

	if code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("first init: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	guidanceBefore, err := os.ReadFile(filepath.Join(root, ".safelane", "agent-guidance.md"))
	if err != nil {
		t.Fatal(err)
	}
	agentsBefore, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("second init: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	report := stdout.String()
	if !strings.Contains(report, "unchanged .safelane/agent-guidance.md") {
		t.Fatalf("want unchanged guidance report, got %q", report)
	}
	if !strings.Contains(report, "unchanged AGENTS.md managed section") {
		t.Fatalf("want unchanged AGENTS.md report, got %q", report)
	}

	guidanceAfter, err := os.ReadFile(filepath.Join(root, ".safelane", "agent-guidance.md"))
	if err != nil {
		t.Fatal(err)
	}
	agentsAfter, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(guidanceBefore, guidanceAfter) {
		t.Fatal("second init changed agent-guidance.md")
	}
	if !bytes.Equal(agentsBefore, agentsAfter) {
		t.Fatal("second init changed AGENTS.md")
	}
}

func TestInit_GuidanceTeachesReleaseExecuteProof(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	if code := InitCommand(root).Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(root, ".safelane", "agent-guidance.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	want := []string{
		"does not authorize a release",
		"Eligibility does not mean the artifact is safe or deployed",
		"safelane release --pr <number>",
		"safelane execute <release-id>",
		"Proof may remain pending",
		"Pending proof is not a completed deployment",
		"Never call Kubernetes or Argo directly",
	}
	for _, phrase := range want {
		if !strings.Contains(text, phrase) {
			t.Errorf("guidance missing %q", phrase)
		}
	}
}

func TestInit_MissingAdapter_ExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := InitCommand(t.TempDir()).Run(context.Background(), nil, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr.String(), "--adapter is required") {
		t.Fatalf("want required-adapter message, got %q", stderr.String())
	}
}

func TestInit_UnknownAdapter_ExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := InitCommand(t.TempDir()).Run(context.Background(), []string{"--adapter", "claude"}, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d", code)
	}
	if !strings.Contains(stderr.String(), `unknown adapter "claude"`) {
		t.Fatalf("want unknown-adapter message, got %q", stderr.String())
	}
}

func TestInit_AmbiguousAgentsMd_SecondRunUnchanged(t *testing.T) {
	root := t.TempDir()
	original := "<!-- BEGIN SAFELANE MANAGED: guidance -->\nno end\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := InitCommand(root)
	var stdout, stderr bytes.Buffer
	if code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("first init: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	fallbackBefore, err := os.ReadFile(filepath.Join(root, ".safelane", "integrations", "codex.md"))
	if err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := cmd.Run(context.Background(), []string{"--adapter", "codex"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("second init: want ExitOK, got %d (stderr: %s)", code, stderr.String())
	}

	report := stdout.String()
	if !strings.Contains(report, "unchanged .safelane/integrations/codex.md") {
		t.Fatalf("want unchanged fallback report, got %q", report)
	}
	if !strings.Contains(report, "skipped AGENTS.md") {
		t.Fatalf("want AGENTS.md still skipped, got %q", report)
	}

	fallbackAfter, err := os.ReadFile(filepath.Join(root, ".safelane", "integrations", "codex.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fallbackBefore, fallbackAfter) {
		t.Fatal("second init changed the Codex fallback")
	}
	got, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatal("second init changed the ambiguous AGENTS.md")
	}
}
