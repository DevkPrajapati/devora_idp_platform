package kubernetes

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// identityNamespace is the namespace whose UID fingerprints a cluster.
//
// kube-system is created once by the control plane during bootstrap and is
// never recreated for the lifetime of the cluster, so its UID is the closest
// thing Kubernetes offers to a cluster ID. Server URL is not a substitute:
// minikube and kind hand out a fresh host port on every start, so the URL
// changes while the cluster stays the same, and a recreated cluster can reuse
// the port it had before while being a completely different cluster.
const identityNamespace = "kube-system"

// ClusterUID returns the cluster's identity fingerprint.
//
// A changed UID means the API server on the other end of this kubeconfig is a
// different cluster than the one whose state the platform recorded, which is
// what happens after `minikube delete && minikube start`. Callers treat that as
// "discard everything derived from the old cluster" rather than presenting
// records that describe namespaces and workloads that no longer exist.
func (c *Client) ClusterUID(ctx context.Context) (string, error) {
	cs, csErr := c.cs()
	if csErr != nil {
		return "", csErr
	}
	ns, err := cs.CoreV1().Namespaces().Get(ctx, identityNamespace, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read cluster identity: %w", err)
	}
	uid := strings.TrimSpace(string(ns.UID))
	if uid == "" {
		return "", fmt.Errorf("cluster identity is empty")
	}
	return uid, nil
}

// ProviderFromKubeconfig guesses which local tool owns a kubeconfig context.
//
// The fleet needs this because a cluster registered as "imported" is treated as
// someone else's compute: stop only disconnects it and delete only drops the
// database row. Recording a local minikube or kind cluster as imported is what
// makes "delete the cluster" leave the real cluster running, so the platform
// keeps showing its namespaces and pods afterwards.
func ProviderFromKubeconfig(data []byte, contextName string) string {
	name := strings.ToLower(strings.TrimSpace(contextName))
	if name == "" {
		if cfg, err := loadKubeconfig(data); err == nil && cfg != nil {
			name = strings.ToLower(strings.TrimSpace(cfg.CurrentContext))
		}
	}
	switch {
	case name == ProviderMinikube || strings.HasPrefix(name, "minikube-"):
		return ProviderMinikube
	case strings.HasPrefix(name, "kind-"):
		return ProviderKind
	default:
		return ProviderImported
	}
}

// ProfileFromContext maps a kubeconfig context back to the provisioner profile
// name. kind prefixes its contexts with "kind-" while the cluster itself is
// named without the prefix; passing the context straight to `kind delete
// cluster --name` would miss.
func ProfileFromContext(provider, contextName string) string {
	name := strings.TrimSpace(contextName)
	if provider == ProviderKind {
		return strings.TrimPrefix(name, "kind-")
	}
	return name
}
