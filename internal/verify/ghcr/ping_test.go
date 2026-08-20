package ghcr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingAcceptsRegistryAuthenticationChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	if err := (&Client{BaseURL: server.URL}).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
