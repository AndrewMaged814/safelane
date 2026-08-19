package execute_test

import (
	"context"
	"strings"
	"testing"
)

// analysisRunJSON is a canned `kubectl get analysisrun -o json`, shaped
// after the live kind cluster's own background AnalysisRun for the
// fixture template's one metric (Appendix C3).
const analysisRunJSON = `{
  "status": {
    "phase": "Successful",
    "metricResults": [
      {
        "name": "request-success-rate",
        "count": 3,
        "successful": 3,
        "measurements": [
          {"value": "[1]"},
          {"value": "[1]"},
          {"value": "[1]"}
        ]
      }
    ]
  },
  "spec": {
    "metrics": [
      {"name": "request-success-rate", "successCondition": "len(result) > 0 && result[0] >= 0.99"}
    ]
  }
}`

func TestGetAnalysisRun_ParsesPhaseAndMeasurement(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue(analysisRunJSON, nil)
	ex := newTestExecutor(fr)

	run, err := ex.GetAnalysisRun(context.Background(), "podinfo-5f9b48bf7c-2")
	if err != nil {
		t.Fatalf("GetAnalysisRun: %v", err)
	}
	if run.Phase != "Successful" {
		t.Errorf("Phase = %q, want Successful", run.Phase)
	}
	if run.Metric.Name != "request-success-rate" || run.Metric.Count != 3 || run.Metric.Successful != 3 {
		t.Errorf("Metric = %+v, want request-success-rate 3/3", run.Metric)
	}
	if run.Metric.Measured != 1.0 {
		t.Errorf("Measured = %v, want 1.0 (the last measurement)", run.Metric.Measured)
	}
	if run.Metric.Condition != ">= 0.99" {
		t.Errorf("Condition = %q, want >= 0.99", run.Metric.Condition)
	}
}

func TestGetAnalysisRun_ReadsTheRealNameNotAFriendlyOne(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue(analysisRunJSON, nil)
	ex := newTestExecutor(fr)

	if _, err := ex.GetAnalysisRun(context.Background(), "podinfo-5f9b48bf7c-2"); err != nil {
		t.Fatalf("GetAnalysisRun: %v", err)
	}
	args := strings.Join(fr.calls[0], " ")
	if !strings.Contains(args, "get analysisrun podinfo-5f9b48bf7c-2") {
		t.Errorf("args = %q, want the real Argo-assigned name queried verbatim", args)
	}
	if strings.Contains(args, "--kubeconfig") || strings.Contains(args, "--context") {
		t.Errorf("args = %q, want no controller flags -- caller identity is enough (Appendix C5)", args)
	}
}

func TestGetAnalysisRun_LowerCaseMeasurement(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue(`{"status":{"phase":"Failed","metricResults":[{"name":"request-success-rate","count":3,`+
		`"successful":2,"measurements":[{"value":"[0.71]"}]}]},`+
		`"spec":{"metrics":[{"name":"request-success-rate","successCondition":"len(result) == 0 || result[0] >= 0.99"}]}}`, nil)
	ex := newTestExecutor(fr)

	run, err := ex.GetAnalysisRun(context.Background(), "podinfo-5f9b48bf7c-3")
	if err != nil {
		t.Fatalf("GetAnalysisRun: %v", err)
	}
	if run.Metric.Measured != 0.71 {
		t.Errorf("Measured = %v, want 0.71", run.Metric.Measured)
	}
	if run.Metric.Condition != ">= 0.99" {
		t.Errorf("Condition = %q, want >= 0.99 regardless of the boilerplate around it", run.Metric.Condition)
	}
}
