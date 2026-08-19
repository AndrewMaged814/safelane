package policy_test

import (
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/release"
)

const (
	mergeSHA = "4f0c1b9e7ac2d5386b1d9f4a5c8e2b7d3a6f0e91"
	digestA  = "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
)

var testTime = time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

func phaseOnePolicy() policy.Policy {
	return policy.Policy{
		Version:                       "2",
		IndependentPRApprovalRequired: true,
		Lanes: map[string]policy.Lane{
			"fast":     {Weights: []int{5, 100}},
			"standard": {Weights: []int{5, 25, 50, 100}},
			"guarded":  {Weights: []int{1, 5, 25, 50, 100}},
		},
		RiskToLane: map[string]string{
			"low": "fast", "medium": "standard", "high": "guarded",
		},
		DefaultLane: "guarded",
	}
}

// testEnvelope resolves risk against phaseOnePolicy() and builds the
// RolloutEnvelope Evaluate expects to have already been resolved --
// standing in for what SubmitRelease does with Policy.LaneFor before
// calling Evaluate.
func testEnvelope(t *testing.T, risk string) release.RolloutEnvelope {
	t.Helper()
	_, lane, err := phaseOnePolicy().LaneFor(risk)
	if err != nil {
		t.Fatalf("LaneFor(%q): %v", risk, err)
	}
	env, err := release.NewRolloutEnvelope(lane.Weights, "start")
	if err != nil {
		t.Fatalf("NewRolloutEnvelope: %v", err)
	}
	return env
}

func verifiedEvidence(t *testing.T) release.EvidenceResult {
	t.Helper()
	ev, err := release.NewReleaseEvidence(release.EvidenceInput{
		Repository: release.RepositoryRef{Owner: "AndrewMaged814", Name: "podinfo"},
		PullRequest: release.VerifiedPullRequest{
			Number: 1, URL: "https://github.com/AndrewMaged814/podinfo/pull/1",
			Author: "AndrewMaged814", BaseBranch: "main", MergedAt: testTime,
		},
		Approval:       release.VerifiedApproval{Reviewer: "ahmed", ApprovedAt: testTime},
		MergeCommitSHA: mergeSHA,
		RequiredCheck: release.VerifiedCheckRun{
			Name: "publish / build-and-push", HeadSHA: mergeSHA,
			Conclusion: release.CheckConclusionSuccess, RunID: 16453210987, CompletedAt: testTime,
		},
		Artifact: release.VerifiedArtifact{
			Reference:      release.ImageReference{Registry: "ghcr.io", Repository: "andrewmaged814/podinfo", Digest: digestA},
			ObservedDigest: digestA,
			ResolvedAt:     testTime,
		},
		VerifiedAt: testTime,
	})
	if err != nil {
		t.Fatalf("NewReleaseEvidence: %v", err)
	}
	result, err := release.VerifiedEvidence(ev)
	if err != nil {
		t.Fatalf("VerifiedEvidence: %v", err)
	}
	return result
}

func TestEvaluate_MissingRequiredEvidence_IsIneligibleNotIndeterminate(t *testing.T) {
	evidence := release.MissingEvidence(release.MissingEvidenceError(
		"approval_missing", "review.approver",
		"pull request has no independent approval",
		"Obtain an approving review from someone other than the author."))

	got, err := policy.Evaluate(phaseOnePolicy(), evidence, testEnvelope(t, "high"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if got.Status() != release.EligibilityIneligible {
		t.Fatalf("status = %s, want ineligible (verification completed; a requirement was missing)", got.Status())
	}
	if got.Retryable() {
		t.Error("a missing requirement is not retryable")
	}
	if _, ok := got.Envelope(); ok {
		t.Error("ineligible must not attach a rollout envelope")
	}
}

func TestEvaluate_UnreachableDependency_IsIndeterminateAndRetryable(t *testing.T) {
	evidence := release.UnknownEvidence(release.UnknownEvidenceError(
		"github_unreachable", "source",
		"GitHub did not answer",
		"Re-run verification once GitHub is reachable."))

	got, err := policy.Evaluate(phaseOnePolicy(), evidence, testEnvelope(t, "high"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if got.Status() != release.EligibilityIndeterminate {
		t.Fatalf("status = %s, want indeterminate", got.Status())
	}
	if !got.Retryable() {
		t.Error("indeterminate must be retryable")
	}
	if _, ok := got.Envelope(); ok {
		t.Error("indeterminate must not attach a rollout envelope")
	}
	if got.ReasonCode() == "" || got.Message() == "" {
		t.Error("indeterminate must record a reason code and actionable message")
	}
}

func TestEvaluate_FailedRequirement_IsIneligibleWithoutEnvelope(t *testing.T) {
	evidence := release.FailedEvidence(release.FailedEvidenceError(
		"required_check_failed", "ci.check_name",
		"required check publish / build-and-push concluded failure",
		"Correct the publish workflow on the merge commit and resubmit."))

	got, err := policy.Evaluate(phaseOnePolicy(), evidence, testEnvelope(t, "high"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if got.Status() != release.EligibilityIneligible {
		t.Fatalf("status = %s, want ineligible", got.Status())
	}
	if got.Retryable() {
		t.Error("ineligible is not retryable; the requirement must be corrected")
	}
	if _, ok := got.Envelope(); ok {
		t.Error("ineligible must not attach a rollout envelope")
	}
	if got.ReasonCode() == "" || got.Message() == "" {
		t.Error("ineligible must record a reason code and actionable message")
	}
	if got.PolicyVersion() != "2" {
		t.Errorf("policy version = %q, want 2", got.PolicyVersion())
	}
}

func TestEvaluate_VerifiedMandatoryEvidence_IsEligibleWithTheResolvedEnvelope(t *testing.T) {
	got, err := policy.Evaluate(phaseOnePolicy(), verifiedEvidence(t), testEnvelope(t, "medium"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	if got.Status() != release.EligibilityEligible {
		t.Fatalf("status = %s, want eligible", got.Status())
	}
	if got.PolicyVersion() != "2" {
		t.Errorf("policy version = %q, want 2", got.PolicyVersion())
	}
	if got.Retryable() {
		t.Error("an eligible release is not retryable")
	}
	env, ok := got.Envelope()
	if !ok {
		t.Fatal("eligible must attach the resolved rollout envelope")
	}
	if want := []int{5, 25, 50, 100}; !intSlicesEqual(env.Stages(), want) {
		t.Errorf("stages = %v, want %v (the standard lane, for risk medium)", env.Stages(), want)
	}
	if env.NextAction() != "start" {
		t.Errorf("next action = %q, want start", env.NextAction())
	}
	if got.ReasonCode() == "" || got.Message() == "" {
		t.Error("eligible still records a reason code and message")
	}
}

// TestEvaluate_NoRiskAvailable_UsesTheDefaultLane is Appendix C1's third
// rule made concrete for the eligibility path: missing, malformed, or
// failed assessment resolves to the operator's most cautious configured
// lane, and this is not itself a reason to withhold eligibility.
func TestEvaluate_NoRiskAvailable_UsesTheDefaultLane(t *testing.T) {
	got, err := policy.Evaluate(phaseOnePolicy(), verifiedEvidence(t), testEnvelope(t, ""))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got.Status() != release.EligibilityEligible {
		t.Fatalf("status = %s, want eligible: no risk available must never block a release", got.Status())
	}
	env, ok := got.Envelope()
	if !ok {
		t.Fatal("eligible must still attach an envelope")
	}
	if want := []int{1, 5, 25, 50, 100}; !intSlicesEqual(env.Stages(), want) {
		t.Errorf("stages = %v, want %v (guarded, the default lane)", env.Stages(), want)
	}
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
