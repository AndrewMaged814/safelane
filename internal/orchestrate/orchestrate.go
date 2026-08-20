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

	"github.com/AndrewMaged814/safelane/internal/assess"
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

	// ChangeFacts collects the Change Facts an assessment is formed from.
	// A nil Fetcher, or one that fails, does not fail the release: both
	// verdicts record themselves unavailable and the lane falls back to
	// the policy's DefaultLane, which is the most cautious one declared.
	ChangeFacts assess.Fetcher
	// Heuristic and Model are the two assessors. Unset, they are built
	// from the Policy's own assessment configuration, which is where the
	// operator declared them. Tests substitute fakes.
	//
	// A Heuristic that returns an error is a configuration defect and
	// refuses the release (Appendix C1's third rule). A Model that
	// cannot run is expected, legitimate, and never a low verdict.
	Heuristic assess.Assessor
	Model     assess.Assessor

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

func (d Deps) heuristic() assess.Assessor {
	if d.Heuristic != nil {
		return d.Heuristic
	}
	return assess.Heuristic(d.policy().Assessment.Heuristic)
}

func (d Deps) model() assess.Assessor {
	if d.Model != nil {
		return d.Model
	}
	return assess.Model(d.policy().Assessment.Model)
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
	result, err := Submit(ctx, intent, d)
	return result.Release, err
}

// Inspection is one pass over a Release Request: the Release it produced,
// plus the two verification results it produced along the way.
//
// The Release alone cannot say *which* evidence check reached which
// answer -- it records the combined outcome and the reasons, which is the
// right shape for a record but not enough to print a per-check report.
// These two results are what `release inspect` reads to say "the merged
// commit was found, the publish check has not concluded, the digest could
// not be looked up because of the first two". They are not persisted:
// they are the working detail behind a decision the record already holds.
type Inspection struct {
	Release *release.Release
	GitHub  github.Result
	GHCR    ghcr.Result
}

// Submit is [SubmitRelease] with the verification detail kept.
func Submit(ctx context.Context, intent release.Intent, d Deps) (Inspection, error) {
	if err := intent.Validate(); err != nil {
		return Inspection{}, err
	}
	if err := d.Project.Validate(); err != nil {
		return Inspection{}, err
	}

	req, evidenceResult, verified, ghResult, ghcrResult := collectAndVerify(ctx, intent, d)
	out := Inspection{GitHub: ghResult, GHCR: ghcrResult}

	// Assessment is a question about an eligible change, so it runs only
	// when evidence verified. A release that may not enter SafeLane gets
	// no risk and no lane, and [release.NewRelease] enforces that.
	var (
		assessment assess.Assessment
		lane       policy.Lane
		err        error
	)
	if verified != nil {
		assessment, lane, err = assessRelease(ctx, intent, d)
	} else {
		_, lane, err = d.policy().LaneFor("")
	}
	if err != nil {
		return out, err
	}

	var bundlePtr *release.RenderedBundle
	envelope, err := release.NewRolloutEnvelope(lane.Weights, "start")
	if err != nil {
		return out, err
	}
	if verified != nil {
		bundle, err := render.Render(d.Template, req.Target, *verified, lane.Weights)
		if err != nil {
			return out, err
		}
		bundlePtr = &bundle
		// The enforced envelope is read back out of the manifest that was
		// hashed, not out of the lane that was resolved a moment ago. The
		// two should agree; reading the bytes is what proves it, and it is
		// what the output claims when it says "read back from the hashed
		// Rollout".
		envelope, _, err = release.DeriveEnvelope(bundle)
		if err != nil {
			return out, err
		}
	}

	id, err := d.newID()
	if err != nil {
		return out, err
	}
	req.Metadata.RequestID = "req_" + strings.TrimPrefix(string(id), "rel_")
	req.Metadata.SubmittedAt = d.now()
	req.Caller = d.caller()

	elig, err := policy.Evaluate(d.policy(), evidenceResult, envelope)
	if err != nil {
		return out, err
	}

	r, err := release.NewRelease(release.ReleaseParams{
		ID:          id,
		Intent:      intent,
		Target:      req.Target,
		Caller:      d.caller(),
		Evidence:    evidenceResult,
		Bundle:      bundlePtr,
		Eligibility: elig,
		Assessment:  assessment,
		CreatedAt:   d.now(),
	})
	if err != nil {
		return out, err
	}
	out.Release = r

	if err := d.Store.Save(r); err != nil {
		return out, release.Internal("release_not_persisted",
			fmt.Sprintf("release %s was recorded but could not be persisted: %v", r.ID, err))
	}

	return out, nil
}

// assessRelease collects the Change Facts, runs both assessors, combines
// them through [assess.Worse], and resolves the lane that risk bought.
//
// Three failure modes, three different answers:
//
//   - Change Facts cannot be collected: both verdicts record themselves
//     unavailable and the lane falls back to DefaultLane, the narrowest
//     one declared. Not being able to look at a change is not a licence
//     to ship it widely.
//   - The heuristic returns an error: that is a malformed operator
//     configuration, and the release is refused. The heuristic is not
//     optional.
//   - The model cannot run: expected, and never a low verdict. Its Risk
//     stays empty, [assess.Worse] ignores it, and the heuristic's floor
//     stands alone.
func assessRelease(ctx context.Context, intent release.Intent, d Deps) (assess.Assessment, policy.Lane, error) {
	facts, factsErr := changeFacts(ctx, intent, d)
	if factsErr != nil {
		unavailable := assess.Verdict{Available: false, Reason: factsErr.Error()}
		name, lane, err := d.policy().LaneFor("")
		if err != nil {
			return assess.Assessment{}, policy.Lane{}, err
		}
		return assess.Combine(facts, unavailable, unavailable, name), lane, nil
	}

	heuristic, err := d.heuristic().Assess(ctx, facts)
	if err != nil {
		return assess.Assessment{}, policy.Lane{}, release.Invalid("heuristic_failed", "assessment.heuristic",
			err.Error(),
			"Correct policy.yml's assessment.heuristic block. The heuristic is not optional; SafeLane will not fall back to a lane it cannot justify.")
	}

	// The model verdict never fails the release: Assess reports an
	// unavailable assessor through the Verdict, not through an error.
	model, _ := d.model().Assess(ctx, facts)

	risk := assess.Worse(heuristic.Risk, model.Risk)
	name, lane, err := d.policy().LaneFor(string(risk))
	if err != nil {
		return assess.Assessment{}, policy.Lane{}, err
	}
	return assess.Combine(facts, heuristic, model, name), lane, nil
}

func changeFacts(ctx context.Context, intent release.Intent, d Deps) (assess.Facts, error) {
	if d.ChangeFacts == nil {
		return assess.Facts{}, fmt.Errorf("no change-facts collector is configured")
	}
	repoName := intent.Repository
	if repoName == "" {
		repoName = d.Project.Repository.Name
	}
	repo, err := release.ParseRepositoryRef(repoName)
	if err != nil {
		return assess.Facts{}, err
	}
	return d.ChangeFacts.FetchChangeFacts(ctx, repo.Owner, repo.Name, intent.PullRequest)
}

func collectAndVerify(ctx context.Context, intent release.Intent, d Deps) (release.ReleaseRequest, release.EvidenceResult, *release.ReleaseEvidence, github.Result, ghcr.Result) {
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
			`Use "owner/name".`)), nil, github.Result{}, ghcr.Result{}
	}

	facts, fetchErr := d.GitHub.FetchPullRequestFacts(ctx, repo.Owner, repo.Name, intent.PullRequest)
	var ghResult github.Result
	if fetchErr != nil {
		ghResult = github.Result{Status: github.StatusUnknown, Reason: fetchReason(fetchErr), Detail: fetchErr.Error()}
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
			return req, evidenceResultFromError(err), nil, ghResult, ghcrResult
		}
		result, err := release.VerifiedEvidence(evidence)
		if err != nil {
			return req, release.UnknownEvidence(release.Internal("evidence_wrap_failed", err.Error())), nil, ghResult, ghcrResult
		}
		return req, result, &evidence, ghResult, ghcrResult
	}

	return req, combineNonVerified(ghResult, ghcrResult), nil, ghResult, ghcrResult
}

// fetchReason classifies a GitHub transport failure. A rate limit is
// called what it is, because Appendix C4 makes it retryable and a caller
// that reads "github_unreachable" would give up on something that fixes
// itself in minutes.
func fetchReason(err error) github.ReasonCode {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "rate limit"):
		return github.ReasonRateLimited
	case strings.Contains(msg, "not found"):
		return github.ReasonPullRequestNotFound
	default:
		return github.ReasonFetchFailed
	}
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
// and/or GHCR did not both verify.
//
// Unknown outranks failed, which outranks missing: if a check could not be
// determined at all, the combined outcome is unknown, never a milder,
// more specific-sounding result. There is one exception, and it is the
// difference between N4 reading "ineligible" and reading "retry me
// forever": a *definite* GitHub rejection outranks an unknown GHCR,
// because digest resolution is downstream of the merge commit. When
// GitHub says the pull request never merged, the registry was never asked
// a question it could have answered -- reporting that as "we could not
// tell" would invite a retry of something that will not change.
//
// The exception is deliberately narrow. It applies only when GitHub
// rejected, never when GitHub itself is unknown.
func combineNonVerified(gh github.Result, gr ghcr.Result) release.EvidenceResult {
	var reasons release.Errors
	if e := githubResultError(gh); e != nil {
		reasons = append(reasons, e)
	}
	if e := ghcrResultError(gr); e != nil {
		reasons = append(reasons, e)
	}

	if gh.Status == github.StatusRejected {
		if isGithubMissingReason(gh.Reason) {
			return release.MissingEvidence(reasons...)
		}
		return release.FailedEvidence(reasons...)
	}

	if gh.Status == github.StatusUnknown || gr.Status == ghcr.StatusUnknown {
		return release.UnknownEvidence(reasons...)
	}

	// ghcr has no "missing" case: an unresolvable reference is unknown, not missing.
	if gr.Status == ghcr.StatusRejected {
		return release.FailedEvidence(reasons...)
	}
	// Unreachable in practice: both Verified takes the other branch in
	// collectAndVerify. Unknown is the safest fallback if it is ever hit.
	return release.UnknownEvidence(reasons...)
}

func githubResultError(r github.Result) *release.Error {
	switch r.Status {
	case github.StatusVerified:
		return nil
	case github.StatusUnknown:
		return release.UnknownEvidenceError(reasonCode(r.Reason), githubReasonFieldName(r.Reason), r.Detail,
			"Re-run verification once GitHub can answer for this pull request.")
	default: // StatusRejected
		field, missing := githubReasonField(r.Reason)
		if missing {
			return release.MissingEvidenceError(reasonCode(r.Reason), field, r.Detail,
				"Provide the missing evidence: an approving review from someone other than the author, or the required check run for the merge commit SHA.")
		}
		return release.FailedEvidenceError(reasonCode(r.Reason), field, r.Detail,
			"Correct the pull request, review, or CI evidence named above and resubmit.")
	}
}

// reasonCode maps a verification reason onto Appendix C4's catalogue.
//
// The two vocabularies are not the same and should not be merged. A
// verify package names what it observed ("not_merged"); the catalogue
// names what a caller has to do about it ("pull_request_not_merged",
// ineligible, not retryable). Most reasons already agree; the ones that
// do not are here, once, rather than spelled differently at each site
// that prints or records them.
func reasonCode(r github.ReasonCode) string {
	switch r {
	case github.ReasonNotMerged:
		return "pull_request_not_merged"
	case github.ReasonFetchFailed:
		return "github_unreachable"
	case github.ReasonRequiredCheckIncomplete:
		return "verification_incomplete"
	default:
		return string(r)
	}
}

func githubReasonFieldName(r github.ReasonCode) string {
	field, _ := githubReasonField(r)
	return field
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
	case github.ReasonRequiredCheckFailed, github.ReasonRequiredCheckWrongSHA,
		github.ReasonRequiredCheckIncomplete:
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
		// Appendix C4 splits these: an image that is simply not published
		// yet is digest_not_found and worth retrying in a minute, while a
		// registry that will not talk is ghcr_unreachable. Both are
		// indeterminate; only the wording tells an agent which wait it is
		// in for.
		code := "digest_not_found"
		if r.Reason != ghcr.ReasonResolveFailed {
			code = "ghcr_unreachable"
		}
		return release.UnknownEvidenceError(code, "artifact.image_reference", r.Detail,
			"Re-run verification once the image for this merge commit is published.")
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
