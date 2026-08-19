// Package assess answers one question: how far may this specific change ship
// per step? It never answers whether the change may ship at all -- that is
// eligibility, and it is decided from evidence, not from risk. See
// docs/adr/0003-risk-selects-the-lane.md.
package assess

import "context"

// Risk is one of the three levels a change may be assessed at. There are
// exactly three; nothing in this package or its callers may introduce a
// fourth.
type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// riskRank orders the three levels so they can be compared. An unrecognised
// Risk ranks below RiskLow, so it never wins a comparison against a real
// verdict -- callers must reject it explicitly rather than let it silently
// participate.
func riskRank(r Risk) int {
	switch r {
	case RiskLow:
		return 0
	case RiskMedium:
		return 1
	case RiskHigh:
		return 2
	default:
		return -1
	}
}

// FileChange is one file touched by a change, as reported by GitHub.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
}

// Facts are collected by SafeLane from GitHub. A caller never supplies
// them: everything here is observed, not claimed.
type Facts struct {
	// Files, TotalAdditions and TotalDeletions describe the change itself.
	Files          []FileChange
	TotalAdditions int
	TotalDeletions int

	// AgentAuthored is true when the merge commit carries a Co-authored-by
	// trailer naming a known coding agent, or when its author is a bot
	// login. AgentEvidence records the exact trailer or login that proved
	// it, so the claim is checkable rather than asserted.
	AgentAuthored bool
	AgentEvidence string

	MergeCommitSHA string

	// UnifiedDiff is the change as text, for the model assessor. It is
	// what the model sees -- not a working directory, not the repository,
	// nothing else. Bounding it to a byte budget is the model assessor's
	// job at the point it builds a prompt, not this package's.
	UnifiedDiff string
}

// Verdict is one assessor's opinion of a change's risk.
type Verdict struct {
	Risk      Risk
	Rationale string
	// Rules records which operator-configured rules fired, by name, so
	// the output can say why. Heuristic only; a model assessor has no
	// rules, only a rationale.
	Rules []string
	// Available is false when the assessor could not run at all --
	// missing binary, timeout, invalid output. A model assessor sets
	// Available:false with Reason and never sets Risk in that case. An
	// unavailable assessor is not a low verdict.
	Available bool
	Reason    string
}

// Assessor rates the risk of a change from its Facts. "heuristic" and
// "claude"/"codex" are the assessors phase one runs.
type Assessor interface {
	Name() string
	Assess(ctx context.Context, f Facts) (Verdict, error)
}
