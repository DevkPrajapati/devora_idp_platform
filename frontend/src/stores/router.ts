import { writable } from 'svelte/store';

export type Route =
  | '/'
  | '/projects'
  | '/namespaces'
  | '/deployments'
  | '/registry'
  | '/builds'
  | '/services'
  | '/workloads'
  | '/databases'
  | '/storage'
  | '/monitoring'
  | '/audit'
  | '/rbac'
  | '/settings';

const ROUTES: readonly Route[] = [
  '/',
  '/projects',
  '/namespaces',
  '/deployments',
  '/registry',
  '/builds',
  '/services',
  '/workloads',
  '/databases',
  '/storage',
  '/monitoring',
  '/audit',
  '/rbac',
  '/settings',
];

function isRoute(path: string): path is Route {
  return (ROUTES as readonly string[]).includes(path);
}

function createRouterStore() {
  const getPath = (): Route => {
    if (typeof window === 'undefined') return '/';
    const path = window.location.pathname;
    return isRoute(path) ? path : '/';
  };

  const { subscribe, set } = writable<Route>(getPath());

  if (typeof window !== 'undefined') {
    window.addEventListener('popstate', () => {
      set(getPath());
    });
  }

  return {
    subscribe,
    navigate: (path: Route, query?: Record<string, string>) => {
      if (typeof window !== 'undefined') {
        const params = query ? new URLSearchParams(query) : null;
        const url = params && [...params.keys()].length > 0 ? `${path}?${params}` : path;
        window.history.pushState(null, '', url);
        set(path);
      }
    },
  };
}

export const router = createRouterStore();
