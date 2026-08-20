package cli

import (
	"path/filepath"

	"github.com/AndrewMaged814/safelane/internal/project"
)

type runtimePaths struct {
	projectFile string
	policyFile  string
	storeDir    string
	configDir   string
}

// resolveRuntime is the only zero-flag path from an application clone to
// operator-owned configuration and records.
func resolveRuntime(root, projectFile, policyFile, storeDir string) (runtimePaths, error) {
	paths := runtimePaths{projectFile: projectFile, policyFile: policyFile, storeDir: storeDir}
	if projectFile != "" {
		paths.configDir = filepath.Dir(projectFile)
		if paths.policyFile == "" {
			paths.policyFile = filepath.Join(paths.configDir, "policy.yml")
		}
		if paths.storeDir == "" {
			paths.storeDir = filepath.Join(paths.configDir, "releases")
		}
		return paths, nil
	}
	loc, err := project.Resolve(root)
	if err != nil {
		return runtimePaths{}, err
	}
	paths.projectFile = loc.ProjectFile
	paths.configDir = loc.AppDir
	if paths.policyFile == "" {
		paths.policyFile = loc.PolicyFile
	}
	if paths.storeDir == "" {
		paths.storeDir = loc.ReleasesDir
	}
	return paths, nil
}
