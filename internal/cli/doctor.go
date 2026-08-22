package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AndrewMaged814/safelane/internal/execute"
	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/release"
	"github.com/AndrewMaged814/safelane/internal/render"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	githubverify "github.com/AndrewMaged814/safelane/internal/verify/github"
)

// DoctorCommand builds the read-only pre-flight report used immediately before a demo.
func DoctorCommand(root string) Command {
	return Command{
		Name:    "doctor",
		Summary: "check whether SafeLane can release right now",
		Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
			return runDoctor(ctx, args, stdout, stderr, root, realDoctorDeps())
		},
	}
}

type doctorFlags struct {
	projectFile string
	policyFile  string
	templateDir string
	jsonOut     bool
}

func parseDoctorFlags(args []string, stderr io.Writer) (doctorFlags, error) {
	var f doctorFlags
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.projectFile, "project", "", "path to project.yml")
	fs.StringVar(&f.policyFile, "policy", "", "path to policy.yml")
	fs.StringVar(&f.templateDir, "template-dir", "", "path to the Release Template")
	fs.BoolVar(&f.jsonOut, "json", false, "print a stable command result")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "safelane doctor: no positional arguments are allowed")
		return f, flag.ErrHelp
	}
	return f, nil
}

type doctorDeps struct {
	run         execute.Runner
	lookPath    func(string) (string, error)
	githubToken func(context.Context) (string, error)
	githubPing  func(context.Context, string) (string, error)
	ghcrPing    func(context.Context) error
}

func realDoctorDeps() doctorDeps {
	return doctorDeps{
		run:      execute.New(execute.Config{}).Run,
		lookPath: exec.LookPath,
		githubToken: func(ctx context.Context) (string, error) {
			cmd := exec.CommandContext(ctx, "gh", "auth", "token")
			out, err := cmd.Output()
			if err != nil {
				return "", fmt.Errorf("read GitHub CLI credential: %w", err)
			}
			token := strings.TrimSpace(string(out))
			if token == "" {
				return "", fmt.Errorf("GitHub CLI returned an empty credential")
			}
			return token, nil
		},
		githubPing: func(ctx context.Context, token string) (string, error) {
			return (&githubverify.Client{Token: token}).Ping(ctx)
		},
		ghcrPing: func(ctx context.Context) error { return (&ghcr.Client{}).Ping(ctx) },
	}
}

type doctorRow struct {
	mark, label, value string
	continuations      []string
	remedy             string
}

type doctorReport struct {
	rows               []doctorRow
	failed             int
	unavailable        int
	kubectlUnavailable bool
	evidenceReady      bool
	executionReady     bool
}

func (r doctorReport) Render() string {
	var b strings.Builder
	if !r.kubectlUnavailable {
		fmt.Fprintln(&b, "SafeLane doctor")
		fmt.Fprintln(&b)
	}
	width := 23
	if r.kubectlUnavailable {
		width = 19
	}
	for _, row := range r.rows {
		fmt.Fprintf(&b, "  %s %-*s%s\n", row.mark, width, row.label, row.value)
		for _, continuation := range row.continuations {
			fmt.Fprintf(&b, "%*s%s\n", 4+width, "", continuation)
		}
		if row.remedy != "" {
			fmt.Fprintf(&b, "      remedy: %s\n", row.remedy)
		}
	}
	fmt.Fprintln(&b)
	if r.failed == 0 && r.unavailable == 0 {
		fmt.Fprintln(&b, "All checks passed.")
	} else {
		fmt.Fprintf(&b, "%d failed, %d unavailable.\n", r.failed, r.unavailable)
		fmt.Fprintf(&b, "Evidence and assessment  %s\n", readiness(r.evidenceReady))
		fmt.Fprintf(&b, "Rollout execution       %s\n", readiness(r.executionReady))
	}
	return b.String()
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer, root string, deps doctorDeps) int {
	f, err := parseDoctorFlags(args, stderr)
	if err != nil {
		return ExitUsage
	}
	paths, err := resolveRuntime(root, f.projectFile, f.policyFile, "")
	if err != nil {
		fmt.Fprintf(stderr, "safelane doctor: %v\n", err)
		return ExitFail
	}
	cfg, err := project.Load(paths.projectFile)
	if err != nil {
		fmt.Fprintf(stderr, "safelane doctor: %v\n", err)
		return ExitFail
	}

	report := doctorReport{evidenceReady: true, executionReady: true}
	pass := func(label, value string, continuations ...string) {
		report.rows = append(report.rows, doctorRow{mark: "✓", label: label, value: value, continuations: continuations})
	}
	fail := func(label, value, remedy string) {
		report.rows = append(report.rows, doctorRow{mark: "✗", label: label, value: value, remedy: remedy})
		report.failed++
	}
	failEvidence := func(label, value, remedy string) {
		fail(label, value, remedy)
		report.evidenceReady = false
	}
	failExecution := func(label, value, remedy string) {
		fail(label, value, remedy)
		report.executionReady = false
	}
	skip := func(label, value string) {
		report.rows = append(report.rows, doctorRow{mark: "–", label: label, value: value})
		report.unavailable++
	}

	pass("operator config", displayDoctorPath(paths.projectFile))
	p, err := policy.Load(paths.policyFile)
	if err != nil {
		failEvidence("release policy", err.Error(), "fix policy.yml and retry")
	} else {
		pass("release policy", fmt.Sprintf("%s  (version %s, %d lanes)", displayDoctorPath(paths.policyFile), p.Version, len(p.Lanes)))
	}

	templateDir := f.templateDir
	if templateDir == "" {
		templateDir = cfg.Release.TemplatePath
		if !filepath.IsAbs(templateDir) {
			templateDir = filepath.Join(paths.configDir, templateDir)
		}
	}
	tmpl, err := render.LoadDir(templateDir)
	if err != nil {
		failEvidence("release template", err.Error(), "fix the Release Template and retry")
	} else {
		identity := tmpl.Identity()
		pass("release template", fmt.Sprintf("%d files, digest %s", identity.FileCount, shortDigest(identity.ContentDigest)))
	}
	if cfg.Version >= 4 {
		if _, err := release.ParseImageReference(cfg.Analysis.ProbeImage); err != nil {
			failEvidence("analysis probe", "not pinned by an immutable OCI digest", "publish the probe and update analysis.probe_image")
		} else {
			pass("analysis probe", shortDigest(strings.SplitN(cfg.Analysis.ProbeImage, "@", 2)[1]))
		}
	}

	githubToken := os.Getenv("GITHUB_TOKEN")
	var tokenErr error
	if githubToken == "" && deps.githubToken != nil {
		githubToken, tokenErr = deps.githubToken(ctx)
	}
	login := ""
	if tokenErr != nil {
		failEvidence("github", tokenErr.Error(), "set GITHUB_TOKEN or authenticate GitHub CLI with `gh auth login`")
	} else if login, err = deps.githubPing(ctx, githubToken); err != nil {
		failEvidence("github", err.Error(), "set a valid GITHUB_TOKEN or refresh GitHub CLI authentication with `gh auth login`")
	} else if login == "" {
		pass("github", "api.github.com reachable")
	} else {
		pass("github", fmt.Sprintf("api.github.com reachable, token valid (%s)", login))
	}
	if err := deps.ghcrPing(ctx); err != nil {
		failEvidence("ghcr", err.Error(), "check registry connectivity")
	} else {
		pass("ghcr", "ghcr.io reachable")
	}

	assessors := []string{"heuristic (always)"}
	for _, name := range []string{"claude", "codex"} {
		if _, err := deps.lookPath(name); err == nil {
			assessors = append(assessors, name+" (found)")
		} else {
			assessors = append(assessors, name+" (not found)")
		}
	}
	pass("assessors", strings.Join(assessors, ", "))

	kubectlVersion, err := deps.run(ctx, []string{"version", "--client", "-o", "json"}, nil)
	if err != nil {
		value := strings.TrimSpace(err.Error())
		if errors.Is(err, exec.ErrNotFound) {
			value = "not found on PATH"
		}
		failExecution("kubectl", value, "install kubectl and the argo-rollouts plugin")
		skipKubectlDependents(&report, skip, "kubectl missing")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	clientVersion, err := parseKubectlVersion(kubectlVersion)
	if err != nil {
		failExecution("kubectl", err.Error(), "install a supported kubectl and the argo-rollouts plugin")
		skipKubectlDependents(&report, skip, "kubectl unavailable")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	pluginVersion, err := deps.run(ctx, []string{"argo", "rollouts", "version", "--short"}, nil)
	if err != nil {
		failExecution("kubectl", fmt.Sprintf("%s, argo-rollouts plugin unavailable", clientVersion), "install kubectl and the argo-rollouts plugin")
		skipKubectlDependents(&report, skip, "kubectl unavailable")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	pass("kubectl", fmt.Sprintf("%s, argo-rollouts plugin %s", clientVersion, strings.TrimSpace(string(pluginVersion))))

	ex := newExecutor(execute.Config{
		Namespace: cfg.Target.Namespace, Rollout: cfg.Target.Rollout,
		ControllerKubeconfig: paths.controllerKubeconfig, ControllerContext: paths.controllerContext,
	})
	ex.Run = deps.run
	rolloutArgs := []string{"get", "rollout", cfg.Target.Rollout, "-n", cfg.Target.Namespace, "-o", "json"}
	rolloutRaw, err := deps.run(ctx, rolloutArgs, nil)
	if err != nil {
		failExecution("cluster", fmt.Sprintf("%s: %v", cfg.Target.Cluster, err), "check the kubeconfig context and the cluster state")
		skip("rollout", "skipped (cluster unreachable)")
		skip("identity", "skipped (cluster unreachable)")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	pass("cluster", fmt.Sprintf("%s reachable, namespace %s exists", cfg.Target.Cluster, cfg.Target.Namespace))
	status, err := ex.GetStatus(ctx)
	if err != nil {
		failExecution("rollout", err.Error(), "check that the configured Rollout exists")
		skip("identity", "skipped (rollout unavailable)")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	pass("rollout", fmt.Sprintf("%s found, phase %s, image %s", cfg.Target.Rollout, status.Phase, shortDigest(status.ImageDigest)))
	if !tmpl.IsZero() {
		contract, contractErr := render.InspectTargetContract(tmpl, cfg.Application, cfg.Target.Namespace)
		if contractErr != nil {
			failExecution("template compatibility", contractErr.Error(), "fix the Release Template and retry")
		} else if contractErr = validateLiveTargetContract(ctx, deps.run, paths, cfg.Target.Namespace, contract, rolloutRaw); contractErr != nil {
			failExecution("template compatibility", contractErr.Error(), "restore the template/live selector and port contract before releasing")
		} else {
			pass("template compatibility", "stored Rollout and Services match the live target")
		}
	}

	controllerIdentity := "system:serviceaccount:" + cfg.Target.Namespace + ":safelane-controller"
	callerIdentity := "system:serviceaccount:" + cfg.Target.Namespace + ":safelane-caller"
	capabilities, err := ex.AssertCapabilities(ctx, controllerIdentity, callerIdentity)
	if err != nil {
		failExecution("identity", err.Error(), "check the configured Kubernetes identities and RBAC")
		skip("credential separation", "skipped (identity unavailable)")
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	controllerRow := doctorRow{
		mark: "✓", label: "controller identity",
		value:         fmt.Sprintf("%s → %s", filepath.Base(paths.controllerKubeconfig), shortServiceAccount(controllerIdentity)),
		continuations: []string{fmt.Sprintf("can patch rollouts.argoproj.io: %s", yesNo(capabilities.ControllerPatchRollouts))},
	}
	if !capabilities.ControllerPatchRollouts {
		controllerRow.mark = "✗"
		controllerRow.remedy = "grant the controller identity patch access to Rollouts"
		report.failed++
		report.executionReady = false
	}
	report.rows = append(report.rows, controllerRow)
	callerRow := doctorRow{
		mark: "✓", label: "caller identity",
		value: fmt.Sprintf("~/.kube/config (default) → %s", shortServiceAccount(callerIdentity)),
		continuations: []string{
			fmt.Sprintf("can get rollouts:   %s", yesNo(capabilities.CallerGetRollouts)),
			fmt.Sprintf("can patch rollouts: %s", yesNo(capabilities.CallerPatchRollouts)),
		},
	}
	if !capabilities.CallerGetRollouts || capabilities.CallerPatchRollouts {
		callerRow.mark = "✗"
		callerRow.remedy = "restrict the caller to read-only Rollout access"
		report.failed++
		report.executionReady = false
	}
	report.rows = append(report.rows, callerRow)

	contexts, err := deps.run(ctx, []string{"config", "get-contexts", "-o", "name"}, nil)
	if err != nil {
		failExecution("credential separation", err.Error(), "check the agent's default kubeconfig")
	} else if containsLine(string(contexts), paths.controllerContext) {
		failExecution("credential separation", "privileged context found in the agent's default kubeconfig", "remove the controller context from ~/.kube/config")
	} else {
		pass("credential separation", "no privileged context in the agent's default kubeconfig")
	}

	if report.failed > 0 {
		return finishDoctor(report, f.jsonOut, stdout, stderr, ExitFail)
	}
	return finishDoctor(report, f.jsonOut, stdout, stderr, ExitOK)
}

func validateLiveTargetContract(ctx context.Context, run execute.Runner, paths runtimePaths, namespace string, expected render.TargetContract, rolloutRaw []byte) error {
	type livePort struct {
		Name          string `json:"name"`
		ContainerPort int    `json:"containerPort"`
		TargetPort    any    `json:"targetPort"`
	}
	type liveRollout struct {
		Spec struct {
			Selector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
			Template struct {
				Metadata struct {
					Labels map[string]string `json:"labels"`
				} `json:"metadata"`
				Spec struct {
					Containers []struct {
						Ports []livePort `json:"ports"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	}
	var rollout liveRollout
	if err := json.Unmarshal(rolloutRaw, &rollout); err != nil {
		return fmt.Errorf("decode live Rollout contract: %w", err)
	}
	if !maps.Equal(expected.RolloutSelector, rollout.Spec.Selector.MatchLabels) {
		return fmt.Errorf("Rollout selector differs: template=%v live=%v", expected.RolloutSelector, rollout.Spec.Selector.MatchLabels)
	}
	if !containsLabels(rollout.Spec.Template.Metadata.Labels, expected.PodLabels) {
		return fmt.Errorf("pod labels differ: template=%v live=%v", expected.PodLabels, rollout.Spec.Template.Metadata.Labels)
	}
	for _, service := range []render.ServiceContract{expected.StableService, expected.CanaryService} {
		args := make([]string, 0, 12)
		if paths.controllerKubeconfig != "" {
			args = append(args, "--kubeconfig", paths.controllerKubeconfig)
		}
		if paths.controllerContext != "" {
			args = append(args, "--context", paths.controllerContext)
		}
		args = append(args, "get", "service", service.Name, "-n", namespace, "-o", "json")
		raw, err := run(ctx, args, nil)
		if err != nil {
			return fmt.Errorf("read live Service %s: %w", service.Name, err)
		}
		var live struct {
			Spec struct {
				Selector map[string]string `json:"selector"`
				Ports    []livePort        `json:"ports"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(raw, &live); err != nil {
			return fmt.Errorf("decode live Service %s: %w", service.Name, err)
		}
		if !containsLabels(live.Spec.Selector, service.Selector) {
			return fmt.Errorf("Service %s selector differs: template=%v live=%v", service.Name, service.Selector, live.Spec.Selector)
		}
		if len(live.Spec.Ports) != 1 || normalizeLivePort(live.Spec.Ports[0].TargetPort) != service.TargetPort {
			return fmt.Errorf("Service %s targetPort differs: template=%s live=%v", service.Name, service.TargetPort, live.Spec.Ports)
		}
	}
	for _, port := range expected.ContainerPorts {
		found := false
		for _, container := range rollout.Spec.Template.Spec.Containers {
			for _, live := range container.Ports {
				if live.Name == port.Name && live.ContainerPort == port.Port {
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("Rollout container port differs: template=%s:%d", port.Name, port.Port)
		}
	}
	return nil
}

func containsLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func normalizeLivePort(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case float64:
		return strconv.Itoa(int(value))
	default:
		return fmt.Sprint(value)
	}
}

func finishDoctor(report doctorReport, jsonOut bool, stdout, stderr io.Writer, code int) int {
	if !jsonOut {
		fmt.Fprint(stdout, report.Render())
		return code
	}
	rows := make([]map[string]any, 0, len(report.rows))
	for _, row := range report.rows {
		rows = append(rows, map[string]any{"status": row.mark, "check": row.label, "value": row.value, "details": row.continuations, "remedy": row.remedy})
	}
	state := "ready"
	next := "safelane release plan --pr <number> --json"
	if code != ExitOK {
		state, next = "not_ready", "safelane doctor --json"
	}
	envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "doctor", OK: code == ExitOK, State: state, NextCommand: next, Warnings: []string{}, Result: map[string]any{"checks": rows, "failed": report.failed, "unavailable": report.unavailable, "evidence_ready": report.evidenceReady, "execution_ready": report.executionReady}}
	if err := jsonEncode(stdout, envelope); err != nil {
		return writeResultError(stderr, "doctor", err)
	}
	return code
}

func readiness(ready bool) string {
	if ready {
		return "ready"
	}
	return "not ready"
}

func skipKubectlDependents(report *doctorReport, skip func(string, string), reason string) {
	// N2 is intentionally the reachability-only form of the GitHub line.
	for i := range report.rows {
		if report.rows[i].label == "github" {
			report.rows[i].value, _, _ = strings.Cut(report.rows[i].value, ", token valid")
			break
		}
	}
	skip("cluster", "skipped ("+reason+")")
	skip("rollout", "skipped ("+reason+")")
	skip("identity", "skipped ("+reason+")")
	report.kubectlUnavailable = true
}

func displayDoctorPath(path string) string {
	home, err := project.Home()
	if err == nil && os.Getenv(project.HomeEnv) == "" {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return "~/.safelane/" + filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func parseKubectlVersion(raw []byte) (string, error) {
	text := string(raw)
	const key = `"gitVersion"`
	idx := strings.Index(text, key)
	if idx < 0 {
		return "", fmt.Errorf("could not parse kubectl version")
	}
	rest := text[idx+len(key):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return "", fmt.Errorf("could not parse kubectl version")
	}
	value := strings.TrimSpace(rest[colon+1:])
	if len(value) < 2 || value[0] != '"' {
		return "", fmt.Errorf("could not parse kubectl version")
	}
	value = value[1:]
	end := strings.IndexByte(value, '"')
	if end < 0 {
		return "", fmt.Errorf("could not parse kubectl version")
	}
	return value[:end], nil
}

func shortServiceAccount(identity string) string {
	parts := strings.Split(identity, ":")
	if len(parts) == 4 && parts[0] == "system" && parts[1] == "serviceaccount" {
		return "sa/" + parts[3]
	}
	return identity
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func containsLine(output, want string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}
