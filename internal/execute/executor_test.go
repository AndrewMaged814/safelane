package execute_test

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

// fakeRunner is Appendix D's "fake cmdFactory returning canned Argo JSON":
// every call this package makes goes through it, so none of these tests
// touch a real cluster. Responses are consumed in call order; calls are
// recorded so a test can assert on the exact argument list.
type fakeRunner struct {
	calls     [][]string
	stdins    [][]byte
	responses [][]byte
	errs      []error
	i         int
}

func (f *fakeRunner) run(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	f.stdins = append(f.stdins, stdin)
	if f.i >= len(f.responses) {
		return nil, errors.New("fakeRunner: no more canned responses")
	}
	out, err := f.responses[f.i], f.errs[f.i]
	f.i++
	return out, err
}

func (f *fakeRunner) enqueue(out string, err error) {
	f.responses = append(f.responses, []byte(out))
	f.errs = append(f.errs, err)
}

func testBundle(t *testing.T) release.RenderedBundle {
	t.Helper()
	svc, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "10-service.yaml.tmpl", APIVersion: "v1", Kind: "Service", Namespace: "podinfo", Name: "podinfo-stable",
	}, []byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: podinfo-stable\n  namespace: podinfo\n"))
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	rollout, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "40-rollout.yaml.tmpl", APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Namespace: "podinfo", Name: "podinfo",
	}, []byte("apiVersion: argoproj.io/v1alpha1\nkind: Rollout\nmetadata:\n  name: podinfo\n  namespace: podinfo\n"+
		"# sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8\n"))
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	tmpl := release.TemplateIdentity{Name: "podinfo-canary", Version: "v0.1.0-fixture", ContentDigest: "sha256:" + strings.Repeat("a1", 32), FileCount: 5}
	target := release.Target{Application: "podinfo", Environment: "production", Cluster: "safelane-demo", Namespace: "podinfo"}
	digest := "sha256:3fbc1d9a7e42c8056d1f9b3e7a5c204d8e6b1f39a7c50d28e4b6f19a3c7d50e8"
	bundle, err := release.NewRenderedBundle(tmpl, target, digest, []release.RenderedResource{svc, rollout})
	if err != nil {
		t.Fatalf("test setup: %v", err)
	}
	return bundle
}

func newTestExecutor(fr *fakeRunner) *execute.Executor {
	ex := execute.New(execute.Config{Namespace: "podinfo", Rollout: "podinfo"})
	ex.Run = fr.run
	ex.Sleep = func(time.Duration) {} // instant: WaitForGate tests drive time via Now
	return ex
}

func TestApply_ReportsOneRowPerResourceInBundleOrder(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo configured\n", nil)
	ex := newTestExecutor(fr)
	bundle := testBundle(t)

	rows, err := ex.Apply(context.Background(), bundle)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if rows[0].Ref.Kind != "Service" || rows[0].Verb != "unchanged" {
		t.Errorf("row 0 = %+v, want Service/unchanged", rows[0])
	}
	if rows[1].Ref.Kind != "Rollout" || rows[1].Verb != "configured" {
		t.Errorf("row 1 = %+v, want Rollout/configured", rows[1])
	}
}

func TestApply_SendsTheExactBundleBytesOnStdin(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo unchanged\n", nil)
	ex := newTestExecutor(fr)
	bundle := testBundle(t)

	if _, err := ex.Apply(context.Background(), bundle); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(fr.stdins) != 1 {
		t.Fatalf("want exactly one apply call, got %d", len(fr.stdins))
	}
	if string(fr.stdins[0]) != string(bundle.Manifest()) {
		t.Error("Apply did not send the exact bundle bytes on stdin -- it must never re-render")
	}
}

func TestApply_IsOneCallOverTheWholeBundle(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo unchanged\n", nil)
	ex := newTestExecutor(fr)

	if _, err := ex.Apply(context.Background(), testBundle(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(fr.calls) != 1 {
		t.Fatalf("want exactly 1 kubectl call, got %d: %v", len(fr.calls), fr.calls)
	}
	args := strings.Join(fr.calls[0], " ")
	if !strings.Contains(args, "apply") || !strings.Contains(args, "-f -") && !strings.Contains(args, "-f") {
		t.Errorf("apply args = %v, want an `apply -f -` invocation", fr.calls[0])
	}
}

func TestApply_MismatchedLineCountIsAnError(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\n", nil) // one line for two resources
	ex := newTestExecutor(fr)

	if _, err := ex.Apply(context.Background(), testBundle(t)); err == nil {
		t.Fatal("want an error when kubectl reports a different number of lines than resources")
	}
}

func TestPrivilegedFlags_ThreadedOnlyWhenConfigured(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo unchanged\n", nil)
	ex := execute.New(execute.Config{
		Namespace: "podinfo", Rollout: "podinfo",
		ControllerKubeconfig: "controller.kubeconfig", ControllerContext: "safelane-controller",
	})
	ex.Run = fr.run

	if _, err := ex.Apply(context.Background(), testBundle(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	args := strings.Join(fr.calls[0], " ")
	if !strings.Contains(args, "--kubeconfig controller.kubeconfig") || !strings.Contains(args, "--context safelane-controller") {
		t.Errorf("privileged apply args = %v, want the controller kubeconfig/context threaded through", fr.calls[0])
	}
}

func TestPrivilegedFlags_AbsentWhenNotConfigured(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo unchanged\n", nil)
	ex := newTestExecutor(fr)

	if _, err := ex.Apply(context.Background(), testBundle(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, a := range fr.calls[0] {
		if a == "--kubeconfig" || a == "--context" {
			t.Errorf("apply args = %v, want no controller flags when none are configured", fr.calls[0])
		}
	}
}

func TestGetStatus_NeverCarriesTheControllerFlags(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue(`{"status":{"phase":"Progressing"}}`, nil)
	ex := execute.New(execute.Config{
		Namespace: "podinfo", Rollout: "podinfo",
		ControllerKubeconfig: "controller.kubeconfig", ControllerContext: "safelane-controller",
	})
	ex.Run = fr.run

	if _, err := ex.GetStatus(context.Background()); err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	for _, a := range fr.calls[0] {
		if a == "--kubeconfig" || a == "--context" {
			t.Errorf("get rollout args = %v, want no controller flags -- caller identity is enough (Appendix C5)", fr.calls[0])
		}
	}
}

func TestArguments_NeverContainFull(t *testing.T) {
	// The one flag this package must never generate: `--full` jumps
	// straight to 100% and would silently defeat every lane.
	fr := &fakeRunner{}
	fr.enqueue("service/podinfo-stable unchanged\nrollout.argoproj.io/podinfo unchanged\n", nil)
	fr.enqueue(`{"status":{"phase":"Progressing"}}`, nil)
	fr.enqueue("rollout.argoproj.io/podinfo promoted\n", nil)
	ex := newTestExecutor(fr)

	if _, err := ex.Apply(context.Background(), testBundle(t)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := ex.GetStatus(context.Background()); err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if err := ex.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	for _, call := range fr.calls {
		for _, a := range call {
			if a == "--full" {
				t.Fatalf("generated argument list %v contains --full", call)
			}
		}
	}
}

func TestPromote_IsArgoRolloutsPromoteWithTheControllerFlags(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("rollout.argoproj.io/podinfo promoted\n", nil)
	ex := execute.New(execute.Config{
		Namespace: "podinfo", Rollout: "podinfo",
		ControllerKubeconfig: "controller.kubeconfig", ControllerContext: "safelane-controller",
	})
	ex.Run = fr.run

	if err := ex.Promote(context.Background()); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	got := strings.Join(fr.calls[0], " ")
	want := "argo rollouts promote podinfo -n podinfo --kubeconfig controller.kubeconfig --context safelane-controller"
	if got != want {
		t.Errorf("promote args = %q, want %q", got, want)
	}
}

func TestPromote_ClassifiesAFailureLikeEveryOtherCall(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})
	ex := newTestExecutor(fr)

	err := ex.Promote(context.Background())
	var rerr *release.Error
	if !errors.As(err, &rerr) || rerr.Code != "kubectl_missing" {
		t.Fatalf("err = %v, want a kubectl_missing *release.Error", err)
	}
}

func TestClassifyRunError_MissingBinaryIsHumanReadable(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("", &exec.Error{Name: "kubectl", Err: exec.ErrNotFound})
	ex := newTestExecutor(fr)

	_, err := ex.Apply(context.Background(), testBundle(t))
	if err == nil {
		t.Fatal("want an error when kubectl is missing")
	}
	var rerr *release.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %v (%T), want a *release.Error, not a raw stack trace", err, err)
	}
	if rerr.Code != "kubectl_missing" {
		t.Errorf("code = %q, want kubectl_missing", rerr.Code)
	}
	if rerr.Tag() != release.TagExecute {
		t.Errorf("tag = %q, want %q", rerr.Tag(), release.TagExecute)
	}
}

func TestClassifyRunError_OtherFailureIsClusterUnreachable(t *testing.T) {
	fr := &fakeRunner{}
	fr.enqueue("", errors.New("dial tcp: connection refused"))
	ex := newTestExecutor(fr)

	_, err := ex.Apply(context.Background(), testBundle(t))
	var rerr *release.Error
	if !errors.As(err, &rerr) {
		t.Fatalf("error = %v (%T), want a *release.Error", err, err)
	}
	if rerr.Code != "cluster_unreachable" {
		t.Errorf("code = %q, want cluster_unreachable", rerr.Code)
	}
}
