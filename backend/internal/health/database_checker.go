package health

import (
	"context"

	"github.com/idp/platform/backend/internal/database"
	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

// DatabaseChecker verifies PostgreSQL connectivity.
type DatabaseChecker struct {
	pool *database.Pool
}

// NewDatabaseChecker creates a database health checker.
func NewDatabaseChecker(pool *database.Pool) *DatabaseChecker {
	return &DatabaseChecker{pool: pool}
}

// Name returns the checker component name.
func (c *DatabaseChecker) Name() string {
	return "database"
}

// Check pings the database and returns health status.
func (c *DatabaseChecker) Check(ctx context.Context) (idpv1.HealthStatus, string) {
	if err := c.pool.Ping(ctx); err != nil {
		return idpv1.HealthStatus_HEALTH_STATUS_UNHEALTHY, err.Error()
	}
	return idpv1.HealthStatus_HEALTH_STATUS_HEALTHY, "connected"
}
