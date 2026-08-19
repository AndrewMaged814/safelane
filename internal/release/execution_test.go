package release_test

import (
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func eligibleRelease(t *testing.T) *release.Release {
	t.Helper()
	req := validRequest()
	ev := mustVerified(t, mustEvidence(t, validEvidenceInput()))
	env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
	if err != nil {
		t.Fatalf("NewRolloutEnvelope: %v", err)
	}
	elig, err := release.Eligible("1", "all_mandatory_evidence_verified",
		"All configured mandatory evidence verified.", env)
	if err != nil {
		t.Fatalf("Eligible: %v", err)
	}
	rel, err := release.NewRelease(release.ReleaseParams{
		ID:          mustID(t),
		Request:     req,
		Evidence:    ev,
		Bundle:      mustBundle(t, req.Target, digestA),
		Eligibility: elig,
		CreatedAt:   testTime,
	})
	if err != nil {
		t.Fatalf("NewRelease: %v", err)
	}
	return rel
}

func TestExecutionEntry_ValidatesVerbAndOutcome(t *testing.T) {
	base := release.ExecutionEntry{At: testTime, Verb: release.VerbStart, Outcome: release.OutcomeGranted}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed entry must validate, got %v", err)
	}

	bad := base
	bad.Verb = "promote" // not one of start/advance/argo_abort/pause/abort
	if err := bad.Validate(); err == nil {
		t.Error("want an error for an unrecognised verb")
	}

	bad = base
	bad.Outcome = "maybe"
	if err := bad.Validate(); err == nil {
		t.Error("want an error for an unrecognised outcome")
	}

	bad = base
	bad.At = time.Time{}
	if err := bad.Validate(); err == nil {
		t.Error("want an error for a missing timestamp")
	}
}

// TestExecutionEntry_ValidatesPauseAndAbort covers the two verbs ticket 11
// added: a caller's own `rollout pause` and `rollout abort --reason`,
// neither of which is ever refused so both only ever carry
// [release.OutcomeGranted] / [release.OutcomeAborted].
func TestExecutionEntry_ValidatesPauseAndAbort(t *testing.T) {
	pause := release.ExecutionEntry{At: testTime, Verb: release.VerbPause, Outcome: release.OutcomeGranted}
	if err := pause.Validate(); err != nil {
		t.Errorf("a well-formed pause entry must validate, got %v", err)
	}

	abort := release.ExecutionEntry{At: testTime, Verb: release.VerbAbort, Outcome: release.OutcomeAborted, Detail: "bad canary"}
	if err := abort.Validate(); err != nil {
		t.Errorf("a well-formed abort entry must validate, got %v", err)
	}
}

func TestRelease_WithExecution_AppendsInOrder(t *testing.T) {
	rel := eligibleRelease(t)
	if got := rel.Execution(); got != nil {
		t.Fatalf("a fresh release must have no execution history, got %v", got)
	}

	started, err := rel.WithExecution(release.ExecutionEntry{
		At: testTime, Verb: release.VerbStart, RequestedWeight: 5, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("WithExecution: %v", err)
	}
	advanced, err := started.WithExecution(release.ExecutionEntry{
		At: testTime.Add(time.Minute), Verb: release.VerbAdvance, RequestedWeight: 25, Outcome: release.OutcomeGranted,
	})
	if err != nil {
		t.Fatalf("WithExecution: %v", err)
	}

	got := advanced.Execution()
	if len(got) != 2 {
		t.Fatalf("execution history has %d entries, want 2", len(got))
	}
	if got[0].Verb != release.VerbStart || got[0].RequestedWeight != 5 {
		t.Errorf("entry 0 = %+v, want the start grant at weight 5", got[0])
	}
	if got[1].Verb != release.VerbAdvance || got[1].RequestedWeight != 25 {
		t.Errorf("entry 1 = %+v, want the advance grant at weight 25", got[1])
	}

	// WithExecution must not mutate the receiver: rel (and started) keep
	// their own history exactly as it was.
	if len(rel.Execution()) != 0 {
		t.Error("WithExecution mutated the original release's execution history")
	}
	if len(started.Execution()) != 1 {
		t.Error("WithExecution mutated an intermediate release's execution history")
	}
}

func TestRelease_WithExecution_RejectsAnInvalidEntry(t *testing.T) {
	rel := eligibleRelease(t)
	if _, err := rel.WithExecution(release.ExecutionEntry{Verb: release.VerbStart, Outcome: release.OutcomeGranted}); err == nil {
		t.Error("want an error for an entry with no timestamp")
	}
}

func TestNewRelease_RejectsExecutionWithoutEligibility(t *testing.T) {
	env, err := release.NewRolloutEnvelope([]int{5, 25, 50, 100}, "start")
	if err != nil {
		t.Fatalf("NewRolloutEnvelope: %v", err)
	}
	_ = env
	elig, err := release.Ineligible("1", "required_check_failed", "The publish check failed for this commit.")
	if err != nil {
		t.Fatalf("Ineligible: %v", err)
	}

	_, err = release.NewRelease(release.ReleaseParams{
		ID:          mustID(t),
		Request:     validRequest(),
		Evidence:    release.FailedEvidence(),
		Eligibility: elig,
		Execution: []release.ExecutionEntry{
			{At: testTime, Verb: release.VerbStart, RequestedWeight: 5, Outcome: release.OutcomeGranted},
		},
		CreatedAt: testTime,
	})
	if err == nil {
		t.Fatal("want an error: nothing may start against a release that never earned a lane and an envelope")
	}
}
