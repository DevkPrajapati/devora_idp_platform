package health

import (
	"context"

	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// Checker defines the interface for individual component health checks.
type Checker interface {
	Name() string
	Check(ctx context.Context) (idpv1.HealthStatus, string)
}

// Service implements health check business logic.
type Service struct {
	version  string
	checkers []Checker
}

// NewService creates a new health service with the given checkers.
func NewService(version string, checkers ...Checker) *Service {
	return &Service{
		version:  version,
		checkers: checkers,
	}
}

// Version returns the running build version, for probes that report it without
// running a full dependency check.
func (s *Service) Version() string {
	return s.version
}

// Check performs health checks on all registered components.
func (s *Service) Check(ctx context.Context) *idpv1.HealthCheckResponse {
	components := make([]*idpv1.ComponentHealth, 0, len(s.checkers))
	overallStatus := idpv1.HealthStatus_HEALTH_STATUS_HEALTHY

	for _, checker := range s.checkers {
		status, message := checker.Check(ctx)
		components = append(components, &idpv1.ComponentHealth{
			Name:    checker.Name(),
			Status:  status,
			Message: message,
		})

		if status == idpv1.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			overallStatus = idpv1.HealthStatus_HEALTH_STATUS_UNHEALTHY
		} else if status == idpv1.HealthStatus_HEALTH_STATUS_DEGRADED &&
			overallStatus != idpv1.HealthStatus_HEALTH_STATUS_UNHEALTHY {
			overallStatus = idpv1.HealthStatus_HEALTH_STATUS_DEGRADED
		}
	}

	return &idpv1.HealthCheckResponse{
		Status:     overallStatus,
		Version:    s.version,
		Components: components,
	}
}
