package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingReturnsAuthenticatedLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("request = %s, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"AndrewMaged814"}`))
	}))
	t.Cleanup(server.Close)

	login, err := (&Client{BaseURL: server.URL, Token: "secret"}).Ping(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "AndrewMaged814" {
		t.Fatalf("login = %q", login)
	}
}
