import { auth } from '$stores/auth';

const API_BASE = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '/rpc' : 'http://localhost:8090');

/** Backend origin used for /apps/ so the tab lands on *.localhost:8090, not 127.0.0.1:18xxx. */
export const APP_ACCESS_ORIGIN =
  import.meta.env.VITE_APP_ACCESS_ORIGIN ?? (import.meta.env.DEV ? 'http://localhost:8090' : '');

export async function apiFetch(procedure: string, body: any = {}, options: RequestInit = {}): Promise<Response> {
  const headers = new Headers(options.headers || {});
  headers.set('Content-Type', 'application/json');
  headers.set('Connect-Protocol-Version', '1');

  if (auth.isEnabled()) {
    const token = (await auth.ensureFreshToken()) ?? auth.getToken();
    if (token) {
      headers.set('Authorization', `Bearer ${token}`);
    }
  }

  const response = await fetch(`${API_BASE}${procedure}`, {
    method: 'POST',
    ...options,
    headers,
    body: JSON.stringify(body),
  });

  // Access tokens expire; try one silent refresh before forcing a login.
  if (response.status === 401 && auth.isEnabled()) {
    const refreshed = await auth.refreshAccessToken();
    if (refreshed) {
      const retryHeaders = new Headers(options.headers || {});
      retryHeaders.set('Content-Type', 'application/json');
      retryHeaders.set('Connect-Protocol-Version', '1');
      const token = auth.getToken();
      if (token) retryHeaders.set('Authorization', `Bearer ${token}`);
      return fetch(`${API_BASE}${procedure}`, {
        method: 'POST',
        ...options,
        headers: retryHeaders,
        body: JSON.stringify(body),
      });
    }
    auth.logout();
  }

  return response;
}

/**
 * Opens a running workload in a new tab.
 *
 * Prefer a stable Ingress URL when provided; otherwise use a sticky localhost
 * port-forward. Ingress hostnames need /etc/hosts + minikube tunnel on Docker.
 *
 * The tab is opened synchronously, before the await, because popup blockers
 * only trust a window.open call made directly inside a click handler.
 */
export async function openWorkload(
  namespace: string,
  name: string,
  stableUrl?: string,
): Promise<void> {
  const tab = window.open('', '_blank', 'noopener,noreferrer');

  try {
    if (stableUrl) {
      if (tab) {
        tab.location.replace(stableUrl);
      } else {
        window.location.assign(stableUrl);
      }
      return;
    }

    const response = await apiFetch('/api/app-access/ticket', { namespace, name });
    if (!response.ok) {
      throw new Error(
        response.status === 401
          ? 'Your session expired. Sign in again to open this app.'
          : 'Could not open this app. It may not be running yet.',
      );
    }

    const { url } = (await response.json()) as { url: string };
    const target = APP_ACCESS_ORIGIN ? `${APP_ACCESS_ORIGIN}${url}` : `${API_BASE}${url}`;

    if (tab) {
      tab.location.replace(target);
    } else {
      // Popup blocked — fall back to the current tab rather than silently
      // doing nothing.
      window.location.assign(target);
    }
  } catch (error) {
    tab?.close();
    throw error;
  }
}
