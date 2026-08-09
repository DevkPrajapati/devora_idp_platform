import { writable, get } from 'svelte/store';

export interface AuthUser {
  id: string;
  email: string;
  username: string;
  roles: string[];
}

interface AuthState {
  user: AuthUser | null;
  token: string | null;
  loading: boolean;
  error: string | null;
}

const KEYCLOAK_URL = import.meta.env.VITE_KEYCLOAK_URL ?? 'http://localhost:8080';
const KEYCLOAK_REALM = import.meta.env.VITE_KEYCLOAK_REALM ?? 'idp';
const KEYCLOAK_CLIENT = import.meta.env.VITE_KEYCLOAK_CLIENT ?? 'idp-frontend';
const AUTH_ENABLED = import.meta.env.VITE_AUTH_ENABLED !== 'false';

const TOKEN_KEY = 'idp_access_token';
const REFRESH_KEY = 'idp_refresh_token';
const USER_KEY = 'idp_user';
const LOGGED_OUT_KEY = 'idp_logged_out';

/** Refresh a minute before expiry so a slow request is not racing the clock. */
const REFRESH_SKEW_MS = 60_000;

function loadStored(): Pick<AuthState, 'token' | 'user'> {
  if (typeof window === 'undefined') return { token: null, user: null };
  try {
    const token = localStorage.getItem(TOKEN_KEY);
    const userRaw = localStorage.getItem(USER_KEY);
    const user = userRaw ? (JSON.parse(userRaw) as AuthUser) : null;
    return { token, user };
  } catch {
    return { token: null, user: null };
  }
}

function tokenExpiryMs(token: string): number | null {
  try {
    const payload = JSON.parse(atob(token.split('.')[1]));
    if (typeof payload.exp !== 'number') return null;
    return payload.exp * 1000;
  } catch {
    return null;
  }
}

function createAuthStore() {
  const stored = loadStored();
  const { subscribe, set, update } = writable<AuthState>({
    user: stored.user,
    token: stored.token,
    loading: false,
    error: null,
  });

  let refreshInFlight: Promise<boolean> | null = null;

  function persist(token: string | null, user: AuthUser | null, refreshToken?: string | null) {
    if (typeof window === 'undefined') return;
    if (token) {
      localStorage.setItem(TOKEN_KEY, token);
    } else {
      localStorage.removeItem(TOKEN_KEY);
    }
    if (refreshToken !== undefined) {
      if (refreshToken) {
        localStorage.setItem(REFRESH_KEY, refreshToken);
      } else {
        localStorage.removeItem(REFRESH_KEY);
      }
    }
    if (user) {
      localStorage.setItem(USER_KEY, JSON.stringify(user));
    } else {
      localStorage.removeItem(USER_KEY);
    }
  }

  function applyTokens(accessToken: string, refreshToken: string | undefined) {
    const payload = JSON.parse(atob(accessToken.split('.')[1]));
    const existing = get({ subscribe }).user;
    const user: AuthUser = {
      id: payload.sub ?? existing?.id ?? '',
      email: payload.email ?? existing?.email ?? '',
      username: payload.preferred_username ?? existing?.username ?? '',
      roles: extractRoles(payload),
    };
    persist(accessToken, user, refreshToken === undefined ? undefined : refreshToken || null);
    set({ user, token: accessToken, loading: false, error: null });
  }

  return {
    subscribe,
    isEnabled: () => AUTH_ENABLED,

    getToken(): string | null {
      return get({ subscribe }).token;
    },

    getUser(): AuthUser | null {
      return get({ subscribe }).user;
    },

    isAuthenticated(): boolean {
      const state = get({ subscribe });
      if (!AUTH_ENABLED) return !!state.user;
      return !!state.token && !!state.user;
    },

    loginDev() {
      if (AUTH_ENABLED) return;
      if (typeof window !== 'undefined') {
        sessionStorage.removeItem(LOGGED_OUT_KEY);
      }
      const devUser: AuthUser = {
        id: 'dev-user',
        email: 'dev@idp.local',
        username: 'dev',
        roles: ['admin'],
      };
      set({ user: devUser, token: null, loading: false, error: null });
    },

    async init() {
      if (!AUTH_ENABLED) {
        if (typeof window !== 'undefined' && sessionStorage.getItem(LOGGED_OUT_KEY) === 'true') {
          set({ user: null, token: null, loading: false, error: null });
          return;
        }
        this.loginDev();
        return;
      }

      const { token, user } = loadStored();
      if (token && user) {
        set({ user, token, loading: false, error: null });
        // Refresh in the background if the access token is already near expiry.
        void this.ensureFreshToken();
      } else {
        set({ user: null, token: null, loading: false, error: null });
      }
    },

    async login(username: string, password: string): Promise<void> {
      update((s) => ({ ...s, loading: true, error: null }));

      try {
        // Trailing spaces from paste/autofill make Keycloak reject the grant.
        const trimmedUser = username.trim();
        const trimmedPass = password.trim();
        const tokenUrl = `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`;
        const params = new URLSearchParams({
          grant_type: 'password',
          client_id: KEYCLOAK_CLIENT,
          username: trimmedUser,
          password: trimmedPass,
        });

        const response = await fetch(tokenUrl, {
          method: 'POST',
          headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
          body: params.toString(),
        });

        if (!response.ok) {
          const err = (await response.json().catch(() => ({}))) as {
            error?: string;
            error_description?: string;
          };
          throw new Error(friendlyLoginError(err.error_description || err.error));
        }

        const data = await response.json();
        const token = data.access_token as string;
        const refresh = (data.refresh_token as string | undefined) ?? '';

        applyTokens(token, refresh);

        // Platform RPCs require a realm role. A Keycloak user without
        // admin/developer/viewer can obtain a token but every API call fails —
        // reject that at login with a clear message.
        const roles = get({ subscribe }).user?.roles ?? [];
        const hasPlatformRole = roles.some((r) =>
          r === 'admin' || r === 'developer' || r === 'viewer',
        );
        if (!hasPlatformRole) {
          persist(null, null, null);
          set({ user: null, token: null, loading: false, error: null });
          throw new Error(
            'Your Keycloak account has no platform role (admin, developer, or viewer). Ask an admin to assign one.',
          );
        }

        if (typeof window !== 'undefined') {
          sessionStorage.removeItem(LOGGED_OUT_KEY);
        }
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : 'Login failed';
        persist(null, null, null);
        set({ user: null, token: null, loading: false, error: message });
        throw err;
      }
    },

    /**
     * Exchanges the stored refresh token for a new access token.
     * Returns false when there is nothing to refresh or Keycloak rejects it.
     */
    async refreshAccessToken(): Promise<boolean> {
      if (!AUTH_ENABLED || typeof window === 'undefined') return false;
      const refresh = localStorage.getItem(REFRESH_KEY);
      if (!refresh) return false;

      if (refreshInFlight) return refreshInFlight;

      refreshInFlight = (async () => {
        try {
          const tokenUrl = `${KEYCLOAK_URL}/realms/${KEYCLOAK_REALM}/protocol/openid-connect/token`;
          const params = new URLSearchParams({
            grant_type: 'refresh_token',
            client_id: KEYCLOAK_CLIENT,
            refresh_token: refresh,
          });
          const response = await fetch(tokenUrl, {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: params.toString(),
          });
          if (!response.ok) return false;
          const data = await response.json();
          const access = data.access_token as string | undefined;
          if (!access) return false;
          applyTokens(access, (data.refresh_token as string | undefined) ?? refresh);
          return true;
        } catch {
          return false;
        } finally {
          refreshInFlight = null;
        }
      })();

      return refreshInFlight;
    },

    /** Ensures the access token is usable for the next API call. */
    async ensureFreshToken(): Promise<string | null> {
      if (!AUTH_ENABLED) return null;
      const token = get({ subscribe }).token;
      if (!token) return null;
      const exp = tokenExpiryMs(token);
      if (exp !== null && Date.now() < exp - REFRESH_SKEW_MS) {
        return token;
      }
      const ok = await this.refreshAccessToken();
      return ok ? get({ subscribe }).token : null;
    },

    logout() {
      persist(null, null, null);
      if (typeof window !== 'undefined') {
        sessionStorage.setItem(LOGGED_OUT_KEY, 'true');
      }
      set({ user: null, token: null, loading: false, error: null });
    },

    hasRole(role: string): boolean {
      const user = get({ subscribe }).user;
      if (!user) return false;
      return user.roles.includes(role);
    },

    canWrite(): boolean {
      return this.hasRole('admin') || this.hasRole('developer');
    },
  };
}

function extractRoles(payload: Record<string, unknown>): string[] {
  const roles = new Set<string>();
  const realmAccess = payload.realm_access as { roles?: string[] } | undefined;
  realmAccess?.roles?.forEach((r) => roles.add(r));
  const resourceAccess = payload.resource_access as Record<string, { roles?: string[] }> | undefined;
  if (resourceAccess) {
    for (const key of Object.keys(resourceAccess)) {
      resourceAccess[key]?.roles?.forEach((r) => roles.add(r));
    }
  }
  return Array.from(roles);
}

/** Map Keycloak token-endpoint errors to concise UI copy. */
function friendlyLoginError(raw?: string): string {
  const msg = (raw || '').trim();
  const lower = msg.toLowerCase();
  if (!msg) return 'Invalid credentials';
  if (lower.includes('not fully set up') || lower.includes('account is not fully')) {
    return 'Account setup is incomplete. Ask an admin to finish your Keycloak profile and assign a platform role.';
  }
  if (lower.includes('invalid user credentials') || lower === 'invalid_grant') {
    return 'Invalid username or password.';
  }
  return msg;
}

export const auth = createAuthStore();
