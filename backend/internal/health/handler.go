package health

import (
	"context"
	"encoding/json"
	"net/http"

	"connectrpc.com/connect"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Probe paths for orchestrators. These exist alongside the Connect RPC because
// Kubernetes httpGet probes issue GET, while every Connect procedure is a POST —
// so the RPC cannot be used as a probe target at all.
const (
	LivenessPath  = "/healthz"
	ReadinessPath = "/readyz"
)

// Handler implements the Connect RPC HealthService.
type Handler struct {
	service *Service
}

// NewHandler creates a new health RPC handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Check handles health check requests.
func (h *Handler) Check(
	ctx context.Context,
	req *connect.Request[idpv1.HealthCheckRequest],
) (*connect.Response[idpv1.HealthCheckResponse], error) {
	resp := h.service.Check(ctx)
	return connect.NewResponse(resp), nil
}

// Liveness reports that the process is running and able to serve.
//
// It deliberately runs no dependency checks. A liveness probe that fails when
// the database is down gets the pod killed and restarted, which cannot fix a
// database outage and turns a degraded platform into a CrashLoopBackOff.
func (h *Handler) Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProbe(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": h.service.Version(),
		})
	})
}

// Readiness reports whether the process can serve real traffic right now.
//
// This one does check dependencies: an unhealthy component means requests will
// fail, so the pod should be pulled from the load balancer until it recovers.
// Degraded is deliberately treated as ready — partial function beats none.
func (h *Handler) Readiness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := h.service.Check(r.Context())

		status := http.StatusOK
		if result.Status == idpv1.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			status = http.StatusServiceUnavailable
		}

		components := make(map[string]string, len(result.Components))
		for _, component := range result.Components {
			components[component.Name] = component.Status.String()
		}

		writeProbe(w, status, map[string]any{
			"status":     result.Status.String(),
			"version":    result.Version,
			"components": components,
		})
	})
}

func writeProbe(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// Probe responses must never be cached; a cached "ok" outlives the
	// condition it described.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
