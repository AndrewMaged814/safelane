package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/skill"
)

// InitCommand builds `safelane init --app <name> --repo <owner/name>`.
// root remains in the API so tests can prove init leaves it untouched.
func InitCommand(root string) Command {
	return Command{
		Name:    "init",
		Summary: "create operator-owned application configuration",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runInit(args, stdout, stderr, root)
		},
	}
}

func runInit(args []string, stdout, stderr io.Writer, root string) int {
	_ = root // init must never inspect or write the application repository.
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	app := flags.String("app", "", "application name (required)")
	repo := flags.String("repo", "", "GitHub owner/name (required)")
	if err := flags.Parse(args); err != nil {
		return ExitUsage
	}
	if *app == "" || *repo == "" {
		fmt.Fprintln(stderr, "safelane init: --app and --repo are required")
		flags.Usage()
		return ExitUsage
	}
	if project.SanitizeApplication(*app) != *app || !release.IsDNSLabel(*app) {
		fmt.Fprintf(stderr, "safelane init: --app %q must be a lowercase DNS label\n", *app)
		return ExitUsage
	}
	if _, err := release.ParseRepositoryRef(*repo); err != nil {
		fmt.Fprintf(stderr, "safelane init: --repo %q must be owner/name\n", *repo)
		return ExitUsage
	}

	home, err := project.Home()
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	loc := project.ForApp(home, *app)
	image := "ghcr.io/" + strings.ToLower(*repo)

	projectAction, err := writeInitFile(loc.ProjectFile, project.DefaultYAML(*app, *repo, "master", image))
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	policyAction, err := writeInitFile(loc.PolicyFile, policy.DefaultYAML())
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	templateAction, count, err := writeInitTemplate(loc.TemplateDir)
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	releasesAction := "unchanged"
	if _, err := os.Stat(loc.ReleasesDir); os.IsNotExist(err) {
		releasesAction = "created"
	} else if err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	if err := os.MkdirAll(loc.ReleasesDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "safelane init: %v\n", err)
		return ExitFail
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: resolve user home for agent skills: %v\n", err)
		return ExitFail
	}
	claudeSkill := filepath.Join(userHome, ".claude", "skills", "safelane", "SKILL.md")
	agentsSkill := filepath.Join(userHome, ".agents", "skills", "safelane", "SKILL.md")
	claudeAction, err := writeSkillFile(claudeSkill)
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: install Claude skill: %v\n", err)
		return ExitFail
	}
	agentsAction, err := writeSkillFile(agentsSkill)
	if err != nil {
		fmt.Fprintf(stderr, "safelane init: install agent skill: %v\n", err)
		return ExitFail
	}

	fmt.Fprintf(stdout, "%s  %s\n", projectAction, displayInitPath(home, loc.ProjectFile, false))
	fmt.Fprintf(stdout, "%s  %s\n", policyAction, displayInitPath(home, loc.PolicyFile, false))
	fmt.Fprintf(stdout, "%s  %s  (%d files)\n", templateAction, displayInitPath(home, loc.TemplateDir, true), count)
	fmt.Fprintf(stdout, "%s  %s\n", releasesAction, displayInitPath(home, loc.ReleasesDir, true))
	fmt.Fprintf(stdout, "%s  ~/.claude/skills/safelane/SKILL.md\n", claudeAction)
	fmt.Fprintf(stdout, "%s  ~/.agents/skills/safelane/SKILL.md\n", agentsAction)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "The operator configuration is outside your application repository.")
	fmt.Fprintf(stdout, "An agent working in %s has no write path to it.\n", *repo)
	return ExitOK
}

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
	_, err := os.ReadFile(path)
	if err == nil {
		return "unchanged", nil // operator-owned: init never overwrites it.
	}
	if !os.IsNotExist(err) {
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

func writeInitTemplate(dest string) (string, int, error) {
	const embedRoot = "testdata/release-template"
	if entries, err := os.ReadDir(dest); err == nil && len(entries) > 0 {
		return "unchanged", countTemplateFiles(entries), nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", 0, err
	}
	count := 0
	err := fs.WalkDir(render.FixtureTemplateFS, embedRoot, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(name, ".yaml.tmpl") {
			return err
		}
		relPath := strings.TrimPrefix(name, embedRoot+"/")
		raw, err := fs.ReadFile(render.FixtureTemplateFS, name)
		if err != nil {
			return err
		}
		out := filepath.Join(dest, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			return err
		}
		count++
		return nil
	})
	return "created", count, err
}

func countTemplateFiles(entries []os.DirEntry) int {
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml.tmpl") {
			count++
		}
	}
	return count
}

func displayInitPath(home, path string, directory bool) string {
	display := filepath.ToSlash(path)
	if os.Getenv(project.HomeEnv) == "" {
		if rel, err := filepath.Rel(home, path); err == nil {
			display = "~/.safelane/" + filepath.ToSlash(rel)
		}
	}
	if directory {
		display += "/"
	}
	return display
}
