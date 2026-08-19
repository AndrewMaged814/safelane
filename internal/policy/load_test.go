package policy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/policy"
)

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture policy: %v", err)
	}
	return path
}

const validPolicy = `
version: 2
independent_pr_approval:
  required: false
lanes:
  fast:
    weights: [5, 100]
  guarded:
    weights: [1, 5, 25, 50, 100]
risk_to_lane:
  low: fast
  high: guarded
default_lane: guarded
`

func TestLoad_ValidPolicy_EditingWeightsChangesTheResult(t *testing.T) {
	p, err := policy.Load(writePolicy(t, validPolicy))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, lane, err := p.LaneFor("low")
	if err != nil {
		t.Fatalf("LaneFor: %v", err)
	}
	if want := []int{5, 100}; !intSlicesEqual(lane.Weights, want) {
		t.Errorf("fast lane weights = %v, want %v", lane.Weights, want)
	}

	edited := writePolicy(t, `
version: 2
lanes:
  fast:
    weights: [10, 100]
  guarded:
    weights: [1, 5, 25, 50, 100]
risk_to_lane:
  low: fast
default_lane: guarded
`)
	p2, err := policy.Load(edited)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, lane2, err := p2.LaneFor("low")
	if err != nil {
		t.Fatalf("LaneFor: %v", err)
	}
	if want := []int{10, 100}; !intSlicesEqual(lane2.Weights, want) {
		t.Errorf("edited fast lane weights = %v, want %v -- editing the policy file must change the resolved weights", lane2.Weights, want)
	}
}

func TestLoad_MissingFile_IsAClearError(t *testing.T) {
	_, err := policy.Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err == nil {
		t.Fatal("want an error for a missing policy.yml")
	}
}

func TestLoad_RiskToLaneNamesUndeclaredLane_RejectedAtLoad(t *testing.T) {
	path := writePolicy(t, `
version: 2
lanes:
  fast:
    weights: [5, 100]
risk_to_lane:
  low: fast
  high: nonexistent
default_lane: fast
`)
	_, err := policy.Load(path)
	if err == nil {
		t.Fatal("want risk_to_lane naming an undeclared lane to be rejected at load, not at rollout time")
	}
}

func TestLoad_DefaultLaneUndeclared_RejectedAtLoad(t *testing.T) {
	path := writePolicy(t, `
version: 2
lanes:
  fast:
    weights: [5, 100]
default_lane: nonexistent
`)
	_, err := policy.Load(path)
	if err == nil {
		t.Fatal("want an undeclared default_lane to be rejected at load")
	}
}

func TestLoad_NoLanesDeclared_RejectedAtLoad(t *testing.T) {
	path := writePolicy(t, `
version: 2
default_lane: guarded
`)
	_, err := policy.Load(path)
	if err == nil {
		t.Fatal("want a policy with no lanes at all to be rejected at load")
	}
}

func TestLoad_LaneWithNoWeights_RejectedAtLoad(t *testing.T) {
	path := writePolicy(t, `
version: 2
lanes:
  empty:
    weights: []
default_lane: empty
`)
	_, err := policy.Load(path)
	if err == nil {
		t.Fatal("want a lane with no weights to be rejected at load")
	}
}
