package ghcr

import (
	"context"
	"testing"
	"time"
)

// TestClient_ResolveDigest_RealGHCR hits the real, public
// ghcr.io/stefanprodan/podinfo package -- no fixture, no mock. It proves
// the token -> manifest HEAD -> docker-content-digest flow this package
// implements actually works against real GHCR, not just against a shape we
// assumed. Confirmed working 2026-08-15:
//
//	curl -sI -X HEAD \
//	  -H "Authorization: Bearer $(curl -s 'https://ghcr.io/token?scope=repository:stefanprodan/podinfo:pull' | ...)" \
//	  -H "Accept: application/vnd.oci.image.index.v1+json, ..." \
//	  https://ghcr.io/v2/stefanprodan/podinfo/manifests/sha256:f9537f72...
//	=> 200 OK, docker-content-digest: sha256:f9537f72...
//
// Skipped in -short runs since it requires network access. The digest below
// is a real, currently-published podinfo image digest and may need
// refreshing if that tag is ever pruned from the registry -- if this test
// starts failing with a resolve error (not a digest mismatch), check
// whether ghcr.io/stefanprodan/podinfo still serves this digest.
func TestClient_ResolveDigest_RealGHCR(t *testing.T) {
	if testing.Short() {
		t.Skip("requires network access to ghcr.io")
	}

	ref, err := ParseReference("ghcr.io/stefanprodan/podinfo@sha256:f9537f729129d339aaef049b76ab2cd4ff06a424f99e5f6a5923c10621018fb1")
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := &Client{}
	got, err := client.ResolveDigest(ctx, ref)
	if err != nil {
		t.Fatalf("real GHCR resolve failed (registry may have pruned this digest -- see test comment): %v", err)
	}
	if got != ref.Digest {
		t.Fatalf("want %s, got %s", ref.Digest, got)
	}
}
