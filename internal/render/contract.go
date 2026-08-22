package render

import (
	"bytes"
	"fmt"
	"strconv"
	"text/template"

	"gopkg.in/yaml.v3"
)

// TargetContract is the workload and Service shape compiled into a Release
// Template. Doctor compares this operator-owned contract with the live target
// before SafeLane declares execution ready.
type TargetContract struct {
	RolloutName     string
	RolloutSelector map[string]string
	PodLabels       map[string]string
	ContainerPorts  []ContainerPort
	StableService   ServiceContract
	CanaryService   ServiceContract
}

type ContainerPort struct {
	Name string
	Port int
}

type ServiceContract struct {
	Name       string
	Selector   map[string]string
	TargetPort string
}

// InspectTargetContract executes the trusted template with inert deterministic
// values and reads back only its target-shape fields. It does not create a
// release bundle or participate in release rendering.
func InspectTargetContract(t Template, application, namespace string) (TargetContract, error) {
	if t.IsZero() {
		return TargetContract{}, fmt.Errorf("release template is not loaded")
	}
	data := TemplateData{
		Application: application, Environment: "production", Cluster: "contract", Namespace: namespace,
		ImageReference: "ghcr.io/safelane/contract@sha256:" + string(bytes.Repeat([]byte{'a'}, 64)),
		ImageRegistry:  "ghcr.io", ImageRepository: "safelane/contract", ImageDigest: "sha256:" + string(bytes.Repeat([]byte{'a'}, 64)),
		SourceRepository: "safelane/contract", SourceRevision: string(bytes.Repeat([]byte{'b'}, 40)), SourceBranch: "main",
		RolloutName: application, StableServiceName: application + "-stable", CanaryServiceName: application + "-canary",
		AnalysisTemplateName: application + "-analysis", IngressName: application,
		ProbeImage: "ghcr.io/safelane/probe@sha256:" + string(bytes.Repeat([]byte{'c'}, 64)), Steps: []int{25},
	}
	type portDoc struct {
		Name          string `yaml:"name"`
		ContainerPort int    `yaml:"containerPort"`
		TargetPort    any    `yaml:"targetPort"`
	}
	type resourceDoc struct {
		Kind     string `yaml:"kind"`
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
		Spec struct {
			Selector map[string]any `yaml:"selector"`
			Ports    []portDoc      `yaml:"ports"`
			Strategy struct {
				Canary struct {
					StableService string `yaml:"stableService"`
					CanaryService string `yaml:"canaryService"`
				} `yaml:"canary"`
			} `yaml:"strategy"`
			Template struct {
				Metadata struct {
					Labels map[string]string `yaml:"labels"`
				} `yaml:"metadata"`
				Spec struct {
					Containers []struct {
						Ports []portDoc `yaml:"ports"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}

	services := map[string]ServiceContract{}
	var contract TargetContract
	var stableName, canaryName string
	for _, resource := range t.resources {
		parsed, err := template.New(resource.path).Option("missingkey=error").Parse(resource.body)
		if err != nil {
			return TargetContract{}, fmt.Errorf("inspect target contract: parse %s: %w", resource.path, err)
		}
		var rendered bytes.Buffer
		if err := parsed.Execute(&rendered, data); err != nil {
			return TargetContract{}, fmt.Errorf("inspect target contract: execute %s: %w", resource.path, err)
		}
		var doc resourceDoc
		if err := yaml.Unmarshal(rendered.Bytes(), &doc); err != nil {
			return TargetContract{}, fmt.Errorf("inspect target contract: decode %s: %w", resource.path, err)
		}
		switch doc.Kind {
		case "Rollout":
			selector, err := stringMap(doc.Spec.Selector["matchLabels"])
			if err != nil {
				return TargetContract{}, fmt.Errorf("inspect target contract: Rollout selector: %w", err)
			}
			contract.RolloutName = doc.Metadata.Name
			contract.RolloutSelector = selector
			contract.PodLabels = doc.Spec.Template.Metadata.Labels
			stableName, canaryName = doc.Spec.Strategy.Canary.StableService, doc.Spec.Strategy.Canary.CanaryService
			for _, container := range doc.Spec.Template.Spec.Containers {
				for _, port := range container.Ports {
					contract.ContainerPorts = append(contract.ContainerPorts, ContainerPort{Name: port.Name, Port: port.ContainerPort})
				}
			}
		case "Service":
			if len(doc.Spec.Ports) != 1 {
				return TargetContract{}, fmt.Errorf("inspect target contract: service %s must declare exactly one port", doc.Metadata.Name)
			}
			target, err := scalarPort(doc.Spec.Ports[0].TargetPort)
			if err != nil {
				return TargetContract{}, fmt.Errorf("inspect target contract: service %s: %w", doc.Metadata.Name, err)
			}
			selector, err := stringMap(doc.Spec.Selector)
			if err != nil {
				return TargetContract{}, fmt.Errorf("inspect target contract: service %s selector: %w", doc.Metadata.Name, err)
			}
			services[doc.Metadata.Name] = ServiceContract{Name: doc.Metadata.Name, Selector: selector, TargetPort: target}
		}
	}
	contract.StableService = services[stableName]
	contract.CanaryService = services[canaryName]
	if contract.RolloutName == "" || len(contract.RolloutSelector) == 0 || len(contract.PodLabels) == 0 || contract.StableService.Name == "" || contract.CanaryService.Name == "" {
		return TargetContract{}, fmt.Errorf("inspect target contract: template must define one Rollout and its stable and canary Services")
	}
	return contract, nil
}

func stringMap(value any) (map[string]string, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected a string map, got %T", value)
	}
	out := make(map[string]string, len(raw))
	for key, value := range raw {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("label %q has non-string value %T", key, value)
		}
		out[key] = text
	}
	return out, nil
}

func scalarPort(value any) (string, error) {
	switch value := value.(type) {
	case string:
		if value == "" {
			return "", fmt.Errorf("targetPort is empty")
		}
		return value, nil
	case int:
		return strconv.Itoa(value), nil
	default:
		return "", fmt.Errorf("targetPort has unsupported type %T", value)
	}
}
