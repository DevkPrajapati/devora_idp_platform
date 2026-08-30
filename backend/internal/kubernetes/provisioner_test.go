package kubernetes

import (
	"context"
	"strings"
	"testing"
)

type fakeCmd struct {
	path map[string]bool
	runs [][]string
	out  map[string]string
	err  map[string]error
}

func (f *fakeCmd) LookPath(file string) (string, error) {
	if f.path[file] {
		return file, nil
	}
	return "", errNotFound{file: file}
}

type errNotFound struct{ file string }

func (e errNotFound) Error() string { return e.file + ": not found" }

func (f *fakeCmd) Run(ctx context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	f.runs = append(f.runs, append([]string{name}, args...))
	var (
		out string
		err error
	)
	if e, ok := f.err[key]; ok {
		err = e
	} else if v, ok := f.out[key]; ok {
		out = v
	} else {
		// Prefix match so `--wait 120s` extras still hit.
		for k, v := range f.out {
			if strings.HasPrefix(key, k) {
				out = v
				break
			}
		}
	}
	if sink := logSinkFrom(ctx); sink != nil && out != "" {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				sink(line)
			}
		}
	}
	return out, err
}

func (f *fakeCmd) Stream(ctx context.Context, name string, args []string, emit func(string)) error {
	out, err := f.Run(ctx, name, args...)
	if emit != nil {
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				emit(line)
			}
		}
	}
	return err
}

func TestKindNodeImage(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"v1.31.0", "kindest/node:v1.31.0"},
		{"1.31.0", "kindest/node:v1.31.0"},
		{"kindest/node:v1.30.0", "kindest/node:v1.30.0"},
	}
	for _, tt := range tests {
		if got := kindNodeImage(tt.in); got != tt.want {
			t.Errorf("kindNodeImage(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteKindConfigIncludesWorkers(t *testing.T) {
	yaml := KindConfigYAML(CreateKindSpec{Name: "dev", WorkerCount: 2, KubernetesVersion: "v1.31.0"})
	if !strings.Contains(yaml, "role: control-plane") {
		t.Fatal("missing control-plane")
	}
	if strings.Count(yaml, "role: worker") != 2 {
		t.Fatalf("want 2 workers, yaml:\n%s", yaml)
	}
	if !strings.Contains(yaml, "kindest/node:v1.31.0") {
		t.Fatalf("missing node image:\n%s", yaml)
	}
}

func TestCreateKindRequiresBinary(t *testing.T) {
	p := &Provisioner{cmd: &fakeCmd{path: map[string]bool{}}}
	_, _, err := p.CreateKind(context.Background(), CreateKindSpec{Name: "dev"})
	if err == nil || !strings.Contains(err.Error(), "kind is not installed") {
		t.Fatalf("err = %v, want kind is not installed", err)
	}
}

func TestCreateKindInvokesKind(t *testing.T) {
	cmd := &fakeCmd{
		path: map[string]bool{"kind": true},
		out: map[string]string{
			"kind create cluster": "created",
			"kind get kubeconfig --name dev": `apiVersion: v1
clusters:
- cluster:
    server: https://127.0.0.1:6443
  name: kind-dev
contexts:
- context:
    cluster: kind-dev
    user: kind-dev
  name: kind-dev
current-context: kind-dev
kind: Config
users:
- name: kind-dev
  user: {}
`,
		},
	}
	p := &Provisioner{cmd: cmd}
	kc, server, err := p.CreateKind(context.Background(), CreateKindSpec{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if server != "https://127.0.0.1:6443" {
		t.Errorf("server = %q", server)
	}
	if !strings.Contains(string(kc), "kind-dev") {
		t.Errorf("kubeconfig missing context")
	}
}

func TestCreateKindEmitsLogSink(t *testing.T) {
	cmd := &fakeCmd{
		path: map[string]bool{"kind": true},
		out: map[string]string{
			"kind create cluster":            "Creating cluster\nReady",
			"kind get kubeconfig --name dev": "apiVersion: v1\nclusters:\n- cluster:\n    server: https://127.0.0.1:6443\n",
		},
	}
	p := &Provisioner{cmd: cmd}
	var lines []string
	ctx := WithLogSink(context.Background(), func(line string) { lines = append(lines, line) })
	_, _, err := p.CreateKind(ctx, CreateKindSpec{Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Creating cluster") {
		t.Fatalf("sink missed create output: %v", lines)
	}
	if strings.Contains(joined, "apiVersion") || strings.Contains(joined, "server:") {
		t.Fatalf("kubeconfig leaked into log sink: %v", lines)
	}
}

func TestStreamNodeLogsKindFollowsDocker(t *testing.T) {
	cmd := &fakeCmd{
		path: map[string]bool{"docker": true},
		out: map[string]string{
			"docker ps --format {{.Names}} --filter label=io.x-k8s.kind.cluster=dev": "dev-control-plane",
			"docker logs --tail 50 -f dev-control-plane":                             "kubelet started",
		},
	}
	p := &Provisioner{cmd: cmd}
	var lines []string
	if err := p.StreamNodeLogs(context.Background(), ProviderKind, "dev", 50, true, func(s string) {
		lines = append(lines, s)
	}); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "kubelet started" {
		t.Fatalf("lines = %v", lines)
	}
}

func TestValidateKubeconfigRejectsEmpty(t *testing.T) {
	if _, err := ValidateKubeconfig([]byte("not yaml: [")); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := ValidateKubeconfig([]byte("apiVersion: v1\nkind: Config\n")); err == nil {
		t.Fatal("expected missing context")
	}
}
