package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/AndrewMaged814/safelane/internal/assess"
)

// RecordSchemaVersion is the version of the persisted Release record shape.
//
// #50 (Release Eligibility) and #52 (proof read model) extend the record
// by *adding* sections - a new field on [Release] and a new key in its JSON - which is
// a backwards-compatible change that does not need a new version. Bump this only if
// identity, evidence or the artifact/target/bundle binding below ever changes shape.
const RecordSchemaVersion = "safelane.release.record/v2"

// ReleaseParams is the argument to [NewRelease].
type ReleaseParams struct {
	// ID must already be minted. #48 requires a stable release ID before
	// eligibility is recorded, so it is an input here rather than something
	// this constructor invents at an arbitrary point in the flow.
	ID ReleaseID
	// Intent is the caller's pruned request, recorded verbatim. Evidence collected by
	// SafeLane belongs in Evidence, never back inside this snapshot.
	Intent Intent
	// Request is the transient evidence-collection shape retained for internal
	// construction compatibility. New orchestration supplies Intent. It is converted
	// to the pruned record request and is never persisted.
	Request ReleaseRequest
	// Target is resolved from operator configuration. It is separate from Request
	// because cluster, namespace and application are not caller-authorized claims.
	Target Target
	// Caller is audit metadata owned by the transport, not part of the caller's
	// release intent. It is persisted outside request.
	Caller CallerIdentity
	// Evidence is the verification outcome. A non-verified outcome is legitimate: the
	// Release still gets a record, an ID and proof, and withholds authority.
	Evidence EvidenceResult
	// Bundle is the single rendering for this Release, or nil when evidence did not
	// verify and nothing was rendered.
	Bundle *RenderedBundle
	// Eligibility is the determination of whether this exact release may enter
	// SafeLane. Every persisted Release has one, including withheld attempts.
	Eligibility Eligibility
	// Assessment is how far this change may ship per step, and why. It is
	// present only for an eligible release: assessment is a question about a
	// change that may ship at all. The zero value means "not assessed".
	Assessment assess.Assessment
	// RecordedAssessment is used when rebuilding a persisted v2 record. Normal
	// orchestration supplies Assessment and NewRelease prunes it into this shape.
	RecordedAssessment AssessmentRecord
	// Execution is the ordered history of what happened after eligibility:
	// a granted start, a refused advance, Argo's own abort. It is present
	// only for an eligible release -- nothing may start against a release
	// that never earned a lane and an envelope. Nil means "never started".
	Execution []ExecutionEntry
	// Boundary is the live capability assertion recorded immediately before start.
	// It is absent until a rollout is started.
	Boundary Boundary
	// CreatedAt is SafeLane's own timestamp, distinct from the caller's claimed
	// metadata.submitted_at.
	CreatedAt time.Time
}

// Release is the persisted Release record: the artifact-proof half of the Complete
// Release Record.
//
// # What it binds, and why that is the point
//
// [NewRelease] refuses to assemble a Release whose parts do not agree:
//
//   - a bundle may exist only when evidence verified, so there is no such thing as a
//     rendered-and-hashed bundle sitting on a release with unknown evidence;
//   - the bundle's pinned digest must equal the verified artifact digest, so a bundle
//     rendered for one artifact cannot be recorded against another;
//   - the bundle's target must equal the operator-resolved target, so a bundle rendered for
//     one cluster/namespace cannot be recorded against another;
//   - the verified merge commit must equal the claimed merge commit, so evidence
//     verified for a different change cannot be combined with this request.
//
// Together these are the "evidence from another change or target cannot be silently
// combined" requirement, enforced at construction rather than checked by convention.
type Release struct {
	// ID is the stable release identity.
	ID ReleaseID `json:"release_id"`
	// CreatedAt is when SafeLane created the record.
	CreatedAt time.Time `json:"created_at"`

	request          Intent
	target           Target
	caller           CallerIdentity
	evidence         EvidenceResult
	bundle           *RenderedBundle
	eligibility      Eligibility
	assessment       assess.Assessment
	assessmentRecord AssessmentRecord
	execution        []ExecutionEntry
	boundary         Boundary
}

// NewRelease validates and assembles the Release record.
func NewRelease(p ReleaseParams) (*Release, error) {
	var errs Errors
	request := p.Intent
	target := p.Target
	caller := p.Caller
	legacyRequest := p.Request.SchemaVersion != ""
	assessment := p.RecordedAssessment
	if assessment.IsZero() && !p.Assessment.IsZero() {
		assessment = assessmentRecordFrom(p.Assessment)
	}
	if request == (Intent{}) && legacyRequest {
		request = Intent{
			SchemaVersion: p.Request.SchemaVersion,
			Repository:    p.Request.Source.Repository,
			PullRequest:   p.Request.Review.PullRequestNumber,
			Environment:   p.Request.Target.Environment,
			Image:         p.Request.Artifact.ImageReference,
		}
		if target.IsZero() {
			target = p.Request.Target
		}
		if caller.Identity == "" {
			caller = p.Request.Caller
		}
	}

	if err := p.ID.Validate(); err != nil {
		errs = append(errs, flatten(err)...)
	}
	if err := request.ValidateIdentity(); err != nil {
		errs = append(errs, flatten(err)...)
	}
	if err := target.Validate(); err != nil {
		errs = append(errs, flatten(err)...)
	}
	if p.CreatedAt.IsZero() {
		errs = append(errs, Internal("missing_created_at", "a Release must record when SafeLane created it"))
	}

	verified, isVerified := p.Evidence.Verified()

	if p.Eligibility.IsZero() {
		errs = append(errs, Internal("missing_eligibility",
			"a Release must record Release Eligibility before it is persisted"))
	} else {
		switch p.Eligibility.Status() {
		case EligibilityEligible:
			if !isVerified {
				errs = append(errs, Internal("eligible_without_verified_evidence",
					"an eligible release requires verified mandatory evidence"))
			}
		case EligibilityIndeterminate:
			if p.Evidence.Outcome() != EvidenceUnknown {
				errs = append(errs, Internal("indeterminate_without_unknown_evidence",
					"indeterminate eligibility is only for verification that could not be completed"))
			}
		case EligibilityIneligible:
			if isVerified {
				errs = append(errs, Internal("ineligible_with_verified_evidence",
					"verified mandatory evidence is eligible in phase one; it is not ineligible"))
			}
			if p.Evidence.Outcome() == EvidenceUnknown {
				errs = append(errs, Internal("ineligible_for_unknown_evidence",
					"unknown evidence is indeterminate, not ineligible"))
			}
		}
	}

	if isVerified {
		if legacyRequest && verified.MergeCommitSHA() != p.Request.Source.MergeCommitSHA {
			errs = append(errs, FailedEvidenceError("source_revision_mismatch", "evidence.merge_commit_sha",
				fmt.Sprintf("verified merge commit %s does not match the collected %s", verified.MergeCommitSHA(), p.Request.Source.MergeCommitSHA),
				"Verify evidence for this pull request. Evidence from another change cannot be combined with it."))
		}
		if request.Image != "" {
			claimed, err := ParseImageReference(request.Image)
			if err == nil {
				if verified.ArtifactDigest() != claimed.Digest {
					errs = append(errs, FailedEvidenceError("artifact_mismatch", "evidence.artifact.reference.digest",
						fmt.Sprintf("verified digest %s does not match the requested %s", verified.ArtifactDigest(), claimed.Digest),
						"Verify the artifact named in this request. Evidence for another artifact cannot be combined with it."))
				}
				if verified.Artifact().Reference.Repository != claimed.Repository ||
					verified.Artifact().Reference.Registry != claimed.Registry {
					errs = append(errs, FailedEvidenceError("artifact_repository_mismatch", "evidence.artifact.reference",
						fmt.Sprintf("verified artifact %s does not match the requested %s",
							verified.Artifact().Reference, claimed),
						"Resolve the digest in the repository named in the request."))
				}
			}
		}
		if repo, err := ParseRepositoryRef(request.Repository); err == nil && verified.Repository() != repo {
			errs = append(errs, FailedEvidenceError("repository_mismatch", "evidence.repository",
				fmt.Sprintf("verified repository %s does not match the requested %s", verified.Repository(), repo),
				"Verify the pull request in the repository named in the request."))
		}
	}

	// The assessment invariant, and the reason it mirrors the bundle's: a
	// risk and a lane recorded against a release that may not enter SafeLane
	// would be a width decision attached to a change that never earned one.
	// An ineligible or indeterminate release is not assessed at all -- no
	// risk, no lane, no envelope.
	if !assessment.IsZero() && p.Eligibility.Status() != EligibilityEligible {
		errs = append(errs, Internal("assessment_without_eligibility",
			fmt.Sprintf("an assessment was attached to a %s release; assessment is a question about an eligible change", p.Eligibility.Status())))
	}
	if !assessment.IsZero() && assessment.Lane == "" {
		errs = append(errs, Internal("assessment_without_lane",
			"an assessment must record the lane its risk resolved to"))
	}
	if !assessment.IsZero() {
		if err := assessment.Validate(); err != nil {
			errs = append(errs, flatten(err)...)
		}
	}

	// The execution invariant mirrors the assessment one just above: a
	// rollout cannot have started against a release that never earned a
	// lane and an envelope.
	if len(p.Execution) > 0 && p.Eligibility.Status() != EligibilityEligible {
		errs = append(errs, Internal("execution_without_eligibility",
			fmt.Sprintf("execution history was attached to a %s release; nothing may start against a release that is not eligible", p.Eligibility.Status())))
	}
	for _, entry := range p.Execution {
		if err := entry.Validate(); err != nil {
			errs = append(errs, flatten(err)...)
		}
	}
	if !p.Boundary.IsZero() {
		if p.Eligibility.Status() != EligibilityEligible {
			errs = append(errs, Internal("boundary_without_eligibility",
				"a capability assertion may be attached only to an eligible release"))
		}
		if err := p.Boundary.Validate(); err != nil {
			errs = append(errs, flatten(err)...)
		}
		if len(p.Execution) > 0 && p.Boundary.CallerCapability.AssertedAt.After(p.Execution[0].At) {
			errs = append(errs, Internal("boundary_asserted_after_start",
				"caller capability must be asserted no later than the start transition"))
		}
	}

	if p.Bundle != nil {
		if p.Bundle.IsZero() {
			errs = append(errs, Internal("unset_bundle",
				"a bundle was supplied but was never rendered; build it with the render package"))
		} else if !isVerified {
			// The single most important negative invariant: rendering happens against
			// a verified digest, so a bundle on a non-verified release would mean
			// SafeLane rendered from an unverified artifact.
			errs = append(errs, Internal("bundle_without_verified_evidence",
				fmt.Sprintf("a rendered bundle was attached to a release whose evidence is %s; rendering requires a verified digest", p.Evidence.Outcome())))
		} else {
			if p.Bundle.PinnedDigest() != verified.ArtifactDigest() {
				errs = append(errs, FailedEvidenceError("bundle_artifact_mismatch", "bundle.pinned_digest",
					fmt.Sprintf("the bundle pins %s but the verified artifact is %s", p.Bundle.PinnedDigest(), verified.ArtifactDigest()),
					"Render the bundle from the verified digest for this release."))
			}
			if p.Bundle.Target() != target {
				errs = append(errs, FailedEvidenceError("bundle_target_mismatch", "bundle.target",
					fmt.Sprintf("the bundle was rendered for %s but the release targets %s", p.Bundle.Target(), target),
					"Render the bundle for this release's target. A bundle rendered for another target cannot be recorded here."))
			}
		}
	}

	if err := errs.OrNil(); err != nil {
		return nil, err
	}

	r := &Release{
		ID:               p.ID,
		CreatedAt:        p.CreatedAt.UTC(),
		request:          request,
		target:           target,
		caller:           caller,
		evidence:         p.Evidence,
		eligibility:      p.Eligibility,
		assessment:       p.Assessment,
		assessmentRecord: assessment,
		boundary:         p.Boundary,
	}
	if len(p.Execution) > 0 {
		r.execution = append([]ExecutionEntry{}, p.Execution...)
	}
	if p.Bundle != nil {
		bundle := *p.Bundle
		r.bundle = &bundle
	}
	return r, nil
}

// Request returns the caller's submission as recorded.
func (r *Release) Request() Intent { return r.request }

// Target returns the operator-resolved release target, stored outside request.
func (r *Release) Target() Target { return r.target }

// Caller returns transport-stamped audit metadata. It is not part of request.
func (r *Release) Caller() CallerIdentity { return r.caller }

// Evidence returns the verification outcome. Read the evidence itself through
// [EvidenceResult.Verified], which yields nothing unless the outcome is verified.
func (r *Release) Evidence() EvidenceResult { return r.evidence }

// SourceRevision returns the verified merge commit SHA, or "" when evidence did not
// verify. It deliberately does not fall back to the caller's claim: a claimed revision
// is not a source revision.
func (r *Release) SourceRevision() string {
	if e, ok := r.evidence.Verified(); ok {
		return e.MergeCommitSHA()
	}
	return ""
}

// ArtifactDigest returns the verified immutable OCI digest, or "" when evidence did not
// verify.
func (r *Release) ArtifactDigest() string {
	if e, ok := r.evidence.Verified(); ok {
		return e.ArtifactDigest()
	}
	return ""
}

// Bundle returns the Release's single rendering. ok is false when nothing was rendered,
// which is the case for every release whose evidence did not verify.
//
// The bundle is returned by value and has no mutating methods, so this is a read: it
// cannot trigger a re-render and cannot alter the recorded hashes.
func (r *Release) Bundle() (RenderedBundle, bool) {
	if r.bundle == nil {
		return RenderedBundle{}, false
	}
	return *r.bundle, true
}

// Eligibility is the persisted determination of whether this release may enter
// SafeLane. It has no setters: re-running assessment cannot rewrite it.
func (r *Release) Eligibility() Eligibility { return r.eligibility }

// Assessment returns how far this change may ship per step, and why. ok is
// false when the release was not assessed, which is every release that is
// not eligible: assessment is a question about a change that may ship at all.
func (r *Release) Assessment() (assess.Assessment, bool) {
	if r.assessment.IsZero() {
		return assess.Assessment{}, false
	}
	return r.assessment, true
}

// RecordedAssessment returns Appendix C2's pruned, persisted assessment proof.
func (r *Release) RecordedAssessment() (AssessmentRecord, bool) {
	if r.assessmentRecord.IsZero() {
		return AssessmentRecord{}, false
	}
	return r.assessmentRecord, true
}

// Execution returns the release's execution history, in order. It is empty
// for a release that never started.
func (r *Release) Execution() []ExecutionEntry {
	if len(r.execution) == 0 {
		return nil
	}
	return append([]ExecutionEntry{}, r.execution...)
}

// Boundary returns the recorded capability assertion. ok is false before start.
func (r *Release) Boundary() (Boundary, bool) {
	if r.boundary.IsZero() {
		return Boundary{}, false
	}
	return r.boundary, true
}

// WithExecution returns a new Release with entry appended to the execution
// history. r itself is not mutated: a Release is otherwise immutable once
// built, and every later transition -- a granted start, a refused advance,
// Argo's own abort -- produces a new value that goes back through
// [NewRelease], so every invariant is re-checked rather than assumed still
// to hold.
func (r *Release) WithExecution(entry ExecutionEntry) (*Release, error) {
	execution := append(append([]ExecutionEntry{}, r.execution...), entry)
	var bundlePtr *RenderedBundle
	if b, ok := r.Bundle(); ok {
		bundlePtr = &b
	}
	return NewRelease(ReleaseParams{
		ID:                 r.ID,
		Intent:             r.request,
		Target:             r.target,
		Caller:             r.caller,
		Evidence:           r.evidence,
		Bundle:             bundlePtr,
		Eligibility:        r.eligibility,
		Assessment:         r.assessment,
		RecordedAssessment: r.assessmentRecord,
		Execution:          execution,
		Boundary:           r.boundary,
		CreatedAt:          r.CreatedAt,
	})
}

// WithBoundary returns a new Release carrying the live assertion made before start.
func (r *Release) WithBoundary(boundary Boundary) (*Release, error) {
	var bundlePtr *RenderedBundle
	if b, ok := r.Bundle(); ok {
		bundlePtr = &b
	}
	return NewRelease(ReleaseParams{
		ID: r.ID, Intent: r.request, Target: r.target, Caller: r.caller, Evidence: r.evidence,
		Bundle: bundlePtr, Eligibility: r.eligibility, Assessment: r.assessment, RecordedAssessment: r.assessmentRecord,
		Execution: r.execution, Boundary: boundary, CreatedAt: r.CreatedAt,
	})
}

// TemplateIdentity returns the pinned Release Template identity, if a bundle exists.
func (r *Release) TemplateIdentity() (TemplateIdentity, bool) {
	if r.bundle == nil {
		return TemplateIdentity{}, false
	}
	return r.bundle.Template(), true
}

// BundleHashes returns the per-resource content hashes recorded on the Release, in
// bundle order. It is empty when nothing was rendered.
func (r *Release) BundleHashes() []ResourceHash {
	if r.bundle == nil {
		return nil
	}
	return r.bundle.Hashes()
}

type releaseJSON struct {
	SchemaVersion string           `json:"schema_version"`
	ID            ReleaseID        `json:"release_id"`
	CreatedAt     time.Time        `json:"created_at"`
	Request       Intent           `json:"request"`
	Target        Target           `json:"target"`
	Caller        CallerIdentity   `json:"caller,omitempty"`
	Evidence      EvidenceResult   `json:"evidence"`
	Bundle        *RenderedBundle  `json:"bundle"`
	Eligibility   Eligibility      `json:"eligibility"`
	Assessment    AssessmentRecord `json:"assessment,omitempty"`
	Execution     []ExecutionEntry `json:"execution,omitempty"`
	Boundary      Boundary         `json:"boundary,omitempty"`
	Envelope      *EnvelopeRecord  `json:"envelope,omitempty"`
	Outcome       string           `json:"outcome"`
}

// MarshalJSON writes the persisted record.
func (r *Release) MarshalJSON() ([]byte, error) {
	var envelope *EnvelopeRecord
	if assessment, ok := r.RecordedAssessment(); ok {
		if bundle, hasBundle := r.Bundle(); hasBundle {
			if rollout, hasEnvelope := r.eligibility.Envelope(); hasEnvelope {
				record := NewEnvelopeRecord(assessment.Lane, rollout.Stages(), bundle.Digest())
				envelope = &record
			}
		}
	}
	return json.Marshal(releaseJSON{
		SchemaVersion: RecordSchemaVersion,
		ID:            r.ID,
		CreatedAt:     r.CreatedAt,
		Request:       r.request,
		Target:        r.target,
		Caller:        r.caller,
		Evidence:      r.evidence,
		Bundle:        r.bundle,
		Eligibility:   r.eligibility,
		Assessment:    r.assessmentRecord,
		Execution:     r.execution,
		Boundary:      r.boundary,
		Envelope:      envelope,
		Outcome:       outcomeFrom(r.execution),
	})
}

// UnmarshalJSON reads a persisted record back through [NewRelease], so every binding
// invariant is re-checked on load rather than assumed to have held when it was written.
//
// Unknown keys are tolerated on purpose: #50 and #52 add sections to this record, and
// an older reader must not reject a newer record's decision section outright.
func (r *Release) UnmarshalJSON(data []byte) error {
	var w releaseJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return Malformed("malformed_release_record", "", "the stored Release record could not be decoded",
			"The record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	if w.SchemaVersion != RecordSchemaVersion {
		return Malformed("unsupported_record_schema_version", "schema_version",
			fmt.Sprintf("record schema version %q is not supported", w.SchemaVersion),
			fmt.Sprintf("This build reads %q records.", RecordSchemaVersion))
	}
	var raw struct {
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw.Request))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&w.Request); err != nil {
		return Malformed("malformed_record_request", "request",
			"the stored request is not the pruned Release Request v2 shape",
			"Regenerate the record; v1 evidence-dossier request fields are not accepted.").WithCause(err)
	}
	built, err := NewRelease(ReleaseParams{
		ID:                 w.ID,
		Intent:             w.Request,
		Target:             w.Target,
		Caller:             w.Caller,
		Evidence:           w.Evidence,
		Bundle:             w.Bundle,
		Eligibility:        w.Eligibility,
		RecordedAssessment: w.Assessment,
		Execution:          w.Execution,
		Boundary:           w.Boundary,
		CreatedAt:          w.CreatedAt,
	})
	if err != nil {
		return err
	}
	var expectedEnvelope *EnvelopeRecord
	if assessment, ok := built.RecordedAssessment(); ok {
		if bundle, hasBundle := built.Bundle(); hasBundle {
			if rollout, hasEnvelope := built.eligibility.Envelope(); hasEnvelope {
				record := NewEnvelopeRecord(assessment.Lane, rollout.Stages(), bundle.Digest())
				expectedEnvelope = &record
			}
		}
	}
	if !reflect.DeepEqual(w.Envelope, expectedEnvelope) {
		return Internal("envelope_record_mismatch", "stored envelope does not match the assessed lane and hashed bundle")
	}
	if expected := outcomeFrom(w.Execution); w.Outcome != expected {
		return Internal("outcome_record_mismatch",
			fmt.Sprintf("stored outcome %q does not match execution outcome %q", w.Outcome, expected))
	}
	*r = *built
	return nil
}
