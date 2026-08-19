package policy_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/policy"
)

// TestDefault_MatchesOperatorPolicyFile loads the actual shipped
// policy.yml through the real Load path and checks it against
// policy.Default() structurally, rather than by substring: a load that
// silently drops a lane or mis-maps a risk level would fail this even
// if the raw text still "looked right".
func TestDefault_MatchesOperatorPolicyFile(t *testing.T) {
	loaded, err := policy.Load(filepath.Join("..", "..", "docs", "policy", "safelane-policy.yml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := policy.Default()

	if loaded.Version != want.Version {
		t.Errorf("version = %q, want %q", loaded.Version, want.Version)
	}
	if loaded.IndependentPRApprovalRequired != want.IndependentPRApprovalRequired {
		t.Error("phase one does not require independent PR approval; that gate is deferred")
	}
	if !reflect.DeepEqual(loaded.Lanes, want.Lanes) {
		t.Errorf("lanes = %+v, want %+v", loaded.Lanes, want.Lanes)
	}
	if !reflect.DeepEqual(loaded.RiskToLane, want.RiskToLane) {
		t.Errorf("risk_to_lane = %+v, want %+v", loaded.RiskToLane, want.RiskToLane)
	}
	if loaded.DefaultLane != want.DefaultLane {
		t.Errorf("default_lane = %q, want %q", loaded.DefaultLane, want.DefaultLane)
	}
}
