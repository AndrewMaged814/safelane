package release

import (
	"gopkg.in/yaml.v3"
)

// DeriveEnvelope parses the rollout steps back out of the Rollout
// resource inside a rendered bundle, so the envelope SafeLane enforces
// is provably the one in the manifest it hashed and applied -- not
// whatever the operator's policy said before rendering, which is a
// separate value that could in principle have drifted.
//
// The final weight is never rendered as its own step -- see
// render.TemplateData.Steps -- so it is not in the bytes to read back.
// It is appended here as 100 unconditionally: a canary strategy that
// runs out of steps always promotes to full traffic, on Argo Rollouts'
// own terms, not on any assumption this package makes about a
// particular lane's configured weights.
//
// It also returns the bundle's template content digest, so a caller can
// record which exact template produced this envelope alongside it.
func DeriveEnvelope(bundle RenderedBundle) (RolloutEnvelope, string, error) {
	var rolloutBytes []byte
	for _, res := range bundle.Resources() {
		if res.Ref().Kind == "Rollout" {
			rolloutBytes = res.Bytes()
			break
		}
	}
	if rolloutBytes == nil {
		return RolloutEnvelope{}, "", RenderError("no_rollout_in_bundle", "bundle.resources",
			"the rendered bundle contains no Rollout resource",
			"The operator's Release Template must render exactly one argoproj.io/v1alpha1 Rollout.")
	}

	var doc struct {
		Spec struct {
			Strategy struct {
				Canary struct {
					Steps []struct {
						SetWeight *int `yaml:"setWeight"`
					} `yaml:"steps"`
				} `yaml:"canary"`
			} `yaml:"strategy"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(rolloutBytes, &doc); err != nil {
		return RolloutEnvelope{}, "", RenderError("unparseable_rollout", "bundle.resources[Rollout]",
			"the rendered Rollout is not valid YAML",
			"This is a SafeLane defect: rendered bytes must always be valid YAML.").WithCause(err)
	}

	var weights []int
	for _, step := range doc.Spec.Strategy.Canary.Steps {
		if step.SetWeight != nil {
			weights = append(weights, *step.SetWeight)
		}
	}
	weights = append(weights, 100)

	env, err := NewRolloutEnvelope(weights, "start")
	if err != nil {
		return RolloutEnvelope{}, "", err
	}
	return env, bundle.Template().ContentDigest, nil
}
