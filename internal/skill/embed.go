// Package skill contains the agent skill installed by `safelane setup`.
package skill

import _ "embed"

// SafeLane is the canonical skill artifact installed for every supported agent
// harness. Keeping one embedded source makes installed copies byte-identical.
//
//go:embed SKILL.md
var SafeLane []byte
