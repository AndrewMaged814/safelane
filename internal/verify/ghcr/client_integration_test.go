package ghcr

import (
	"context"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// TestClient_ResolveDigest_RealGHCR hits the real, public
// ghcr.io/andrewmaged814/safelane-demo-api package -- no fixture, no mock. It proves
// the token -> manifest HEAD -> docker-content-digest flow this package
// implements actually works against real GHCR, not just against a shape we
// assumed. Confirmed working 2026-08-15:
//
//	curl -sI -X HEAD \
//	  -H "Authorization: Bearer $(curl -s 'https://ghcr.io/token?scope=repository:andrewmaged814/safelane-demo-api:pull' | ...)" \
//	  -H "Accept: application/vnd.oci.image.index.v1+json, ..." \
//	  https://ghcr.io/v2/andrewmaged814/safelane-demo-api/manifests/sha256:8a609c87...
//	=> 200 OK, docker-content-digest: sha256:8a609c87...
//
// Skipped in -short runs since it requires network access. The digest below
// is a real, currently-published safelane-demo-api image digest and may need
// refreshing if that tag is ever pruned from the registry -- if this test
// starts failing with a resolve error (not a digest mismatch), check
// whether ghcr.io/andrewmaged814/safelane-demo-api still serves this digest.
func TestClient_ResolveDigest_RealGHCR(t *testing.T) {
	if testing.Short() {
		t.Skip("requires network access to ghcr.io")
	}

	ref, err := release.ParseImageReference("ghcr.io/andrewmaged814/safelane-demo-api@sha256:8a609c87fe699ea23de802847a6e56396e5f11867800b507056417013397e021")
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
