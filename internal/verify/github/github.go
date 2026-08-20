// Package github verifies GitHub evidence for a Release Request: that a
// pull request exists, is merged into the expected branch, that the
// submitted source revision is that merge's commit (not the PR head), and that
// required check runs succeeded for that exact merge commit SHA.
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
	// failed check, mutable check target, ...).
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
	ReasonRequiredCheckMissing  ReasonCode = "required_check_missing"
	ReasonRequiredCheckFailed   ReasonCode = "required_check_failed"
	ReasonRequiredCheckWrongSHA ReasonCode = "required_check_wrong_sha"
	// ReasonRequiredCheckIncomplete means the required check run exists but
	// has not concluded yet. It is unknown, never rejected: a check that is
	// still running has not said no, and a release waiting on one is worth
	// retrying rather than refused.
	ReasonRequiredCheckIncomplete ReasonCode = "required_check_incomplete"
	// ReasonRateLimited means GitHub refused to answer because the token's
	// (or the anonymous) quota is exhausted. Unknown, and retryable.
	ReasonRateLimited ReasonCode = "rate_limited"
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
	RequiredCheckName  string
	RequiredCheckNames []string
	// ExpectedBaseRef is the branch the PR must have merged into, e.g. "main".
	ExpectedBaseRef string
}

// CheckRun is one check run GitHub reports against a commit SHA.
type CheckRun struct {
	Name string
	// Status is the run's lifecycle state: "queued", "in_progress" or
	// "completed". A run that has not completed has no conclusion yet, and
	// a missing conclusion is not a failed one.
	Status      string
	Conclusion  string // "success", "failure", "neutral", "cancelled", ...
	HeadSHA     string // the exact commit this run ran against
	RunID       int64
	URL         string
	StartedAt   time.Time
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
	// CheckRuns holds check runs GitHub reported for MergeCommitSHA. Runs
	// reported against any other SHA must not appear here (see Fetcher).
	CheckRuns []CheckRun
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

// unknownWith is unknown for the case where SafeLane did observe the pull
// request and simply cannot conclude from it yet -- a check run that has
// not finished, say. The facts are carried through because they are real:
// the merge commit was found, and a report that dropped them would say
// SafeLane knows nothing when it knows most of it.
func unknownWith(reason ReasonCode, facts Facts, detailf string, args ...any) Result {
	r := unknown(reason, detailf, args...)
	f := facts
	r.Facts = &f
	return r
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

	names := claim.RequiredCheckNames
	if len(names) == 0 && claim.RequiredCheckName != "" {
		names = []string{claim.RequiredCheckName}
	}
	if len(names) == 0 {
		return unknown(ReasonRequiredCheckMissing, "no required check name was configured to verify")
	}

	for _, name := range names {
		var found *CheckRun
		for i := range facts.CheckRuns {
			if facts.CheckRuns[i].Name == name {
				found = &facts.CheckRuns[i]
				break
			}
		}
		if found == nil {
			return unknownWith(ReasonRequiredCheckMissing, facts,
				"required check %q was not found for merge commit %q", name, facts.MergeCommitSHA)
		}
		if found.HeadSHA != facts.MergeCommitSHA {
			// Defensive: Fetcher implementations must only return check runs for
			// the requested SHA, but a passing check on the PR head must never
			// satisfy this claim, so this is enforced again here.
			return rejected(ReasonRequiredCheckWrongSHA, facts,
				"required check %q ran against %q, not the merge commit %q",
				name, found.HeadSHA, facts.MergeCommitSHA)
		}
		// A run that has not concluded is unknown, not rejected. Collapsing
		// "still running" into "failed" would turn a release worth retrying in
		// forty seconds into one that is refused outright.
		if found.Status != "" && found.Status != "completed" {
			return unknownWith(ReasonRequiredCheckIncomplete, facts,
				"required check %q is %s for merge commit %q",
				name, found.Status, facts.MergeCommitSHA)
		}
		if found.Conclusion == "" {
			return unknownWith(ReasonRequiredCheckIncomplete, facts,
				"required check %q has not concluded for merge commit %q",
				name, facts.MergeCommitSHA)
		}
		if found.Conclusion != "success" {
			return rejected(ReasonRequiredCheckFailed, facts,
				"required check %q concluded %q for merge commit %q",
				name, found.Conclusion, facts.MergeCommitSHA)
		}
	}

	return verified(facts)
}
