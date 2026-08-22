// Package project loads operator-owned runtime configuration from the
// application's directory under SAFELANE_HOME. This file is a real source of truth: SafeLane
// reads application, target, required check, image repository, and
// Release Template path from it. Callers do not supply those fields.
package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/release"
	"gopkg.in/yaml.v3"
)

// Config is the operator-owned project configuration.
type Config struct {
	Version              int        `yaml:"version"`
	Application          string     `yaml:"application"`
	Repository           Repository `yaml:"repository"`
	Release              Release    `yaml:"release"`
	Target               Target     `yaml:"target"`
	ControllerKubeconfig string     `yaml:"controller_kubeconfig"`
	ControllerContext    string     `yaml:"controller_context"`
	Analysis             Analysis   `yaml:"analysis"`
}

// Repository identifies the GitHub source SafeLane collects evidence from.
type Repository struct {
	Name          string `yaml:"name"`
	DefaultBranch string `yaml:"default_branch"`
}

// Release names the environment, artifact, required check, and template.
type Release struct {
	Environment     string   `yaml:"environment"`
	ImageRepository string   `yaml:"image_repository"`
	ImageTag        string   `yaml:"image_tag"`
	RequiredCheck   string   `yaml:"required_check"`
	RequiredChecks  []string `yaml:"required_checks"`
	TemplatePath    string   `yaml:"template_path"`
}

// Target is the cluster destination. SafeLane never infers these from the caller.
type Target struct {
	Cluster   string `yaml:"cluster"`
	Namespace string `yaml:"namespace"`
	Rollout   string `yaml:"rollout"`
}

// Analysis declares operator-approved black-box assertions. The probe image
// is digest pinned and receives no Kubernetes API credential.
type Analysis struct {
	ProbeImage string             `yaml:"probe_image"`
	Assertions []RuntimeAssertion `yaml:"assertions"`
}

type RuntimeAssertion struct {
	ID          string `yaml:"id"`
	Surface     string `yaml:"surface"`
	Expectation string `yaml:"expectation"`
	Covers      string `yaml:"covers"`
}

func (a Analysis) AssertionIDs() []string {
	ids := make([]string, 0, len(a.Assertions))
	for _, assertion := range a.Assertions {
		ids = append(ids, assertion.ID)
	}
	return ids
}

// Load reads and validates a project.yml file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, release.Invalid("missing_project_config", "project",
				"no operator configuration for this repository",
				"run safelane setup from the application repository")
		}
		return Config{}, release.Invalid("unreadable_project_config", "project",
			fmt.Sprintf("could not read %s: %v", path, err),
			"Fix the project file path and retry.")
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, release.Malformed("invalid_project_config", "project",
			"project.yml is not valid YAML",
			"Match the SafeLane project.yml schema.").WithCause(err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate reports every missing or unsafe operator field at once.
func (c Config) Validate() error {
	var errs release.Errors
	if c.Version != 1 && c.Version != 2 && c.Version != 3 && c.Version != 4 {
		errs = append(errs, release.Malformed("unsupported_project_version", "version",
			fmt.Sprintf("project version %d is not supported", c.Version),
			"Set version to 2."))
	}
	if c.Version >= 4 {
		if strings.TrimSpace(c.Analysis.ProbeImage) == "" {
			errs = append(errs, release.Invalid("missing_project_field", "analysis.probe_image", "no external analysis probe image was configured", "Set a digest-pinned probe image."))
		}
		if len(c.Analysis.Assertions) == 0 {
			errs = append(errs, release.Invalid("missing_project_field", "analysis.assertions", "no concrete runtime assertions were configured", "Configure assertions for critical application behavior."))
		}
		seenAssertions := map[string]bool{}
		for i, assertion := range c.Analysis.Assertions {
			if assertion.ID == "" || assertion.Surface == "" || assertion.Expectation == "" || assertion.Covers == "" {
				errs = append(errs, release.Invalid("invalid_runtime_assertion", fmt.Sprintf("analysis.assertions[%d]", i), "runtime assertions require id, surface, expectation, and covers", "Complete or remove the assertion."))
			}
			if seenAssertions[assertion.ID] {
				errs = append(errs, release.Invalid("duplicate_runtime_assertion", "analysis.assertions", fmt.Sprintf("assertion %q is duplicated", assertion.ID), "Use each assertion ID once."))
			}
			seenAssertions[assertion.ID] = true
		}
	}
	for _, f := range []struct {
		name, value string
	}{
		{"application", c.Application},
		{"release.environment", c.Release.Environment},
		{"target.cluster", c.Target.Cluster},
		{"target.namespace", c.Target.Namespace},
	} {
		if f.value == "" {
			errs = append(errs, release.Invalid("missing_project_field", f.name,
				"project configuration is incomplete",
				"Set application, release.environment, target.cluster and target.namespace in project.yml."))
			continue
		}
		if !release.IsDNSLabel(f.value) {
			errs = append(errs, release.Invalid("unsafe_project_field", f.name,
				fmt.Sprintf("%q is not a lowercase DNS label", f.value),
				"Use lowercase letters, digits and hyphens (RFC 1123 label)."))
		}
	}
	if c.Repository.Name == "" {
		errs = append(errs, release.Invalid("missing_project_field", "repository.name",
			"no repository was configured",
			"Set repository.name to owner/name in project.yml."))
	} else if _, err := release.ParseRepositoryRef(c.Repository.Name); err != nil {
		errs = append(errs, release.Invalid("malformed_repository", "repository.name",
			fmt.Sprintf("%q is not a repository reference", c.Repository.Name),
			`Use "owner/name".`))
	}
	if c.Repository.DefaultBranch == "" {
		errs = append(errs, release.Invalid("missing_project_field", "repository.default_branch",
			"no default branch was configured",
			"Set repository.default_branch to the branch pull requests merge into."))
	}
	if c.Release.ImageRepository == "" {
		errs = append(errs, release.Invalid("missing_project_field", "release.image_repository",
			"no image repository was configured",
			"Set release.image_repository to ghcr.io/owner/name."))
	}
	if (c.Version == 3 && len(c.Release.RequiredChecks) == 0) || (c.Version < 3 && len(c.Release.RequiredCheckNames()) == 0) {
		errs = append(errs, release.Invalid("missing_project_field", "release.required_checks",
			"no required check was configured",
			"Set release.required_checks to every mandatory GitHub check run name."))
	}
	seenChecks := map[string]bool{}
	for i, name := range c.Release.RequiredCheckNames() {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, release.Invalid("empty_required_check", fmt.Sprintf("release.required_checks[%d]", i), "mandatory check names cannot be empty", "Remove the empty entry or name the GitHub check run."))
		} else if seenChecks[name] {
			errs = append(errs, release.Invalid("duplicate_required_check", "release.required_checks", fmt.Sprintf("mandatory check %q is listed more than once", name), "List each mandatory check once."))
		}
		seenChecks[name] = true
	}
	if c.Release.TemplatePath == "" {
		errs = append(errs, release.Invalid("missing_project_field", "release.template_path",
			"no Release Template path was configured",
			"Set release.template_path to the operator-owned template directory."))
	}
	if c.Release.ImageTag == "" {
		c.Release.ImageTag = DefaultImageTag
	}
	return errs.OrNil()
}

// RequiredCheckNames returns the v3 mandatory set, with the singular field retained
// only so version 1/2 fixtures remain readable.
func (r Release) RequiredCheckNames() []string {
	if len(r.RequiredChecks) > 0 {
		return append([]string(nil), r.RequiredChecks...)
	}
	if r.RequiredCheck != "" {
		return []string{r.RequiredCheck}
	}
	return nil
}

// DefaultImageTag is the tag pattern used when project.yml omits image_tag.
const DefaultImageTag = "sha-{{merge_sha}}"

// ImageTag renders the configured tag pattern against a merge commit SHA.
func ImageTag(pattern, mergeSHA string) string {
	if pattern == "" {
		pattern = DefaultImageTag
	}
	short8 := mergeSHA
	if len(short8) > 8 {
		short8 = short8[:8]
	}
	out := strings.ReplaceAll(pattern, "{{merge_sha}}", mergeSHA)
	return strings.ReplaceAll(out, "{{merge_sha_short8}}", short8)
}

// ParseImageRepository splits "registry/owner/name" into registry and repository.
func ParseImageRepository(s string) (registry, repository string, err error) {
	s = strings.TrimSpace(s)
	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash == len(s)-1 {
		return "", "", release.Invalid("malformed_image_repository", "release.image_repository",
			fmt.Sprintf("%q is not a registry repository", s),
			"Use ghcr.io/owner/name.")
	}
	return s[:slash], s[slash+1:], nil
}

// ReleaseTarget builds the release.Target from operator config and the
// selected environment.
func (c Config) ReleaseTarget(environment string) release.Target {
	if environment == "" {
		environment = c.Release.Environment
	}
	ns := c.Target.Namespace
	if ns == "" {
		ns = c.Application
	}
	return release.Target{
		Application: c.Application,
		Environment: environment,
		Cluster:     c.Target.Cluster,
		Namespace:   ns,
		Rollout:     c.Target.Rollout,
	}
}

// DetectGitHubRepo returns origin's GitHub owner/name when root is a clone.
func DetectGitHubRepo(root string) (string, error) {
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseGitHubRemote(strings.TrimSpace(string(out)))
}

// DetectDefaultBranch returns origin's HEAD branch name when available.
func DetectDefaultBranch(root string) string {
	cmd := exec.Command("git", "-C", root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		if b, ok := strings.CutPrefix(ref, "origin/"); ok && b != "" {
			return b
		}
	}
	cmd = exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD")
	out, err = cmd.Output()
	if err == nil {
		b := strings.TrimSpace(string(out))
		if b != "" && b != "HEAD" {
			return b
		}
	}
	return "master"
}

func parseGitHubRemote(url string) (string, error) {
	url = strings.TrimSuffix(url, ".git")
	url = strings.TrimSuffix(url, "/")
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		return strings.TrimPrefix(url, "git@github.com:"), nil
	case strings.HasPrefix(url, "https://github.com/"):
		return strings.TrimPrefix(url, "https://github.com/"), nil
	case strings.HasPrefix(url, "http://github.com/"):
		return strings.TrimPrefix(url, "http://github.com/"), nil
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		return strings.TrimPrefix(url, "ssh://git@github.com/"), nil
	default:
		return "", fmt.Errorf("origin %q is not a GitHub remote", url)
	}
}

// DefaultYAML renders a project.yml for init when none exists.
func DefaultYAML(application, repo, defaultBranch, imageRepository string) []byte {
	return YAML(application, repo, defaultBranch, imageRepository, []string{"build-and-push"})
}

// YAML renders a project.yml with repository-derived required check names.
// The legacy DefaultYAML wrapper intentionally preserves init's historical
// defaults; setup uses this function after inspecting the repository's CI.
func YAML(application, repo, defaultBranch, imageRepository string, requiredChecks []string, proposedAssertions ...[]RuntimeAssertion) []byte {
	if application == "" {
		application = "app"
	}
	if defaultBranch == "" {
		defaultBranch = "master"
	}
	if imageRepository == "" && repo != "" {
		imageRepository = "ghcr.io/" + strings.ToLower(repo)
	}
	if imageRepository == "" {
		imageRepository = "ghcr.io/owner/name"
	}
	if repo == "" {
		repo = "owner/name"
	}
	if len(requiredChecks) == 0 {
		requiredChecks = []string{"build-and-push"}
	}
	var checks strings.Builder
	for _, check := range requiredChecks {
		fmt.Fprintf(&checks, "    - %s\n", check)
	}
	assertions := []RuntimeAssertion{
		{ID: "demo-response", Surface: "GET /api/demo", Expectation: `HTTP 200 and JSON status equals "ok"`, Covers: "correctness"},
		{ID: "demo-success-rate", Surface: "GET /api/demo", Expectation: "success rate is at least 95 percent over 20 requests", Covers: "availability"},
		{ID: "demo-latency", Surface: "GET /api/demo", Expectation: "p95 latency is at most 500ms over 20 requests", Covers: "latency"},
		{ID: "canary-identity", Surface: "GET /version", Expectation: "commit equals the inspected merge commit", Covers: "artifact-identity"},
	}
	if len(proposedAssertions) > 0 && len(proposedAssertions[0]) > 0 {
		assertions = proposedAssertions[0]
	}
	var assertionYAML strings.Builder
	for _, assertion := range assertions {
		fmt.Fprintf(&assertionYAML, "    - id: %q\n      surface: %q\n      expectation: %q\n      covers: %q\n", assertion.ID, assertion.Surface, assertion.Expectation, assertion.Covers)
	}
	body := fmt.Sprintf(`version: 4

application: %s

repository:
  name: %s
  default_branch: %s

release:
  environment: production
  image_repository: %s
  image_tag: "sha-{{merge_sha}}"
  required_checks:
%s  template_path: release-template

target:
  cluster: safelane-demo
  namespace: %s
  rollout: %s

analysis:
  probe_image: ghcr.io/andrewmaged814/safelane-demo-probe@sha256:REPLACE_WITH_PUBLISHED_DIGEST
  assertions:
%s

controller_kubeconfig: controller.kubeconfig
controller_context: safelane-controller
`, application, repo, defaultBranch, imageRepository, checks.String(), application, application, assertionYAML.String())
	return []byte(body)
}

// SanitizeApplication turns a directory name into a DNS label, or "app".
func SanitizeApplication(name string) string {
	name = strings.ToLower(filepath.Base(name))
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.':
			if b.Len() > 0 {
				b.WriteByte('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = out[:63]
		out = strings.Trim(out, "-")
	}
	if out == "" || !release.IsDNSLabel(out) {
		return "app"
	}
	return out
}
