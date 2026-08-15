package proof

import (
	"fmt"
	"strconv"
	"strings"
)

// Concise is the 10–15 second live summary: identity, artifact evidence,
// eligibility, static envelope when eligible, and pending execution/boundary.
func (p Proof) Concise() string {
	var b strings.Builder
	fmt.Fprintf(&b, "release_id: %s\n", p.releaseID)
	fmt.Fprintf(&b, "created_at: %s\n", p.createdAt.UTC().Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(&b, "application: %s  environment: %s\n", p.application, p.environment)
	fmt.Fprintf(&b, "caller: %s (%s)\n", p.caller.Identity, p.caller.Kind)
	writeArtifactConcise(&b, p.artifact)
	fmt.Fprintf(&b, "eligibility: %s\n", p.decision.Eligibility)
	fmt.Fprintf(&b, "policy_version: %s\n", p.decision.PolicyVersion)
	fmt.Fprintf(&b, "reason: %s\n", p.decision.ReasonCode)
	if p.decision.Message != "" {
		fmt.Fprintf(&b, "  %s\n", p.decision.Message)
	}
	fmt.Fprintf(&b, "retryable: %v\n", p.decision.Retryable)
	if p.decision.Envelope != nil {
		fmt.Fprintf(&b, "rollout_envelope: %s\n", joinStages(p.decision.Envelope.Stages()))
		fmt.Fprintf(&b, "next_action: %s\n", p.decision.Envelope.NextAction())
	}
	fmt.Fprintf(&b, "execution: %s\n", p.execution.Status)
	fmt.Fprintf(&b, "boundary: %s\n", p.boundary.Status)
	return b.String()
}

func writeArtifactConcise(b *strings.Builder, a Artifact) {
	fmt.Fprintf(b, "artifact: %s\n", a.Outcome)
	if a.PullRequest != nil {
		reviewer := a.PullRequest.Reviewer
		if reviewer == "" {
			fmt.Fprintf(b, "  pull request: #%d\n", a.PullRequest.Number)
		} else {
			fmt.Fprintf(b, "  pull request: #%d (approved by %s)\n", a.PullRequest.Number, reviewer)
		}
	}
	if a.Revision != "" {
		fmt.Fprintf(b, "  merge: %s\n", a.Revision)
	}
	if a.Digest != "" {
		fmt.Fprintf(b, "  digest: %s\n", a.Digest)
	}
	if a.Outcome != "verified" {
		for _, reason := range a.Reasons {
			fmt.Fprintf(b, "  - [%s] %s: %s\n", reason.Category, reason.Code, reason.Message)
		}
	}
}

// Details is the complete human-readable record: all four sections, with
// Artifact and Decision populated and Execution/Boundary explicitly pending.
func (p Proof) Details() string {
	var b strings.Builder
	fmt.Fprintf(&b, "release_id: %s\n", p.releaseID)
	fmt.Fprintf(&b, "created_at: %s\n", p.createdAt.UTC().Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(&b, "application: %s  environment: %s\n", p.application, p.environment)
	fmt.Fprintf(&b, "caller: %s (%s)\n", p.caller.Identity, p.caller.Kind)
	if p.caller.Tool != "" {
		fmt.Fprintf(&b, "  tool: %s %s\n", p.caller.Tool, p.caller.ToolVersion)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Artifact")
	fmt.Fprintf(&b, "  outcome: %s\n", p.artifact.Outcome)
	fmt.Fprintf(&b, "  sources: %s\n", strings.Join(p.artifact.Sources, ", "))
	fmt.Fprintf(&b, "  target: %s\n", p.artifact.Target)
	if p.artifact.Repository != "" {
		fmt.Fprintf(&b, "  repository: %s\n", p.artifact.Repository)
	}
	if p.artifact.Revision != "" {
		fmt.Fprintf(&b, "  merge: %s\n", p.artifact.Revision)
	}
	if pr := p.artifact.PullRequest; pr != nil {
		fmt.Fprintf(&b, "  pull request: #%d %s\n", pr.Number, pr.URL)
		fmt.Fprintf(&b, "    author: %s\n", pr.Author)
		if pr.Reviewer != "" {
			fmt.Fprintf(&b, "    reviewer: %s\n", pr.Reviewer)
		}
		fmt.Fprintf(&b, "    source: %s\n", pr.Source)
	}
	if ci := p.artifact.CI; ci != nil {
		fmt.Fprintf(&b, "  required check: %s (%s)\n", ci.Name, ci.Conclusion)
		fmt.Fprintf(&b, "    source: %s\n", ci.Source)
	}
	if p.artifact.Digest != "" {
		fmt.Fprintf(&b, "  digest: %s\n", p.artifact.Digest)
		fmt.Fprintf(&b, "    source: %s\n", p.artifact.DigestSource)
	}
	if tmpl := p.artifact.Template; tmpl != nil {
		fmt.Fprintf(&b, "  template: %s %s\n", tmpl.Name, tmpl.Version)
		fmt.Fprintf(&b, "    content_digest: %s\n", tmpl.ContentDigest)
		fmt.Fprintf(&b, "    source: safelane\n")
	}
	if p.artifact.BundleDigest != "" {
		fmt.Fprintf(&b, "  bundle_digest: %s\n", p.artifact.BundleDigest)
		fmt.Fprintf(&b, "    source: safelane\n")
	}
	for _, h := range p.artifact.BundleHashes {
		fmt.Fprintf(&b, "  hash %s: %s\n", h.Ref, h.Hash)
	}
	for _, reason := range p.artifact.Reasons {
		fmt.Fprintf(&b, "  - [%s] %s: %s\n", reason.Category, reason.Code, reason.Message)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Decision")
	fmt.Fprintf(&b, "  eligibility: %s\n", p.decision.Eligibility)
	fmt.Fprintf(&b, "  source: safelane\n")
	fmt.Fprintf(&b, "  policy_version: %s\n", p.decision.PolicyVersion)
	fmt.Fprintf(&b, "  reason: %s\n", p.decision.ReasonCode)
	if p.decision.Message != "" {
		fmt.Fprintf(&b, "    %s\n", p.decision.Message)
	}
	fmt.Fprintf(&b, "  retryable: %v\n", p.decision.Retryable)
	if p.decision.Envelope != nil {
		fmt.Fprintf(&b, "  rollout_envelope: %s\n", joinStages(p.decision.Envelope.Stages()))
		fmt.Fprintf(&b, "  next_action: %s\n", p.decision.Envelope.NextAction())
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Execution")
	fmt.Fprintf(&b, "  status: %s\n", p.execution.Status)

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Boundary")
	fmt.Fprintf(&b, "  status: %s\n", p.boundary.Status)
	return b.String()
}

func joinStages(stages []int) string {
	if len(stages) == 0 {
		return ""
	}
	parts := make([]string, len(stages))
	for i, s := range stages {
		parts[i] = strconv.Itoa(s)
	}
	return strings.Join(parts, " → ")
}
