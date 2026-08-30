package kubernetes

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Provider names stored on the fleet row.
const (
	ProviderKind     = "kind"
	ProviderMinikube = "minikube"
	ProviderImported = "imported"
)

// Commander runs host binaries. Tests swap it so Create/Stop never shell out.
type Commander interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) (stdout string, err error)
	// Stream runs a command and emits each stdout/stderr line as it is written.
	// Used for `docker logs -f` / `minikube logs -f`.
	Stream(ctx context.Context, name string, args []string, emit func(string)) error
}

type execCommander struct{}

func (execCommander) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (execCommander) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	stdoutW := io.Writer(&stdout)
	stderrW := io.Writer(&stderr)
	var outSplit, errSplit *lineEmitter
	if sink := logSinkFrom(ctx); sink != nil {
		outSplit = &lineEmitter{emit: sink}
		errSplit = &lineEmitter{emit: sink}
		stdoutW = io.MultiWriter(&stdout, outSplit)
		stderrW = io.MultiWriter(&stderr, errSplit)
	}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	err := cmd.Run()
	if outSplit != nil {
		outSplit.Flush()
	}
	if errSplit != nil {
		errSplit.Flush()
	}
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = out
		}
		if detail == "" {
			detail = err.Error()
		}
		return out, fmt.Errorf("%s: %s", name, detail)
	}
	return out, nil
}

func (execCommander) Stream(ctx context.Context, name string, args []string, emit func(string)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 64*1024), maxSinkLineBytes)
		for s.Scan() {
			if emit != nil {
				emit(s.Text())
			}
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if waitErr != nil {
		return fmt.Errorf("%s: %w", name, waitErr)
	}
	return nil
}

// Provisioner creates and tears down local Kubernetes clusters via kind or minikube.
type Provisioner struct {
	cmd Commander
	// kubeconfigPath is the host kubeconfig kind and minikube write their
	// contexts into. Reading it is orders of magnitude cheaper than asking the
	// tools, so it is the preferred source when it holds the context wanted.
	kubeconfigPath string
	// minikubeHome is the root of minikube's profile directory, used to answer
	// "does this profile exist" from the filesystem.
	minikubeHome string
}

// NewProvisioner uses the host PATH.
func NewProvisioner() *Provisioner {
	return &Provisioner{cmd: execCommander{}}
}

// WithHostPaths tells the provisioner where the host kubeconfig and minikube
// state live, letting it answer from the filesystem instead of shelling out.
func (p *Provisioner) WithHostPaths(kubeconfigPath, minikubeHome string) *Provisioner {
	p.kubeconfigPath = kubeconfigPath
	p.minikubeHome = minikubeHome
	return p
}

// hostKubeconfig reads the kubeconfig file kind and minikube maintain.
func (p *Provisioner) hostKubeconfig() ([]byte, error) {
	path := p.kubeconfigPath
	if path == "" {
		return nil, fmt.Errorf("no kubeconfig path configured")
	}
	return os.ReadFile(filepath.Clean(path))
}

// KindAvailable reports whether the kind binary is on PATH.
func (p *Provisioner) KindAvailable() bool {
	_, err := p.cmd.LookPath("kind")
	return err == nil
}

// MinikubeAvailable reports whether the minikube binary is on PATH.
func (p *Provisioner) MinikubeAvailable() bool {
	_, err := p.cmd.LookPath("minikube")
	return err == nil
}

// CreateKindSpec is the local kind cluster the admin asked to provision.
type CreateKindSpec struct {
	Name              string
	KubernetesVersion string
	WorkerCount       int32
}

// CreateKind runs `kind create cluster` and returns the kubeconfig for that cluster.
func (p *Provisioner) CreateKind(ctx context.Context, spec CreateKindSpec) (kubeconfig []byte, server string, err error) {
	if !p.KindAvailable() {
		return nil, "", fmt.Errorf("kind is not installed; install https://kind.sigs.k8s.io/ or import an existing kubeconfig")
	}

	if exists, err := p.KindClusterExists(ctx, spec.Name); err == nil && exists {
		if delErr := p.DeleteKind(ctx, spec.Name); delErr != nil {
			return nil, "", fmt.Errorf("replace existing kind cluster %s: %w", spec.Name, delErr)
		}
	}

	args := []string{"create", "cluster", "--name", spec.Name, "--wait", "120s"}
	if spec.WorkerCount > 0 || spec.KubernetesVersion != "" {
		cfgPath, cfgErr := writeKindConfig(spec)
		if cfgErr != nil {
			return nil, "", cfgErr
		}
		defer func() { _ = os.Remove(cfgPath) }()
		args = append(args, "--config", cfgPath)
	}

	if _, err := p.cmd.Run(ctx, "kind", args...); err != nil {
		return nil, "", err
	}
	return p.kindKubeconfig(ctx, spec.Name)
}

func writeKindConfig(spec CreateKindSpec) (string, error) {
	var b strings.Builder
	b.WriteString("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\nnodes:\n")
	nodeImage := kindNodeImage(spec.KubernetesVersion)
	writeNode := func(role string) {
		b.WriteString("- role: ")
		b.WriteString(role)
		b.WriteByte('\n')
		if nodeImage != "" {
			b.WriteString("  image: ")
			b.WriteString(nodeImage)
			b.WriteByte('\n')
		}
	}
	writeNode("control-plane")
	for i := int32(0); i < spec.WorkerCount; i++ {
		writeNode("worker")
	}

	f, err := os.CreateTemp("", "idp-kind-*.yaml")
	if err != nil {
		return "", fmt.Errorf("kind config: %w", err)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func kindNodeImage(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	if strings.HasPrefix(version, "kindest/node:") {
		return version
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return "kindest/node:" + version
}

// KindKubeconfig returns the kubeconfig document kind wrote for name.
func (p *Provisioner) KindKubeconfig(ctx context.Context, name string) ([]byte, string, error) {
	return p.kindKubeconfig(ctx, name)
}

func (p *Provisioner) kindKubeconfig(ctx context.Context, name string) ([]byte, string, error) {
	out, err := p.cmd.Run(WithoutLogSink(ctx), "kind", "get", "kubeconfig", "--name", name)
	if err != nil {
		return nil, "", err
	}
	server := kubeconfigServer([]byte(out))
	return []byte(out), server, nil
}

// CreateMinikube starts a minikube profile and returns its kubeconfig.
//
// A leftover profile of the same name is destroyed first. Create means a new
// cluster; starting an existing profile is RestartCluster. Reusing the leftover
// is how deleting a cluster and then creating it again showed the old namespaces.
func (p *Provisioner) CreateMinikube(ctx context.Context, name, kubernetesVersion string) ([]byte, string, error) {
	if !p.MinikubeAvailable() {
		return nil, "", fmt.Errorf("minikube is not installed; install https://minikube.sigs.k8s.io/ or import an existing kubeconfig")
	}
	if exists, err := p.MinikubeProfileExists(ctx, name); err == nil && exists {
		if delErr := p.DeleteMinikube(ctx, name); delErr != nil {
			return nil, "", fmt.Errorf("replace existing minikube profile %s: %w", name, delErr)
		}
	}
	args := []string{"start", "-p", name, "--wait=all"}
	if kubernetesVersion != "" {
		args = append(args, "--kubernetes-version", kubernetesVersion)
	}
	if _, err := p.cmd.Run(ctx, "minikube", args...); err != nil {
		return nil, "", err
	}
	return p.minikubeKubeconfig(ctx, name)
}

// MinikubeKubeconfig returns a flattened kubeconfig for a minikube profile.
func (p *Provisioner) MinikubeKubeconfig(ctx context.Context, name string) ([]byte, string, error) {
	return p.minikubeKubeconfig(ctx, name)
}

func (p *Provisioner) minikubeKubeconfig(ctx context.Context, name string) ([]byte, string, error) {
	// minikube rewrites its context in the host kubeconfig on every start, so
	// that file already carries the current API server address and CA. Prefer
	// it: `minikube kubectl` resolves and executes a matching kubectl binary
	// first, which costs tens of seconds on a loaded machine and made server
	// startup appear to hang.
	if raw, err := p.hostKubeconfig(); err == nil {
		if pinned, pinErr := pinKubeconfigContext(raw, name); pinErr == nil {
			return pinned, kubeconfigServer(pinned), nil
		}
	}

	out, err := p.cmd.Run(WithoutLogSink(ctx), "minikube", "-p", name, "kubectl", "--", "config", "view", "--flatten")
	if err != nil {
		return nil, "", err
	}
	// `minikube kubectl` reads the user's kubeconfig, so the document it prints
	// carries every context and a current-context that belongs to whichever
	// cluster the user last selected — not necessarily the profile asked for
	// here. Handing that straight back would activate the wrong cluster while
	// reporting the requested name.
	pinned, err := pinKubeconfigContext([]byte(out), name)
	if err != nil {
		return nil, "", err
	}
	return pinned, kubeconfigServer(pinned), nil
}

// pinKubeconfigContext rewrites a kubeconfig so contextName is the current
// context, and drops the rest. The result describes exactly one cluster, so a
// later read of it cannot resolve to a different one.
func pinKubeconfigContext(data []byte, contextName string) ([]byte, error) {
	cfg, err := loadKubeconfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	kubeCtx, ok := cfg.Contexts[contextName]
	if !ok || kubeCtx == nil {
		return nil, fmt.Errorf("kubeconfig has no context named %q", contextName)
	}
	cluster, ok := cfg.Clusters[kubeCtx.Cluster]
	if !ok || cluster == nil {
		return nil, fmt.Errorf("context %q references missing cluster %q", contextName, kubeCtx.Cluster)
	}

	minified := clientcmdapi.NewConfig()
	minified.CurrentContext = contextName
	minified.Contexts[contextName] = kubeCtx
	minified.Clusters[kubeCtx.Cluster] = cluster
	if authInfo, ok := cfg.AuthInfos[kubeCtx.AuthInfo]; ok && authInfo != nil {
		minified.AuthInfos[kubeCtx.AuthInfo] = authInfo
	}

	pinned, err := clientcmd.Write(*minified)
	if err != nil {
		return nil, fmt.Errorf("serialise kubeconfig: %w", err)
	}
	return pinned, nil
}

// StopKind pauses a kind cluster by stopping its Docker nodes.
func (p *Provisioner) StopKind(ctx context.Context, name string) error {
	ids, err := p.kindContainerIDs(ctx, name, false)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("kind cluster %s has no nodes to stop", name)
	}
	args := append([]string{"stop"}, ids...)
	_, err = p.cmd.Run(ctx, "docker", args...)
	return err
}

// StartKind resumes a stopped kind cluster.
func (p *Provisioner) StartKind(ctx context.Context, name string) ([]byte, string, error) {
	ids, err := p.kindContainerIDs(ctx, name, true)
	if err != nil {
		return nil, "", err
	}
	if len(ids) == 0 {
		return nil, "", fmt.Errorf("kind cluster %s has no nodes to start", name)
	}
	args := append([]string{"start"}, ids...)
	if _, err := p.cmd.Run(ctx, "docker", args...); err != nil {
		return nil, "", err
	}
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		kc, server, err := p.kindKubeconfig(ctx, name)
		if err == nil {
			return kc, server, nil
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if last == nil {
		last = fmt.Errorf("kind cluster %s did not become ready", name)
	}
	return nil, "", last
}

func (p *Provisioner) kindContainerIDs(ctx context.Context, name string, all bool) ([]string, error) {
	args := []string{"ps", "-q", "--filter", "label=io.x-k8s.kind.cluster=" + name}
	if all {
		args = []string{"ps", "-aq", "--filter", "label=io.x-k8s.kind.cluster=" + name}
	}
	out, err := p.cmd.Run(ctx, "docker", args...)
	if err != nil {
		return nil, err
	}
	return splitNonEmpty(out), nil
}

// DeleteKind removes a kind cluster and its Docker nodes.
func (p *Provisioner) DeleteKind(ctx context.Context, name string) error {
	if !p.KindAvailable() {
		return fmt.Errorf("kind is not installed")
	}
	_, err := p.cmd.Run(ctx, "kind", "delete", "cluster", "--name", name)
	return err
}

// StopMinikube stops a minikube profile.
func (p *Provisioner) StopMinikube(ctx context.Context, name string) error {
	if !p.MinikubeAvailable() {
		return fmt.Errorf("minikube is not installed")
	}
	_, err := p.cmd.Run(ctx, "minikube", "stop", "-p", name)
	return err
}

// StartMinikube starts a stopped minikube profile.
func (p *Provisioner) StartMinikube(ctx context.Context, name string) ([]byte, string, error) {
	if !p.MinikubeAvailable() {
		return nil, "", fmt.Errorf("minikube is not installed")
	}
	if _, err := p.cmd.Run(ctx, "minikube", "start", "-p", name, "--wait=all"); err != nil {
		return nil, "", err
	}
	return p.minikubeKubeconfig(ctx, name)
}

// DeleteMinikube deletes a minikube profile.
func (p *Provisioner) DeleteMinikube(ctx context.Context, name string) error {
	if !p.MinikubeAvailable() {
		return fmt.Errorf("minikube is not installed")
	}
	_, err := p.cmd.Run(ctx, "minikube", "delete", "-p", name)
	return err
}

// EnableMinikubeAddon turns on a minikube addon (metrics-server for HPA).
func (p *Provisioner) EnableMinikubeAddon(ctx context.Context, profile, addon string) error {
	if !p.MinikubeAvailable() {
		return fmt.Errorf("minikube is not installed")
	}
	_, err := p.cmd.Run(ctx, "minikube", "addons", "enable", addon, "-p", profile)
	return err
}

// AddMinikubeNode adds a worker to a minikube profile. Local stand-in for a
// cloud node autoscaler; capped by the caller.
func (p *Provisioner) AddMinikubeNode(ctx context.Context, profile string) error {
	if !p.MinikubeAvailable() {
		return fmt.Errorf("minikube is not installed")
	}
	_, err := p.cmd.Run(ctx, "minikube", "node", "add", "-p", profile)
	return err
}

// MinikubeProfileExists reports whether a minikube profile is still on disk.
//
// A profile deleted outside the platform (`minikube delete` in a terminal) is
// gone, not stopped, and its fleet row describes nothing. Without this check
// the row keeps reporting the cluster's last known version, node count and
// namespaces, which is the "old cluster data is still showing" symptom.
func (p *Provisioner) MinikubeProfileExists(ctx context.Context, name string) (bool, error) {
	if !p.MinikubeAvailable() {
		return false, fmt.Errorf("minikube is not installed")
	}
	// A profile is a directory with a config.json in it. Checking for the file
	// is instant, where `minikube profile list` inspects every profile's
	// running state and takes seconds.
	if p.minikubeHome != "" {
		if _, err := os.Stat(filepath.Join(p.minikubeHome, "profiles", name, "config.json")); err == nil {
			return true, nil
		} else if os.IsNotExist(err) {
			return false, nil
		}
	}

	// `profile list -o json` exits non-zero when there are no profiles at all,
	// which is a legitimate "does not exist" rather than a failure to answer.
	out, err := p.cmd.Run(WithoutLogSink(ctx), "minikube", "profile", "list", "-o", "json")
	if err != nil && strings.TrimSpace(out) == "" {
		return false, nil
	}
	return strings.Contains(out, `"Name":"`+name+`"`), nil
}

// KindClusterExists reports whether kind still knows about a cluster.
func (p *Provisioner) KindClusterExists(ctx context.Context, name string) (bool, error) {
	if !p.KindAvailable() {
		return false, fmt.Errorf("kind is not installed")
	}
	out, err := p.cmd.Run(WithoutLogSink(ctx), "kind", "get", "clusters")
	if err != nil {
		return false, err
	}
	for _, line := range splitNonEmpty(out) {
		if line == name {
			return true, nil
		}
	}
	return false, nil
}

// KindNodesRunning reports whether any kind node containers are up.
func (p *Provisioner) KindNodesRunning(ctx context.Context, name string) (bool, error) {
	ids, err := p.kindContainerIDs(ctx, name, false)
	if err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// StreamNodeLogs tails control-plane / minikube logs. follow keeps the stream
// open until ctx is cancelled.
func (p *Provisioner) StreamNodeLogs(ctx context.Context, provider, name string, tail int, follow bool, emit func(string)) error {
	if tail <= 0 {
		tail = 100
	}
	if tail > 5000 {
		tail = 5000
	}
	switch provider {
	case ProviderKind:
		target, err := p.kindLogContainer(ctx, name)
		if err != nil {
			return err
		}
		args := []string{"logs", "--tail", strconv.Itoa(tail)}
		if follow {
			args = append(args, "-f")
		}
		args = append(args, target)
		return p.cmd.Stream(ctx, "docker", args, emit)
	case ProviderMinikube:
		args := []string{"logs", "-p", name, "-n", strconv.Itoa(tail)}
		if follow {
			args = append(args, "-f")
		}
		return p.cmd.Stream(ctx, "minikube", args, emit)
	default:
		return nil
	}
}

func (p *Provisioner) kindLogContainer(ctx context.Context, name string) (string, error) {
	out, err := p.cmd.Run(ctx, "docker", "ps", "--format", "{{.Names}}", "--filter", "label=io.x-k8s.kind.cluster="+name)
	if err != nil {
		return "", err
	}
	names := splitNonEmpty(out)
	for _, n := range names {
		if strings.HasSuffix(n, "-control-plane") {
			return n, nil
		}
	}
	if len(names) > 0 {
		return names[0], nil
	}
	ids, err := p.kindContainerIDs(ctx, name, false)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("kind cluster %s has no running nodes", name)
	}
	return ids[0], nil
}

func kubeconfigServer(data []byte) string {
	cfg, err := clientcmdLoad(data)
	if err != nil || cfg == nil {
		return ""
	}
	ctx, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok || ctx == nil {
		return ""
	}
	cluster, ok := cfg.Clusters[ctx.Cluster]
	if !ok || cluster == nil {
		return ""
	}
	return cluster.Server
}

func clientcmdLoad(data []byte) (*clientcmdAPIConfig, error) {
	// Isolated so provisioner_test can assert on server extraction without
	// pulling the whole client-go config surface into every test.
	cfg, err := loadKubeconfig(data)
	return cfg, err
}

func splitNonEmpty(s string) []string {
	fields := strings.Fields(s)
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// KindConfigYAML is exported for tests that lock the generated kind config.
func KindConfigYAML(spec CreateKindSpec) string {
	path, err := writeKindConfig(spec)
	if err != nil {
		return ""
	}
	defer func() { _ = os.Remove(path) }()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return ""
	}
	return string(b)
}
