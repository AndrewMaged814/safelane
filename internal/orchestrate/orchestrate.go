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
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
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
	// Project is the operator-owned runtime configuration. Required.
	Project project.Config
	// Caller is stamped by the CLI or other transport. The zero value
	// becomes safelane-cli / agent.
	Caller release.CallerIdentity
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

func (d Deps) caller() release.CallerIdentity {
	if d.Caller.Identity != "" {
		return d.Caller
	}
	return release.CallerIdentity{Identity: "safelane-cli", Kind: release.CallerAgent, Tool: "safelane"}
}

// SubmitRelease runs one Release Intent through collection, verification,
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
func SubmitRelease(ctx context.Context, intent release.Intent, d Deps) (*release.Release, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	if err := d.Project.Validate(); err != nil {
		return nil, err
	}

	req, evidenceResult, verified := collectAndVerify(ctx, intent, d)

	// No assessment is wired into this path yet -- collecting Change
	// Facts and running the heuristic/model assessors is a later
	// ticket's caller. An empty risk resolves to the policy's
	// DefaultLane, which is deliberately the most cautious configured
	// lane (Appendix C1's third rule): "no assessment available" is an
	// expected, legitimate case here, not a defect.
	_, lane, err := d.policy().LaneFor("")
	if err != nil {
		return nil, err
	}

	var bundlePtr *release.RenderedBundle
	if verified != nil {
		bundle, err := render.Render(d.Template, req.Target, *verified, lane.Weights)
		if err != nil {
			return nil, err
		}
		bundlePtr = &bundle
	}

	id, err := d.newID()
	if err != nil {
		return nil, err
	}
	req.Metadata.RequestID = "req_" + strings.TrimPrefix(string(id), "rel_")
	req.Metadata.SubmittedAt = d.now()
	req.Caller = d.caller()

	// The same weights just rendered are what gets recorded: one lane
	// resolution feeds both, so the enforced envelope cannot silently
	// disagree with what was actually rendered and hashed.
	envelope, err := release.NewRolloutEnvelope(lane.Weights, "start")
	if err != nil {
		return nil, err
	}
	elig, err := policy.Evaluate(d.policy(), evidenceResult, envelope)
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

func collectAndVerify(ctx context.Context, intent release.Intent, d Deps) (release.ReleaseRequest, release.EvidenceResult, *release.ReleaseEvidence) {
	repoName := intent.Repository
	if repoName == "" {
		repoName = d.Project.Repository.Name
	}
	env := intent.Environment
	if env == "" {
		env = d.Project.Release.Environment
	}

	req := release.ReleaseRequest{
		SchemaVersion: release.RequestSchemaVersion,
		Target:        d.Project.ReleaseTarget(env),
		Source: release.ClaimedSource{
			Repository: repoName,
			BaseBranch: d.Project.Repository.DefaultBranch,
		},
		Review: release.ClaimedReview{PullRequestNumber: intent.PullRequest},
		CI:     release.ClaimedCI{CheckName: d.Project.Release.RequiredCheck, Workflow: d.Project.Release.RequiredCheck},
		Caller: d.caller(),
		Metadata: release.RequestMetadata{
			RequestID:   "req_pending",
			SubmittedAt: d.now(),
		},
	}

	repo, err := release.ParseRepositoryRef(repoName)
	if err != nil {
		return req, release.UnknownEvidence(release.Invalid("malformed_repository", "repository",
			fmt.Sprintf("%q is not a repository reference", repoName),
			`Use "owner/name".`)), nil
	}

	facts, fetchErr := d.GitHub.FetchPullRequestFacts(ctx, repo.Owner, repo.Name, intent.PullRequest)
	var ghResult github.Result
	if fetchErr != nil {
		reason := github.ReasonFetchFailed
		if strings.Contains(strings.ToLower(fetchErr.Error()), "not found") {
			reason = github.ReasonPullRequestNotFound
		}
		ghResult = github.Result{Status: github.StatusUnknown, Reason: reason, Detail: fetchErr.Error()}
	} else {
		req.Source.MergeCommitSHA = facts.MergeCommitSHA
		req.Review.PullRequestURL = facts.URL
		req.Review.Author = facts.AuthorLogin
		if appr, ok := facts.IndependentApprover(); ok {
			req.Review.Approver = appr.Reviewer
		}
		if check, ok := facts.CheckRun(d.Project.Release.RequiredCheck); ok {
			req.CI.RunID = check.RunID
			req.CI.RunURL = check.URL
		}
		ghResult = github.Evaluate(github.Claim{
			Repository:              repo.String(),
			PullRequestNumber:       intent.PullRequest,
			ExpectedMergeCommitSHA:  facts.MergeCommitSHA,
			RequiredCheckName:       d.Project.Release.RequiredCheck,
			ExpectedBaseRef:         d.Project.Repository.DefaultBranch,
			SkipIndependentApproval: !d.policy().IndependentPRApprovalRequired,
		}, facts)
	}

	ghcrResult, imageRef := resolveArtifact(ctx, intent, d, req.Source.MergeCommitSHA)
	if !imageRef.IsZero() {
		req.Artifact.ImageReference = imageRef.String()
	}

	if ghResult.Status == github.StatusVerified && ghcrResult.Status == ghcr.StatusVerified {
		evidence, err := buildReleaseEvidence(req, ghResult, ghcrResult, d.now())
		if err != nil {
			return req, evidenceResultFromError(err), nil
		}
		result, err := release.VerifiedEvidence(evidence)
		if err != nil {
			return req, release.UnknownEvidence(release.Internal("evidence_wrap_failed", err.Error())), nil
		}
		return req, result, &evidence
	}

	return req, combineNonVerified(ghResult, ghcrResult), nil
}

func resolveArtifact(ctx context.Context, intent release.Intent, d Deps, mergeSHA string) (ghcr.Result, release.ImageReference) {
	registry, repository, err := project.ParseImageRepository(d.Project.Release.ImageRepository)
	if err != nil {
		return ghcr.Result{Status: ghcr.StatusUnknown, Reason: ghcr.ReasonResolveFailed, Detail: err.Error()}, release.ImageReference{}
	}

	var digest string
	switch {
	case intent.Image != "":
		pin, pinErr := release.ParseImageReference(intent.Image)
		if pinErr != nil {
			return ghcr.Result{Status: ghcr.StatusRejected, Reason: ghcr.ReasonRepositoryMismatch, Detail: pinErr.Error()}, release.ImageReference{}
		}
		if pin.Registry != registry || pin.Repository != repository {
			return ghcr.Result{
				Status: ghcr.StatusRejected,
				Reason: ghcr.ReasonRepositoryMismatch,
				Detail: fmt.Sprintf("pinned image %s is not in configured repository %s/%s", pin, registry, repository),
			}, release.ImageReference{}
		}
		digest = pin.Digest
	case mergeSHA != "":
		tag := project.ImageTag(d.Project.Release.ImageTag, mergeSHA)
		resolved, resErr := d.GHCR.ResolveTag(ctx, repository, tag)
		if resErr != nil {
			return ghcr.Result{Status: ghcr.StatusUnknown, Reason: ghcr.ReasonResolveFailed, Detail: resErr.Error()}, release.ImageReference{}
		}
		digest = resolved
	default:
		return ghcr.Result{Status: ghcr.StatusUnknown, Reason: ghcr.ReasonResolveFailed, Detail: "no merge commit SHA to resolve an image tag"}, release.ImageReference{}
	}

	ref := release.ImageReference{Registry: registry, Repository: repository, Digest: digest}
	return ghcr.Verify(ctx, d.GHCR, ghcr.Claim{
		ExpectedRegistry:   registry,
		ExpectedRepository: repository,
		Reference:          ref,
	}), ref
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
