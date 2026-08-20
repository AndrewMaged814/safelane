package release_test

import (
	"testing"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func TestExecutionBindingRequiresPostMutationGeneration(t *testing.T) {
	id := release.ReleaseID("rel_01ARZ3NDEKTSV4RRFFQ69G5FAV")
	target := release.Target{Application: "app", Environment: "production", Cluster: "cluster", Namespace: "app", Rollout: "app"}
	b := release.ExecutionBinding{ReleaseID: id, Application: "app", Environment: "production", Cluster: "cluster", Namespace: "app", Rollout: "app", Digest: "sha256:abc", PreGeneration: 7}
	if b.Matches(id, target, "sha256:abc") {
		t.Fatal("a write-ahead binding fabricated a runtime match before result generation was persisted")
	}
	b.Generation = 8
	if !b.Matches(id, target, "sha256:abc") {
		t.Fatal("complete execution binding did not match")
	}
	b.Rollout = "other"
	if b.Matches(id, target, "sha256:abc") {
		t.Fatal("rollout target mismatch correlated")
	}
}
