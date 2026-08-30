import { beforeEach, describe, expect, it, vi } from 'vitest';
import { apiFetch, openWorkload } from './client';
import { auth } from '$stores/auth';

// Matches client.ts: in Vitest/DEV the proxy base is /rpc.
const API_BASE = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '/rpc' : 'http://localhost:8090');

function mockFetch(response: Record<string, unknown>) {
  const fetchMock = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => ({}),
    ...response,
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('apiFetch', () => {
  beforeEach(() => {
    auth.logout();
  });

  it('sends the Connect protocol header so the backend accepts the call', async () => {
    const fetchMock = mockFetch({});
    await apiFetch('/idp.v1.HealthService/Check');

    const [, init] = fetchMock.mock.calls[0];
    const headers = init.headers as Headers;
    expect(headers.get('Connect-Protocol-Version')).toBe('1');
    expect(headers.get('Content-Type')).toBe('application/json');
    expect(init.method).toBe('POST');
  });

  it('omits Authorization when no token is stored', async () => {
    const fetchMock = mockFetch({});
    await apiFetch('/idp.v1.HealthService/Check');

    const [, init] = fetchMock.mock.calls[0];
    expect((init.headers as Headers).get('Authorization')).toBeNull();
  });

  // A 401 with no refresh token must clear the session; otherwise a dead
  // access token strands the user on a logged-in-looking UI.
  it('clears the session when the backend rejects the token and refresh fails', async () => {
    localStorage.setItem('idp_access_token', 'stale-token');
    localStorage.setItem(
      'idp_user',
      JSON.stringify({ id: 'u1', email: 'a@b.c', username: 'a', roles: ['admin'] }),
    );

    mockFetch({ ok: false, status: 401 });
    await apiFetch('/idp.v1.ProjectService/ListProjects');

    expect(localStorage.getItem('idp_access_token')).toBeNull();
    expect(localStorage.getItem('idp_user')).toBeNull();
  });
});

describe('openWorkload', () => {
  it('opens a stable Ingress URL directly without minting a ticket', async () => {
    const replace = vi.fn();
    vi.stubGlobal('open', vi.fn().mockReturnValue({ location: { replace }, close: vi.fn() }));
    const fetchMock = mockFetch({});

    await openWorkload('demo', 'web', 'http://web.demo.idp.local');

    expect(fetchMock).not.toHaveBeenCalled();
    expect(replace).toHaveBeenCalledWith('http://web.demo.idp.local');
  });

  it('mints a ticket and navigates the opened tab to the returned URL', async () => {
    const replace = vi.fn();
    vi.stubGlobal('open', vi.fn().mockReturnValue({ location: { replace }, close: vi.fn() }));
    const fetchMock = mockFetch({
      json: async () => ({ url: '/apps/demo/web?ticket=abc', expiresInSeconds: 60 }),
    });

    await openWorkload('demo', 'web');

    // The ticket endpoint is what carries the caller's credentials; the
    // redirect itself cannot.
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${API_BASE}/api/app-access/ticket`);
    expect(JSON.parse(init.body)).toEqual({ namespace: 'demo', name: 'web' });
    expect(replace).toHaveBeenCalledWith('http://localhost:8090/apps/demo/web?ticket=abc');
  });

  // Regression guard: the tab must be opened synchronously inside the click
  // handler, before the await, or popup blockers suppress it.
  it('opens the tab before awaiting the network call', async () => {
    const order: string[] = [];
    vi.stubGlobal(
      'open',
      vi.fn(() => {
        order.push('open');
        return { location: { replace: vi.fn() }, close: vi.fn() };
      }),
    );
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        order.push('fetch');
        return { ok: true, status: 200, json: async () => ({ url: '/apps/demo/web?ticket=x' }) };
      }),
    );

    await openWorkload('demo', 'web');
    expect(order).toEqual(['open', 'fetch']);
  });

  it('closes the blank tab and reports a friendly error when minting fails', async () => {
    const close = vi.fn();
    vi.stubGlobal('open', vi.fn().mockReturnValue({ location: { replace: vi.fn() }, close }));
    mockFetch({ ok: false, status: 502 });

    await expect(openWorkload('demo', 'web')).rejects.toThrow(/could not open this app/i);
    expect(close).toHaveBeenCalled();
  });

  it('surfaces an expired session distinctly from a failed app', async () => {
    vi.stubGlobal('open', vi.fn().mockReturnValue({ location: { replace: vi.fn() }, close: vi.fn() }));
    mockFetch({ ok: false, status: 401 });

    await expect(openWorkload('demo', 'web')).rejects.toThrow(/session expired/i);
  });
});
