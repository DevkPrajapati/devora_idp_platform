package kubernetes

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/idp/platform/backend/internal/config"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// Client wraps the Kubernetes clientset.
//
// Bind swaps the underlying API connection so the cluster fleet can change
// which cluster every platform service talks to without reconstructing them.
// Reads of Clientset during a Bind can race; that window is an admin switch,
// not a request hot path.
type Client struct {
	mu sync.RWMutex

	Clientset kubernetes.Interface
	Config    *rest.Config
	// Name is the fleet name of the cluster this client currently drives.
	Name string
	// Ingress holds the cluster's ingress conventions. It lives on the client
	// because every read that reports a workload's URL needs the domain and
	// scheme, and threading them through each call site would be noise.
	Ingress IngressConfig

	cache liveCache
	// streamCS is a clientset with Timeout=0 so follow streams are not cut by
	// the short per-request deadline used for list/get calls.
	streamCS kubernetes.Interface

	// reachable is the last probe result. A Clientset can exist for a cluster
	// that has been stopped: kubeconfig is still on disk, so NewClient succeeds,
	// but every API call then waits for the full dial/request timeout. Serving
	// those as "available" is what made the UI hang on every page when minikube
	// was down. Available() requires this flag so reads fail immediately and
	// the fleet start path can still rebuild the client via Bound()+Ping().
	reachable atomic.Bool
}

// ErrNotConnected is returned when a call is made while no cluster is bound.
var ErrNotConnected = errors.New("kubernetes cluster not connected")

// Available reports whether the platform can serve live cluster reads.
//
// Bound-but-unreachable (a kubeconfig for a stopped cluster) returns false so
// list RPCs fail immediately instead of waiting out the transport timeout.
func (c *Client) Available() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Clientset != nil && c.reachable.Load()
}

// Bound reports whether a clientset is attached, including one whose last
// probe failed. Ping and reconnect use this; request handlers use Available.
func (c *Client) Bound() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Clientset != nil
}

// SetReachable records the outcome of a probe without swapping the clientset.
func (c *Client) SetReachable(ok bool) {
	if c == nil {
		return
	}
	c.reachable.Store(ok)
}

// cs returns the bound clientset, or ErrNotConnected.
//
// Every API call must take the clientset through here exactly once and then use
// the local copy. Reading the c.Clientset field repeatedly inside one call is a
// data race against Bind, and it panicked in production: an admin stopping a
// cluster, or the watcher dropping a dead one, nils the field between two reads
// of the same request and the second dereferences nil.
func (c *Client) cs() (kubernetes.Interface, error) {
	if c == nil {
		return nil, ErrNotConnected
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Clientset == nil {
		return nil, ErrNotConnected
	}
	return c.Clientset, nil
}

// restConfig returns the bound REST config, or ErrNotConnected. Needed by the
// callers that build their own round trippers (exec, port-forward).
func (c *Client) restConfig() (*rest.Config, error) {
	if c == nil {
		return nil, ErrNotConnected
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Config == nil {
		return nil, ErrNotConnected
	}
	return c.Config, nil
}

// Bind replaces the live API connection. Passing nil disconnects the platform
// from Kubernetes without dropping the Client pointer services already hold.
func (c *Client) Bind(src *Client) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.clear()
	c.streamCS = nil
	if src == nil {
		c.Clientset = nil
		c.Config = nil
		c.Name = ""
		c.reachable.Store(false)
		return
	}
	c.Clientset = src.Clientset
	c.Config = src.Config
	c.Name = src.Name
	c.Ingress = src.Ingress
	c.reachable.Store(true)
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

	c := &Client{
		Clientset: clientset,
		Config:    restConfig,
		Name:      currentContextName(cfg),
		Ingress: IngressConfig{
			Enabled:       cfg.IngressEnabled,
			Domain:        cfg.IngressDomain,
			Class:         cfg.IngressClass,
			TLSSecretName: cfg.IngressTLSSecret,
		}.Normalize(),
	}
	c.reachable.Store(true)
	return c, nil
}

// NewClientFromKubeconfig builds a client from a kubeconfig document. Used
// when activating a fleet cluster whose kubeconfig is stored encrypted.
func NewClientFromKubeconfig(data []byte, name string, cfg config.KubernetesConfig) (*Client, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("kubeconfig is empty")
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	applyTransportTimeouts(restConfig, cfg)

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	c := &Client{
		Clientset: clientset,
		Config:    restConfig,
		Name:      name,
		Ingress: IngressConfig{
			Enabled:       cfg.IngressEnabled,
			Domain:        cfg.IngressDomain,
			Class:         cfg.IngressClass,
			TLSSecretName: cfg.IngressTLSSecret,
		}.Normalize(),
	}
	c.reachable.Store(true)
	return c, nil
}

// KubeconfigPath resolves the kubeconfig the host tools read and write:
// the configured path, then $KUBECONFIG, then ~/.kube/config.
func KubeconfigPath(cfg config.KubernetesConfig) string {
	if cfg.Kubeconfig != "" {
		return cfg.Kubeconfig
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env
	}
	if home := homedir.HomeDir(); home != "" {
		return filepath.Join(home, ".kube", "config")
	}
	return ""
}

// MinikubeHome resolves minikube's state directory, where each profile keeps a
// config.json. Honours $MINIKUBE_HOME the same way minikube itself does.
func MinikubeHome() string {
	if env := os.Getenv("MINIKUBE_HOME"); env != "" {
		return filepath.Join(env, ".minikube")
	}
	if home := homedir.HomeDir(); home != "" {
		return filepath.Join(home, ".minikube")
	}
	return ""
}

// NewHostProvisioner builds a provisioner that can answer from the host
// filesystem instead of shelling out to kind and minikube for questions the
// files already answer.
func NewHostProvisioner(cfg config.KubernetesConfig) *Provisioner {
	return NewProvisioner().WithHostPaths(KubeconfigPath(cfg), MinikubeHome())
}

func currentContextName(cfg config.KubernetesConfig) string {
	kubeconfig := KubeconfigPath(cfg)
	if kubeconfig == "" {
		return "kubernetes"
	}
	apiCfg, err := clientcmd.LoadFromFile(kubeconfig)
	if err != nil || apiCfg.CurrentContext == "" {
		return "kubernetes"
	}
	return apiCfg.CurrentContext
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

	kubeconfig := KubeconfigPath(cfg)
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
		// Cluster-wide lists (pods across every namespace) routinely take
		// longer than 5s on a busy local cluster; timing them out is what
		// turned the dashboard into a wall of 503s.
		timeout = 15 * time.Second
	}
	restConfig.Timeout = timeout

	dialTimeout := cfg.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}
	restConfig.Dial = dialer.DialContext
	restConfig.QPS = 30
	restConfig.Burst = 60

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
