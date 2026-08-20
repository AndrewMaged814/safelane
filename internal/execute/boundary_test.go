package execute

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestAssertBoundaryUsesCallerIdentityAndRecordsCapability(t *testing.T) {
	var calls [][]string
	e := New(Config{Namespace: "podinfo", ControllerKubeconfig: "controller.kubeconfig", ControllerContext: "controller"})
	e.Now = func() time.Time { return time.Date(2026, 8, 20, 14, 26, 0, 0, time.UTC) }
	e.Run = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		if args[6] == "get" {
			return []byte("yes\n"), nil
		}
		return []byte("no\n"), nil
	}
	b, err := e.AssertBoundary(context.Background(), "system:serviceaccount:podinfo:safelane-controller", "system:serviceaccount:podinfo:safelane-caller")
	if err != nil {
		t.Fatal(err)
	}
	if !b.CallerCapability.GetRollouts || b.CallerCapability.PatchRollouts {
		t.Fatalf("capability = %+v", b.CallerCapability)
	}
	wantGet := []string{"--kubeconfig", "controller.kubeconfig", "--context", "controller", "auth", "can-i", "get", "rollouts.argoproj.io", "--namespace", "podinfo", "--as", "system:serviceaccount:podinfo:safelane-caller"}
	if !reflect.DeepEqual(calls[0], wantGet) {
		t.Fatalf("get args = %v", calls[0])
	}
	if len(calls) != 2 || calls[1][6] != "patch" {
		t.Fatalf("calls = %v", calls)
	}
}
