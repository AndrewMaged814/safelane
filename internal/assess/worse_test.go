package assess

import "testing"

// TestWorse_Exhaustive3x3 is, per PLAN.md Appendix D, one of the two most
// valuable tests in the repository: it is the executable form of "the
// model can only narrow the lane, never widen it."
func TestWorse_Exhaustive3x3(t *testing.T) {
	risks := []Risk{RiskLow, RiskMedium, RiskHigh}
	rank := func(r Risk) int { return riskRank(r) }

	for _, a := range risks {
		for _, b := range risks {
			got := Worse(a, b)
			wantRank := rank(a)
			if rank(b) > wantRank {
				wantRank = rank(b)
			}
			if riskRank(got) != wantRank {
				t.Errorf("Worse(%q, %q) = %q, want the higher-ranked of the two", a, b, got)
			}
			// Worse is commutative: which verdict is "heuristic" and
			// which is "model" must never change the result.
			if reverse := Worse(b, a); reverse != got {
				t.Errorf("Worse(%q, %q) = %q but Worse(%q, %q) = %q; must be commutative", a, b, got, b, a, reverse)
			}
		}
	}
}

func TestWorse_NeverLowerThanEitherInput(t *testing.T) {
	risks := []Risk{RiskLow, RiskMedium, RiskHigh}
	for _, a := range risks {
		for _, b := range risks {
			got := Worse(a, b)
			if riskRank(got) < riskRank(a) || riskRank(got) < riskRank(b) {
				t.Errorf("Worse(%q, %q) = %q is lower than one of its inputs -- a model must never be able to lower the risk", a, b, got)
			}
		}
	}
}
