package kubernetes

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type clientcmdAPIConfig = clientcmdapi.Config

func loadKubeconfig(data []byte) (*clientcmdapi.Config, error) {
	return clientcmd.Load(data)
}

// ValidateKubeconfig parses a kubeconfig document and returns the API server URL.
func ValidateKubeconfig(data []byte) (server string, err error) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return "", fmt.Errorf("invalid kubeconfig: %w", err)
	}
	if cfg.CurrentContext == "" {
		return "", fmt.Errorf("kubeconfig has no current context")
	}
	ctx, ok := cfg.Contexts[cfg.CurrentContext]
	if !ok || ctx == nil {
		return "", fmt.Errorf("current context %q is missing", cfg.CurrentContext)
	}
	cluster, ok := cfg.Clusters[ctx.Cluster]
	if !ok || cluster == nil || cluster.Server == "" {
		return "", fmt.Errorf("cluster %q has no server URL", ctx.Cluster)
	}
	return cluster.Server, nil
}
