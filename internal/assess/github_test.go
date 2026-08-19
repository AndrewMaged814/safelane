package assess

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fixtureServer serves canned GitHub API responses for one pull request,
// matching the real shapes of GET .../pulls/{n}, .../pulls/{n}/files,
// .../commits/{sha}, and .../pulls/{n} with the diff media type.
func fixtureServer(t *testing.T, pullBody, filesBody, commitBody, diffBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/podinfo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(diffBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(pullBody))
	})
	mux.HandleFunc("/repos/acme/podinfo/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(filesBody))
	})
	mux.HandleFunc("/repos/acme/podinfo/commits/merge-sha-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(commitBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const fixturePull = `{"merge_commit_sha": "merge-sha-1"}`

const fixtureFiles = `[
	{"filename": "pkg/api/echo.go", "additions": 41, "deletions": 6},
	{"filename": "pkg/version/version.go", "additions": 1, "deletions": 1}
]`

const fixtureHumanCommit = `{
	"commit": {"message": "Bump version\n"},
	"author": {"login": "andrew", "type": "User"}
}`

const fixtureAgentCommit = `{
	"commit": {"message": "Fix echo handler\n\nCo-authored-by: Claude <noreply@anthropic.com>\n"},
	"author": {"login": "andrew", "type": "User"}
}`

const fixtureDiff = "diff --git a/pkg/api/echo.go b/pkg/api/echo.go\n+// changed\n"

func TestClient_FetchChangeFacts_CollectsFilesAndTotals(t *testing.T) {
	srv := fixtureServer(t, fixturePull, fixtureFiles, fixtureHumanCommit, fixtureDiff)
	client := &Client{BaseURL: srv.URL}

	facts, err := client.FetchChangeFacts(context.Background(), "acme", "podinfo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(facts.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(facts.Files))
	}
	if facts.TotalAdditions != 42 || facts.TotalDeletions != 7 {
		t.Errorf("totals = +%d -%d, want +42 -7", facts.TotalAdditions, facts.TotalDeletions)
	}
	if facts.MergeCommitSHA != "merge-sha-1" {
		t.Errorf("MergeCommitSHA = %q", facts.MergeCommitSHA)
	}
	if facts.UnifiedDiff != fixtureDiff {
		t.Errorf("UnifiedDiff = %q, want %q", facts.UnifiedDiff, fixtureDiff)
	}
}

func TestClient_FetchChangeFacts_HumanCommitIsNotAgentAuthored(t *testing.T) {
	srv := fixtureServer(t, fixturePull, fixtureFiles, fixtureHumanCommit, fixtureDiff)
	client := &Client{BaseURL: srv.URL}

	facts, err := client.FetchChangeFacts(context.Background(), "acme", "podinfo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if facts.AgentAuthored {
		t.Errorf("want AgentAuthored false, got true with evidence %q", facts.AgentEvidence)
	}
	if facts.AgentEvidence != "" {
		t.Errorf("want no AgentEvidence, got %q", facts.AgentEvidence)
	}
}

func TestClient_FetchChangeFacts_AgentTrailerIsRecordedVerbatim(t *testing.T) {
	srv := fixtureServer(t, fixturePull, fixtureFiles, fixtureAgentCommit, fixtureDiff)
	client := &Client{BaseURL: srv.URL}

	facts, err := client.FetchChangeFacts(context.Background(), "acme", "podinfo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !facts.AgentAuthored {
		t.Fatal("want AgentAuthored true from the Co-authored-by trailer")
	}
	want := "Co-authored-by: Claude <noreply@anthropic.com>"
	if facts.AgentEvidence != want {
		t.Errorf("AgentEvidence = %q, want %q", facts.AgentEvidence, want)
	}
}

func TestClient_FetchChangeFacts_BotAuthorLoginIsAgentAuthored(t *testing.T) {
	const fixtureBotCommit = `{
		"commit": {"message": "Bump dependency\n"},
		"author": {"login": "dependabot[bot]", "type": "Bot"}
	}`
	srv := fixtureServer(t, fixturePull, fixtureFiles, fixtureBotCommit, fixtureDiff)
	client := &Client{BaseURL: srv.URL}

	facts, err := client.FetchChangeFacts(context.Background(), "acme", "podinfo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !facts.AgentAuthored {
		t.Fatal("want AgentAuthored true from the bot author login")
	}
	if facts.AgentEvidence != "author login: dependabot[bot]" {
		t.Errorf("AgentEvidence = %q", facts.AgentEvidence)
	}
}

func TestClient_FetchChangeFacts_PaginatesFiles(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/podinfo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
			w.Write([]byte(fixtureDiff))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixturePull))
	})
	pageOne := make([]byte, 0)
	pageOne = append(pageOne, '[')
	for i := 0; i < 100; i++ {
		if i > 0 {
			pageOne = append(pageOne, ',')
		}
		pageOne = append(pageOne, []byte(`{"filename":"f.go","additions":1,"deletions":0}`)...)
	}
	pageOne = append(pageOne, ']')
	mux.HandleFunc("/repos/acme/podinfo/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "2":
			w.Write([]byte(`[{"filename":"g.go","additions":1,"deletions":0}]`))
		default:
			w.Write(pageOne)
		}
	})
	mux.HandleFunc("/repos/acme/podinfo/commits/merge-sha-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureHumanCommit))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := &Client{BaseURL: srv.URL}

	facts, err := client.FetchChangeFacts(context.Background(), "acme", "podinfo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(facts.Files) != 101 {
		t.Fatalf("want 101 files across two pages, got %d", len(facts.Files))
	}
}
