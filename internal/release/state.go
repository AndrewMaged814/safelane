package release

import "fmt"

// State is the canonical, locally recorded lifecycle state of one release attempt.
type State string

const (
	StateReady         State = "ready"
	StateIneligible    State = "ineligible"
	StateIndeterminate State = "indeterminate"
	StateStarting      State = "starting"
	StateProgressing   State = "progressing"
	StateAnalysing     State = "analysing"
	StateAtGate        State = "at_gate"
	StatePaused        State = "paused"
	StatePromoted      State = "promoted"
	StateAborted       State = "aborted"
	StateFailed        State = "failed"
	StateBlocked       State = "blocked"
	StateUnknown       State = "unknown"
)

func (s State) Validate() error {
	switch s {
	case StateReady, StateIneligible, StateIndeterminate, StateStarting, StateProgressing,
		StateAnalysing, StateAtGate, StatePaused, StatePromoted, StateAborted, StateFailed,
		StateBlocked, StateUnknown:
		return nil
	default:
		return fmt.Errorf("invalid release state %q", s)
	}
}

func (s State) Active() bool {
	switch s {
	case StateStarting, StateProgressing, StateAnalysing, StateAtGate, StatePaused:
		return true
	default:
		return false
	}
}

func (s State) Retryable() bool {
	switch s {
	case StateAborted, StateFailed, StateBlocked, StateIndeterminate:
		return true
	default:
		return false
	}
}

// ExecutionBinding is the correlation proof between a local attempt and one Rollout execution.
type ExecutionBinding struct {
	ReleaseID       ReleaseID `json:"release_id"`
	Application     string    `json:"application"`
	Environment     string    `json:"environment"`
	Cluster         string    `json:"cluster"`
	Namespace       string    `json:"namespace"`
	Rollout         string    `json:"rollout"`
	Digest          string    `json:"digest,omitempty"`
	Generation      int64     `json:"generation,omitempty"`
	ArgoRevision    string    `json:"argo_revision,omitempty"`
	AnalysisRunName string    `json:"analysis_run_name,omitempty"`
	PreGeneration   int64     `json:"pre_generation,omitempty"`
	PreDigest       string    `json:"pre_digest,omitempty"`
	PreArgoRevision string    `json:"pre_argo_revision,omitempty"`
	PreAnalysisRun  string    `json:"pre_analysis_run_name,omitempty"`
}

func (b ExecutionBinding) IsZero() bool { return b == ExecutionBinding{} }

func (b ExecutionBinding) Matches(id ReleaseID, target Target, digest string) bool {
	return b.ReleaseID == id && b.Application == target.Application && b.Environment == target.Environment &&
		b.Cluster == target.Cluster && b.Namespace == target.Namespace && b.Rollout == target.Rollout &&
		b.Digest != "" && b.Digest == digest && b.Generation > 0
}
