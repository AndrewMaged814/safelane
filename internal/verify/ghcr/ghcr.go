// Package ghcr verifies that a caller-submitted OCI image reference targets
// the expected registry repository and resolves to the claimed digest —
// using the public GHCR anonymous flow: request a pull-scoped token, issue a
// manifest HEAD with an OCI index Accept header, and compare
// docker-content-digest.
//
// Verified working against a public GHCR package on 2026-08-15 (see #48).
//
// Reference parsing and immutable-digest rejection are not this package's
// job: [release.ParseImageReference] is the single parser, and intake
// (via [release.ReleaseRequest.Validate]) rejects a mutable or malformed
// reference before verification is ever attempted. This package only asks
// "does this already-valid reference resolve where we expect?".
package ghcr

import (
	"fmt"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// Status is the outcome of verifying one OCI digest reference.
type Status string

const (
	StatusVerified Status = "verified"
	StatusRejected Status = "rejected"
	StatusUnknown  Status = "unknown"
)

// ReasonCode names why a Result is not Verified.
type ReasonCode string

const (
	ReasonNone               ReasonCode = ""
	ReasonRegistryMismatch   ReasonCode = "registry_mismatch"
	ReasonRepositoryMismatch ReasonCode = "repository_mismatch"
	ReasonResolveFailed      ReasonCode = "resolve_failed"
	ReasonDigestMismatch     ReasonCode = "digest_mismatch"
)

// Claim is what the caller submitted, plus what SafeLane expects for this
// release target. Verify never trusts Reference.Digest as authoritative on
// its own — it must resolve against the real registry.
type Claim struct {
	ExpectedRegistry   string
	ExpectedRepository string // "owner/name", e.g. "acme/safelane-demo-api"
	Reference          release.ImageReference
}

// Result is the typed, actionable outcome of verifying one Claim.
type Result struct {
	Status         Status
	Reason         ReasonCode
	Detail         string
	ResolvedDigest string // populated whenever a resolution attempt completed, even on mismatch
}

func rejected(reason ReasonCode, detailf string, args ...any) Result {
	return Result{Status: StatusRejected, Reason: reason, Detail: fmt.Sprintf(detailf, args...)}
}

func unknown(reason ReasonCode, detailf string, args ...any) Result {
	return Result{Status: StatusUnknown, Reason: reason, Detail: fmt.Sprintf(detailf, args...)}
}

// evaluateBinding checks the claimed reference targets the expected
// registry repository, before any network resolution happens. Pure, and
// exported indirectly through Verify/EvaluateResolved for testability.
func evaluateBinding(claim Claim) Result {
	ref := claim.Reference
	if claim.ExpectedRegistry != "" && ref.Registry != claim.ExpectedRegistry {
		return rejected(ReasonRegistryMismatch, "expected registry %q, got %q", claim.ExpectedRegistry, ref.Registry)
	}
	if claim.ExpectedRepository != "" && ref.Repository != claim.ExpectedRepository {
		return rejected(ReasonRepositoryMismatch, "expected repository %q, got %q", claim.ExpectedRepository, ref.Repository)
	}
	return Result{Status: StatusVerified}
}

// EvaluateResolved combines the binding check with an already-resolved
// digest (or resolution error), so the digest-comparison rule is testable
// without a network dependency.
func EvaluateResolved(claim Claim, resolvedDigest string, resolveErr error) Result {
	if bound := evaluateBinding(claim); bound.Status != StatusVerified {
		return bound
	}
	if resolveErr != nil {
		return unknown(ReasonResolveFailed, "could not resolve digest for %s: %v", claim.Reference, resolveErr)
	}
	if resolvedDigest != claim.Reference.Digest {
		return Result{
			Status:         StatusRejected,
			Reason:         ReasonDigestMismatch,
			Detail:         fmt.Sprintf("registry reports digest %q for %s, submitted digest was %q", resolvedDigest, claim.Reference.Repository, claim.Reference.Digest),
			ResolvedDigest: resolvedDigest,
		}
	}
	return Result{Status: StatusVerified, ResolvedDigest: resolvedDigest}
}
