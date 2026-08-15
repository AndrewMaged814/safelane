package release

import (
	"encoding/json"
	"fmt"
)

// EvidenceOutcome is the status of evidence verification for one Release Request.
//
// The zero value is [EvidenceUnknown] deliberately: a field nobody populated, a
// struct built with a literal, a decode of a truncated record, or a verification step
// that panicked all read as "unknown". Nothing defaults to verified.
//
// There is intentionally no method converting an outcome to a boolean, a severity, or
// a risk tier. Mapping evidence status onto Release Eligibility is #50's job.
// If such a helper existed here, a future caller would eventually treat
// "!failed" as "pass".
type EvidenceOutcome uint8

const (
	// EvidenceUnknown means SafeLane could not determine the answer: GitHub or the
	// registry was unreachable, timed out, or replied unintelligibly. It is the
	// zero value. It withholds authority; it is never low risk.
	EvidenceUnknown EvidenceOutcome = iota
	// EvidenceVerified means every required check passed and a [ReleaseEvidence]
	// exists. This is the only outcome that carries evidence.
	EvidenceVerified
	// EvidenceMissing means required evidence does not exist: no approving review,
	// no check run for the merge commit.
	EvidenceMissing
	// EvidenceFailed means required evidence exists and is negative: the check
	// failed, the pull request is not merged, the approver is the author.
	EvidenceFailed
)

func (o EvidenceOutcome) String() string {
	switch o {
	case EvidenceVerified:
		return "verified"
	case EvidenceMissing:
		return "missing"
	case EvidenceFailed:
		return "failed"
	case EvidenceUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// MarshalJSON writes the outcome as its string form.
func (o EvidenceOutcome) MarshalJSON() ([]byte, error) { return json.Marshal(o.String()) }

// UnmarshalJSON reads the string form. An unrecognized value decodes to
// [EvidenceUnknown] rather than failing open.
func (o *EvidenceOutcome) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	switch s {
	case "verified":
		*o = EvidenceVerified
	case "missing":
		*o = EvidenceMissing
	case "failed":
		*o = EvidenceFailed
	default:
		*o = EvidenceUnknown
	}
	return nil
}

// EvidenceResult is the outcome of evidence verification, carried on a [Release].
//
// A Release exists even when verification did not succeed - it needs a stable
// identity so a withheld or denied decision (#50) and its proof (#52) have something
// to attach to. That makes this type the exact place where "unknown must never become
// a pass" has to be enforced, so its fields are unexported and it has no setters:
//
//   - only [VerifiedEvidence] can produce an [EvidenceVerified] result, and it
//     refuses anything that is not a constructor-built [ReleaseEvidence];
//   - [EvidenceResult.Verified] is the only way to reach the evidence, and it returns
//     ok == false for every other outcome, so there is no code path that reads
//     evidence out of a non-verified result;
//   - the zero value is unknown with no evidence and no reasons.
type EvidenceResult struct {
	outcome  EvidenceOutcome
	evidence *ReleaseEvidence
	reasons  Errors
}

// VerifiedEvidence wraps constructor-built evidence as a verified result. It fails if
// handed an unset [ReleaseEvidence], which is the only way a caller outside this
// package could try to fake one.
func VerifiedEvidence(e ReleaseEvidence) (EvidenceResult, error) {
	if e.IsZero() {
		return EvidenceResult{}, Internal("unverified_evidence",
			"attempted to record unverified evidence as verified; build it with NewReleaseEvidence")
	}
	copied := e
	return EvidenceResult{outcome: EvidenceVerified, evidence: &copied}, nil
}

// MissingEvidence records that required evidence does not exist.
func MissingEvidence(reasons ...*Error) EvidenceResult {
	return EvidenceResult{outcome: EvidenceMissing, reasons: defaultReasons(CategoryEvidenceMissing, reasons)}
}

// FailedEvidence records that required evidence exists and is negative.
func FailedEvidence(reasons ...*Error) EvidenceResult {
	return EvidenceResult{outcome: EvidenceFailed, reasons: defaultReasons(CategoryEvidenceFailed, reasons)}
}

// UnknownEvidence records that SafeLane could not determine the answer.
func UnknownEvidence(reasons ...*Error) EvidenceResult {
	return EvidenceResult{outcome: EvidenceUnknown, reasons: defaultReasons(CategoryEvidenceUnknown, reasons)}
}

func defaultReasons(category ErrorCategory, reasons []*Error) Errors {
	if len(reasons) > 0 {
		out := make(Errors, 0, len(reasons))
		for _, r := range reasons {
			if r != nil {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return Errors{newError(category, "unspecified_"+string(category), "evidence",
		"evidence verification did not succeed and no reason was recorded",
		"Report the specific evidence failure. An unexplained non-verified outcome still withholds authority.")}
}

// Outcome returns the verification outcome.
func (r EvidenceResult) Outcome() EvidenceOutcome { return r.outcome }

// Verified returns the verified evidence. ok is true only for [EvidenceVerified];
// for every other outcome it returns the zero value and false. This is the only
// accessor for the evidence, so no caller can read evidence out of a result that did
// not verify.
func (r EvidenceResult) Verified() (ReleaseEvidence, bool) {
	if r.outcome != EvidenceVerified || r.evidence == nil {
		return ReleaseEvidence{}, false
	}
	return *r.evidence, true
}

// IsVerified reports whether evidence verified.
func (r EvidenceResult) IsVerified() bool {
	_, ok := r.Verified()
	return ok
}

// Reasons returns why verification did not succeed. It is empty for a verified
// result.
func (r EvidenceResult) Reasons() Errors {
	out := make(Errors, len(r.reasons))
	copy(out, r.reasons)
	return out
}

func (r EvidenceResult) String() string {
	if r.IsVerified() {
		return "verified"
	}
	return fmt.Sprintf("%s (%d reason(s))", r.outcome, len(r.reasons))
}

type evidenceResultJSON struct {
	Outcome  EvidenceOutcome  `json:"outcome"`
	Evidence *ReleaseEvidence `json:"evidence"`
	Reasons  Errors           `json:"reasons,omitempty"`
}

// MarshalJSON writes the result.
func (r EvidenceResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(evidenceResultJSON{Outcome: r.outcome, Evidence: r.evidence, Reasons: r.reasons})
}

// UnmarshalJSON reads the result and re-enforces the invariant that only a verified
// outcome carries evidence. A record claiming "verified" with no evidence, or
// carrying evidence under any other outcome, is rejected rather than trusted.
func (r *EvidenceResult) UnmarshalJSON(data []byte) error {
	var w evidenceResultJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return Malformed("malformed_evidence_result", "evidence", "stored evidence result could not be decoded",
			"The Release record is corrupt or was written by an incompatible version.").WithCause(err)
	}
	switch {
	case w.Outcome == EvidenceVerified && (w.Evidence == nil || w.Evidence.IsZero()):
		return FailedEvidenceError("verified_without_evidence", "evidence",
			`the record claims verified evidence but carries none`,
			"Re-run verification. A verified outcome without evidence is not a pass.")
	case w.Outcome != EvidenceVerified && w.Evidence != nil && !w.Evidence.IsZero():
		return FailedEvidenceError("evidence_under_non_verified_outcome", "evidence",
			fmt.Sprintf("the record carries evidence under a %q outcome", w.Outcome),
			"Re-run verification. Evidence exists only for a verified outcome.")
	}
	if w.Outcome == EvidenceVerified {
		*r = EvidenceResult{outcome: EvidenceVerified, evidence: w.Evidence, reasons: w.Reasons}
		return nil
	}
	*r = EvidenceResult{outcome: w.Outcome, reasons: defaultReasons(categoryFor(w.Outcome), w.Reasons)}
	return nil
}

func categoryFor(o EvidenceOutcome) ErrorCategory {
	switch o {
	case EvidenceMissing:
		return CategoryEvidenceMissing
	case EvidenceFailed:
		return CategoryEvidenceFailed
	default:
		return CategoryEvidenceUnknown
	}
}
