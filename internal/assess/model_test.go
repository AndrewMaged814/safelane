package assess

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func fakeModel(t *testing.T, run runner) modelAssessor {
	t.Helper()
	return modelAssessor{
		cfg: ModelConfig{Assessors: []string{"claude"}, Timeout: time.Second},
		run: run,
	}
}

const canonicalModelJSON = `{"findings":[],"risk_level":"high","risk_rationale":"error path swallows the status code"}`

func TestModel_CannedJSON_WholePayload(t *testing.T) {
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		return []byte(canonicalModelJSON), nil
	})
	v, err := m.Assess(context.Background(), Facts{UnifiedDiff: "diff --git a b"})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !v.Available || v.Risk != RiskHigh {
		t.Fatalf("got %+v, want Available=true Risk=high", v)
	}
}

func TestModel_CannedJSON_NestedInStreamEvent(t *testing.T) {
	// Approximates claude's --output-format stream-json: a line of
	// preamble, then a "result" event carrying the structured answer as
	// a JSON-encoded string.
	stream := `{"type":"system","subtype":"init"}
{"type":"result","subtype":"success","result":"{\"findings\":[],\"risk_level\":\"medium\",\"risk_rationale\":\"touches a request path\"}"}
`
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		return []byte(stream), nil
	})
	v, err := m.Assess(context.Background(), Facts{UnifiedDiff: "diff --git a b"})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !v.Available || v.Risk != RiskMedium {
		t.Fatalf("got %+v, want Available=true Risk=medium", v)
	}
}

func TestModel_InvalidRiskLevel_IsUnavailableNotAnError(t *testing.T) {
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		return []byte(`{"findings":[],"risk_level":"critical","risk_rationale":"n/a"}`), nil
	})
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if v.Available {
		t.Fatal("want Available=false for a risk_level outside low/medium/high")
	}
	if v.Risk != "" {
		t.Errorf("want no Risk set on an unavailable verdict, got %q", v.Risk)
	}
}

func TestModel_MissingBinary_IsUnavailableImmediately_NoRetry(t *testing.T) {
	calls := 0
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		calls++
		return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
	})
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if v.Available {
		t.Fatal("want Available=false for a missing binary")
	}
	if calls != 1 {
		t.Errorf("want exactly 1 call for a missing binary (not transient, no point retrying), got %d", calls)
	}
}

func TestModel_Timeout_IsUnavailableWithReason(t *testing.T) {
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	m.cfg.Timeout = 10 * time.Millisecond
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if v.Available {
		t.Fatal("want Available=false on timeout")
	}
	if v.Reason == "" {
		t.Error("want a non-empty reason on timeout")
	}
}

func TestModel_UnparseableOutput_IsUnavailableWithReason(t *testing.T) {
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		return []byte("not json at all"), nil
	})
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if v.Available {
		t.Fatal("want Available=false for unparseable output")
	}
	if v.Risk == RiskLow {
		t.Fatal("unparseable output must never be treated as a low verdict")
	}
}

func TestModel_RetriesTransientFailureTwiceThenGivesUp(t *testing.T) {
	calls := 0
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		calls++
		return nil, errors.New("transient: connection reset")
	})
	v, _ := m.Assess(context.Background(), Facts{})
	if v.Available {
		t.Fatal("want Available=false after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("want 1 initial attempt + 2 retries = 3 calls, got %d", calls)
	}
}

func TestModel_RecoversOnRetry(t *testing.T) {
	calls := 0
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		calls++
		if calls < 2 {
			return nil, errors.New("transient: connection reset")
		}
		return []byte(canonicalModelJSON), nil
	})
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !v.Available || v.Risk != RiskHigh {
		t.Fatalf("got %+v, want a successful verdict once the retry succeeds", v)
	}
	if calls != 2 {
		t.Errorf("want exactly 2 calls (1 failure + 1 success), got %d", calls)
	}
}

func TestModel_TriesConfiguredAssessorsInOrder_FirstAvailableWins(t *testing.T) {
	var tried []string
	m := modelAssessor{
		cfg: ModelConfig{Assessors: []string{"claude", "codex"}, Timeout: time.Second},
		run: func(ctx context.Context, name, prompt string) ([]byte, error) {
			tried = append(tried, name)
			if name == "claude" {
				return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
			}
			return []byte(canonicalModelJSON), nil
		},
	}
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !v.Available || v.Risk != RiskHigh {
		t.Fatalf("got %+v, want codex's verdict once claude is unavailable", v)
	}
	if len(tried) != 2 || tried[0] != "claude" || tried[1] != "codex" {
		t.Errorf("tried = %v, want [claude codex] in that order", tried)
	}
}

func TestModel_AllAssessorsUnavailable_ReasonNamesEachAttempt(t *testing.T) {
	m := modelAssessor{
		cfg: ModelConfig{Assessors: []string{"claude", "codex"}, Timeout: time.Second},
		run: func(ctx context.Context, name, prompt string) ([]byte, error) {
			return nil, &exec.Error{Name: name, Err: exec.ErrNotFound}
		},
	}
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if v.Available {
		t.Fatal("want Available=false when every configured assessor is unavailable")
	}
	if v.Reason == "" {
		t.Fatal("want a reason naming the attempts")
	}
}

func TestModel_ModelOutputIsSanitisedBeforeItReachesTheVerdict(t *testing.T) {
	// Built with json.Marshal, not a hand-written literal: json.Marshal
	// escapes the ESC bytes below the same way a real CLI's JSON encoder
	// would, so decoding this payload is exactly the shape sanitize must
	// see through -- an escaped control character inside a valid JSON
	// string, not a raw byte a real encoder would never emit.
	rationale := "\x1b[31mDANGER\x1b[0m has control chars"
	payload, err := json.Marshal(modelOutput{RiskLevel: "high", RiskRationale: rationale})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	m := fakeModel(t, func(ctx context.Context, name, prompt string) ([]byte, error) {
		return payload, nil
	})
	v, err := m.Assess(context.Background(), Facts{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !v.Available {
		t.Fatalf("want Available=true, got Reason %q", v.Reason)
	}
	if v.Rationale != "DANGER has control chars" {
		t.Errorf("Rationale = %q, want ANSI escapes stripped", v.Rationale)
	}
}

func TestModel_Name(t *testing.T) {
	if got := (modelAssessor{}).Name(); got != "model" {
		t.Errorf("Name() = %q, want %q", got, "model")
	}
}

func TestModel_DiffIsTruncatedToMaxDiffBytes(t *testing.T) {
	var seenPrompt string
	m := modelAssessor{
		cfg: ModelConfig{Assessors: []string{"claude"}, Timeout: time.Second, MaxDiffBytes: 10},
		run: func(ctx context.Context, name, prompt string) ([]byte, error) {
			seenPrompt = prompt
			return []byte(canonicalModelJSON), nil
		},
	}
	longDiff := "0123456789ABCDEFGHIJ" // 20 bytes, over the 10-byte bound
	if _, err := m.Assess(context.Background(), Facts{UnifiedDiff: longDiff}); err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if !strings.Contains(seenPrompt, `"diff":"0123456789"`) || !strings.Contains(seenPrompt, `"diff_truncated":true`) {
		t.Errorf("prompt did not contain a bounded dossier: %s", seenPrompt)
	}
	if strings.Contains(seenPrompt, "ABCDEFGHIJ") {
		t.Error("prompt leaked the truncated diff tail")
	}
}
