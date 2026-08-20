package release

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CheckConclusionSuccess is the only check-run conclusion SafeLane accepts as
// verified CI evidence.
const CheckConclusionSuccess = "success"

// VerifiedPullRequest is a pull request SafeLane read from GitHub and found merged
// into the expected base branch of the expected repository.
type VerifiedPullRequest struct {
	Number     int       `json:"number"`
	URL        string    `json:"url"`
	Author     string    `json:"author"`
	BaseBranch string    `json:"base_branch"`
	MergedAt   time.Time `json:"merged_at"`
}

// VerifiedCheckRun is a check run SafeLane read from GitHub.
//
// HeadSHA is the commit the check actually ran against. [NewReleaseEvidence] requires
// it to equal the merge commit SHA: a green check on the pull request head is not CI
// evidence for the merged revision, and this is the one place that distinction can be
// enforced structurally.
type VerifiedCheckRun struct {
	Name        string    `json:"name"`
	HeadSHA     string    `json:"head_sha"`
	Conclusion  string    `json:"conclusion"`
	RunID       int64     `json:"run_id,omitempty"`
	URL         string    `json:"url,omitempty"`
	CompletedAt time.Time `json:"completed_at"`
}

// VerifiedArtifact is an OCI artifact SafeLane resolved in the registry.
//
// Reference is the immutable reference SafeLane verified. ObservedDigest is the
// digest the registry reported (GHCR's docker-content-digest response header).
// [NewReleaseEvidence] requires the two to match; recording both is what makes
// "verified" mean "we looked" rather than "we assumed".
type VerifiedArtifact struct {
	Reference      ImageReference `json:"reference"`
	ObservedDigest string         `json:"observed_digest"`
	ResolvedAt     time.Time      `json:"resolved_at"`
}

// EvidenceInput is the argument to [NewReleaseEvidence]. Its fields are exported
// because a verification step must be able to fill them in; the resulting
// [ReleaseEvidence] is not mutable in the same way.
type EvidenceInput struct {
	Repository     RepositoryRef
	PullRequest    VerifiedPullRequest
	MergeCommitSHA string
	RequiredCheck  VerifiedCheckRun
	RequiredChecks []VerifiedCheckRun
	Artifact       VerifiedArtifact
	VerifiedAt     time.Time
}

// ReleaseEvidence is the verified evidence for one Release Request: every field
// records something SafeLane observed against GitHub or the registry, never something
// a caller declared.
//
// # Why the fields are unexported
//
// A [ReleaseEvidence] is a security claim. If its fields were exported, any package
// could take a legitimately verified value, copy it, change the merge commit SHA or
// the digest, and hand the result to [NewRelease] - the value would still look
// verified. Unexported fields plus a validating constructor mean the only ways to
// obtain a non-zero ReleaseEvidence outside this package are [NewReleaseEvidence] and
// JSON decoding, and both run the same invariant checks.
//
// # Invariants guaranteed by construction
//
//   - repository, pull request, merge commit, required check and artifact
//     are all present;
//   - the merge commit SHA is a full 40-hex object id;
//   - the required check ran against the merge commit SHA, not the pull request head;
//   - the required check concluded "success";
//   - the artifact reference is an immutable sha256 digest, and the digest the
//     registry reported matches it.
//
// Anything that cannot satisfy these is not evidence. It is a rejection - see
// [FailedEvidenceError], [MissingEvidenceError] and [UnknownEvidenceError] - carried
// on an [EvidenceResult].
type ReleaseEvidence struct {
	validated      bool
	repository     RepositoryRef
	pullRequest    VerifiedPullRequest
	mergeCommitSHA string
	requiredCheck  VerifiedCheckRun
	requiredChecks []VerifiedCheckRun
	artifact       VerifiedArtifact
	verifiedAt     time.Time
}

// NewReleaseEvidence validates and builds verified evidence. It is the only
// constructor; a verification step is expected to call it exactly once, after every
// external check has passed.
func NewReleaseEvidence(in EvidenceInput) (ReleaseEvidence, error) {
	var errs Errors

	if in.Repository.IsZero() {
		errs = append(errs, MissingEvidenceError("missing_repository", "evidence.repository",
			"no verified repository", "Verify the pull request against the expected repository before building evidence."))
	}
	if in.PullRequest.Number <= 0 || in.PullRequest.Author == "" {
		errs = append(errs, MissingEvidenceError("missing_pull_request", "evidence.pull_request",
			"no verified merged pull request", "Verify that the pull request exists and is merged into the expected base branch."))
	}
	if in.PullRequest.MergedAt.IsZero() {
		errs = append(errs, FailedEvidenceError("pull_request_not_merged", "evidence.pull_request.merged_at",
			"the pull request is not merged", "Merge the pull request. An unmerged change is not release evidence."))
	}
	if !IsCommitSHA(in.MergeCommitSHA) {
		errs = append(errs, FailedEvidenceError("malformed_merge_commit_sha", "evidence.merge_commit_sha",
			fmt.Sprintf("%q is not a full 40-character commit SHA", in.MergeCommitSHA),
			"Record the merge commit produced by the pull request on the base branch."))
	}
	checks := in.RequiredChecks
	if len(checks) == 0 && in.RequiredCheck.Name != "" {
		checks = []VerifiedCheckRun{in.RequiredCheck}
	}
	if len(checks) == 0 {
		errs = append(errs, MissingEvidenceError("missing_required_check", "evidence.required_check",
			"no required check run", "Verify the required check run for the merge commit SHA."))
	}
	for _, check := range checks {
		if check.Conclusion != CheckConclusionSuccess {
			errs = append(errs, FailedEvidenceError("required_check_not_successful", "evidence.required_check.conclusion",
				fmt.Sprintf("required check %q concluded %q", check.Name, check.Conclusion),
				`Only a "success" conclusion is CI evidence. A failed, cancelled, skipped or in-progress check is not.`))
		}
		if check.HeadSHA != in.MergeCommitSHA {
			errs = append(errs, FailedEvidenceError("check_run_commit_mismatch", "evidence.required_check.head_sha",
				fmt.Sprintf("required check %q ran against %q, not the merge commit %q", check.Name, check.HeadSHA, in.MergeCommitSHA),
				"Verify the required check run for the merge commit SHA. A passing check on the pull request head does not satisfy this."))
		}
	}
	if in.Artifact.Reference.IsZero() {
		errs = append(errs, MissingEvidenceError("missing_artifact", "evidence.artifact.reference",
			"no verified artifact", "Resolve the immutable OCI digest in the expected registry repository."))
	} else {
		if !IsContentDigest(in.Artifact.Reference.Digest) {
			errs = append(errs, FailedEvidenceError("mutable_artifact_reference", "evidence.artifact.reference.digest",
				fmt.Sprintf("%q is not a sha256 digest", in.Artifact.Reference.Digest),
				"Bind the release to an immutable digest. Tags are mutable and are never evidence."))
		}
		if in.Artifact.ObservedDigest != in.Artifact.Reference.Digest {
			errs = append(errs, FailedEvidenceError("artifact_digest_mismatch", "evidence.artifact.observed_digest",
				fmt.Sprintf("registry reported %q for reference digest %q", in.Artifact.ObservedDigest, in.Artifact.Reference.Digest),
				"Re-resolve the manifest digest in the expected repository. A mismatch means the reference does not identify the artifact."))
		}
		if in.Artifact.ResolvedAt.IsZero() {
			errs = append(errs, UnknownEvidenceError("artifact_not_resolved", "evidence.artifact.resolved_at",
				"the artifact digest was never resolved against the registry",
				"Resolve the digest against the registry. An unresolved reference is unknown, not verified."))
		}
	}
	if in.VerifiedAt.IsZero() {
		errs = append(errs, UnknownEvidenceError("missing_verification_timestamp", "evidence.verified_at",
			"no verification timestamp", "Stamp the time verification completed."))
	}

	if err := errs.OrNil(); err != nil {
		return ReleaseEvidence{}, err
	}

	return ReleaseEvidence{
		validated:      true,
		repository:     in.Repository,
		pullRequest:    in.PullRequest,
		mergeCommitSHA: in.MergeCommitSHA,
		requiredCheck:  checks[0],
		requiredChecks: append([]VerifiedCheckRun(nil), checks...),
		artifact:       in.Artifact,
		verifiedAt:     in.VerifiedAt.UTC(),
	}, nil
}

// Repository returns the verified GitHub repository.
func (e ReleaseEvidence) Repository() RepositoryRef { return e.repository }

// PullRequest returns the verified merged pull request.
func (e ReleaseEvidence) PullRequest() VerifiedPullRequest { return e.pullRequest }

// MergeCommitSHA returns the verified source revision: the merge commit on the base
// branch. This, not the pull request head, is SafeLane's source identity.
func (e ReleaseEvidence) MergeCommitSHA() string { return e.mergeCommitSHA }

// RequiredCheck returns the verified required check run for the merge commit SHA.
func (e ReleaseEvidence) RequiredCheck() VerifiedCheckRun { return e.requiredCheck }
func (e ReleaseEvidence) RequiredChecks() []VerifiedCheckRun {
	return append([]VerifiedCheckRun(nil), e.requiredChecks...)
}

// Artifact returns the verified immutable artifact.
func (e ReleaseEvidence) Artifact() VerifiedArtifact { return e.artifact }

// ArtifactDigest returns the verified immutable OCI digest. This is the only digest
// the renderer will pin into a pod template.
func (e ReleaseEvidence) ArtifactDigest() string { return e.artifact.Reference.Digest }

// VerifiedAt returns when verification completed.
func (e ReleaseEvidence) VerifiedAt() time.Time { return e.verifiedAt }

// IsZero reports whether this is the unset zero value rather than verified evidence.
func (e ReleaseEvidence) IsZero() bool { return !e.validated }

// evidenceJSON is the wire shape of ReleaseEvidence.
type evidenceJSON struct {
	Repository     RepositoryRef       `json:"repository"`
	PullRequest    VerifiedPullRequest `json:"pull_request"`
	MergeCommitSHA string              `json:"merge_commit_sha"`
	RequiredCheck  *VerifiedCheckRun   `json:"required_check,omitempty"`
	RequiredChecks []VerifiedCheckRun  `json:"required_checks,omitempty"`
	Artifact       VerifiedArtifact    `json:"artifact"`
	VerifiedAt     time.Time           `json:"verified_at"`
}

// MarshalJSON writes verified evidence. An unset value marshals to null, never to a
// hollow object that could be mistaken for evidence.
func (e ReleaseEvidence) MarshalJSON() ([]byte, error) {
	if !e.validated {
		return []byte("null"), nil
	}
	return json.Marshal(evidenceJSON{
		Repository:     e.repository,
		PullRequest:    e.pullRequest,
		MergeCommitSHA: e.mergeCommitSHA,
		RequiredChecks: e.requiredChecks,
		Artifact:       e.artifact,
		VerifiedAt:     e.verifiedAt,
	})
}

// UnmarshalJSON re-runs every invariant through [NewReleaseEvidence].
//
// This matters: a persisted Release is reloaded by #50 and #52, and an edited or
// forged record must not become verified evidence just because it round-tripped
// through storage.
func (e *ReleaseEvidence) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*e = ReleaseEvidence{}
		return nil
	}
	var w evidenceJSON
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w); err != nil {
		return Malformed("malformed_evidence_record", "evidence", "stored evidence could not be decoded",
			"The Release record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	built, err := NewReleaseEvidence(EvidenceInput{
		Repository:     w.Repository,
		PullRequest:    w.PullRequest,
		MergeCommitSHA: w.MergeCommitSHA,
		RequiredCheck: func() VerifiedCheckRun {
			if w.RequiredCheck != nil {
				return *w.RequiredCheck
			}
			return VerifiedCheckRun{}
		}(),
		RequiredChecks: w.RequiredChecks,
		Artifact:       w.Artifact,
		VerifiedAt:     w.VerifiedAt,
	})
	if err != nil {
		return err
	}
	*e = built
	return nil
}
