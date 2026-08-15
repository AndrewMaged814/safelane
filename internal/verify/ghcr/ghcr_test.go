package ghcr

import (
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/release"
)

const validDigest = "sha256:" + "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

func mustParse(t *testing.T, s string) release.ImageReference {
	t.Helper()
	ref, err := release.ParseImageReference(s)
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return ref
}

func baseClaim(t *testing.T) Claim {
	t.Helper()
	return Claim{
		ExpectedRegistry:   "ghcr.io",
		ExpectedRepository: "acme/podinfo",
		Reference:          mustParse(t, "ghcr.io/acme/podinfo@"+validDigest),
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
	claim.ExpectedRepository = "someone-else/podinfo"
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
