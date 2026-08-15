package github

import "testing"

func baseFacts() Facts {
	return Facts{
		Repository:     "acme/podinfo",
		Number:          42,
		Merged:          true,
		BaseRef:         "main",
		MergeCommitSHA:  "merge-sha-1",
		AuthorLogin:     "andrew",
		ApprovedBy:      []string{"ahmed"},
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
	facts.ApprovedBy = nil
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonApprovalMissing)
}

func TestEvaluate_ApproverIsAuthor(t *testing.T) {
	facts := baseFacts()
	facts.ApprovedBy = []string{"andrew"} // same as AuthorLogin
	got := Evaluate(baseClaim(), facts)
	assertRejected(t, got, ReasonApproverIsAuthor)
}

func TestEvaluate_ApproverIsAuthor_AmongOthers_StillCountsOtherApprover(t *testing.T) {
	facts := baseFacts()
	facts.ApprovedBy = []string{"andrew", "ahmed"} // author's own stale approval plus a real one
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

func assertRejected(t *testing.T, got Result, reason ReasonCode) {
	t.Helper()
	if got.Status != StatusRejected {
		t.Fatalf("want Rejected(%s), got %+v", reason, got)
	}
	if got.Reason != reason {
		t.Fatalf("want reason %s, got %s (%s)", reason, got.Reason, got.Detail)
	}
}
