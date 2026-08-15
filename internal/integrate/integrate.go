// Package integrate generates SafeLane-owned discovery files for a caller
// adapter. It changes how an agent finds the CLI; it never authorizes a
// release, and it never invokes Codex, an LLM, the network, or MCP.
package integrate

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed guidance.md
var agentGuidance []byte

const guidanceRelPath = ".safelane/agent-guidance.md"

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
