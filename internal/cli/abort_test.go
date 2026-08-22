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

// TestAbortRollout_RecordsReasonAndCaller exercises `rollout abort
// --reason`'s core: it is never refused, restores stable traffic, and
// records both the reason and the caller.
func TestAbortRollout_RecordsReasonAndCaller(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("rollout.argoproj.io/safelane-demo-api aborted\n", nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run
	now := time.Date(2026, 8, 20, 14, 30, 0, 0, time.UTC)

	updated, err := abortRollout(context.Background(), rel, ex, "canary regression confirmed manually", func() time.Time { return now })
	if err != nil {
		t.Fatalf("abortRollout: %v", err)
	}

	got := strings.Join(q.calls[0], " ")
	if want := "argo rollouts abort safelane-demo-api -n safelane-demo-api"; got != want {
		t.Errorf("abort args = %q, want %q", got, want)
	}

	entries := updated.Execution()
	last := entries[len(entries)-1]
	if last.Verb != release.VerbAbort || last.Outcome != release.OutcomeAborted ||
		last.Detail != "canary regression confirmed manually" || last.At != now {
		t.Errorf("last entry = %+v, want an aborted entry recording the reason", last)
	}

	out := renderAbort(updated, "canary regression confirmed manually")
	if !strings.Contains(out, "reason        canary regression confirmed manually") {
		t.Errorf("render = %q, want the reason reported", out)
	}
	if !strings.Contains(out, "caller        "+updated.Caller().Identity) {
		t.Errorf("render = %q, want the caller identity reported", out)
	}
}

func TestAbortRollout_NeverGeneratesFull(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("rollout.argoproj.io/safelane-demo-api aborted\n", nil)

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run

	if _, err := abortRollout(context.Background(), rel, ex, "bad canary", time.Now); err != nil {
		t.Fatalf("abortRollout: %v", err)
	}
	for _, call := range q.calls {
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

func TestAbortRollout_ClassifiesAFailure(t *testing.T) {
	rel := guardedLaneStarted(t)

	q := &queueRunner{}
	q.enqueue("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})

	ex := execute.New(execute.Config{Namespace: "safelane-demo-api", Rollout: "safelane-demo-api"})
	ex.Run = q.run

	_, err := abortRollout(context.Background(), rel, ex, "bad canary", time.Now)
	var rerr *release.Error
	if !errors.As(err, &rerr) || rerr.Code != "kubectl_missing" {
		t.Fatalf("err = %v, want a kubectl_missing *release.Error", err)
	}
}

func TestParseRolloutAbortFlags_RequiresReleaseIDAndReason(t *testing.T) {
	var stderr strings.Builder
	if _, _, err := parseRolloutAbortFlags([]string{"rel_x"}, &stderr, "store"); err == nil {
		t.Error("want an error with no --reason")
	}
	stderr.Reset()
	if _, _, err := parseRolloutAbortFlags(nil, &stderr, "store"); err == nil {
		t.Error("want an error with no release id")
	}
	stderr.Reset()
	f, id, err := parseRolloutAbortFlags([]string{"--reason", "bad canary", "rel_x"}, &stderr, "store")
	if err != nil {
		t.Fatalf("parseRolloutAbortFlags: %v", err)
	}
	if id != "rel_x" || f.reason != "bad canary" {
		t.Errorf("got id=%q reason=%q, want rel_x/bad canary", id, f.reason)
	}
}
