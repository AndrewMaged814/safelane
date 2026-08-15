// Package ghcr verifies that a caller-submitted OCI image reference is an
// immutable digest reference, that it targets the expected registry
// repository, and that the digest actually resolves there — using the
// public GHCR anonymous flow: request a pull-scoped token, issue a manifest
// HEAD with an OCI index Accept header, and compare docker-content-digest.
//
// Verified working against a public GHCR package on 2026-08-15 (see #48).
package ghcr

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
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
	ReasonMalformedReference ReasonCode = "malformed_reference"
	ReasonMutableTag         ReasonCode = "mutable_tag"
	ReasonRegistryMismatch   ReasonCode = "registry_mismatch"
	ReasonRepositoryMismatch ReasonCode = "repository_mismatch"
	ReasonResolveFailed      ReasonCode = "resolve_failed"
	ReasonDigestMismatch     ReasonCode = "digest_mismatch"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Reference is a parsed, already-validated-as-immutable OCI image reference:
// registry/owner/package@sha256:<digest>.
type Reference struct {
	Registry string // e.g. "ghcr.io"
	Owner    string // e.g. "acme"
	Package  string // e.g. "podinfo"; may itself contain "/" for nested packages
	Digest   string // "sha256:<64 hex chars>"
}

// Repository is the "<owner>/<package>" form GHCR's token scope and
// manifest path both use.
func (r Reference) Repository() string {
	return r.Owner + "/" + r.Package
}

func (r Reference) String() string {
	return fmt.Sprintf("%s/%s@%s", r.Registry, r.Repository(), r.Digest)
}

// ParseReference parses "registry/owner/package[/more]@sha256:<digest>".
// Any reference using a mutable tag (":tag" or bare, with no "@sha256:"
// digest) is rejected here rather than silently accepted — an immutable
// digest is required, never a tag.
func ParseReference(s string) (Reference, error) {
	at := strings.Index(s, "@")
	if at < 0 {
		return Reference{}, fmt.Errorf("%w: %q has no @sha256:<digest> — mutable tags are not accepted", errMutableTag, s)
	}
	head, digest := s[:at], s[at+1:]
	if !digestPattern.MatchString(digest) {
		return Reference{}, fmt.Errorf("%w: %q is not a well-formed sha256 digest", errMalformed, digest)
	}
	parts := strings.SplitN(head, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Reference{}, fmt.Errorf("%w: %q must be registry/owner/package", errMalformed, head)
	}
	return Reference{Registry: parts[0], Owner: parts[1], Package: parts[2], Digest: digest}, nil
}

var errMutableTag = fmt.Errorf("mutable image reference")
var errMalformed = fmt.Errorf("malformed image reference")

// RejectionForParseError turns a ParseReference error into a typed Result,
// so intake can reject a mutable tag or malformed reference the same way it
// reports any other evidence rejection.
func RejectionForParseError(err error) Result {
	if errors.Is(err, errMutableTag) {
		return rejected(ReasonMutableTag, "%v", err)
	}
	return rejected(ReasonMalformedReference, "%v", err)
}

// Claim is what the caller submitted, plus what SafeLane expects for this
// release target. Verify never trusts Reference.Digest as authoritative on
// its own — it must resolve against the real registry.
type Claim struct {
	ExpectedRegistry string
	ExpectedOwner    string
	ExpectedPackage  string
	Reference        Reference
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
// exported indirectly through Verify/VerifyResolved for testability.
func evaluateBinding(claim Claim) Result {
	ref := claim.Reference
	if claim.ExpectedRegistry != "" && ref.Registry != claim.ExpectedRegistry {
		return rejected(ReasonRegistryMismatch, "expected registry %q, got %q", claim.ExpectedRegistry, ref.Registry)
	}
	if (claim.ExpectedOwner != "" && ref.Owner != claim.ExpectedOwner) ||
		(claim.ExpectedPackage != "" && ref.Package != claim.ExpectedPackage) {
		return rejected(ReasonRepositoryMismatch, "expected repository %q/%q, got %q/%q",
			claim.ExpectedOwner, claim.ExpectedPackage, ref.Owner, ref.Package)
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
			Detail:         fmt.Sprintf("registry reports digest %q for %s, submitted digest was %q", resolvedDigest, claim.Reference.Repository(), claim.Reference.Digest),
			ResolvedDigest: resolvedDigest,
		}
	}
	return Result{Status: StatusVerified, ResolvedDigest: resolvedDigest}
}
