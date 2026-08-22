package release

import (
	"fmt"

	"github.com/AndrewMaged814/safelane/internal/assess"
)

// AssessmentRecord is Appendix C2's immutable assessment proof. It records the
// two verdicts and their combination without retaining the per-file dossier.
type AssessmentRecord struct {
	Facts           AssessmentFacts `json:"facts"`
	Heuristic       assess.Verdict  `json:"heuristic"`
	Model           assess.Verdict  `json:"model"`
	Risk            assess.Risk     `json:"risk"`
	CombinedBy      string          `json:"combined_by"`
	Lane            string          `json:"lane"`
	AuthorizedUntil int             `json:"authorized_until"`
	AssessmentMode  string          `json:"assessment_mode,omitempty"`
}

type AssessmentFacts struct {
	FilesChanged      int      `json:"files_changed"`
	Additions         int      `json:"additions"`
	Deletions         int      `json:"deletions"`
	AgentAuthored     bool     `json:"agent_authored"`
	AgentEvidence     string   `json:"agent_evidence,omitempty"`
	RuntimeAssertions []string `json:"runtime_assertions,omitempty"`
}

func assessmentRecordFrom(a assess.Assessment) AssessmentRecord {
	return AssessmentRecord{
		Facts: AssessmentFacts{FilesChanged: len(a.Facts.Files), Additions: a.Facts.TotalAdditions, Deletions: a.Facts.TotalDeletions,
			AgentAuthored: a.Facts.AgentAuthored, AgentEvidence: a.Facts.AgentEvidence, RuntimeAssertions: append([]string(nil), a.Facts.RuntimeAssertions...)},
		Heuristic: a.Heuristic, Model: a.Model, Risk: a.Risk, CombinedBy: a.CombinedBy, Lane: a.Lane, AuthorizedUntil: a.AuthorizedUntil, AssessmentMode: a.Mode,
	}
}

func (a AssessmentRecord) IsZero() bool {
	return a.Facts.FilesChanged == 0 && a.Facts.Additions == 0 && a.Facts.Deletions == 0 &&
		!a.Facts.AgentAuthored && a.Facts.AgentEvidence == "" && len(a.Facts.RuntimeAssertions) == 0 &&
		a.Risk == "" && a.CombinedBy == "" && a.Lane == "" && a.AssessmentMode == "" &&
		!a.Heuristic.Available && a.Heuristic.Risk == "" && len(a.Heuristic.Rules) == 0 &&
		!a.Model.Available && a.Model.Risk == "" && a.Model.Assessor == "" && a.Model.Rationale == ""
}

func (a AssessmentRecord) HeuristicOnly() bool { return !a.Model.Available }

func (a AssessmentRecord) Validate() error {
	var errs Errors
	if a.CombinedBy != "worse-of" {
		errs = append(errs, Internal("invalid_assessment_combiner",
			fmt.Sprintf("assessment combined_by must be worse-of, got %q", a.CombinedBy)))
	}
	if want := assess.Worse(a.Heuristic.Risk, a.Model.Risk); a.Risk != want {
		errs = append(errs, Internal("assessment_risk_mismatch",
			fmt.Sprintf("recorded risk %q does not match worse-of verdict %q", a.Risk, want)))
	}
	if !a.Heuristic.Available && a.Heuristic.Risk != "" {
		errs = append(errs, Internal("unavailable_heuristic_has_risk", "an unavailable heuristic verdict cannot carry risk"))
	}
	if !a.Model.Available && a.Model.Risk != "" {
		errs = append(errs, Internal("unavailable_model_has_risk", "an unavailable model verdict cannot carry risk"))
	}
	if a.Lane == "" {
		errs = append(errs, Internal("assessment_without_lane", "an assessment must record its resolved lane"))
	}
	if a.Facts.FilesChanged < 0 || a.Facts.Additions < 0 || a.Facts.Deletions < 0 {
		errs = append(errs, Internal("invalid_assessment_facts", "assessment fact counts cannot be negative"))
	}
	return errs.OrNil()
}
