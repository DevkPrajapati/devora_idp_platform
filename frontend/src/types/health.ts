export enum HealthStatus {
  UNSPECIFIED = 0,
  HEALTHY = 1,
  DEGRADED = 2,
  UNHEALTHY = 3,
}

export interface ComponentHealth {
  name: string;
  status: HealthStatus;
  message: string;
}

export interface HealthCheckResponse {
  status: HealthStatus;
  version: string;
  components: ComponentHealth[];
}

export function healthStatusLabel(status: HealthStatus): string {
  switch (status) {
    case HealthStatus.HEALTHY:
      return 'Healthy';
    case HealthStatus.DEGRADED:
      return 'Degraded';
    case HealthStatus.UNHEALTHY:
      return 'Unhealthy';
    default:
      return 'Unknown';
  }
}

export function healthStatusColor(status: HealthStatus): string {
  switch (status) {
    case HealthStatus.HEALTHY:
      return 'text-success';
    case HealthStatus.DEGRADED:
      return 'text-warning';
    case HealthStatus.UNHEALTHY:
      return 'text-destructive';
    default:
      return 'text-muted-foreground';
  }
}
