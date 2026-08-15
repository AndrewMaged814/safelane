package ghcr

import (
	"strings"
	"testing"
)

const validDigest = "sha256:" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

func TestParseReference_Valid(t *testing.T) {
	ref, err := ParseReference("ghcr.io/acme/podinfo@" + validDigest)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.Registry != "ghcr.io" || ref.Owner != "acme" || ref.Package != "podinfo" || ref.Digest != validDigest {
		t.Fatalf("unexpected reference: %+v", ref)
	}
	if ref.Repository() != "acme/podinfo" {
		t.Fatalf("unexpected repository: %s", ref.Repository())
	}
}

func TestParseReference_MutableTag_Rejected(t *testing.T) {
	_, err := ParseReference("ghcr.io/acme/podinfo:latest")
	if err == nil {
		t.Fatal("want error for mutable tag reference")
	}
	got := RejectionForParseError(err)
	if got.Status != StatusRejected || got.Reason != ReasonMutableTag {
		t.Fatalf("want Rejected/MutableTag, got %+v", got)
	}
}

func TestParseReference_BareMutableTag_Rejected(t *testing.T) {
	_, err := ParseReference("ghcr.io/acme/podinfo")
	if err == nil {
		t.Fatal("want error for bare (implicit-latest) reference")
	}
	got := RejectionForParseError(err)
	if got.Reason != ReasonMutableTag {
		t.Fatalf("want MutableTag, got %+v", got)
	}
}

func TestParseReference_MalformedDigest_Rejected(t *testing.T) {
	_, err := ParseReference("ghcr.io/acme/podinfo@sha256:not-hex")
	if err == nil {
		t.Fatal("want error for malformed digest")
	}
	got := RejectionForParseError(err)
	if got.Status != StatusRejected || got.Reason != ReasonMalformedReference {
		t.Fatalf("want Rejected/MalformedReference, got %+v", got)
	}
}

func TestParseReference_MissingOwnerOrPackage_Rejected(t *testing.T) {
	_, err := ParseReference("ghcr.io/podinfo@" + validDigest)
	if err == nil {
		t.Fatal("want error for missing owner/package segment")
	}
}

func baseClaim(t *testing.T) Claim {
	t.Helper()
	ref, err := ParseReference("ghcr.io/acme/podinfo@" + validDigest)
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return Claim{
		ExpectedRegistry: "ghcr.io",
		ExpectedOwner:    "acme",
		ExpectedPackage:  "podinfo",
		Reference:        ref,
	}
}

func TestEvaluateResolved_Verified(t *testing.T) {
	claim := baseClaim(t)
	got := EvaluateResolved(claim, validDigest, nil)
	if got.Status != StatusVerified {
		t.Fatalf("want Verified, got %+v", got)
	}
}

func TestEvaluateResolved_RegistryMismatch(t *testing.T) {
	claim := baseClaim(t)
	claim.ExpectedRegistry = "docker.io"
	got := EvaluateResolved(claim, validDigest, nil)
	if got.Status != StatusRejected || got.Reason != ReasonRegistryMismatch {
		t.Fatalf("want Rejected/RegistryMismatch, got %+v", got)
	}
}

func TestEvaluateResolved_RepositoryMismatch(t *testing.T) {
	claim := baseClaim(t)
	claim.ExpectedOwner = "someone-else"
	got := EvaluateResolved(claim, validDigest, nil)
	if got.Status != StatusRejected || got.Reason != ReasonRepositoryMismatch {
		t.Fatalf("want Rejected/RepositoryMismatch, got %+v", got)
	}
}

func TestEvaluateResolved_DigestMismatch(t *testing.T) {
	claim := baseClaim(t)
	otherDigest := "sha256:" + strings.Repeat("0", 64)
	got := EvaluateResolved(claim, otherDigest, nil)
	if got.Status != StatusRejected || got.Reason != ReasonDigestMismatch {
		t.Fatalf("want Rejected/DigestMismatch, got %+v", got)
	}
	if got.ResolvedDigest != otherDigest {
		t.Fatalf("want ResolvedDigest recorded even on mismatch, got %+v", got)
	}
}

func TestEvaluateResolved_ResolveFailed_IsUnknownNeverPassing(t *testing.T) {
	claim := baseClaim(t)
	got := EvaluateResolved(claim, "", errUnreachable)
	if got.Status != StatusUnknown || got.Reason != ReasonResolveFailed {
		t.Fatalf("want Unknown/ResolveFailed, got %+v", got)
	}
}

var errUnreachable = &fixtureErr{"registry unreachable"}

type fixtureErr struct{ msg string }

func (e *fixtureErr) Error() string { return e.msg }
