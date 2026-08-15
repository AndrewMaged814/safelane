// Package github verifies GitHub evidence for a Release Request: that a
// pull request exists, is merged into the expected branch, that the
// submitted source revision is that merge's commit (not the PR head), that
// an approval exists from someone other than the author, and that a
// required check run succeeded for that exact merge commit SHA.
//
// This package never trusts a caller's declared claims. A Claim is what the
// caller asserts; a Facts is what SafeLane itself observed by calling the
// GitHub API. Verify only ever returns Verified when the observed Facts
// satisfy the Claim under these rules.
package github

import (
	"fmt"
	"time"
)

// Status is the outcome of verifying one piece of GitHub evidence.
type Status string

const (
	// StatusVerified means the observed facts satisfy every rule below.
	StatusVerified Status = "verified"
	// StatusRejected means SafeLane was able to observe the facts and they
	// deterministically fail a rule (unmerged, wrong repo, wrong commit,
	// self-approval, failed check, mutable check target, ...).
	StatusRejected Status = "rejected"
	// StatusUnknown means SafeLane could not determine the facts at all
	// (API error, not found, malformed response). Unknown must never be
	// treated as passing evidence by any caller of this package.
	StatusUnknown Status = "unknown"
)

// ReasonCode names why a Result is not Verified, so callers can branch on
// category instead of parsing Detail strings.
type ReasonCode string

const (
	ReasonNone                  ReasonCode = ""
	ReasonFetchFailed           ReasonCode = "fetch_failed"
	ReasonPullRequestNotFound   ReasonCode = "pull_request_not_found"
	ReasonRepositoryMismatch    ReasonCode = "repository_mismatch"
	ReasonNotMerged             ReasonCode = "not_merged"
	ReasonBaseRefMismatch       ReasonCode = "base_ref_mismatch"
	ReasonMergeCommitMismatch   ReasonCode = "merge_commit_mismatch"
	ReasonApprovalMissing       ReasonCode = "approval_missing"
	ReasonApproverIsAuthor      ReasonCode = "approver_is_author"
	ReasonRequiredCheckMissing  ReasonCode = "required_check_missing"
	ReasonRequiredCheckFailed   ReasonCode = "required_check_failed"
	ReasonRequiredCheckWrongSHA ReasonCode = "required_check_wrong_sha"
)

// Claim is what the caller declared about the reviewed change. It is
// input to verify, never authority on its own.
type Claim struct {
	// Repository is the expected "owner/repo" the pull request must target.
	Repository string
	// PullRequestNumber identifies the pull request to verify.
	PullRequestNumber int
	// ExpectedMergeCommitSHA is the source revision the caller submitted.
	// It must equal the PR's actual merge commit SHA, not its head SHA.
	ExpectedMergeCommitSHA string
	// RequiredCheckName is the check run name that must have succeeded
	// against ExpectedMergeCommitSHA specifically.
	RequiredCheckName string
	// ExpectedBaseRef is the branch the PR must have merged into, e.g. "main".
	ExpectedBaseRef string
	// SkipIndependentApproval, when true, means the operator Release Policy
	// does not require an independent PR approval. The zero value keeps
	// fail-closed required-approval behavior.
	SkipIndependentApproval bool
}

// CheckRun is one check run GitHub reports against a commit SHA.
type CheckRun struct {
	Name        string
	Conclusion  string // "success", "failure", "neutral", "cancelled", ...
	HeadSHA     string // the exact commit this run ran against
	RunID       int64
	URL         string
	CompletedAt time.Time
}

// Facts is everything SafeLane itself observed about a pull request via the
// GitHub API. Every field here is a SafeLane-verified fact, never a
// caller-declared claim.
type Facts struct {
	Repository     string
	Number         int
	URL            string
	Merged         bool
	MergedAt       time.Time
	BaseRef        string
	MergeCommitSHA string
	AuthorLogin    string
	// Approvals holds every reviewer's latest review state, after collapsing
	// multiple reviews per user to their most recent one. Only entries whose
	// State is "APPROVED" count as an approval; the others are kept so a
	// dismissal or change-request is visible rather than discarded.
	Approvals []Approval
	// CheckRuns holds check runs GitHub reported for MergeCommitSHA. Runs
	// reported against any other SHA must not appear here (see Fetcher).
	CheckRuns []CheckRun
}

// Approval is one reviewer's latest review state on the pull request.
type Approval struct {
	Reviewer   string
	State      string // "APPROVED", "CHANGES_REQUESTED", "COMMENTED", "DISMISSED"
	ApprovedAt time.Time
}

// approvedBy returns the logins whose latest review state is APPROVED, in
// the order they first appear -- the shape [Evaluate] reasons about.
func (f Facts) approvedBy() []string {
	var out []string
	for _, a := range f.Approvals {
		if a.State == "APPROVED" {
			out = append(out, a.Reviewer)
		}
	}
	return out
}

// IndependentApprover returns the first APPROVED review from someone other
// than the pull request author, matching the rule [Evaluate] enforces.
// Wiring code that has already seen StatusVerified uses this to build the
// verified approval it records; it is meaningless before that, since
// Evaluate is what proves an independent approver exists at all.
func (f Facts) IndependentApprover() (Approval, bool) {
	for _, a := range f.Approvals {
		if a.State != "APPROVED" || a.Reviewer == f.AuthorLogin {
			continue
		}
		return a, true
	}
	return Approval{}, false
}

// CheckRun returns the check run matching name, if any were reported for
// this Facts' merge commit SHA.
func (f Facts) CheckRun(name string) (CheckRun, bool) {
	for _, c := range f.CheckRuns {
		if c.Name == name {
			return c, true
		}
	}
	return CheckRun{}, false
}

// approvalFor returns the given reviewer's latest review, if any.
func (f Facts) approvalFor(reviewer string) (Approval, bool) {
	for _, a := range f.Approvals {
		if a.Reviewer == reviewer && a.State == "APPROVED" {
			return a, true
		}
	}
	return Approval{}, false
}

// Result is the typed, actionable outcome of verifying one Claim.
type Result struct {
	Status Status
	Reason ReasonCode
	Detail string
	// Facts is populated whenever SafeLane could observe them, even when
	// the result is Rejected, so callers/operators can see what was found.
	Facts *Facts
}

func rejected(reason ReasonCode, facts Facts, detailf string, args ...any) Result {
	f := facts
	return Result{Status: StatusRejected, Reason: reason, Detail: fmt.Sprintf(detailf, args...), Facts: &f}
}

func unknown(reason ReasonCode, detailf string, args ...any) Result {
	return Result{Status: StatusUnknown, Reason: reason, Detail: fmt.Sprintf(detailf, args...)}
}

func verified(facts Facts) Result {
	f := facts
	return Result{Status: StatusVerified, Facts: &f}
}

// Evaluate applies the evidence rules to already-fetched Facts. It is pure
// and takes no network dependency, so every rejection/acceptance case can be
// tested directly without an HTTP fixture.
func Evaluate(claim Claim, facts Facts) Result {
	if claim.Repository != "" && facts.Repository != claim.Repository {
		return rejected(ReasonRepositoryMismatch, facts,
			"expected repository %q, pull request targets %q", claim.Repository, facts.Repository)
	}

	if !facts.Merged {
		return rejected(ReasonNotMerged, facts, "pull request #%d is not merged", facts.Number)
	}

	if claim.ExpectedBaseRef != "" && facts.BaseRef != claim.ExpectedBaseRef {
		return rejected(ReasonBaseRefMismatch, facts,
			"expected base ref %q, pull request merged into %q", claim.ExpectedBaseRef, facts.BaseRef)
	}

	if facts.MergeCommitSHA == "" {
		return unknown(ReasonMergeCommitMismatch, "pull request #%d has no recorded merge commit SHA", facts.Number)
	}

	if claim.ExpectedMergeCommitSHA != facts.MergeCommitSHA {
		return rejected(ReasonMergeCommitMismatch, facts,
			"submitted source revision %q does not match the pull request's merge commit %q",
			claim.ExpectedMergeCommitSHA, facts.MergeCommitSHA)
	}

	approvedBy := facts.approvedBy()
	if !claim.SkipIndependentApproval {
		approverFound := false
		for _, login := range approvedBy {
			if login == facts.AuthorLogin {
				continue // self-approval never counts
			}
			approverFound = true
			break
		}
		if !approverFound {
			if len(approvedBy) == 1 && approvedBy[0] == facts.AuthorLogin {
				return rejected(ReasonApproverIsAuthor, facts,
					"pull request #%d is only approved by its own author %q", facts.Number, facts.AuthorLogin)
			}
			return rejected(ReasonApprovalMissing, facts,
				"pull request #%d has no approval from a reviewer other than the author", facts.Number)
		}
	}

	if claim.RequiredCheckName == "" {
		return unknown(ReasonRequiredCheckMissing, "no required check name was configured to verify")
	}

	var found *CheckRun
	for i := range facts.CheckRuns {
		if facts.CheckRuns[i].Name == claim.RequiredCheckName {
			found = &facts.CheckRuns[i]
			break
		}
	}
	if found == nil {
		return rejected(ReasonRequiredCheckMissing, facts,
			"required check %q was not found for merge commit %q", claim.RequiredCheckName, facts.MergeCommitSHA)
	}
	if found.HeadSHA != facts.MergeCommitSHA {
		// Defensive: Fetcher implementations must only return check runs for
		// the requested SHA, but a passing check on the PR head must never
		// satisfy this claim, so this is enforced again here.
		return rejected(ReasonRequiredCheckWrongSHA, facts,
			"required check %q ran against %q, not the merge commit %q",
			claim.RequiredCheckName, found.HeadSHA, facts.MergeCommitSHA)
	}
	if found.Conclusion != "success" {
		return rejected(ReasonRequiredCheckFailed, facts,
			"required check %q concluded %q for merge commit %q",
			claim.RequiredCheckName, found.Conclusion, facts.MergeCommitSHA)
	}

	return verified(facts)
}
