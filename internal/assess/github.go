package assess

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Fetcher collects Change Facts about one merged pull request from
// GitHub. A caller never supplies Facts directly; SafeLane always
// collects them itself through this seam.
type Fetcher interface {
	FetchChangeFacts(ctx context.Context, owner, repo string, number int) (Facts, error)
}

// Client is the real Fetcher, backed by the GitHub REST API. It is a
// separate client from internal/verify/github.Client: that one verifies
// review and CI evidence, this one collects the change itself, and the
// two have no reason to share a type.
type Client struct {
	BaseURL    string // defaults to https://api.github.com
	Token      string // optional; unauthenticated calls work against public repos, rate-limited
	HTTPClient *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://api.github.com"
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) get(ctx context.Context, path, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL()+path, nil)
	if err != nil {
		return nil, err
	}
	if accept == "" {
		accept = "application/vnd.github+json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("assess: GET %s: unexpected status %d", path, resp.StatusCode)
	}
	return body, nil
}

type pullRequestResponse struct {
	MergeCommitSHA string `json:"merge_commit_sha"`
}

type commitFileResponse struct {
	Filename  string `json:"filename"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type commitResponse struct {
	Commit struct {
		Message string `json:"message"`
	} `json:"commit"`
	Author *struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"author"`
}

// FetchChangeFacts collects everything assess.Facts needs for one merged
// pull request: the merge commit, the files it touched with their
// per-file additions and deletions, agent-authorship evidence from the
// merge commit's trailers and author, and the change as a unified diff.
func (c *Client) FetchChangeFacts(ctx context.Context, owner, repo string, number int) (Facts, error) {
	prBody, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), "")
	if err != nil {
		return Facts{}, fmt.Errorf("assess: fetch pull request: %w", err)
	}
	var pr pullRequestResponse
	if err := json.Unmarshal(prBody, &pr); err != nil {
		return Facts{}, fmt.Errorf("assess: decode pull request: %w", err)
	}

	files, err := c.fetchFiles(ctx, owner, repo, number)
	if err != nil {
		return Facts{}, err
	}

	facts := Facts{
		Files:          files,
		MergeCommitSHA: pr.MergeCommitSHA,
	}
	for _, f := range files {
		facts.TotalAdditions += f.Additions
		facts.TotalDeletions += f.Deletions
	}

	if pr.MergeCommitSHA != "" {
		agentAuthored, evidence, err := c.fetchAgentAuthorship(ctx, owner, repo, pr.MergeCommitSHA)
		if err != nil {
			return Facts{}, err
		}
		facts.AgentAuthored = agentAuthored
		facts.AgentEvidence = evidence
	}

	diff, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), "application/vnd.github.v3.diff")
	if err != nil {
		return Facts{}, fmt.Errorf("assess: fetch diff: %w", err)
	}
	facts.UnifiedDiff = string(diff)

	return facts, nil
}

func (c *Client) fetchFiles(ctx context.Context, owner, repo string, number int) ([]FileChange, error) {
	var files []FileChange
	for page := 1; ; page++ {
		body, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", owner, repo, number, page), "")
		if err != nil {
			return nil, fmt.Errorf("assess: fetch pull request files: %w", err)
		}
		var pageItems []commitFileResponse
		if err := json.Unmarshal(body, &pageItems); err != nil {
			return nil, fmt.Errorf("assess: decode pull request files: %w", err)
		}
		for _, f := range pageItems {
			files = append(files, FileChange{Path: f.Filename, Additions: f.Additions, Deletions: f.Deletions})
		}
		if len(pageItems) < 100 {
			break
		}
	}
	return files, nil
}

// knownAgentSignatures matches an author name, login or email against a
// small allowlist of known coding-agent identities. It is deliberately
// narrow: a human pairing trailer (e.g. a colleague's own
// Co-authored-by) must never be misread as agent authorship.
var knownAgentSignatures = regexp.MustCompile(`(?i)claude|anthropic|codex|openai|copilot|\[bot\]`)

// coAuthorTrailer matches a git trailer of the form
// "Co-authored-by: Name <email>".
var coAuthorTrailer = regexp.MustCompile(`(?m)^Co-authored-by:\s*(.+)$`)

// fetchAgentAuthorship inspects the merge commit's message trailers and
// author for evidence that a coding agent, not a human, produced the
// change. It returns the exact trailer or login that proved it, so the
// claim is checkable rather than asserted.
func (c *Client) fetchAgentAuthorship(ctx context.Context, owner, repo, sha string) (bool, string, error) {
	body, err := c.get(ctx, fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha), "")
	if err != nil {
		return false, "", fmt.Errorf("assess: fetch merge commit: %w", err)
	}
	var commit commitResponse
	if err := json.Unmarshal(body, &commit); err != nil {
		return false, "", fmt.Errorf("assess: decode merge commit: %w", err)
	}

	for _, m := range coAuthorTrailer.FindAllStringSubmatch(commit.Commit.Message, -1) {
		trailer := strings.TrimSpace(m[1])
		if knownAgentSignatures.MatchString(trailer) {
			return true, "Co-authored-by: " + trailer, nil
		}
	}

	if commit.Author != nil {
		if commit.Author.Type == "Bot" || knownAgentSignatures.MatchString(commit.Author.Login) {
			return true, "author login: " + commit.Author.Login, nil
		}
	}

	return false, "", nil
}
