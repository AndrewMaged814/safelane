package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadVerifiedInstallsOnlyMatchingArtifact(t *testing.T) {
	binary := []byte("pinned demo tool")
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	mux := http.NewServeMux()
	mux.HandleFunc("/tool", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(binary) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintf(w, "%s  tool-asset\n", digest) })
	server := httptest.NewServer(mux)
	defer server.Close()

	target := filepath.Join(t.TempDir(), executableName("tool"))
	err := downloadVerified(context.Background(), server.Client(), target, demoTool{
		asset: "tool-asset", binaryURL: server.URL + "/tool", checksumURL: server.URL + "/checksums", checksumName: "tool-asset",
	})
	if err != nil {
		t.Fatalf("downloadVerified: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(binary) {
		t.Fatalf("installed artifact = %q, %v", got, err)
	}
}

func TestDemoManifestCreatesSeparatedNamespaceScopedIdentities(t *testing.T) {
	manifest := string(demoBaselineManifest("ghcr.io/example/app@sha256:"+strings.Repeat("a", 64), "sha-abc"))
	for _, want := range []string{
		"name: safelane-controller", "name: safelane-caller", "kind: Role", "kind: RoleBinding",
		`verbs: ["get", "list", "watch"]`, `verbs: ["get", "list", "watch", "create", "update", "patch"]`,
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("demo manifest missing %q", want)
		}
	}
}

func TestDownloadVerifiedRejectsChecksumMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/tool", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("artifact")) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  tool-asset")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	target := filepath.Join(t.TempDir(), executableName("tool"))
	if err := downloadVerified(context.Background(), server.Client(), target, demoTool{asset: "tool-asset", binaryURL: server.URL + "/tool", checksumURL: server.URL + "/checksums", checksumName: "tool-asset"}); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("mismatched artifact was installed: %v", err)
	}
}

func TestChecksumForAcceptsSingleDigestSidecar(t *testing.T) {
	const digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	got, err := checksumFor(digest+"\n", "kubectl.exe")
	if err != nil || got != digest {
		t.Fatalf("checksumFor = %q, %v", got, err)
	}
}
