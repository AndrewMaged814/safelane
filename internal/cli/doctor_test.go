package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/AndrewMaged814/safelane/internal/policy"
	"github.com/AndrewMaged814/safelane/internal/project"
)

func doctorRuntime(t *testing.T) (projectFile, policyFile, templateDir string) {
	t.Helper()
	dir := t.TempDir()
	projectFile = filepath.Join(dir, "project.yml")
	policyFile = filepath.Join(dir, "policy.yml")
	templateDir = filepath.Join(dir, "release-template")
	if err := os.WriteFile(projectFile, project.DefaultYAML(
		"podinfo", "AndrewMaged814/podinfo", "master", "ghcr.io/andrewmaged814/podinfo",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policyFile, policy.DefaultYAML(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := filepath.Join(templateDir, string(rune('a'+i))+".yaml.tmpl")
		if err := os.WriteFile(name, []byte("kind: ConfigMap\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return projectFile, policyFile, templateDir
}

func healthyDoctorRunner(t *testing.T) (func(context.Context, []string, []byte) ([]byte, error), *[][]string) {
	t.Helper()
	var calls [][]string
	digest := strings.Repeat("1", 60) + "7742"
	run := func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		joined := strings.Join(args, " ")
		switch {
		case strings.HasSuffix(joined, "--context safelane-controller auth whoami -o json"):
			return []byte(`{"status":{"userInfo":{"username":"system:serviceaccount:podinfo:safelane-controller"}}}`), nil
		case joined == "auth whoami -o json":
			return []byte(`{"status":{"userInfo":{"username":"system:serviceaccount:podinfo:safelane-caller"}}}`), nil
		case strings.HasSuffix(joined, "--context safelane-controller auth can-i patch rollouts.argoproj.io --namespace podinfo"):
			return []byte("yes\n"), nil
		case joined == "auth can-i get rollouts.argoproj.io --namespace podinfo":
			return []byte("yes\n"), nil
		case joined == "auth can-i patch rollouts.argoproj.io --namespace podinfo":
			return []byte("no\n"), nil
		}
		switch joined {
		case "version --client -o json":
			return []byte(`{"clientVersion":{"gitVersion":"v1.31.2"}}`), nil
		case "argo rollouts version --short":
			return []byte("v1.7.2\n"), nil
		case "get rollout podinfo -n podinfo -o json":
			return []byte(`{"status":{"phase":"Healthy","stableRS":"abc","currentPodHash":"abc"},` +
				`"spec":{"template":{"spec":{"containers":[{"image":"ghcr.io/andrewmaged814/podinfo@sha256:` + digest + `"}]}}}}`), nil
		case "config get-contexts -o name":
			return []byte("safelane-caller\n"), nil
		}
		return nil, errors.New("unexpected kubectl call: " + strings.Join(args, " "))
	}
	return run, &calls
}

func TestDoctorClusterReachabilityUsesCallerRolloutRead(t *testing.T) {
	projectFile, policyFile, templateDir := doctorRuntime(t)
	run, calls := healthyDoctorRunner(t)
	deps := doctorDeps{
		run:        run,
		lookPath:   func(string) (string, error) { return "found", nil },
		githubPing: func(context.Context, string) (string, error) { return "AndrewMaged814", nil },
		ghcrPing:   func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)

	want := []string{"get", "rollout", "podinfo", "-n", "podinfo", "-o", "json"}
	for _, call := range *calls {
		if slices.Equal(call, want) {
			continue
		}
		if strings.Contains(strings.Join(call, " "), "get namespace") {
			t.Fatalf("doctor probed Namespace outside the restricted identity contract: %v", call)
		}
	}
	if !slices.ContainsFunc(*calls, func(call []string) bool { return slices.Equal(call, want) }) {
		t.Fatal("caller Rollout read was not used for cluster reachability")
	}
}

func TestDoctorHealthyMatchesA1Golden(t *testing.T) {
	projectFile, policyFile, templateDir := doctorRuntime(t)
	run, _ := healthyDoctorRunner(t)
	deps := doctorDeps{
		run:        run,
		lookPath:   func(string) (string, error) { return "found", nil },
		githubPing: func(context.Context, string) (string, error) { return "AndrewMaged814", nil },
		ghcrPing:   func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)
	if code != ExitOK {
		t.Fatalf("doctor exit = %d, stderr: %s\n%s", code, stderr.String(), stdout.String())
	}
	actual := strings.ReplaceAll(stdout.String(), filepath.ToSlash(projectFile), "~/.safelane/apps/podinfo/project.yml")
	actual = strings.ReplaceAll(actual, filepath.ToSlash(policyFile), "~/.safelane/apps/podinfo/policy.yml")
	assertGolden(t, "a1-doctor.txt", actual)
}

func TestDoctorGitHubFailureReportsEvidenceNotReadyAndExecutionReady(t *testing.T) {
	projectFile, policyFile, templateDir := doctorRuntime(t)
	run, _ := healthyDoctorRunner(t)
	deps := doctorDeps{
		run:      run,
		lookPath: func(string) (string, error) { return "found", nil },
		githubPing: func(context.Context, string) (string, error) {
			return "", errors.New("github: GET /user: unexpected status 401")
		},
		ghcrPing: func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)
	if code != ExitFail {
		t.Fatalf("doctor exit = %d, want %d", code, ExitFail)
	}
	out := stdout.String()
	for _, want := range []string{"Evidence and assessment  not ready", "Rollout execution       ready"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "can read evidence") {
		t.Fatalf("doctor contradicted the GitHub failure:\n%s", out)
	}
}

func TestDoctorUsesGitHubCLIKeyringTokenWhenEnvironmentIsEmpty(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	projectFile, policyFile, templateDir := doctorRuntime(t)
	run, _ := healthyDoctorRunner(t)
	resolved := false
	deps := doctorDeps{
		run:      run,
		lookPath: func(string) (string, error) { return "found", nil },
		githubToken: func(context.Context) (string, error) {
			resolved = true
			return "keyring-token", nil
		},
		githubPing: func(_ context.Context, token string) (string, error) {
			if token != "keyring-token" {
				t.Fatalf("github token = %q, want keyring token", token)
			}
			return "AndrewMaged814", nil
		},
		ghcrPing: func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)
	if code != ExitOK {
		t.Fatalf("doctor exit = %d, stderr: %s\n%s", code, stderr.String(), stdout.String())
	}
	if !resolved {
		t.Fatal("doctor did not consult the GitHub CLI keyring")
	}
}

func TestDoctorMissingKubectlMatchesN2AndSkipsDependents(t *testing.T) {
	projectFile, policyFile, templateDir := doctorRuntime(t)
	var calls int
	deps := doctorDeps{
		run: func(context.Context, []string, []byte) ([]byte, error) {
			calls++
			return nil, exec.ErrNotFound
		},
		lookPath:   func(string) (string, error) { return "found", nil },
		githubPing: func(context.Context, string) (string, error) { return "AndrewMaged814", nil },
		ghcrPing:   func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)
	if code != ExitFail {
		t.Fatalf("doctor exit = %d, want %d", code, ExitFail)
	}
	if calls != 1 {
		t.Fatalf("kubectl runner calls = %d, want only the availability probe", calls)
	}
	actual := strings.ReplaceAll(stdout.String(), filepath.ToSlash(projectFile), "~/.safelane/apps/podinfo/project.yml")
	actual = strings.ReplaceAll(actual, filepath.ToSlash(policyFile), "~/.safelane/apps/podinfo/policy.yml")
	assertGolden(t, "n2-doctor-kubectl-missing.txt", actual)
}

func TestDoctorUnreachableClusterMatchesN3Fragment(t *testing.T) {
	projectFile, policyFile, templateDir := doctorRuntime(t)
	healthy, _ := healthyDoctorRunner(t)
	run := func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
		if strings.Join(args, " ") == "get rollout podinfo -n podinfo -o json" {
			return nil, errors.New("dial tcp 10.0.0.12:6443: i/o timeout")
		}
		return healthy(ctx, args, stdin)
	}
	deps := doctorDeps{
		run:        run,
		lookPath:   func(string) (string, error) { return "found", nil },
		githubPing: func(context.Context, string) (string, error) { return "AndrewMaged814", nil },
		ghcrPing:   func(context.Context) error { return nil },
	}
	var stdout, stderr bytes.Buffer
	code := runDoctor(context.Background(), []string{
		"--project", projectFile, "--policy", policyFile, "--template-dir", templateDir,
	}, &stdout, &stderr, ".", deps)
	if code != ExitFail {
		t.Fatalf("doctor exit = %d, want %d", code, ExitFail)
	}
	assertGoldenFragment(t, "n3-doctor-cluster-unreachable.txt", stdout.String())
}
