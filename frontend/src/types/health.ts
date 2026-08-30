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
      return 'bg-emerald-500/15 text-emerald-500';
    case HealthStatus.DEGRADED:
      return 'bg-amber-500/15 text-amber-500';
    case HealthStatus.UNHEALTHY:
      return 'bg-red-500/15 text-red-500';
    default:
      return 'bg-muted text-muted-foreground';
  }
}
