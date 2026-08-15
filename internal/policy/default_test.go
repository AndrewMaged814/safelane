package policy_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/policy"
)

func TestDefault_MatchesOperatorPolicyFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "policy", "safelane-policy.yml"))
	if err != nil {
		t.Fatalf("read operator policy: %v", err)
	}
	text := string(raw)
	p := policy.Default()
	if p.Version != "1" {
		t.Errorf("version = %q, want 1", p.Version)
	}
	if !p.IndependentPRApprovalRequired {
		t.Error("demo policy requires independent PR approval")
	}
	if !strings.Contains(text, "stages: [5, 25, 50, 100]") {
		t.Error("operator policy file must declare stages: [5, 25, 50, 100]")
	}
	if !strings.Contains(text, "next_action: start") {
		t.Error("operator policy file must declare next_action: start")
	}
	if !strings.Contains(text, "independent_pr_approval:") {
		t.Error("operator policy file must declare independent_pr_approval")
	}
}
