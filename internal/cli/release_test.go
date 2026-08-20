package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func TestReleaseCommand_MissingSelector_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand(t.TempDir(), "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), nil, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--pr or --file is required") {
		t.Fatalf("want a message about --pr or --file, got %q", stderr.String())
	}
}

func TestReleaseCommand_UnreadableFile_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand(t.TempDir(), "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--file", filepath.Join(t.TempDir(), "does-not-exist.json")}, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "could not read") {
		t.Fatalf("want a file-read error message, got %q", stderr.String())
	}
}

func TestReleaseCommand_EmptyJSON_IsInvalidRequestNotTemplateError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "request.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("test setup: %v", err)
	}

	cmd := ReleaseCommand(dir, filepath.Join(dir, "store"))
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--file", file}, &stdout, &stderr)

	if code != ExitFail {
		t.Fatalf("want ExitFail, got %d (stderr: %s)", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "could not load the Release Template") {
		t.Fatalf("empty request must fail before template load, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "pull_request") && !strings.Contains(stderr.String(), "invalid") {
		t.Fatalf("want an invalid-request mentioning pull_request, got %q", stderr.String())
	}
}

func TestReleaseCommand_BothPRAndFile_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand(t.TempDir(), "store")
	var stdout, stderr bytes.Buffer
	code := cmd.Run(context.Background(), []string{"--pr", "2", "--file", "x.json"}, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("want ExitUsage, got %d (stderr: %s)", code, stderr.String())
	}
}

func TestReleaseCommand_UnknownFlag_ExitsUsage(t *testing.T) {
	cmd := ReleaseCommand(t.TempDir(), "store-dir-unused")
	var stdout, stderr bytes.Buffer

	code := cmd.Run(context.Background(), []string{"--not-a-real-flag"}, &stdout, &stderr)

	if code != ExitUsage {
		t.Fatalf("want ExitUsage for an unrecognized flag, got %d", code)
	}
}

func TestPrintSummary_Eligible_IncludesEnvelopeAndNextAction(t *testing.T) {
	rel := mustCLIRelease(t, true)
	var buf bytes.Buffer
	printSummary(&buf, rel)
	out := buf.String()
	for _, want := range []string{
		"eligibility: eligible",
		"policy_version: 1",
		"retryable: false",
		"rollout_envelope: 5 → 25 → 50 → 100",
		"next_action: start",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q\n%s", want, out)
		}
	}
}

func TestPrintSummary_Indeterminate_IsRetryableWithoutEnvelope(t *testing.T) {
	rel := mustCLIRelease(t, false)
	var buf bytes.Buffer
	printSummary(&buf, rel)
	out := buf.String()
	if !strings.Contains(out, "eligibility: indeterminate") {
		t.Errorf("want indeterminate\n%s", out)
	}
	if !strings.Contains(out, "retryable: true") {
		t.Errorf("want retryable true\n%s", out)
	}
	if strings.Contains(out, "rollout_envelope:") || strings.Contains(out, "next_action:") {
		t.Errorf("indeterminate must not print an envelope\n%s", out)
	}
}

func TestOutcomeExitCode_EligibleOnly(t *testing.T) {
	if code := outcomeExitCode(mustCLIRelease(t, true)); code != ExitOK {
		t.Errorf("eligible exit = %d, want ExitOK", code)
	}
	if code := outcomeExitCode(mustCLIRelease(t, false)); code != ExitFail {
		t.Errorf("indeterminate exit = %d, want ExitFail", code)
	}
}

const (
	cliMergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	cliDigest   = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
)

func mustCLIRelease(t *testing.T, eligible bool) *release.Release {
	t.Helper()
	now := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)
	req := release.ReleaseRequest{
		SchemaVersion: release.RequestSchemaVersion,
		Target:        release.Target{Application: "podinfo", Environment: "production", Cluster: "safelane-demo", Namespace: "podinfo"},
		Source:        release.ClaimedSource{Repository: "AndrewMaged814/podinfo", BaseBranch: "main", MergeCommitSHA: cliMergeSHA},
		PullRequest:   release.ClaimedPullRequest{PullRequestNumber: 1, PullRequestURL: "https://github.com/AndrewMaged814/podinfo/pull/1", Author: "AndrewMaged814"},
		CI:            release.ClaimedCI{Workflow: "publish", CheckName: "publish / build-and-push", RunID: 1},
		Artifact:      release.ClaimedArtifact{ImageReference: "ghcr.io/andrewmaged814/podinfo@" + cliDigest},
		Caller:        release.CallerIdentity{Identity: "codex-cli", Kind: release.CallerAgent},
		Metadata:      release.RequestMetadata{RequestID: "req-cli", SubmittedAt: now},
	}
	id, err := release.NewReleaseID(now, strings.NewReader("0123456789"))
	if err != nil {
		t.Fatalf("NewReleaseID: %v", err)
	}

	var evidence release.EvidenceResult
	var bundle *release.RenderedBundle
	var elig release.Eligibility
	if eligible {
		ev, err := release.NewReleaseEvidence(release.EvidenceInput{
			Repository:     release.RepositoryRef{Owner: "AndrewMaged814", Name: "podinfo"},
			PullRequest:    release.VerifiedPullRequest{Number: 1, URL: req.PullRequest.PullRequestURL, Author: "AndrewMaged814", BaseBranch: "main", MergedAt: now},
			MergeCommitSHA: cliMergeSHA,
			RequiredCheck:  release.VerifiedCheckRun{Name: "publish / build-and-push", HeadSHA: cliMergeSHA, Conclusion: release.CheckConclusionSuccess, CompletedAt: now},
			Artifact:       release.VerifiedArtifact{Reference: release.ImageReference{Registry: "ghcr.io", Repository: "andrewmaged814/podinfo", Digest: cliDigest}, ObservedDigest: cliDigest, ResolvedAt: now},
			VerifiedAt:     now,
		})
		if err != nil {
			t.Fatalf("NewReleaseEvidence: %v", err)
		}
		evidence, err = release.VerifiedEvidence(ev)
		if err != nil {
			t.Fatalf("VerifiedEvidence: %v", err)
		}
		body := []byte("apiVersion: argoproj.io/v1alpha1\nkind: Rollout\nmetadata:\n  name: podinfo\n  namespace: podinfo\nspec:\n  image: ghcr.io/andrewmaged814/podinfo@" + cliDigest + "\n")
		res, err := release.NewRenderedResource(release.ResourceRef{
			TemplatePath: "40-rollout.yaml.tmpl", APIVersion: "argoproj.io/v1alpha1",
			Kind: "Rollout", Namespace: "podinfo", Name: "podinfo",
		}, body)
		if err != nil {
			t.Fatalf("NewRenderedResource: %v", err)
		}
		b, err := release.NewRenderedBundle(release.TemplateIdentity{Name: "podinfo-canary", Version: "v0.1.0-fixture", ContentDigest: "sha256:0011223344556677889900aabbccddeeff00112233445566778899aabbccddee", FileCount: 1}, req.Target, cliDigest, []release.RenderedResource{res})
		if err != nil {
			t.Fatalf("NewRenderedBundle: %v", err)
		}
		bundle = &b
		env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
		if err != nil {
			t.Fatalf("NewRolloutEnvelope: %v", err)
		}
		elig, err = release.Eligible("1", "all_mandatory_evidence_verified", "All configured mandatory evidence verified.", env)
		if err != nil {
			t.Fatalf("Eligible: %v", err)
		}
	} else {
		evidence = release.UnknownEvidence(release.UnknownEvidenceError("github_unreachable", "source", "GitHub did not answer", "Retry once GitHub is reachable."))
		elig, err = release.Indeterminate("1", "github_unreachable", "GitHub did not answer. Retry once GitHub is reachable.")
		if err != nil {
			t.Fatalf("Indeterminate: %v", err)
		}
	}

	rel, err := release.NewRelease(release.ReleaseParams{
		ID: id, Request: req, Evidence: evidence, Bundle: bundle, Eligibility: elig, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	return rel
}
