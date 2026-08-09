import { apiFetch } from './client';
import type { HealthCheckResponse } from '$types/health';
import { HealthStatus } from '$types/health';

export async function checkHealth(): Promise<HealthCheckResponse> {
  const response = await apiFetch('/idp.v1.HealthService/Check', {});

  if (!response.ok) {
    throw new Error(`Health check failed: ${response.statusText}`);
  }

  const data = await response.json();

  return {
    status: data.status ?? HealthStatus.UNSPECIFIED,
    version: data.version ?? 'unknown',
    components: (data.components ?? []).map(
      (c: { name: string; status: HealthStatus; message: string }) => ({
        name: c.name,
        status: c.status,
        message: c.message,
      }),
    ),
  };
}
