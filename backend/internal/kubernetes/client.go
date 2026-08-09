package kubernetes

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/idp/platform/backend/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client wraps the Kubernetes clientset.
type Client struct {
	Clientset kubernetes.Interface
	Config    *rest.Config
	// Ingress holds the cluster's ingress conventions. It lives on the client
	// because every read that reports a workload's URL needs the domain and
	// scheme, and threading them through each call site would be noise.
	Ingress IngressConfig
}

// NewClient creates a Kubernetes client from configuration.
func NewClient(cfg config.KubernetesConfig) (*Client, error) {
	restConfig, err := buildConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return &Client{
		Clientset: clientset,
		Config:    restConfig,
		Ingress: IngressConfig{
			Enabled:       cfg.IngressEnabled,
			Domain:        cfg.IngressDomain,
			Class:         cfg.IngressClass,
			TLSSecretName: cfg.IngressTLSSecret,
		}.Normalize(),
	}, nil
}

func buildConfig(cfg config.KubernetesConfig) (*rest.Config, error) {
	if cfg.InCluster {
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		applyTransportTimeouts(restConfig, cfg)
		return restConfig, nil
	}

	kubeconfig := cfg.Kubeconfig
	if kubeconfig == "" {
		if env := os.Getenv("KUBECONFIG"); env != "" {
			kubeconfig = env
		} else if home := homedir.HomeDir(); home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}

	if kubeconfig != "" {
		if _, err := os.Stat(kubeconfig); err == nil {
			restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
			if err != nil {
				return nil, err
			}
			applyTransportTimeouts(restConfig, cfg)
			return restConfig, nil
		}
	}

	return nil, fmt.Errorf("no kubeconfig found; set KUBECONFIG or enable in-cluster config")
}

func applyTransportTimeouts(restConfig *rest.Config, cfg config.KubernetesConfig) {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	restConfig.Timeout = timeout

	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 3 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	restConfig.Dial = dialer.DialContext
	restConfig.QPS = 50
	restConfig.Burst = 100

	// TLSHandshakeTimeout lives on http.Transport, not rest.Config (client-go v0.36+).
	restConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if t, ok := rt.(*http.Transport); ok {
			clone := t.Clone()
			clone.TLSHandshakeTimeout = 3 * time.Second
			if clone.DialContext == nil {
				clone.DialContext = dialer.DialContext
			}
			return clone
		}
		return rt
	}
}
