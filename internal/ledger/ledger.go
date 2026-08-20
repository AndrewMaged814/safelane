// Package ledger owns local release-attempt identity and transition rules.
package ledger

import (
	"errors"
	"fmt"
	"sort"

	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/store"
)

// ReleaseLedger is a single-writer ledger over the atomic per-release FileStore.
type ReleaseLedger struct{ Store *store.FileStore }

type Subject struct {
	Repository  string
	PullRequest int
	Target      release.Target
}

func SubjectOf(r *release.Release) Subject {
	return Subject{Repository: r.Request().Repository, PullRequest: r.Request().PullRequest, Target: r.Target()}
}

func (s Subject) Matches(r *release.Release) bool {
	got := SubjectOf(r)
	return got.Repository == s.Repository && got.PullRequest == s.PullRequest && got.Target == s.Target
}

func (l ReleaseLedger) History(subject Subject) ([]*release.Release, error) {
	all, err := l.Store.List()
	if err != nil {
		return nil, err
	}
	var history []*release.Release
	for _, r := range all {
		if subject.Matches(r) {
			history = append(history, r)
		}
	}
	sort.Slice(history, func(i, j int) bool {
		if history[i].AttemptNumber() != history[j].AttemptNumber() {
			return history[i].AttemptNumber() < history[j].AttemptNumber()
		}
		return history[i].CreatedAt.Before(history[j].CreatedAt)
	})
	return history, nil
}

// Resolve returns the latest existing attempt. Inspection must call this before creating one.
func (l ReleaseLedger) Resolve(subject Subject) (*release.Release, []*release.Release, error) {
	h, err := l.History(subject)
	if err != nil || len(h) == 0 {
		return nil, h, err
	}
	return h[len(h)-1], h, nil
}

func (l ReleaseLedger) Save(r *release.Release) error {
	h, err := l.History(SubjectOf(r))
	if err != nil {
		return err
	}
	if len(h) > 0 {
		last := h[len(h)-1]
		if r.AttemptNumber() != last.AttemptNumber()+1 || r.RetryOf() != last.ID {
			return fmt.Errorf("ledger: attempt %d must retry latest attempt %s", r.AttemptNumber(), last.ID)
		}
		if !last.State().Retryable() {
			return fmt.Errorf("ledger: release %s in state %s cannot be retried", last.ID, last.State())
		}
	} else if r.AttemptNumber() != 1 {
		return fmt.Errorf("ledger: first attempt must be attempt 1")
	}
	return l.Store.Save(r)
}

func (l ReleaseLedger) Update(r *release.Release) error                     { return l.Store.Update(r) }
func (l ReleaseLedger) Load(id release.ReleaseID) (*release.Release, error) { return l.Store.Load(id) }

// RetryParent validates that id is the latest attempt and has an explicitly retryable outcome.
func (l ReleaseLedger) RetryParent(id release.ReleaseID) (*release.Release, int, error) {
	r, err := l.Load(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, 0, fmt.Errorf("ledger: release %s not found", id)
		}
		return nil, 0, err
	}
	latest, history, err := l.Resolve(SubjectOf(r))
	if err != nil {
		return nil, 0, err
	}
	if latest.ID != id {
		return nil, 0, fmt.Errorf("ledger: retry the latest attempt %s, not %s", latest.ID, id)
	}
	if !r.State().Retryable() {
		return nil, 0, fmt.Errorf("ledger: release %s in state %s cannot be retried", id, r.State())
	}
	return r, len(history) + 1, nil
}
