// Package integrate generates SafeLane-owned discovery files for a caller
// adapter. It changes how an agent finds the CLI; it never authorizes a
// release, and it never invokes Codex, an LLM, the network, or MCP.
package integrate

import (
	_ "embed"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed guidance.md
var agentGuidance []byte

const (
	guidanceRelPath = ".safelane/agent-guidance.md"
	agentsRelPath   = "AGENTS.md"
	beginMarker     = "<!-- BEGIN SAFELANE MANAGED: guidance -->"
	endMarker       = "<!-- END SAFELANE MANAGED: guidance -->"
)

// ManagedSection is the Codex-discoverable AGENTS.md block. It is a pointer
// to local guidance, not a copy of the workflow and not a security boundary.
const ManagedSection = beginMarker + "\n" +
	"See `.safelane/agent-guidance.md` for the protected release workflow. Use `safelane release --file ...`, follow the returned `safelane execute <release-id>` action when eligible, and use `safelane proof <release-id>` to retrieve the outcome. Do not call Kubernetes or Argo directly for the protected application.\n" +
	endMarker + "\n"

// Apply writes SafeLane-owned discovery files under root and reports each
// created, updated, unchanged, or skipped path.
func Apply(root string) ([]Change, error) {
	guidance, err := WriteGuidance(root)
	if err != nil {
		return nil, err
	}
	agents, err := writeAgents(root)
	if err != nil {
		return nil, err
	}
	return []Change{guidance, agents}, nil
}

// Change is one line of the init/sync report.
type Change struct {
	Action string // created, updated, unchanged, skipped
	Path   string
	Reason string
}

func (c Change) String() string {
	if c.Reason != "" {
		return fmt.Sprintf("%s %s (%s)", c.Action, c.Path, c.Reason)
	}
	return fmt.Sprintf("%s %s", c.Action, c.Path)
}

// WriteGuidance writes `.safelane/agent-guidance.md` under root and reports
// whether the file was created, updated, or already matched.
func WriteGuidance(root string) (Change, error) {
	path := filepath.Join(root, filepath.FromSlash(guidanceRelPath))
	existing, readErr := os.ReadFile(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		return Change{}, readErr
	}
	if readErr == nil && string(existing) == string(agentGuidance) {
		return Change{Action: "unchanged", Path: guidanceRelPath}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Change{}, err
	}
	if err := os.WriteFile(path, agentGuidance, 0o644); err != nil {
		return Change{}, err
	}
	if os.IsNotExist(readErr) {
		return Change{Action: "created", Path: guidanceRelPath}, nil
	}
	return Change{Action: "updated", Path: guidanceRelPath}, nil
}

func writeAgents(root string) (Change, error) {
	path := filepath.Join(root, agentsRelPath)
	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(ManagedSection), 0o644); err != nil {
			return Change{}, err
		}
		return Change{Action: "created", Path: "AGENTS.md managed section"}, nil
	}
	if err != nil {
		return Change{}, err
	}

	if bytes.Contains(existing, []byte(beginMarker)) || bytes.Contains(existing, []byte(endMarker)) {
		return Change{Action: "unchanged", Path: "AGENTS.md managed section"}, nil
	}

	next := appendManaged(existing)
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return Change{}, err
	}
	return Change{Action: "updated", Path: "AGENTS.md managed section"}, nil
}

func appendManaged(existing []byte) []byte {
	out := existing
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, []byte(ManagedSection)...)
}
