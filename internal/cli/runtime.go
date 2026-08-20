package cli

import (
	"path/filepath"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/project"
)

type runtimePaths struct {
	projectFile          string
	policyFile           string
	storeDir             string
	configDir            string
	controllerKubeconfig string
	controllerContext    string
}

var newExecutor = execute.New

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
		return paths.withControllerDefaults()
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
	return paths.withControllerDefaults()
}

func (p runtimePaths) withControllerDefaults() (runtimePaths, error) {
	cfg, err := project.Load(p.projectFile)
	if err != nil {
		return runtimePaths{}, err
	}
	p.controllerKubeconfig = cfg.ControllerKubeconfig
	if p.controllerKubeconfig != "" && !filepath.IsAbs(p.controllerKubeconfig) {
		p.controllerKubeconfig = filepath.Join(p.configDir, p.controllerKubeconfig)
	}
	p.controllerContext = cfg.ControllerContext
	return p, nil
}

func (p runtimePaths) controllerCredentials(kubeconfig, context string) (string, string) {
	if kubeconfig == "" {
		kubeconfig = p.controllerKubeconfig
	}
	if context == "" {
		context = p.controllerContext
	}
	return kubeconfig, context
}
