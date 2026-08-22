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
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// Facts are collected by SafeLane from GitHub. A caller never supplies
// them: everything here is observed, not claimed.
type Facts struct {
	// Files, TotalAdditions and TotalDeletions describe the change itself.
	Files          []FileChange `json:"files"`
	TotalAdditions int          `json:"additions"`
	TotalDeletions int          `json:"deletions"`

	// AgentAuthored is true when the merge commit carries a Co-authored-by
	// trailer naming a known coding agent, or when its author is a bot
	// login. AgentEvidence records the exact trailer or login that proved
	// it, so the claim is checkable rather than asserted.
	AgentAuthored bool   `json:"agent_authored"`
	AgentEvidence string `json:"agent_evidence,omitempty"`

	MergeCommitSHA string `json:"merge_commit_sha,omitempty"`

	// UnifiedDiff is the change as text, for the model assessor. It is
	// what the model sees -- not a working directory, not the repository,
	// nothing else. Bounding it to a byte budget is the model assessor's
	// job at the point it builds a prompt, not this package's.
	UnifiedDiff string `json:"-"`
	// RuntimeAssertions are operator-approved assertion IDs available to
	// cover a cited semantic hazard. The model may request one; SafeLane,
	// not the model, determines whether that ID is configured.
	RuntimeAssertions     []string `json:"runtime_assertions,omitempty"`
	ArtifactIdentity      string   `json:"artifact_identity,omitempty"`
	CIEvidence            []string `json:"ci_evidence,omitempty"`
	CriticalSurfaces      []string `json:"critical_surfaces,omitempty"`
	DeterministicRisk     Risk     `json:"deterministic_risk,omitempty"`
	DeterministicFindings []string `json:"deterministic_findings,omitempty"`
}

// Hazard is a cited failure mode raised by semantic assessment.
type Hazard struct {
	ID                string `json:"id"`
	Category          string `json:"category"`
	Severity          Risk   `json:"severity"`
	FailureMode       string `json:"failure_mode"`
	File              string `json:"file,omitempty"`
	Line              int    `json:"line,omitempty"`
	AffectedSurface   string `json:"affected_surface"`
	Reversibility     string `json:"reversibility"`
	RequiredAssertion string `json:"required_assertion"`
	Covered           bool   `json:"covered"`
}

// Verdict is one assessor's opinion of a change's risk.
type Verdict struct {
	Risk      Risk   `json:"risk,omitempty"`
	Rationale string `json:"rationale,omitempty"`
	// Rules records which operator-configured rules fired, by name, so
	// the output can say why. Heuristic only; a model assessor has no
	// rules, only a rationale.
	Rules []string `json:"rules,omitempty"`
	// Available is false when the assessor could not run at all --
	// missing binary, timeout, invalid output. A model assessor sets
	// Available:false with Reason and never sets Risk in that case. An
	// unavailable assessor is not a low verdict.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Assessor names the concrete assessor that produced this verdict
	// -- "claude" or "codex" for a model verdict, empty for the
	// heuristic, whose name is fixed. The output says which model
	// answered, and the record has to be able to as well.
	Assessor     string   `json:"assessor,omitempty"`
	Hazards      []Hazard `json:"hazards,omitempty"`
	Insufficient bool     `json:"insufficient,omitempty"`
}

// Assessor rates the risk of a change from its Facts. "heuristic" and
// "claude"/"codex" are the assessors phase one runs.
type Assessor interface {
	Name() string
	Assess(ctx context.Context, f Facts) (Verdict, error)
}

// Assessment is one change's complete risk determination: the Change
// Facts it was formed from, both assessors' verdicts, the combined risk,
// and the lane that risk bought. It is what lands on a Release record.
//
// It exists only for a release that is eligible. Assessment is a
// question about a change that may ship at all -- an ineligible or
// indeterminate release is not assessed, gets no risk and no lane, and
// carries the zero value here. [release.NewRelease] enforces that.
type Assessment struct {
	// Facts are the observed Change Facts both verdicts were formed
	// from. UnifiedDiff is deliberately not persisted: it is model
	// input, not a decision, and it can be large.
	Facts Facts `json:"facts"`
	// Heuristic is the operator-owned deterministic verdict. It always
	// ran; an unavailable heuristic is a configuration error, not an
	// assessment.
	Heuristic Verdict `json:"heuristic"`
	// Model is the best-effort model verdict. Available:false when no
	// model assessor ran, which is legitimate and never a low verdict.
	Model Verdict `json:"model"`
	// Risk is Worse(Heuristic.Risk, Model.Risk) and nothing else. There
	// is no other path by which these two combine.
	Risk Risk `json:"risk"`
	// Lane is the operator-declared lane Risk resolved to through
	// policy.RiskToLane. No assessor emits it and no caller names it.
	Lane string `json:"lane"`
	// CombinedBy records how Risk was reached, so a reader of the
	// record does not have to trust that it was the worse of the two.
	CombinedBy string `json:"combined_by"`
	// AuthorizedUntil is the maximum exposure allowed by uncovered hazards.
	AuthorizedUntil int `json:"authorized_until"`
	// Mode makes model-outage fallback explicit in proof instead of asking a
	// reader to infer it from an unavailable verdict and the guarded lane.
	Mode string `json:"assessment_mode"`
}

// IsZero reports whether no assessment was performed.
func (a Assessment) IsZero() bool {
	return a.Lane == "" && a.Risk == "" && a.Mode == "" && !a.Heuristic.Available && !a.Model.Available
}

// HeuristicOnly reports whether the combined risk came from the
// heuristic alone, which is what the output says when no model
// assessor was available.
func (a Assessment) HeuristicOnly() bool { return !a.Model.Available }

// Combine builds an Assessment from the two verdicts and the lane the
// combined risk resolved to. It is the only constructor: the risk is
// computed here through [Worse] rather than accepted as an argument, so
// no caller can record a combined risk that is not the worse of the two.
func Combine(facts Facts, heuristic, model Verdict, lane string) Assessment {
	facts.UnifiedDiff = "" // model input, never a recorded decision
	mode := "deterministic_and_semantic"
	if !model.Available {
		mode = "deterministic_guarded_fallback"
	}
	return Assessment{
		Facts:           facts,
		Heuristic:       heuristic,
		Model:           model,
		Risk:            Worse(heuristic.Risk, model.Risk),
		Lane:            lane,
		CombinedBy:      "worse-of",
		AuthorizedUntil: authorityFor(model.Hazards),
		Mode:            mode,
	}
}

func authorityFor(hazards []Hazard) int {
	authority := 100
	for _, hazard := range hazards {
		if hazard.Covered {
			continue
		}
		switch hazard.Severity {
		case RiskHigh:
			return 0
		case RiskMedium:
			if authority > 25 {
				authority = 25
			}
		}
	}
	return authority
}
