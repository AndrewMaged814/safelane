package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AndrewMaged814/safelane/internal/project"
	"github.com/AndrewMaged814/safelane/internal/verify/ghcr"
	"github.com/spf13/pflag"
)

const (
	demoClusterName = "safelane-demo"
	kindVersion     = "v0.26.0"
	kubectlVersion  = "v1.31.2"
	demoNodeImage   = "kindest/node:v1.31.2@sha256:18fbefc20a7113353c7b75b5c869d7145a6abd6269154825872dc59c1329912e"
	argoVersion     = "v1.7.2"
	demoApp         = "safelane-demo-api"
	demoRepo        = "andrewmaged814/safelane-demo-api"
	demoBaselineTag = "sha-726662d2c396b54cfc047721a41bc67e77643924"
	demoProbeRepo   = "andrewmaged814/safelane-demo-probe"
)

// DemoCommand manages only SafeLane's named Kind cluster and private kubeconfig.
func DemoCommand(action string) Command {
	return Command{Name: action, Summary: "manage the isolated SafeLane demo", Run: func(ctx context.Context, args []string, stdout, stderr io.Writer) int {
		return runDemo(ctx, action, args, stdout, stderr)
	}}
}

func runDemo(ctx context.Context, action string, args []string, stdout, stderr io.Writer) int {
	fs := pflag.NewFlagSet("demo "+action, pflag.ContinueOnError)
	fs.SetOutput(stderr)
	yes := fs.Bool("yes", false, "skip confirmation")
	jsonOut := fs.Bool("json", false, "print a stable command result")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return ExitUsage
	}
	if !*yes {
		if !isTerminalFile(os.Stdin) {
			fmt.Fprintf(stderr, "safelane demo %s: --yes is required noninteractively\n", action)
			return ExitUsage
		}
		fmt.Fprintf(stderr, "SafeLane will %s only the Kind cluster %q. Type APPLY: ", action, demoClusterName)
		var answer string
		if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil || answer != "APPLY" {
			fmt.Fprintln(stderr, "cancelled")
			return ExitFail
		}
	}

	home, err := project.Home()
	if err != nil {
		return writeResultError(stderr, "demo "+action, err)
	}
	dir := filepath.Join(home, "demo")
	m := demoManager{dir: dir, binDir: filepath.Join(dir, "bin"), kubeconfig: filepath.Join(dir, "kubeconfig"), stderr: stderr, client: http.DefaultClient}
	var state, next string
	switch action {
	case "up":
		err = m.up(ctx)
		state, next = "ready", "safelane doctor"
	case "reset":
		err = m.reset(ctx, home)
		state, next = "ready", "safelane release plan --pr <number> --json"
	case "down":
		err = m.down(ctx)
		state = "down"
	default:
		return ExitUsage
	}
	if err != nil {
		return writeResultError(stderr, "demo "+action, err)
	}
	envelope := ResultEnvelope{SchemaVersion: "safelane.command.result/v1", Command: "demo " + action, OK: true, State: state, NextCommand: next, Warnings: []string{}, Result: map[string]any{"cluster": demoClusterName, "kubeconfig": m.kubeconfig}}
	if *jsonOut {
		return writeEnvelopeCode(stdout, stderr, envelope)
	}
	fmt.Fprintf(stdout, "SafeLane demo %s: %s\n", action, state)
	if next != "" {
		fmt.Fprintf(stdout, "Next: %s\n", next)
	}
	return ExitOK
}

type demoManager struct {
	dir        string
	binDir     string
	kubeconfig string
	stderr     io.Writer
	client     *http.Client
}

func (m demoManager) up(ctx context.Context) error {
	if err := m.prepare(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	clusters, err := m.run(ctx, "kind", nil, "get", "clusters")
	if err != nil {
		return err
	}
	clusterExists := linePresent(clusters, demoClusterName)
	if clusterExists {
		config, configErr := m.run(ctx, "kind", nil, "get", "kubeconfig", "--name", demoClusterName)
		if configErr != nil {
			fmt.Fprintln(m.stderr, "The owned demo cluster is stale; recreating it…")
			if _, deleteErr := m.run(ctx, "kind", nil, "delete", "cluster", "--name", demoClusterName); deleteErr != nil {
				return fmt.Errorf("remove stale owned demo cluster: %w", deleteErr)
			}
			clusterExists = false
		} else if err := os.WriteFile(m.kubeconfig, []byte(config), 0o600); err != nil {
			return err
		}
	}
	if !clusterExists {
		fmt.Fprintln(m.stderr, "Creating the isolated Kind cluster…")
		if _, err := m.run(ctx, "kind", nil, "create", "cluster", "--name", demoClusterName, "--image", demoNodeImage, "--kubeconfig", m.kubeconfig, "--wait", "120s"); err != nil {
			return err
		}
	}
	argoURL := fmt.Sprintf("https://github.com/argoproj/argo-rollouts/releases/download/%s/install.yaml", argoVersion)
	if _, err := m.kubectl(ctx, nil, "create", "namespace", "argo-rollouts", "--dry-run=client", "-o", "yaml"); err == nil {
		// Apply the namespace independently so the remote manifest remains pinned and untouched.
		_, _ = m.kubectl(ctx, []byte("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argo-rollouts\n"), "apply", "-f", "-")
	}
	if _, err := m.kubectl(ctx, nil, "apply", "-n", "argo-rollouts", "-f", argoURL); err != nil {
		return err
	}
	if _, err := m.kubectl(ctx, nil, "rollout", "status", "deployment/argo-rollouts", "-n", "argo-rollouts", "--timeout=120s"); err != nil {
		return err
	}
	return m.seed(ctx)
}

func (m demoManager) prepare(ctx context.Context) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("Docker is required for the demo: %w", err)
	}
	if _, err := m.run(ctx, "docker", nil, "info"); err != nil {
		return fmt.Errorf("Docker is not ready: %w", err)
	}
	return m.ensureTools(ctx)
}

func (m demoManager) seed(ctx context.Context) error {
	registry := &ghcr.Client{}
	baselineDigest, err := registry.ResolveTag(ctx, demoRepo, demoBaselineTag)
	if err != nil {
		return fmt.Errorf("resolve healthy baseline fixture: %w", err)
	}
	manifest := demoBaselineManifest("ghcr.io/"+demoRepo+"@"+baselineDigest, demoBaselineTag)
	if _, err := m.kubectl(ctx, manifest, "apply", "-f", "-"); err != nil {
		return err
	}
	if _, err := m.kubectl(ctx, nil, "argo", "rollouts", "status", demoApp, "-n", demoApp, "--timeout", "180s"); err != nil {
		return err
	}
	if err := m.writeDemoCredentials(ctx); err != nil {
		return err
	}
	probeDigest, err := registry.ResolveTag(ctx, demoProbeRepo, "latest")
	if err != nil {
		return fmt.Errorf("resolve probe fixture (publish the companion probe image first): %w", err)
	}
	return m.bindOperatorConfig("ghcr.io/" + demoProbeRepo + "@" + probeDigest)
}

func (m demoManager) writeDemoCredentials(ctx context.Context) error {
	view, err := m.kubectl(ctx, nil, "config", "view", "--raw", "--minify", "-o", "json")
	if err != nil {
		return err
	}
	var config struct {
		Clusters []struct {
			Cluster struct {
				Server                   string `json:"server"`
				CertificateAuthorityData string `json:"certificate-authority-data"`
			} `json:"cluster"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(view), &config); err != nil {
		return fmt.Errorf("read private demo cluster identity: %w", err)
	}
	if len(config.Clusters) != 1 {
		return fmt.Errorf("read private demo cluster identity: expected one cluster, got %d", len(config.Clusters))
	}
	for _, identity := range []string{"safelane-controller", "safelane-caller"} {
		token, err := m.kubectl(ctx, nil, "create", "token", identity, "-n", demoApp, "--duration=24h")
		if err != nil {
			return err
		}
		body := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
  - name: safelane-demo
    cluster:
      server: %s
      certificate-authority-data: %s
contexts:
  - name: %s
    context:
      cluster: safelane-demo
      namespace: %s
      user: %s
current-context: %s
users:
  - name: %s
    user:
      token: %s
`, config.Clusters[0].Cluster.Server, config.Clusters[0].Cluster.CertificateAuthorityData, identity, demoApp, identity, identity, identity, strings.TrimSpace(token))
		if err := os.WriteFile(filepath.Join(m.dir, identity+".kubeconfig"), []byte(body), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func (m demoManager) bindOperatorConfig(probeImage string) error {
	home, err := project.Home()
	if err != nil {
		return err
	}
	loc := project.ForApp(home, demoApp)
	if _, err := os.Stat(loc.ProjectFile); err != nil {
		if os.IsNotExist(err) {
			return nil // setup may intentionally run after demo up
		}
		return err
	}
	raw, err := os.ReadFile(loc.ProjectFile)
	if err != nil {
		return err
	}
	updated := strings.ReplaceAll(string(raw), "ghcr.io/andrewmaged814/safelane-demo-probe@sha256:REPLACE_WITH_PUBLISHED_DIGEST", probeImage)
	if err := os.WriteFile(loc.ProjectFile, []byte(updated), 0o600); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(m.dir, "safelane-controller.kubeconfig"), filepath.Join(loc.AppDir, "controller.kubeconfig")); err != nil {
		return err
	}
	return copyFile(filepath.Join(m.dir, "safelane-caller.kubeconfig"), filepath.Join(loc.AppDir, "caller.kubeconfig"))
}

func (m demoManager) reset(ctx context.Context, home string) error {
	if err := m.prepare(ctx); err != nil {
		return err
	}
	if err := m.requireOwnedCluster(ctx); err != nil {
		return err
	}
	loc := project.ForApp(home, demoApp)
	if entries, err := os.ReadDir(loc.ReleasesDir); err == nil && len(entries) > 0 {
		archive := filepath.Join(loc.AppDir, "archive", time.Now().UTC().Format("20060102T150405Z"))
		if err := os.MkdirAll(filepath.Dir(archive), 0o700); err != nil {
			return err
		}
		if err := os.Rename(loc.ReleasesDir, archive); err != nil {
			return err
		}
	}
	if _, err := m.kubectl(ctx, nil, "delete", "namespace", demoApp, "--ignore-not-found", "--wait=true"); err != nil {
		return err
	}
	return m.seed(ctx)
}

func (m demoManager) down(ctx context.Context) error {
	if err := m.prepare(ctx); err != nil {
		return err
	}
	if err := m.requireOwnedCluster(ctx); err != nil {
		return err
	}
	_, err := m.run(ctx, "kind", nil, "delete", "cluster", "--name", demoClusterName)
	return err
}

func (m demoManager) requireOwnedCluster(ctx context.Context) error {
	clusters, err := m.run(ctx, "kind", nil, "get", "clusters")
	if err != nil {
		return err
	}
	if !linePresent(clusters, demoClusterName) {
		return fmt.Errorf("owned Kind cluster %q does not exist", demoClusterName)
	}
	return nil
}

func (m demoManager) kubectl(ctx context.Context, stdin []byte, args ...string) (string, error) {
	base := []string{"--kubeconfig", m.kubeconfig, "--context", "kind-" + demoClusterName}
	if len(args) > 0 && args[0] == "argo" {
		return m.run(ctx, "kubectl", stdin, append(args, base...)...)
	}
	return m.run(ctx, "kubectl", stdin, append(base, args...)...)
}

func (m demoManager) run(ctx context.Context, name string, stdin []byte, args ...string) (string, error) {
	if name != "docker" {
		managed := filepath.Join(m.binDir, executableName(name))
		if _, err := os.Stat(managed); err == nil {
			name = managed
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "PATH="+m.binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %s: %w", name, strings.Join(args, " "), strings.TrimSpace(errOut.String()), err)
	}
	return out.String(), nil
}

type demoTool struct {
	name, asset, binaryURL, checksumURL, checksumName string
}

func (m demoManager) ensureTools(ctx context.Context) error {
	if err := os.MkdirAll(m.binDir, 0o700); err != nil {
		return err
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	kindAsset := "kind-" + platform
	kubectlAsset := "kubectl"
	if runtime.GOOS == "windows" {
		kubectlAsset += ".exe"
	}
	argoAsset := "kubectl-argo-rollouts-" + platform
	tools := []demoTool{
		{name: "kind", asset: kindAsset, binaryURL: "https://github.com/kubernetes-sigs/kind/releases/download/" + kindVersion + "/" + kindAsset, checksumURL: "https://github.com/kubernetes-sigs/kind/releases/download/" + kindVersion + "/" + kindAsset + ".sha256sum", checksumName: kindAsset},
		{name: "kubectl", asset: kubectlAsset, binaryURL: "https://dl.k8s.io/release/" + kubectlVersion + "/bin/" + runtime.GOOS + "/" + runtime.GOARCH + "/" + kubectlAsset, checksumURL: "https://dl.k8s.io/release/" + kubectlVersion + "/bin/" + runtime.GOOS + "/" + runtime.GOARCH + "/" + kubectlAsset + ".sha256", checksumName: kubectlAsset},
		{name: "kubectl-argo-rollouts", asset: argoAsset, binaryURL: "https://github.com/argoproj/argo-rollouts/releases/download/" + argoVersion + "/" + argoAsset, checksumURL: "https://github.com/argoproj/argo-rollouts/releases/download/" + argoVersion + "/argo-rollouts-checksums.txt", checksumName: argoAsset},
	}
	for _, tool := range tools {
		target := filepath.Join(m.binDir, executableName(tool.name))
		if _, err := os.Stat(target); err == nil {
			continue
		}
		fmt.Fprintf(m.stderr, "Installing pinned %s for the isolated demo…\n", tool.name)
		if err := downloadVerified(ctx, m.client, target, tool); err != nil {
			return fmt.Errorf("install %s: %w", tool.name, err)
		}
	}
	return nil
}

func downloadVerified(ctx context.Context, client *http.Client, target string, tool demoTool) error {
	binary, err := downloadBytes(ctx, client, tool.binaryURL)
	if err != nil {
		return err
	}
	checksums, err := downloadBytes(ctx, client, tool.checksumURL)
	if err != nil {
		return err
	}
	want, err := checksumFor(string(checksums), tool.checksumName)
	if err != nil {
		return err
	}
	got := fmt.Sprintf("%x", sha256.Sum256(binary))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", tool.asset)
	}
	temp := target + ".download"
	if err := os.WriteFile(temp, binary, 0o700); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Chmod(target, 0o700)
}

func downloadBytes(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func checksumFor(contents, asset string) (string, error) {
	for _, line := range strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 1 && len(fields[0]) == 64 {
			return fields[0], nil
		}
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s was not published", asset)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func linePresent(value, target string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

func copyFile(source, target string) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o600)
}

func demoBaselineManifest(image, commit string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %[1]s
  labels:
    safelane.dev/owned-demo: "true"
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: safelane-controller
  namespace: %[1]s
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: safelane-caller
  namespace: %[1]s
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: safelane-controller
  namespace: %[1]s
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["rollouts", "analysisruns", "analysistemplates"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: [""]
    resources: ["services", "pods", "events"]
    verbs: ["get", "list", "watch", "create", "update", "patch"]
  - apiGroups: ["apps"]
    resources: ["replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["batch"]
    resources: ["jobs"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: safelane-controller
  namespace: %[1]s
subjects:
  - kind: ServiceAccount
    name: safelane-controller
    namespace: %[1]s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: safelane-controller
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: safelane-caller
  namespace: %[1]s
rules:
  - apiGroups: ["argoproj.io"]
    resources: ["rollouts"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: safelane-caller
  namespace: %[1]s
subjects:
  - kind: ServiceAccount
    name: safelane-caller
    namespace: %[1]s
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: safelane-caller
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s-stable
  namespace: %[1]s
spec:
  selector: { app.kubernetes.io/name: %[1]s }
  ports: [{ name: http, port: 80, targetPort: http }]
---
apiVersion: v1
kind: Service
metadata:
  name: %[1]s-canary
  namespace: %[1]s
spec:
  selector: { app.kubernetes.io/name: %[1]s }
  ports: [{ name: http, port: 80, targetPort: http }]
---
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: %[1]s
  namespace: %[1]s
  labels: { safelane.dev/owned-demo: "true" }
spec:
  replicas: 2
  selector:
    matchLabels: { app.kubernetes.io/name: %[1]s }
  strategy:
    canary:
      stableService: %[1]s-stable
      canaryService: %[1]s-canary
  template:
    metadata:
      labels: { app.kubernetes.io/name: %[1]s }
    spec:
      containers:
        - name: api
          image: %[2]s
          env:
            - { name: GIT_SHA, value: "%[3]s" }
          ports: [{ name: http, containerPort: 8080 }]
          readinessProbe: { httpGet: { path: /healthz, port: http } }
          livenessProbe: { httpGet: { path: /healthz, port: http } }
`, demoApp, image, strings.TrimPrefix(commit, "sha-")))
}

func isTerminalFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
