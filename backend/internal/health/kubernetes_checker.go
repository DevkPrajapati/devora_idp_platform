package health

import (
	"context"
	"time"

	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
	"github.com/idp/platform/backend/internal/kubernetes"
)

// KubernetesChecker verifies Kubernetes cluster connectivity.
type KubernetesChecker struct {
	client *kubernetes.Client
}

// NewKubernetesChecker creates a checker for the Kubernetes cluster.
func NewKubernetesChecker(client *kubernetes.Client) *KubernetesChecker {
	return &KubernetesChecker{client: client}
}

func (k *KubernetesChecker) Name() string {
	return "kubernetes"
}

func (k *KubernetesChecker) Check(ctx context.Context) (idpv1.HealthStatus, string) {
	if !k.client.Available() {
		return idpv1.HealthStatus_HEALTH_STATUS_DEGRADED, "cluster not configured"
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := k.client.Ping(pingCtx); err != nil {
		return idpv1.HealthStatus_HEALTH_STATUS_DEGRADED, kubernetes.HumanizeClusterError(err.Error())
	}
	return idpv1.HealthStatus_HEALTH_STATUS_HEALTHY, "connected"
}
