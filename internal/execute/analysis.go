package execute

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/AndrewMaged814/safelane/internal/release"
)

// AnalysisMetric is one measured metric off a background AnalysisRun: the
// evidence Appendix A2.3 prints alongside the coarse phase Argo already put
// on the Rollout's own status.
type AnalysisMetric struct {
	Name     string
	Measured float64
	// Condition is the threshold half of the metric's successCondition
	// expression -- ">= 0.99", not the whole `len(result) > 0 && ...`
	// boilerplate every template repeats around it.
	Condition  string
	Count      int
	Successful int
	// FailureLimit is the metric's own `failureLimit` (Appendix C3's
	// fixture template sets 1): how many failed measurements Argo
	// Rollouts tolerates before it aborts the rollout on its own (A3.4).
	FailureLimit int
}

// AnalysisRun is the subset of `kubectl get analysisrun -o json` Appendix
// A2.3's measurement line needs. SafeLane's fixture template declares
// exactly one metric per AnalysisTemplate (Appendix C3), so this reads the
// first one; a template with more would need more than one line here
// anyway, and nothing in this build declares one.
type AnalysisRun struct {
	Phase  string
	Metric AnalysisMetric
}

type analysisRunDoc struct {
	Status struct {
		Phase         string `json:"phase"`
		MetricResults []struct {
			Name         string `json:"name"`
			Count        int    `json:"count"`
			Successful   int    `json:"successful"`
			Measurements []struct {
				Value string `json:"value"`
			} `json:"measurements"`
		} `json:"metricResults"`
	} `json:"status"`
	Spec struct {
		Metrics []struct {
			Name             string `json:"name"`
			SuccessCondition string `json:"successCondition"`
			FailureLimit     int    `json:"failureLimit"`
		} `json:"metrics"`
	} `json:"spec"`
}

// GetAnalysisRun reads one AnalysisRun by its real Argo-assigned name
// (Status.AnalysisRunName, not the friendly display label a caller prints
// -- Appendix A2.3's measurement line comes from here; the Rollout's own
// status never carries more than the coarse phase). The caller is allowed
// to read only the Rollout, so the controller credential reads this separate
// Argo resource without widening the caller's RBAC boundary.
func (e *Executor) GetAnalysisRun(ctx context.Context, name string) (AnalysisRun, error) {
	args := append([]string{"get", "analysisrun", name, "-n", e.Namespace, "-o", "json"}, e.privilegedFlags()...)
	out, err := e.run(ctx, "kubectl get analysisrun", args, nil)
	if err != nil {
		return AnalysisRun{}, err
	}
	return parseAnalysisRun(out)
}

func parseAnalysisRun(raw []byte) (AnalysisRun, error) {
	var doc analysisRunDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return AnalysisRun{}, release.Internal("unparseable_analysisrun_status",
			fmt.Sprintf("kubectl get analysisrun -o json did not decode: %v", err))
	}
	run := AnalysisRun{Phase: doc.Status.Phase}
	if len(doc.Status.MetricResults) == 0 {
		return run, nil
	}
	mr := doc.Status.MetricResults[0]
	metric := AnalysisMetric{Name: mr.Name, Count: mr.Count, Successful: mr.Successful}
	if len(mr.Measurements) > 0 {
		metric.Measured = lastMeasuredValue(mr.Measurements[len(mr.Measurements)-1].Value)
	}
	for _, m := range doc.Spec.Metrics {
		if m.Name == mr.Name {
			metric.Condition = successConditionThreshold(m.SuccessCondition)
			metric.FailureLimit = m.FailureLimit
			break
		}
	}
	run.Metric = metric
	return run, nil
}

// lastMeasuredValue decodes a measurement's raw value, which the
// prometheus provider reports as a JSON-encoded vector string like "[1]"
// or "[0.71]", and returns its first (only) element. A value that does
// not decode this way -- a provider this build never exercises -- reads
// as 0 rather than panicking; the phase and count printed alongside it
// still tell the truth.
func lastMeasuredValue(raw string) float64 {
	var vec []float64
	if err := json.Unmarshal([]byte(raw), &vec); err != nil || len(vec) == 0 {
		return 0
	}
	return vec[0]
}

// successConditionRE pulls the threshold clause -- ">= 0.99"-- out of a
// successCondition expression built around `result[0]`. The fixture
// template's exact boilerplate around it (`len(result) > 0 && ...`) varies
// across the demo's two templates (Appendix C5's live-cluster note), so
// this searches for the clause rather than assuming the whole string.
var successConditionRE = regexp.MustCompile(`result\[0\]\s*(>=|<=|==|>|<)\s*([0-9]*\.?[0-9]+)`)

func successConditionThreshold(expr string) string {
	m := successConditionRE.FindStringSubmatch(expr)
	if m == nil {
		return ""
	}
	return m[1] + " " + m[2]
}
