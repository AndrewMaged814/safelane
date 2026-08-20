package ledger_test

import (
	"testing"
	"time"

	"github.com/AndrewMaged814/safelane/internal/ledger"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

func attempt(t *testing.T, id release.ReleaseID, number int, retryOf release.ReleaseID) *release.Release {
	t.Helper()
	elig, err := release.Indeterminate("3", "verification_incomplete", "mandatory evidence is incomplete")
	if err != nil {
		t.Fatal(err)
	}
	r, err := release.NewRelease(release.ReleaseParams{
		ID: id, AttemptNumber: number, RetryOf: retryOf, State: release.StateIndeterminate,
		Intent:      release.Intent{SchemaVersion: release.RequestSchemaVersion, Repository: "owner/repo", PullRequest: 4, Environment: "production"},
		Target:      release.Target{Application: "app", Environment: "production", Cluster: "cluster", Namespace: "app", Rollout: "app"},
		Evidence:    release.UnknownEvidence(release.UnknownEvidenceError("check_incomplete", "ci", "waiting", "retry")),
		Eligibility: elig, CreatedAt: time.Date(2026, 8, 20, number, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ineligibleAttempt(t *testing.T, id release.ReleaseID) *release.Release {
	t.Helper()
	elig, err := release.Ineligible("3", "required_check_failed", "a mandatory check failed")
	if err != nil {
		t.Fatal(err)
	}
	r, err := release.NewRelease(release.ReleaseParams{ID: id, AttemptNumber: 1, State: release.StateIneligible,
		Intent:      release.Intent{SchemaVersion: release.RequestSchemaVersion, Repository: "owner/repo", PullRequest: 5, Environment: "production"},
		Target:      release.Target{Application: "app", Environment: "production", Cluster: "cluster", Namespace: "app", Rollout: "app"},
		Evidence:    release.FailedEvidence(release.FailedEvidenceError("required_check_failed", "ci", "failed", "fix CI")),
		Eligibility: elig, CreatedAt: time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolveReusesLatestAttemptAndRetryLinksHistory(t *testing.T) {
	l := ledger.ReleaseLedger{Store: &store.FileStore{Dir: t.TempDir()}}
	first := attempt(t, "rel_01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "")
	if err := l.Save(first); err != nil {
		t.Fatal(err)
	}
	latest, history, err := l.Resolve(ledger.SubjectOf(first))
	if err != nil || latest.ID != first.ID || len(history) != 1 {
		t.Fatalf("resolve = %v %v %v", latest, len(history), err)
	}
	second := attempt(t, "rel_01ARZ3NDEKTSV4RRFFQ69G5FAW", 2, first.ID)
	if err := l.Save(second); err != nil {
		t.Fatal(err)
	}
	latest, history, err = l.Resolve(ledger.SubjectOf(first))
	if err != nil || latest.ID != second.ID || len(history) != 2 || history[1].RetryOf() != first.ID {
		t.Fatalf("retry history is not ordered/linked: latest=%v history=%v err=%v", latest.ID, len(history), err)
	}
}

func TestRetryRefusesNonLatestOrNonRetryableAttempt(t *testing.T) {
	l := ledger.ReleaseLedger{Store: &store.FileStore{Dir: t.TempDir()}}
	first := attempt(t, "rel_01ARZ3NDEKTSV4RRFFQ69G5FAV", 1, "")
	if err := l.Save(first); err != nil {
		t.Fatal(err)
	}
	second := attempt(t, "rel_01ARZ3NDEKTSV4RRFFQ69G5FAW", 2, first.ID)
	if err := l.Save(second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.RetryParent(first.ID); err == nil {
		t.Fatal("retrying a superseded attempt succeeded")
	}
	nonRetryable := ineligibleAttempt(t, "rel_01ARZ3NDEKTSV4RRFFQ69G5FAX")
	if err := l.Save(nonRetryable); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.RetryParent(nonRetryable.ID); err == nil {
		t.Fatal("retrying an ineligible attempt succeeded")
	}
}
