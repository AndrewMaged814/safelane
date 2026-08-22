package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/release"
)

const HomeEnv = "SAFELANE_HOME"

// Locations are all operator-owned paths for one application. None of
// these paths are inside the application repository.
type Locations struct {
	Home        string
	AppDir      string
	ProjectFile string
	PolicyFile  string
	TemplateDir string
	ReleasesDir string
}

// Home returns SAFELANE_HOME, or ~/.safelane when it is unset.
func Home() (string, error) {
	if home := strings.TrimSpace(os.Getenv(HomeEnv)); home != "" {
		return filepath.Abs(home)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve SafeLane home: %w", err)
	}
	return filepath.Join(userHome, ".safelane"), nil
}

// ForApp returns the fixed operator-owned layout for an application name.
func ForApp(home, app string) Locations {
	appDir := filepath.Join(home, "apps", app)
	return Locations{
		Home:        home,
		AppDir:      appDir,
		ProjectFile: filepath.Join(appDir, "project.yml"),
		PolicyFile:  filepath.Join(appDir, "policy.yml"),
		TemplateDir: filepath.Join(appDir, "release-template"),
		ReleasesDir: filepath.Join(appDir, "releases"),
	}
}

// Resolve identifies the current GitHub repository and finds the one app
// whose operator configuration names it.
func Resolve(root string) (Locations, error) {
	home, err := Home()
	if err != nil {
		return Locations{}, err
	}
	return ResolveIn(root, home)
}

// ResolveIn is Resolve with an explicit home, for deterministic tests.
func ResolveIn(root, home string) (Locations, error) {
	repo, err := DetectGitHubRepo(root)
	if err != nil {
		return Locations{}, missingConfig()
	}

	appsDir := filepath.Join(home, "apps")
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Locations{}, missingConfig()
		}
		return Locations{}, fmt.Errorf("read SafeLane applications: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var matches []Locations
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		loc := ForApp(home, entry.Name())
		cfg, loadErr := Load(loc.ProjectFile)
		if loadErr != nil {
			continue
		}
		if strings.EqualFold(cfg.Repository.Name, repo) {
			matches = append(matches, loc)
		}
	}
	if len(matches) == 0 {
		return Locations{}, missingConfig()
	}
	if len(matches) > 1 {
		return Locations{}, release.Invalid("ambiguous_project_config", "project",
			fmt.Sprintf("more than one application is configured for repository %s", repo),
			"Remove or rename the duplicate operator configuration.")
	}
	return matches[0], nil
}

func missingConfig() error {
	return release.Invalid("missing_project_config", "project",
		"no operator configuration for this repository",
		"run safelane setup from the application repository")
}
