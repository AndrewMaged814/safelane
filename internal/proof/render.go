package proof

import (
	"fmt"
	"strconv"
	"strings"
)

func (p Proof) Concise() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release %s: %s\n", p.releaseID, p.outcome)
	if p.artifact.PullRequest != nil {
		fmt.Fprintf(&b, "  change       %s#%d (%s)\n", p.artifact.Repository, p.artifact.PullRequest.Number, p.artifact.Revision)
	}
	fmt.Fprintf(&b, "  eligibility  %s\n", p.decision.Eligibility)
	if p.assessment != nil {
		fmt.Fprintf(&b, "  risk / lane  %s / %s\n", p.assessment.Risk, p.assessment.Lane)
		fmt.Fprintf(&b, "  authority    %d%%\n", p.assessment.AuthorizedUntil)
	}
	if p.decision.Envelope != nil {
		fmt.Fprintf(&b, "  progression  %s\n", joinStages(p.decision.Envelope.Stages()))
	}
	if len(p.execution) > 0 {
		last := p.execution[len(p.execution)-1]
		fmt.Fprintf(&b, "  last event   %s at %s\n", last.Verb, last.At.UTC().Format("2006-01-02 15:04:05Z"))
	}
	fmt.Fprintln(&b, "  details      rerun with --details")
	return b.String()
}

// Details renders Appendix A3.5's sections using recorded values only.
func (p Proof) Details() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release Proof                             %s\n\n", p.releaseID)
	fmt.Fprintln(&b, "ARTIFACT")
	if p.artifact.Repository != "" {
		fmt.Fprintf(&b, "  repository        %s\n", p.artifact.Repository)
	}
	if pr := p.artifact.PullRequest; pr != nil {
		fmt.Fprintf(&b, "  pull request      #%d, merged into %s\n", pr.Number, pr.BaseBranch)
	}
	if p.artifact.Revision != "" {
		fmt.Fprintf(&b, "  merge commit      %s\n", p.artifact.Revision)
	}
	if ci := p.artifact.CI; ci != nil {
		fmt.Fprintf(&b, "  required check    %s (%s)\n", ci.Name, ci.Conclusion)
	}
	if p.artifact.Image != "" {
		fmt.Fprintf(&b, "  image             %s\n", p.artifact.Image)
	}
	if p.artifact.Template != nil {
		fmt.Fprintf(&b, "  bundle            %d resources, template digest %s\n", len(p.artifact.BundleHashes), p.artifact.Template.ContentDigest)
	}
	for _, reason := range p.artifact.Reasons {
		fmt.Fprintf(&b, "  evidence          [%s] %s: %s\n", reason.Category, reason.Code, reason.Message)
	}

	fmt.Fprintln(&b, "\nASSESSMENT")
	if a := p.assessment; a != nil {
		fmt.Fprintf(&b, "  change            %d files, +%d −%d\n", a.Facts.FilesChanged, a.Facts.Additions, a.Facts.Deletions)
		if a.AssessmentMode != "" {
			fmt.Fprintf(&b, "  mode              %s\n", a.AssessmentMode)
		}
		if a.Facts.AgentAuthored {
			fmt.Fprintf(&b, "  authored by       agent (%s)\n", a.Facts.AgentEvidence)
		}
		fmt.Fprintf(&b, "  heuristic         %s", a.Heuristic.Risk)
		if len(a.Heuristic.Rules) > 0 {
			fmt.Fprintf(&b, "   rules: %s", strings.Join(a.Heuristic.Rules, ", "))
		}
		fmt.Fprintln(&b)
		if a.Model.Available {
			fmt.Fprintf(&b, "  model (%s)    %s     %q\n", a.Model.Assessor, a.Model.Risk, a.Model.Rationale)
		} else {
			fmt.Fprintf(&b, "  model             unavailable (%s); guarded fallback\n", a.Model.Reason)
		}
		fmt.Fprintf(&b, "  combined by       %s\n  risk              %s\n  lane              %s\n  authority ceiling %d%%\n", a.CombinedBy, a.Risk, a.Lane, a.AuthorizedUntil)
		if len(a.Facts.RuntimeAssertions) > 0 {
			fmt.Fprintf(&b, "  assertions        %s\n", strings.Join(a.Facts.RuntimeAssertions, ", "))
		}
		for _, hazard := range a.Model.Hazards {
			coverage := "uncovered"
			if hazard.Covered {
				coverage = "covered"
			}
			fmt.Fprintf(&b, "  hazard %-10s %s (%s, %s)\n", hazard.ID, hazard.FailureMode, hazard.Severity, coverage)
			fmt.Fprintf(&b, "                    surface %s; assertion %s\n", hazard.AffectedSurface, hazard.RequiredAssertion)
		}
	} else {
		fmt.Fprintln(&b, "  unavailable")
	}

	fmt.Fprintln(&b, "\nDECISION")
	fmt.Fprintf(&b, "  eligibility       %s (policy %s)\n  evidence          %s\n", p.decision.Eligibility, p.decision.PolicyVersion, p.artifact.Outcome)
	if p.artifact.Outcome == "verified" {
		fmt.Fprintf(&b, "                    %d verified, %d failed, %d unavailable\n",
			p.artifact.Evidence.Verified, p.artifact.Evidence.Failed, p.artifact.Evidence.Unavailable)
	}
	if p.decision.Envelope != nil {
		stages := p.decision.Envelope.Stages()
		fmt.Fprintf(&b, "  envelope          %s, %d gates\n", joinStages(stages), len(stages)-1)
		if p.artifact.BundleDigest != "" {
			fmt.Fprintf(&b, "                    read from the hashed bundle, digest %s\n", p.artifact.BundleDigest)
		}
	}

	fmt.Fprintln(&b, "\nEXECUTION")
	if len(p.execution) == 0 {
		fmt.Fprintln(&b, "  pending")
	}
	for _, e := range p.execution {
		fmt.Fprintf(&b, "  %s  %-10s", e.At.UTC().Format("15:04:05Z"), e.Verb)
		if e.RequestedWeight != 0 {
			fmt.Fprintf(&b, " weight %-5d", e.RequestedWeight)
		} else {
			fmt.Fprint(&b, "              ")
		}
		outcome := string(e.Outcome)
		if e.Outcome == "refused" {
			outcome = "REFUSED"
		}
		fmt.Fprintf(&b, " %s", outcome)
		if e.ReasonCode != "" {
			fmt.Fprintf(&b, "  %s", e.ReasonCode)
		}
		fmt.Fprintln(&b)
		if e.Analysis != "" || e.Detail != "" {
			fmt.Fprintf(&b, "                         %s", e.Analysis)
			if e.Analysis != "" && e.Detail != "" {
				fmt.Fprint(&b, ": ")
			}
			fmt.Fprintln(&b, e.Detail)
		}
	}

	fmt.Fprintln(&b, "\nBOUNDARY")
	if p.boundary == nil {
		fmt.Fprintln(&b, "  pending")
	} else {
		fmt.Fprintf(&b, "  controller identity   %s  (from controller.kubeconfig)\n  caller identity       %s\n", shortServiceAccount(p.boundary.ControllerIdentity), shortServiceAccount(p.boundary.CallerIdentity))
		fmt.Fprintf(&b, "  caller capability     get rollouts: %s | patch rollouts: %s\n", yesNo(p.boundary.CallerCapability.GetRollouts), yesNo(p.boundary.CallerCapability.PatchRollouts))
		fmt.Fprintf(&b, "                        asserted by %s at %s\n", p.boundary.CallerCapability.Method, p.boundary.CallerCapability.AssertedAt.UTC().Format("15:04:05Z"))
	}
	fmt.Fprintf(&b, "\nOUTCOME  %s\n", p.outcome)
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func shortServiceAccount(identity string) string {
	parts := strings.Split(identity, ":")
	if len(parts) == 4 && parts[0] == "system" && parts[1] == "serviceaccount" {
		return "sa/" + parts[3]
	}
	return identity
}

func joinStages(stages []int) string {
	parts := make([]string, len(stages))
	for i, stage := range stages {
		parts[i] = strconv.Itoa(stage)
	}
	return strings.Join(parts, " → ")
}
