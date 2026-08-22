package ghcr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixtureRegistry serves the real GHCR anonymous flow shape: a token
// endpoint and a manifest HEAD that reports Docker-Content-Digest, so
// Client can be tested against real-looking endpoints without a network
// dependency.
func fixtureRegistry(t *testing.T, reportedDigest string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("scope"); got != "repository:acme/safelane-demo-api:pull" {
			t.Errorf("unexpected token scope: %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token": "fixture-anonymous-token"}`))
	})
	mux.HandleFunc("/v2/acme/safelane-demo-api/manifests/"+validDigest, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("want HEAD, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-anonymous-token" {
			t.Errorf("unexpected Authorization header: %s", got)
		}
		w.Header().Set("Docker-Content-Digest", reportedDigest)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_ResolveDigest_MatchesRealFlowShape(t *testing.T) {
	srv := fixtureRegistry(t, validDigest)
	client := &Client{BaseURL: srv.URL}
	ref := mustParse(t, "ghcr.io/acme/safelane-demo-api@"+validDigest)

	got, err := client.ResolveDigest(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != validDigest {
		t.Fatalf("want %s, got %s", validDigest, got)
	}
}

func TestVerify_EndToEnd_UsesRealResolver(t *testing.T) {
	srv := fixtureRegistry(t, validDigest)
	client := &Client{BaseURL: srv.URL}
	claim := baseClaim(t)

	got := Verify(context.Background(), client, claim)
	if got.Status != StatusVerified {
		t.Fatalf("want Verified, got %+v", got)
	}
}

func TestVerify_EndToEnd_DigestMismatchFromRegistry(t *testing.T) {
	otherDigest := "sha256:" + strings.Repeat("1", 64)
	srv := fixtureRegistry(t, otherDigest)
	client := &Client{BaseURL: srv.URL}
	claim := baseClaim(t)

	got := Verify(context.Background(), client, claim)
	if got.Status != StatusRejected || got.Reason != ReasonDigestMismatch {
		t.Fatalf("want Rejected/DigestMismatch, got %+v", got)
	}
}

func TestClient_ResolveDigest_TokenEndpointFails_IsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := &Client{BaseURL: srv.URL}
	ref := mustParse(t, "ghcr.io/acme/safelane-demo-api@"+validDigest)

	_, err := client.ResolveDigest(context.Background(), ref)
	if err == nil {
		t.Fatal("want error when token endpoint fails")
	}
}

func TestClient_ResolveDigest_ManifestMissingDigestHeader_IsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"token": "t"}`))
	})
	mux.HandleFunc("/v2/acme/safelane-demo-api/manifests/"+validDigest, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // no Docker-Content-Digest header
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := &Client{BaseURL: srv.URL}
	ref := mustParse(t, "ghcr.io/acme/safelane-demo-api@"+validDigest)

	_, err := client.ResolveDigest(context.Background(), ref)
	if err == nil {
		t.Fatal("want error when Docker-Content-Digest header is missing")
	}
}
