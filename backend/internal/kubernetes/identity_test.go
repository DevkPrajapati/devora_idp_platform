package kubernetes

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestClusterUIDReadsKubeSystemUID(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "kube-system", UID: "abc-123"},
	})
	c := &Client{Clientset: cs}

	uid, err := c.ClusterUID(context.Background())
	if err != nil {
		t.Fatalf("ClusterUID: %v", err)
	}
	if uid != "abc-123" {
		t.Fatalf("uid = %q, want abc-123", uid)
	}
}

func TestClusterUIDWithoutClientReportsDisconnected(t *testing.T) {
	var c *Client
	if _, err := c.ClusterUID(context.Background()); err == nil {
		t.Fatal("expected an error from a nil client")
	}
}

func TestProviderFromKubeconfig(t *testing.T) {
	tests := []struct {
		name        string
		contextName string
		want        string
	}{
		// A local cluster recorded as "imported" is treated as foreign compute:
		// stop only disconnects and delete only drops the database row, leaving
		// the real cluster running with all of its old state on show.
		{"minikube default profile", "minikube", ProviderMinikube},
		{"minikube named profile", "minikube-dev", ProviderMinikube},
		{"kind prefixes its contexts", "kind-idp", ProviderKind},
		{"a remote cluster is imported", "arn:aws:eks:eu-west-1:1:cluster/prod", ProviderImported},
		{"case is not significant", "MINIKUBE", ProviderMinikube},
		{"empty falls back to imported", "", ProviderImported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ProviderFromKubeconfig(nil, tc.contextName); got != tc.want {
				t.Fatalf("provider = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProviderFromKubeconfigFallsBackToCurrentContext(t *testing.T) {
	raw := []byte(kubeconfigFixture("minikube", "https://127.0.0.1:1234"))
	if got := ProviderFromKubeconfig(raw, ""); got != ProviderMinikube {
		t.Fatalf("provider = %q, want %q", got, ProviderMinikube)
	}
}

func TestProfileFromContextStripsKindPrefix(t *testing.T) {
	if got := ProfileFromContext(ProviderKind, "kind-idp"); got != "idp" {
		t.Fatalf("profile = %q, want idp", got)
	}
	if got := ProfileFromContext(ProviderMinikube, "minikube"); got != "minikube" {
		t.Fatalf("profile = %q, want minikube", got)
	}
}

func TestPinKubeconfigContextSelectsTheRequestedCluster(t *testing.T) {
	// `minikube kubectl -- config view` prints the user's whole kubeconfig, so
	// current-context belongs to whichever cluster they last selected. Reading
	// it unchanged activated the wrong cluster under the requested name.
	raw := []byte(multiContextKubeconfig())

	pinned, err := pinKubeconfigContext(raw, "dev")
	if err != nil {
		t.Fatalf("pinKubeconfigContext: %v", err)
	}

	cfg, err := loadKubeconfig(pinned)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.CurrentContext != "dev" {
		t.Fatalf("current context = %q, want dev", cfg.CurrentContext)
	}
	if len(cfg.Contexts) != 1 {
		t.Fatalf("contexts = %d, want only the pinned one", len(cfg.Contexts))
	}
	if server := kubeconfigServer(pinned); server != "https://127.0.0.1:2222" {
		t.Fatalf("server = %q, want the dev cluster", server)
	}
}

func TestPinKubeconfigContextRejectsMissingContext(t *testing.T) {
	raw := []byte(multiContextKubeconfig())
	if _, err := pinKubeconfigContext(raw, "absent"); err == nil {
		t.Fatal("expected an error for a context that is not present")
	}
}

func kubeconfigFixture(name, server string) string {
	return strings.Join([]string{
		"apiVersion: v1",
		"kind: Config",
		"current-context: " + name,
		"clusters:",
		"- name: " + name,
		"  cluster:",
		"    server: " + server,
		"contexts:",
		"- name: " + name,
		"  context:",
		"    cluster: " + name,
		"    user: " + name,
		"users:",
		"- name: " + name,
		"  user: {}",
		"",
	}, "\n")
}

// multiContextKubeconfig has two clusters and points current-context at the one
// the caller did not ask for.
func multiContextKubeconfig() string {
	return strings.Join([]string{
		"apiVersion: v1",
		"kind: Config",
		"current-context: prod",
		"clusters:",
		"- name: prod",
		"  cluster:",
		"    server: https://127.0.0.1:1111",
		"- name: dev",
		"  cluster:",
		"    server: https://127.0.0.1:2222",
		"contexts:",
		"- name: prod",
		"  context:",
		"    cluster: prod",
		"    user: prod",
		"- name: dev",
		"  context:",
		"    cluster: dev",
		"    user: dev",
		"users:",
		"- name: prod",
		"  user: {}",
		"- name: dev",
		"  user: {}",
		"",
	}, "\n")
}
