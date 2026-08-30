package kubernetes

import (
	"context"
	"testing"
	"time"

	"github.com/idp/platform/backend/internal/config"
)

func TestClassifyNamespaceKind(t *testing.T) {
	tests := []struct {
		name   string
		ns     string
		labels map[string]string
		want   string
	}{
		{"managed tenant", "user-menagement", map[string]string{"idp.platform/managed": "true"}, "tenant"},
		{"kube-system", "kube-system", nil, "system"},
		{"kube-public", "kube-public", nil, "system"},
		{"kube-node-lease", "kube-node-lease", nil, "system"},
		{"kubectl created", "test-1", nil, "cluster"},
		{"infra", "ingress-nginx", nil, "cluster"},
		{"system label does not override managed", "kube-system", map[string]string{"idp.platform/managed": "true"}, "tenant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyNamespaceKind(tt.ns, tt.labels); got != tt.want {
				t.Errorf("ClassifyNamespaceKind(%q) = %q, want %q", tt.ns, got, tt.want)
			}
		})
	}
}

func TestReplicaStatus(t *testing.T) {
	if got := replicaStatus(2, 2); got != "Ready" {
		t.Errorf("ready replicas: got %q", got)
	}
	if got := replicaStatus(1, 3); got != "Progressing" {
		t.Errorf("partial replicas: got %q", got)
	}
	if got := replicaStatus(0, 0); got != "Scaled" {
		t.Errorf("scaled to zero: got %q", got)
	}
}

func TestListClusterNamespacesLive(t *testing.T) {
	client, err := NewClient(config.KubernetesConfig{})
	if err != nil {
		t.Skipf("no kubernetes client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	namespaces, err := client.ListClusterNamespaces(ctx)
	if err != nil {
		t.Skipf("cluster not reachable: %v", err)
	}
	if len(namespaces) == 0 {
		t.Fatal("expected at least one cluster namespace")
	}

	names := make(map[string]bool, len(namespaces))
	for _, ns := range namespaces {
		names[ns.Name] = true
		if ns.Phase == "" {
			t.Errorf("namespace %s has empty phase", ns.Name)
		}
		if ns.Kind == "" {
			t.Errorf("namespace %s has empty kind", ns.Name)
		}
	}

	if !names["default"] {
		t.Error("expected default namespace from the live cluster")
	}

	inv, err := client.GetNamespaceResources(ctx, "kube-system")
	if err != nil {
		t.Fatalf("GetNamespaceResources(kube-system): %v", err)
	}
	if inv.TotalResources == 0 {
		t.Fatal("expected kube-system to contain resources")
	}
	if len(inv.Groups) != 4 {
		t.Fatalf("expected 4 resource groups, got %d", len(inv.Groups))
	}
}
