package cli

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/release"
)

// TestPauseRollout_RecordsCallerAndOutcome exercises `rollout pause`'s core:
// it is never refused (Appendix A's "shape of it" table), only a
// privileged kubectl call and a granted execution entry.
func TestPauseRollout_RecordsCallerAndOutcome(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("rollout.argoproj.io/safelane-demo-api paused\n", nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	now := time.Date(2026, 8, 20, 14, 30, 0, 0, time.UTC)

	updated, err := pauseRollout(context.Background(), rel, ex, func() time.Time { return now })
	if err != nil {
		t.Fatalf("pauseRollout: %v", err)
	}

	got := strings.Join(q.calls[0], " ")
	if want := "argo rollouts pause safelane-demo-api -n safelane-demo-api"; got != want {
		t.Errorf("pause args = %q, want %q", got, want)
	}

	entries := updated.Execution()
	last := entries[len(entries)-1]
	if last.Verb != release.VerbPause || last.Outcome != release.OutcomeGranted || last.At != now {
		t.Errorf("last entry = %+v, want a granted pause at %s", last, now)
	}

	out := renderPause(updated)
	if !strings.Contains(out, "caller        "+updated.Caller().Identity) {
		t.Errorf("render = %q, want the caller identity reported", out)
	}
}

func TestPauseRollout_NarrowsOnlyNeverPromotes(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("rollout.argoproj.io/safelane-demo-api paused\n", nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run

	if _, err := pauseRollout(context.Background(), rel, ex, time.Now); err != nil {
		t.Fatalf("pauseRollout: %v", err)
	}
	for _, call := range q.calls {
		if call[0] == "argo" && len(call) > 2 && call[2] == "promote" {
			t.Errorf("pause must never promote, got: %v", call)
		}
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

func TestPauseRollout_ClassifiesAFailure(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run

	_, err := pauseRollout(context.Background(), rel, ex, time.Now)
	var rerr *release.Error
	if !errors.As(err, &rerr) || rerr.Code != "kubectl_missing" {
		t.Fatalf("err = %v, want a kubectl_missing *release.Error", err)
	}
}

func TestParseRolloutPauseFlags_RequiresExactlyOneReleaseID(t *testing.T) {
	var stderr strings.Builder
	if _, _, err := parseRolloutPauseFlags(nil, &stderr, "store"); err == nil {
		t.Error("want an error with no release id")
	}
	stderr.Reset()
	if _, _, err := parseRolloutPauseFlags([]string{"a", "b"}, &stderr, "store"); err == nil {
		t.Error("want an error with two release ids")
	}
}
