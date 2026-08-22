package release_test

import (
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/release"
)

func hexDigest(seed string) string {
	return "sha256:" + strings.Repeat(seed, 64/len(seed))
}

func testTemplateIdentity() release.TemplateIdentity {
	return release.TemplateIdentity{
		Name:          "safelane-demo-api-canary",
		Version:       "v0.1.0-fixture",
		ContentDigest: hexDigest("a1b2"),
		FileCount:     5,
	}
}

func testEnvelopeTarget() release.Target {
	return release.Target{
		Application: "safelane-demo-api", Environment: "production",
		Cluster: "safelane-demo", Namespace: "safelane-demo-api",
	}
}

func rolloutResource(t *testing.T, stepsYAML string) release.RenderedResource {
	t.Helper()
	body := "apiVersion: argoproj.io/v1alpha1\n" +
		"kind: Rollout\n" +
		"metadata:\n" +
		"  name: safelane-demo-api\n" +
		"  namespace: safelane-demo-api\n" +
		"spec:\n" +
		"  strategy:\n" +
		"    canary:\n" +
		stepsYAML
	res, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "40-rollout.yaml.tmpl",
		APIVersion:   "argoproj.io/v1alpha1",
		Kind:         "Rollout",
		Namespace:    "safelane-demo-api",
		Name:         "safelane-demo-api",
	}, []byte(body))
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	return res
}

func serviceResource(t *testing.T, digest string) release.RenderedResource {
	t.Helper()
	res, err := release.NewRenderedResource(release.ResourceRef{
		TemplatePath: "10-service.yaml.tmpl",
		APIVersion:   "v1",
		Kind:         "Service",
		Namespace:    "safelane-demo-api",
		Name:         "safelane-demo-api-stable",
	}, []byte("apiVersion: v1\nkind: Service\nmetadata:\n  name: safelane-demo-api-stable\n# "+digest+"\n"))
	if err != nil {
		t.Fatalf("NewRenderedResource: %v", err)
	}
	return res
}

func TestDeriveEnvelope_ParsesExplicitStepsAndAppendsTheImplicitFinalWeight(t *testing.T) {
	digest := hexDigest("c3d4")
	rollout := rolloutResource(t, "      steps:\n"+
		"        - setWeight: 1\n"+
		"        - pause: {}\n"+
		"        - setWeight: 5\n"+
		"        - pause: {}\n"+
		"        - setWeight: 25\n"+
		"        - pause: {}\n"+
		"        - setWeight: 50\n"+
		"        - pause: {}\n")

	bundle, err := release.NewRenderedBundle(testTemplateIdentity(), testEnvelopeTarget(), digest,
		[]release.RenderedResource{serviceResource(t, digest), rollout})
	if err != nil {
		t.Fatalf("NewRenderedBundle: %v", err)
	}

	env, templateDigest, err := release.DeriveEnvelope(bundle)
	if err != nil {
		t.Fatalf("DeriveEnvelope: %v", err)
	}
	want := []int{1, 5, 25, 50, 100}
	if got := env.Stages(); !intSliceEqual(got, want) {
		t.Errorf("stages = %v, want %v (the guarded lane, read back from the rendered bytes)", got, want)
	}
	if env.NextAction() != "start" {
		t.Errorf("next action = %q, want start", env.NextAction())
	}
	if templateDigest != testTemplateIdentity().ContentDigest {
		t.Errorf("template digest = %q, want %q -- the derived envelope must carry the template digest", templateDigest, testTemplateIdentity().ContentDigest)
	}
}

func TestDeriveEnvelope_FastLane_OneGate(t *testing.T) {
	digest := hexDigest("e5f6")
	rollout := rolloutResource(t, "      steps:\n"+
		"        - setWeight: 5\n"+
		"        - pause: {}\n")
	bundle, err := release.NewRenderedBundle(testTemplateIdentity(), testEnvelopeTarget(), digest,
		[]release.RenderedResource{serviceResource(t, digest), rollout})
	if err != nil {
		t.Fatalf("NewRenderedBundle: %v", err)
	}
	env, _, err := release.DeriveEnvelope(bundle)
	if err != nil {
		t.Fatalf("DeriveEnvelope: %v", err)
	}
	if want := []int{5, 100}; !intSliceEqual(env.Stages(), want) {
		t.Errorf("stages = %v, want %v", env.Stages(), want)
	}
}

func TestDeriveEnvelope_NoRolloutInBundle_IsAnError(t *testing.T) {
	digest := hexDigest("0102")
	bundle, err := release.NewRenderedBundle(testTemplateIdentity(), testEnvelopeTarget(), digest,
		[]release.RenderedResource{serviceResource(t, digest)})
	if err != nil {
		t.Fatalf("NewRenderedBundle: %v", err)
	}
	if _, _, err := release.DeriveEnvelope(bundle); err == nil {
		t.Fatal("want an error when the bundle contains no Rollout resource")
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
