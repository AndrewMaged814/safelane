package ghcr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// Resolver resolves what digest a registry actually reports for an
// [release.ImageReference]. Verify depends on this interface, not on
// *Client directly, so tests can supply a fixture Resolver and this seam
// can be pointed at a different OCI-compatible registry without changing
// verification logic.
type Resolver interface {
	ResolveDigest(ctx context.Context, ref release.ImageReference) (string, error)
}

// Client is the real Resolver, implementing the public GHCR anonymous flow:
//  1. GET {BaseURL}/token?scope=repository:{owner}/{package}:pull
//  2. HEAD {BaseURL}/v2/{owner}/{package}/manifests/{digest} with an OCI
//     index Accept header
//  3. compare the Docker-Content-Digest response header to the claimed digest
//
// BaseURL defaults to https://ghcr.io and can be overridden to point at a
// fixture httptest.Server in tests. No credentials are used or stored; this
// flow only works for public packages (see #48 out-of-scope: private
// registry credentials are not built here).
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://ghcr.io"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

type tokenResponse struct {
	Token string `json:"token"`
}

func (c *Client) fetchToken(ctx context.Context, repository string) (string, error) {
	url := fmt.Sprintf("%s/token?scope=repository:%s:pull", c.baseURL(), repository)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("ghcr: token request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ghcr: token request returned status %d", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", fmt.Errorf("ghcr: could not decode token response: %w", err)
	}
	if tok.Token == "" {
		return "", fmt.Errorf("ghcr: token response had no token")
	}
	return tok.Token, nil
}

// manifestAcceptHeaders covers an OCI image index, an OCI image manifest,
// and their Docker-media-type equivalents, since real GHCR packages vary in
// which they were pushed as. The digest comparison below does not depend on
// which media type the registry actually returns.
const manifestAcceptHeaders = "application/vnd.oci.image.index.v1+json, " +
	"application/vnd.oci.image.manifest.v1+json, " +
	"application/vnd.docker.distribution.manifest.list.v2+json, " +
	"application/vnd.docker.distribution.manifest.v2+json"

// ResolveDigest performs the anonymous token → manifest HEAD flow and
// returns the registry's reported docker-content-digest for ref.
func (c *Client) ResolveDigest(ctx context.Context, ref release.ImageReference) (string, error) {
	token, err := c.fetchToken(ctx, ref.Repository)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/v2/%s/manifests/%s", c.baseURL(), ref.Repository, ref.Digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", manifestAcceptHeaders)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("ghcr: manifest HEAD failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ghcr: manifest HEAD returned status %d for %s", resp.StatusCode, ref)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("ghcr: manifest response for %s had no Docker-Content-Digest header", ref)
	}
	return digest, nil
}

// Verify resolves claim.Reference's digest via resolver and evaluates it
// against claim. Resolution failures (network error, missing header,
// non-2xx status) produce StatusUnknown, never a passing result.
func Verify(ctx context.Context, resolver Resolver, claim Claim) Result {
	if bound := evaluateBinding(claim); bound.Status != StatusVerified {
		return bound
	}
	digest, err := resolver.ResolveDigest(ctx, claim.Reference)
	return EvaluateResolved(claim, digest, err)
}
