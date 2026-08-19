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
		} `json:"metrics"`
	} `json:"spec"`
}

// GetAnalysisRun reads one AnalysisRun by its real Argo-assigned name
// (Status.AnalysisRunName, not the friendly display label a caller prints
// -- Appendix A2.3's measurement line comes from here; the Rollout's own
// status never carries more than the coarse phase). It is an unprivileged
// call, the same as GetStatus.
func (e *Executor) GetAnalysisRun(ctx context.Context, name string) (AnalysisRun, error) {
	args := []string{"get", "analysisrun", name, "-n", e.Namespace, "-o", "json"}
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
