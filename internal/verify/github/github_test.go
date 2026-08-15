package github

import (
	"testing"
	"time"
)

func baseFacts() Facts {
	return Facts{
		Repository:     "acme/podinfo",
		Number:         42,
		Merged:         true,
		MergedAt:       time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		BaseRef:        "main",
		MergeCommitSHA: "merge-sha-1",
		AuthorLogin:    "andrew",
		Approvals:      []Approval{{Reviewer: "ahmed", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)}},
		CheckRuns: []CheckRun{
			{Name: "publish", Conclusion: "success", HeadSHA: "merge-sha-1"},
		},
	}
}

func baseClaim() Claim {
	return Claim{
		Repository:             "acme/podinfo",
		PullRequestNumber:      42,
		ExpectedMergeCommitSHA: "merge-sha-1",
		RequiredCheckName:      "publish",
		ExpectedBaseRef:        "main",
	}
}

func TestEvaluate_Verified(t *testing.T) {
	got := Evaluate(baseClaim(), baseFacts())
	if got.Status != StatusVerified {
		t.Fatalf("want Verified, got %+v", got)
	}
	if got.Facts == nil {
		t.Fatalf("want Facts populated on a verified result")
	}
}

func TestEvaluate_RepositoryMismatch(t *testing.T) {
	facts := baseFacts()
	facts.Repository = "someone-else/podinfo"
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonRepositoryMismatch)
}

func TestEvaluate_NotMerged(t *testing.T) {
	facts := baseFacts()
	facts.Merged = false
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonNotMerged)
}

func TestEvaluate_BaseRefMismatch(t *testing.T) {
	facts := baseFacts()
	facts.BaseRef = "release-branch"
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonBaseRefMismatch)
}

func TestEvaluate_MergeCommitMismatch_PRHeadIsNotEnough(t *testing.T) {
	claim := baseClaim()
	claim.ExpectedMergeCommitSHA = "pr-head-sha-not-merge-commit"
	got := Evaluate(claim, baseFacts())
	assertRejected(t, got, ReasonMergeCommitMismatch)
}

func TestEvaluate_ApprovalMissing(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = nil
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonApprovalMissing)
}

func TestEvaluate_ApprovalMissing_SkippedWhenPolicyDoesNotRequireIt(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = nil
	claim := baseClaim()
	claim.SkipIndependentApproval = true
	got := Evaluate(claim, facts)
	if got.Status != StatusVerified {
		t.Fatalf("want Verified when independent approval is not required, got %+v", got)
	}
}

func TestEvaluate_ApproverIsAuthor(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = []Approval{{Reviewer: "andrew", State: "APPROVED"}} // same as AuthorLogin
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonApproverIsAuthor)
}

func TestEvaluate_ApproverIsAuthor_AmongOthers_StillCountsOtherApprover(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = []Approval{
		{Reviewer: "andrew", State: "APPROVED"}, // author's own stale approval
		{Reviewer: "ahmed", State: "APPROVED"},  // plus a real one
	}
	got := Evaluate(baseClaim(), facts)
	if got.Status != StatusVerified {
		t.Fatalf("a non-author approval among others must still verify, got %+v", got)
	}
}

func TestEvaluate_RequiredCheckMissing(t *testing.T) {
	facts := baseFacts()
	facts.CheckRuns = nil
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonRequiredCheckMissing)
}

func TestEvaluate_RequiredCheckFailed(t *testing.T) {
	facts := baseFacts()
	facts.CheckRuns = []CheckRun{{Name: "publish", Conclusion: "failure", HeadSHA: "merge-sha-1"}}
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonRequiredCheckFailed)
}

func TestEvaluate_RequiredCheckOnlyOnPRHead_NotMergeCommit(t *testing.T) {
	// A passing check recorded against the PR head SHA alone must not
	// satisfy the claim; only a check run recorded for the merge commit does.
	facts := baseFacts()
	facts.CheckRuns = []CheckRun{{Name: "publish", Conclusion: "success", HeadSHA: "pr-head-sha"}}
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonRequiredCheckWrongSHA)
}

func TestEvaluate_NoRequiredCheckConfigured_IsUnknownNotPassing(t *testing.T) {
	claim := baseClaim()
	claim.RequiredCheckName = ""
	got := Evaluate(claim, baseFacts())
	if got.Status != StatusUnknown {
		t.Fatalf("missing required-check configuration must be unknown, not %v", got.Status)
	}
}

func TestFacts_IndependentApprover_SkipsAuthorsOwnApproval(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = []Approval{
		{Reviewer: "andrew", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)}, // author's own
		{Reviewer: "ahmed", State: "APPROVED", ApprovedAt: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)},
	}
	got, ok := facts.IndependentApprover()
	if !ok || got.Reviewer != "ahmed" {
		t.Fatalf("want ahmed as the independent approver, got %+v (ok=%v)", got, ok)
	}
}

func TestFacts_IndependentApprover_NoneWhenOnlyAuthorApproved(t *testing.T) {
	facts := baseFacts()
	facts.Approvals = []Approval{{Reviewer: "andrew", State: "APPROVED"}}
	if _, ok := facts.IndependentApprover(); ok {
		t.Fatal("want no independent approver when only the author approved")
	}
}

func TestFacts_CheckRun_FindsByName(t *testing.T) {
	facts := baseFacts()
	got, ok := facts.CheckRun("publish")
	if !ok || got.Conclusion != "success" {
		t.Fatalf("want the publish check run, got %+v (ok=%v)", got, ok)
	}
	if _, ok := facts.CheckRun("does-not-exist"); ok {
		t.Fatal("want no match for an unknown check name")
	}
}

func assertRejected(t *testing.T, got Result, reason ReasonCode) {
	t.Helper()
	if got.Status != StatusRejected {
		t.Fatalf("want Rejected(%s), got %+v", reason, got)
	}
	if got.Reason != reason {
		t.Fatalf("want reason %s, got %s (%s)", reason, got.Reason, got.Detail)
	}
}
