import { auth } from '$stores/auth';

const API_BASE = import.meta.env.VITE_API_URL ?? 'http://localhost:8090';

/** One point on the resource-usage trend, as recorded by the backend sampler. */
export interface MetricsSample {
  /** Unix seconds. */
  t: number;
  /** Whole percentages, 0-100. */
  cpu: number;
  mem: number;
}

export interface PlatformInfo {
  version: string;
  environment: string;
  authEnabled: boolean;
  authIssuer: string;
  clusterConnected: boolean;
  /** Empty when the cluster is unreachable — render "unknown", not a guess. */
  kubernetesVersion: string;
  ingressEnabled: boolean;
  ingressDomain: string;
  buildsEnabled: boolean;
  history: MetricsSample[];
  sampleIntervalSeconds: number;
}

/**
 * Reads live platform configuration and cluster usage history.
 *
 * This replaces values that used to be hardcoded in the UI — an app version, a
 * Kubernetes version, and three constant arrays standing in for trend data.
 * Anything the backend cannot supply is now shown as unknown rather than
 * invented.
 *
 * A plain GET rather than apiFetch, which posts Connect RPC envelopes.
 */
export async function getPlatformInfo(): Promise<PlatformInfo> {
  const headers = new Headers();
  if (auth.isEnabled()) {
    const token = auth.getToken();
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
  }

  const response = await fetch(`${API_BASE}/api/platform`, { headers });

  if (response.status === 401 && auth.isEnabled()) {
    auth.logout();
  }
  if (!response.ok) {
    throw new Error(`Failed to load platform info: ${response.statusText}`);
  }

  return response.json();
}
