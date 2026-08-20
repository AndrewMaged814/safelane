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
}

// Repository identifies the GitHub source SafeLane collects evidence from.
type Repository struct {
	Name          string `yaml:"name"`
	DefaultBranch string `yaml:"default_branch"`
}

// Release names the environment, artifact, required check, and template.
type Release struct {
	Environment     string `yaml:"environment"`
	ImageRepository string `yaml:"image_repository"`
	ImageTag        string `yaml:"image_tag"`
	RequiredCheck   string `yaml:"required_check"`
	TemplatePath    string `yaml:"template_path"`
}

// Target is the cluster destination. SafeLane never infers these from the caller.
type Target struct {
	Cluster   string `yaml:"cluster"`
	Namespace string `yaml:"namespace"`
	Rollout   string `yaml:"rollout"`
}

// Load reads and validates a project.yml file.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, release.Invalid("missing_project_config", "project",
				"no operator configuration for this repository",
				"run safelane init --app <name> --repo <owner/name>")
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
	if c.Version != 1 && c.Version != 2 {
		errs = append(errs, release.Malformed("unsupported_project_version", "version",
			fmt.Sprintf("project version %d is not supported", c.Version),
			"Set version to 2."))
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
	if c.Release.RequiredCheck == "" {
		errs = append(errs, release.Invalid("missing_project_field", "release.required_check",
			"no required check was configured",
			"Set release.required_check to the GitHub check run name that publishes the image."))
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
	body := fmt.Sprintf(`version: 2

application: %s

repository:
  name: %s
  default_branch: %s

release:
  environment: production
  image_repository: %s
  image_tag: "sha-{{merge_sha}}"
  required_check: build-and-push
  template_path: release-template

target:
  cluster: safelane-demo
  namespace: %s
  rollout: %s

controller_kubeconfig: controller.kubeconfig
controller_context: safelane-controller
`, application, repo, defaultBranch, imageRepository, application, application)
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
