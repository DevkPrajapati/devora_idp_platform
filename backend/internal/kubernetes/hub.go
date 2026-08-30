package kubernetes

import (
	"sync"

	"github.com/idp/platform/backend/internal/config"
)

// Hub is the single Client pointer every platform service holds.
//
// Bind swaps the live Kubernetes API underneath that pointer so deployments,
// builds, logs, and the rest keep working after an admin activates another
// cluster — they never stored a stale clientset of their own.
type Hub struct {
	mu     sync.Mutex
	live   *Client
	k8sCfg config.KubernetesConfig
}

// NewHub wraps bootstrap (possibly nil / disconnected) as the live client.
func NewHub(bootstrap *Client, cfg config.KubernetesConfig) *Hub {
	live := bootstrap
	if live == nil {
		live = &Client{
			Ingress: IngressConfig{
				Enabled:       cfg.IngressEnabled,
				Domain:        cfg.IngressDomain,
				Class:         cfg.IngressClass,
				TLSSecretName: cfg.IngressTLSSecret,
			}.Normalize(),
		}
	}
	return &Hub{live: live, k8sCfg: cfg}
}

// Live is the Client services were constructed with. Never replaced as a
// pointer; only rebound.
func (h *Hub) Live() *Client {
	if h == nil {
		return nil
	}
	return h.live
}

// Config returns the platform Kubernetes settings used when building clients
// from stored kubeconfigs (timeouts, ingress domain).
func (h *Hub) Config() config.KubernetesConfig {
	if h == nil {
		return config.KubernetesConfig{}
	}
	return h.k8sCfg
}

// Bind attaches next as the live cluster. nil disconnects the platform.
func (h *Hub) Bind(next *Client) {
	if h == nil || h.live == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.live.Bind(next)
}
