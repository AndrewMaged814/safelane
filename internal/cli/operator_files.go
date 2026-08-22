package cli

import (
	"os"
	"path/filepath"

	"github.com/AndrewMaged814/safelane/internal/skill"
)

func writeSkillFile(path string) (string, error) {
	action := "created"
	if _, err := os.Stat(path); err == nil {
		action = "updated"
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, skill.SafeLane, 0o644); err != nil {
		return "", err
	}
	return action, nil
}

func writeInitFile(path string, body []byte) (string, error) {
	if _, err := os.ReadFile(path); err == nil {
		return "unchanged", nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	return "created", nil
}
