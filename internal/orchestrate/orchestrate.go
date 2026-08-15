// Package orchestrate is the release-intake orchestration boundary
// described in #47/#48: it validates a Release Request, verifies GitHub
// and GHCR evidence against the real world rather than the caller's
// claims, renders the operator-owned bundle exactly once when evidence
// verifies, records Release Eligibility, and persists the Release.
//
// This is the one orchestration path every transport uses. The CLI calls
// SubmitRelease directly; a later HTTP API or MCP adapter must call the
// same function rather than reimplementing any part of this sequence, so
// discovery changes without release semantics changing (#47, "Transport
// scope").
package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AndrewMaged814/safelane/internal/intake"
	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/AndrewMaged814/safelane/internal/verify/github"
)

// Store persists a completed Release record. [github.com/AndrewMaged814/safelane/internal/store.FileStore]
// is the real implementation; tests supply an in-memory fake.
type Store interface {
	Save(r *release.Release) error
}

// Deps are the external seams SubmitRelease depends on. Production wiring
// supplies a real *github.Client, a real *ghcr.Client, the operator's
// loaded Release Template, and a real Store. Tests supply fixtures for
// all four, plus a fixed Now/NewID so output is deterministic.
type Deps struct {
	GitHub   github.Fetcher
	GHCR     ghcr.Resolver
	Template render.Template
	Store    Store
	// Policy is the operator-owned Release Policy. The zero value uses
	// [policy.Default], the compiled phase-one policy.
	Policy policy.Policy

	// Now and NewID default to time.Now and release.MintReleaseID. Tests
	// override them for deterministic output; production wiring leaves
	// them unset.
	Now   func() time.Time
	NewID func() (release.ReleaseID, error)
}

func (d Deps) policy() policy.Policy {
	if d.Policy.Version == "" {
		return policy.Default()
	}
	return d.Policy
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) newID() (release.ReleaseID, error) {
	if d.NewID != nil {
		return d.NewID()
	}
	return release.MintReleaseID()
}

// SubmitRelease runs one Release Request through intake, verification,
// rendering and persistence.
//
// # What produces a Release record, and what does not
//
// A rejection at intake - malformed JSON, a forbidden field, a
// structurally invalid value - never reaches this far and never produces
// a Release: there is nothing yet to bind an ID to, and #48 requires
// exactly that request-identity-and-evidence-only boundary to reject such
// requests outright.
//
// Everything past intake produces a persisted Release, including evidence
// that is missing, failed, or unknown. A withheld or denied release still
// needs a stable identity so eligibility (#50) and proof (#52) have
// something to attach to. Only a verified release additionally carries a
// RenderedBundle: rendering happens exactly once, against the verified
// digest, and the resulting bundle is what SubmitRelease persists,
// reusing nothing about it afterward.
//
// If a rendering failure follows verified evidence - an operator template
// defect, most likely, since verified evidence is a precondition Render
// itself enforces - SubmitRelease returns that error and persists nothing.
// A broken operator template is a system misconfiguration to fix and
// resubmit against, not a release outcome to record.
func SubmitRelease(ctx context.Context, raw []byte, d Deps) (*release.Release, error) {
	req, err := intake.Parse(raw)
	if err != nil {
		return nil, err
	}

	evidenceResult, verified := verifyEvidence(ctx, req, d)

	var bundlePtr *release.RenderedBundle
	if verified != nil {
		bundle, err := render.Render(d.Template, req.Target, *verified)
		if err != nil {
			return nil, err
		}
		bundlePtr = &bundle
	}

	id, err := d.newID()
	if err != nil {
		return nil, err
	}

	elig, err := policy.Evaluate(d.policy(), evidenceResult)
	if err != nil {
		return nil, err
	}

	r, err := release.NewRelease(release.ReleaseParams{
		ID:          id,
		Request:     req,
		Evidence:    evidenceResult,
		Bundle:      bundlePtr,
		Eligibility: elig,
		CreatedAt:   d.now(),
	})
	if err != nil {
		return nil, err
	}

	if err := d.Store.Save(r); err != nil {
		return nil, release.Internal("release_not_persisted",
			fmt.Sprintf("release %s was recorded but could not be persisted: %v", r.ID, err))
	}

	return r, nil
}

// verifyEvidence checks GitHub and GHCR evidence and returns the combined
// EvidenceResult for the Release record. The returned *release.ReleaseEvidence
// is non-nil only when both checks verified; it is what Render renders
// against.
func verifyEvidence(ctx context.Context, req release.ReleaseRequest, d Deps) (release.EvidenceResult, *release.ReleaseEvidence) {
	// req has already passed intake.Parse -> ReleaseRequest.Validate, which
	// calls both of these successfully. A failure here would be a defect in
	// that validation, not a caller problem.
	repo, repoErr := req.Repository()
	imageRef, imageErr := req.ImageReference()
	if repoErr != nil || imageErr != nil {
		return release.UnknownEvidence(release.Internal("unverifiable_request",
			"the request passed intake validation but its repository or image reference could not be re-parsed")), nil
	}

	ghClaim := github.Claim{
		Repository:              repo.String(),
		PullRequestNumber:       req.Review.PullRequestNumber,
		ExpectedMergeCommitSHA:  req.Source.MergeCommitSHA,
		RequiredCheckName:       req.CI.CheckName,
		ExpectedBaseRef:         req.Source.BaseBranch,
		SkipIndependentApproval: !d.policy().IndependentPRApprovalRequired,
	}
	ghResult := github.Verify(ctx, d.GitHub, ghClaim, repo.Owner, repo.Name)

	// #48 defines no operator config for "which registry repository should
	// this application's artifact come from" - the only source of truth is
	// the claim itself, so the expected registry/repository is derived from
	// the same reference being verified. The binding check therefore cannot
	// fail today; it exists so a later ticket that introduces a real
	// per-application registry policy has somewhere to plug in an
	// independent expectation without changing this call site.
	ghcrClaim := ghcr.Claim{
		ExpectedRegistry:   imageRef.Registry,
		ExpectedRepository: imageRef.Repository,
		Reference:          imageRef,
	}
	ghcrResult := ghcr.Verify(ctx, d.GHCR, ghcrClaim)

	if ghResult.Status == github.StatusVerified && ghcrResult.Status == ghcr.StatusVerified {
		evidence, err := buildReleaseEvidence(req, ghResult, ghcrResult, d.now())
		if err != nil {
			return evidenceResultFromError(err), nil
		}
		result, err := release.VerifiedEvidence(evidence)
		if err != nil {
			return release.UnknownEvidence(release.Internal("evidence_wrap_failed", err.Error())), nil
		}
		return result, &evidence
	}

	return combineNonVerified(ghResult, ghcrResult), nil
}

// buildReleaseEvidence assembles release.EvidenceInput from verified GitHub
// Facts and the GHCR-resolved digest. It is only called when both checks
// returned StatusVerified, so the check-run lookup below is guaranteed to
// find what Evaluate already proved exists. An independent approver is
// present only when the Release Policy required one.
func buildReleaseEvidence(req release.ReleaseRequest, gh github.Result, gr ghcr.Result, now time.Time) (release.ReleaseEvidence, error) {
	facts := *gh.Facts

	approver, hasApprover := facts.IndependentApprover()
	approval := release.VerifiedApproval{}
	if hasApprover {
		approval = release.VerifiedApproval{
			Reviewer:   approver.Reviewer,
			ApprovedAt: approver.ApprovedAt,
		}
	}
	check, _ := facts.CheckRun(req.CI.CheckName)

	repo, _ := req.Repository()
	imageRef, _ := req.ImageReference()

	return release.NewReleaseEvidence(release.EvidenceInput{
		Repository: repo,
		PullRequest: release.VerifiedPullRequest{
			Number:     facts.Number,
			URL:        facts.URL,
			Author:     facts.AuthorLogin,
			BaseBranch: facts.BaseRef,
			MergedAt:   facts.MergedAt,
		},
		Approval:       approval,
		MergeCommitSHA: facts.MergeCommitSHA,
		RequiredCheck: release.VerifiedCheckRun{
			Name:        check.Name,
			HeadSHA:     check.HeadSHA,
			Conclusion:  check.Conclusion,
			RunID:       check.RunID,
			URL:         check.URL,
			CompletedAt: check.CompletedAt,
		},
		Artifact: release.VerifiedArtifact{
			Reference:      imageRef,
			ObservedDigest: gr.ResolvedDigest,
			ResolvedAt:     now,
		},
		VerifiedAt: now,
	})
}

// evidenceResultFromError maps a release.NewReleaseEvidence rejection onto
// the matching EvidenceResult bucket, so a defect caught this late (for
// example a malformed timestamp) still withholds authority through the
// right category instead of defaulting to "failed".
func evidenceResultFromError(err error) release.EvidenceResult {
	var errs release.Errors
	if !errors.As(err, &errs) {
		errs = release.Errors{release.Internal("evidence_construction_failed", err.Error())}
	}
	switch release.Categorize(err) {
	case release.CategoryEvidenceUnknown:
		return release.UnknownEvidence(errs...)
	case release.CategoryEvidenceMissing:
		return release.MissingEvidence(errs...)
	default:
		return release.FailedEvidence(errs...)
	}
}

// combineNonVerified builds the EvidenceResult for the case where GitHub
// and/or GHCR did not both verify. Unknown outranks failed, which outranks
// missing: if either check could not be determined at all, the combined
// outcome is unknown, never a milder, more specific-sounding result.
func combineNonVerified(gh github.Result, gr ghcr.Result) release.EvidenceResult {
	var reasons release.Errors
	if e := githubResultError(gh); e != nil {
		reasons = append(reasons, e)
	}
	if e := ghcrResultError(gr); e != nil {
		reasons = append(reasons, e)
	}

	if gh.Status == github.StatusUnknown || gr.Status == ghcr.StatusUnknown {
		return release.UnknownEvidence(reasons...)
	}

	ghMissing := gh.Status == github.StatusRejected && isGithubMissingReason(gh.Reason)
	ghFailed := gh.Status == github.StatusRejected && !isGithubMissingReason(gh.Reason)
	grFailed := gr.Status == ghcr.StatusRejected // ghcr has no "missing" case: an unresolvable reference is unknown, not missing.

	switch {
	case ghFailed || grFailed:
		return release.FailedEvidence(reasons...)
	case ghMissing:
		return release.MissingEvidence(reasons...)
	default:
		// Unreachable in practice: both Verified takes the other branch in
		// verifyEvidence. Unknown is the safest fallback if it is ever hit.
		return release.UnknownEvidence(reasons...)
	}
}

func githubResultError(r github.Result) *release.Error {
	switch r.Status {
	case github.StatusVerified:
		return nil
	case github.StatusUnknown:
		return release.UnknownEvidenceError(string(r.Reason), "source", r.Detail,
			"Re-run verification once GitHub is reachable and the pull request can be found.")
	default: // StatusRejected
		field, missing := githubReasonField(r.Reason)
		if missing {
			return release.MissingEvidenceError(string(r.Reason), field, r.Detail,
				"Provide the missing evidence: an approving review from someone other than the author, or the required check run for the merge commit SHA.")
		}
		return release.FailedEvidenceError(string(r.Reason), field, r.Detail,
			"Correct the pull request, review, or CI evidence named above and resubmit.")
	}
}

func githubReasonField(reason github.ReasonCode) (field string, missing bool) {
	switch reason {
	case github.ReasonApprovalMissing:
		return "review.approver", true
	case github.ReasonRequiredCheckMissing:
		return "ci.check_name", true
	case github.ReasonPullRequestNotFound:
		return "review.pull_request_number", true
	case github.ReasonApproverIsAuthor:
		return "review.approver", false
	case github.ReasonRequiredCheckFailed, github.ReasonRequiredCheckWrongSHA:
		return "ci.check_name", false
	case github.ReasonMergeCommitMismatch:
		return "source.merge_commit_sha", false
	case github.ReasonNotMerged, github.ReasonBaseRefMismatch:
		return "source.base_branch", false
	case github.ReasonRepositoryMismatch:
		return "source.repository", false
	default:
		return "source", false
	}
}

func ghcrResultError(r ghcr.Result) *release.Error {
	switch r.Status {
	case ghcr.StatusVerified:
		return nil
	case ghcr.StatusUnknown:
		return release.UnknownEvidenceError(string(r.Reason), "artifact.image_reference", r.Detail,
			"Re-run verification once the registry is reachable.")
	default: // StatusRejected
		return release.FailedEvidenceError(string(r.Reason), "artifact.image_reference", r.Detail,
			"Resolve the correct immutable digest in the expected repository and resubmit.")
	}
}

func isGithubMissingReason(reason github.ReasonCode) bool {
	switch reason {
	case github.ReasonApprovalMissing, github.ReasonRequiredCheckMissing, github.ReasonPullRequestNotFound:
		return true
	default:
		return false
	}
}
