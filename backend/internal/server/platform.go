package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/idp/platform/backend/internal/auth"
	"github.com/idp/platform/backend/internal/config"
	"github.com/idp/platform/backend/internal/kubernetes"
	"github.com/idp/platform/backend/internal/metrics"
)

// PlatformPath serves the platform's own configuration and a rolling window of
// cluster resource usage.
//
// Both previously lived as literals in the frontend: Settings.svelte hardcoded
// a Kubernetes version and an app version that disagreed with the running
// build, and Monitoring.svelte drew trend lines from three constant arrays. A
// console that displays invented numbers is worse than one that displays none,
// because nobody can tell which panels are real.
const PlatformPath = "/api/platform"

type platformResponse struct {
	Version     string `json:"version"`
	Environment string `json:"environment"`
	AuthEnabled bool   `json:"authEnabled"`
	// AuthIssuer is the Keycloak realm actually in use, not a guess.
	AuthIssuer string `json:"authIssuer"`

	ClusterConnected  bool   `json:"clusterConnected"`
	KubernetesVersion string `json:"kubernetesVersion"`

	IngressEnabled bool   `json:"ingressEnabled"`
	IngressDomain  string `json:"ingressDomain"`
	BuildsEnabled  bool   `json:"buildsEnabled"`

	// History is oldest-first and may be empty when the process has just
	// started or no cluster is connected.
	History []metrics.Sample `json:"history"`
	// SampleIntervalSeconds lets the chart label its x-axis without assuming
	// a cadence the backend might change.
	SampleIntervalSeconds int `json:"sampleIntervalSeconds"`
}

// platformHandler returns platform configuration and metrics history.
//
// Authenticated: the response names the auth issuer, ingress domain, and
// cluster version, which together describe the deployment's attack surface and
// are not worth handing to anonymous callers.
func platformHandler(
	cfg *config.Config,
	validator *auth.Validator,
	k8sClient *kubernetes.Client,
	history *metrics.History,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if _, err := authenticateRequest(r, validator); err != nil {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		resp := platformResponse{
			Version:               cfg.App.Version,
			Environment:           cfg.App.Env,
			AuthEnabled:           cfg.Auth.Enabled,
			AuthIssuer:            cfg.Auth.Issuer,
			IngressEnabled:        cfg.Kubernetes.IngressEnabled,
			IngressDomain:         cfg.Kubernetes.IngressDomain,
			BuildsEnabled:         cfg.Build.Enabled && k8sClient.Available(),
			ClusterConnected:      k8sClient.Available() && clusterReachable(k8sClient),
			History:               history.Snapshot(),
			SampleIntervalSeconds: int(metrics.DefaultInterval.Seconds()),
		}

		// Queried live rather than cached: an operator upgrading the cluster
		// should see the new version without restarting the platform. A failed
		// lookup leaves the field empty so the UI can say "unknown" instead of
		// showing a stale value.
		if k8sClient.Available() {
			if v, err := k8sClient.Clientset.Discovery().ServerVersion(); err == nil {
				resp.KubernetesVersion = v.GitVersion
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
	})
}

func clusterReachable(client *kubernetes.Client) bool {
	if !client.Available() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.Ping(ctx) == nil
}
