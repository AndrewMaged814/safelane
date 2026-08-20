package execute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// State is SafeLane's read of an Argo Rollout's status, mapped from
// Appendix C5's table. It has exactly the seven states that table names;
// nothing here invents an eighth.
type State string

const (
	StateNotStarted  State = "not_started"
	StateProgressing State = "progressing"
	StateAnalysing   State = "analysing"
	StateAtGate      State = "at_gate"
	StateComplete    State = "complete"
	StateDegraded    State = "degraded"
	StateAborted     State = "aborted"
)

// Status is one read of the Rollout's status.
type Status struct {
	State State
	// Phase is Argo's raw phase for human diagnostics such as doctor.
	Phase string
	// ImageDigest is the immutable digest observed in the Rollout pod template.
	ImageDigest string
	// CurrentWeight is the canary weight Argo has actually granted,
	// preferring `.status.canary.weights.canary.weight` and falling back
	// to the last `setWeight` step at or before currentStepIndex when
	// that field is absent (Appendix C5) -- one code path serves every
	// lane either way.
	CurrentWeight int
	// Gate is the 1-based gate number this status corresponds to, when
	// State is StateAtGate: the number of setWeight steps completed.
	Gate int
	// AnalysisRunName is the name Argo Rollouts itself gave the
	// background AnalysisRun tracking this revision's canary (Rollout's
	// own `.status.canary.currentBackgroundAnalysisRunStatus.name`), or
	// "" when none is running. This is the real object name to query --
	// it is not the friendly `<application>-success-rate-<N>` label a
	// caller prints; that label is built separately from this name's own
	// trailing ordinal (task 10).
	AnalysisRunName string
	// AnalysisRunPhase is Argo's own coarse read of that AnalysisRun
	// ("Running", "Successful", "Failed", ...), straight off the
	// Rollout's status. The measurement detail behind it -- which metric,
	// what was measured, against what condition -- is not on the Rollout
	// at all; [Executor.GetAnalysisRun] fetches the AnalysisRun object
	// itself for that.
	AnalysisRunPhase string
	// CurrentPodHash and Revision are what a background AnalysisRun's own
	// name is built from (`<rollout>-<CurrentPodHash>-<Revision>`,
	// confirmed against the live cluster). Unlike AnalysisRunName -- which
	// Argo clears from the Rollout's status once it settles Healthy, a
	// race this build hit in its own live rehearsal -- these two persist
	// on a Healthy Rollout, so a caller that reaches Complete after that
	// field is already gone can still reconstruct the name deterministically.
	CurrentPodHash string
	Revision       string
}

// rolloutStatusDoc is the subset of `kubectl get rollout -o json` this
// package reads. Everything else in the document is ignored.
type rolloutStatusDoc struct {
	Metadata struct {
		Annotations struct {
			Revision string `json:"rollout.argoproj.io/revision"`
		} `json:"annotations"`
	} `json:"metadata"`
	Status struct {
		Phase            string `json:"phase"`
		Abort            bool   `json:"abort"`
		StableRS         string `json:"stableRS"`
		CurrentPodHash   string `json:"currentPodHash"`
		CurrentStepIndex *int   `json:"currentStepIndex"`
		PauseConditions  []any  `json:"pauseConditions"`
		Canary           struct {
			CurrentStepAnalysisRunStatus struct {
				Status string `json:"status"`
			} `json:"currentStepAnalysisRunStatus"`
			Weights struct {
				Canary struct {
					Weight *int `json:"weight"`
				} `json:"canary"`
			} `json:"weights"`
			CurrentBackgroundAnalysisRunStatus struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"currentBackgroundAnalysisRunStatus"`
		} `json:"canary"`
	} `json:"status"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
		Strategy struct {
			Canary struct {
				Steps []struct {
					SetWeight *int `json:"setWeight"`
				} `json:"steps"`
			} `json:"canary"`
		} `json:"strategy"`
	} `json:"spec"`
}

// GetStatus reads the Rollout's status. It is an unprivileged call
// (Appendix C5: "caller identity is enough"), so it never carries the
// controller kubeconfig/context flags.
func (e *Executor) GetStatus(ctx context.Context) (Status, error) {
	args := []string{"get", "rollout", e.Rollout, "-n", e.Namespace, "-o", "json"}
	out, err := e.run(ctx, "kubectl get rollout", args, nil)
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out)
}

func parseStatus(raw []byte) (Status, error) {
	var doc rolloutStatusDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Status{}, release.Internal("unparseable_rollout_status",
			fmt.Sprintf("kubectl get rollout -o json did not decode: %v", err))
	}
	weight, stepsCompleted := currentWeight(doc)
	return Status{
		State:            classifyState(doc),
		Phase:            doc.Status.Phase,
		ImageDigest:      rolloutImageDigest(doc),
		CurrentWeight:    weight,
		Gate:             stepsCompleted,
		AnalysisRunName:  doc.Status.Canary.CurrentBackgroundAnalysisRunStatus.Name,
		AnalysisRunPhase: doc.Status.Canary.CurrentBackgroundAnalysisRunStatus.Status,
		CurrentPodHash:   doc.Status.CurrentPodHash,
		Revision:         doc.Metadata.Annotations.Revision,
	}, nil
}

func rolloutImageDigest(doc rolloutStatusDoc) string {
	for _, container := range doc.Spec.Template.Spec.Containers {
		if _, digest, ok := strings.Cut(container.Image, "@"); ok {
			return digest
		}
	}
	return ""
}

// classifyState maps the observed document onto Appendix C5's table.
// Order matters: the terminal states (aborted, degraded, complete) are
// checked before the in-progress ones, so a Rollout that is both
// Progressing and aborted -- Argo sets both -- is reported as aborted.
func classifyState(doc rolloutStatusDoc) State {
	switch {
	case doc.Status.Abort:
		return StateAborted
	case doc.Status.Phase == "Degraded":
		return StateDegraded
	case doc.Status.Phase == "Healthy" && doc.Status.StableRS != "" && doc.Status.StableRS == doc.Status.CurrentPodHash:
		return StateComplete
	case doc.Status.Phase == "Paused" && len(doc.Status.PauseConditions) > 0:
		return StateAtGate
	case doc.Status.Canary.CurrentStepAnalysisRunStatus.Status == "Running":
		return StateAnalysing
	case doc.Status.Phase == "Progressing":
		return StateProgressing
	default:
		return StateNotStarted
	}
}

// currentWeight implements Appendix C5's fallback: prefer the traffic
// router's own observed weight, and fall back to scanning the completed
// canary steps for the last setWeight otherwise. It also returns how many
// setWeight steps have completed, which is the gate number once the
// Rollout is paused.
func currentWeight(doc rolloutStatusDoc) (weight, stepsCompleted int) {
	idx := -1
	if doc.Status.CurrentStepIndex != nil {
		idx = *doc.Status.CurrentStepIndex
	}
	steps := doc.Spec.Strategy.Canary.Steps
	for i := 0; i <= idx && i < len(steps); i++ {
		if steps[i].SetWeight != nil {
			weight = *steps[i].SetWeight
			stepsCompleted++
		}
	}
	if w := doc.Status.Canary.Weights.Canary.Weight; w != nil {
		weight = *w
	}
	return weight, stepsCompleted
}

// ErrGateTimeout is returned by [Executor.WaitForGate] when the Rollout
// does not reach a stopping state within the given timeout. Appendix C6:
// the caller must exit 3, never treat this as exit 1, and never retry the
// promotion that led here -- it may already have taken effect.
var ErrGateTimeout = errors.New("timed out waiting for the rollout")

// WaitForGate polls GetStatus every PollInterval until the Rollout reaches
// a stopping state (at_gate, complete, degraded, aborted) or timeout
// elapses (Appendix C5). onTransition, if non-nil, is called once for
// every distinct State observed, in order, so a caller can print progress
// as it happens rather than only at the end.
func (e *Executor) WaitForGate(ctx context.Context, timeout time.Duration, onTransition func(Status)) (Status, error) {
	deadline := e.now().Add(timeout)
	var last State
	first := true

	for {
		st, err := e.GetStatus(ctx)
		if err != nil {
			return Status{}, err
		}
		if first || st.State != last {
			if onTransition != nil {
				onTransition(st)
			}
			last = st.State
			first = false
		}

		switch st.State {
		case StateAtGate, StateComplete, StateDegraded, StateAborted:
			return st, nil
		}

		if !e.now().Before(deadline) {
			return st, ErrGateTimeout
		}
		e.sleep(e.pollInterval())
	}
}
