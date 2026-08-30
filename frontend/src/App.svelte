<script lang="ts">
  import type { Component } from 'svelte';
  import AppLayout from '$layouts/AppLayout.svelte';
  import Login from '$routes/Login.svelte';
  import { queryClient } from '$lib/query-client';
  import { theme } from '$stores/theme';
  import { router, type Route } from '$stores/router';
  import { auth } from '$stores/auth';
  import { QueryClientProvider } from '@tanstack/svelte-query';
  import { onMount } from 'svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';

  const routeLoaders: Record<string, () => Promise<{ default: Component }>> = {
    '/': () => import('$routes/Dashboard.svelte'),
    '/projects': () => import('$routes/Projects.svelte'),
    '/namespaces': () => import('$routes/Namespaces.svelte'),
    '/deployments': () => import('$routes/Deployments.svelte'),
    '/registry': () => import('$routes/Registry.svelte'),
    '/builds': () => import('$routes/Builds.svelte'),
    '/services': () => import('$routes/Services.svelte'),
    '/workloads': () => import('$routes/Workloads.svelte'),
    '/databases': () => import('$routes/Databases.svelte'),
    '/storage': () => import('$routes/Storage.svelte'),
    '/monitoring': () => import('$routes/Monitoring.svelte'),
    '/audit': () => import('$routes/AuditLog.svelte'),
    '/rbac': () => import('$routes/Rbac.svelte'),
    '/clusters': () => import('$routes/Clusters.svelte'),
    '/settings': () => import('$routes/Settings.svelte'),
  };

  let page = $state<Component | null>(null);
  let pageLoading = $state(true);

  onMount(() => {
    theme.init();
    auth.init();
  });

  $effect(() => {
    const path = $router;
    let active = true;
    pageLoading = true;

    void (async () => {
      try {
        const loader = routeLoaders[path] ?? routeLoaders['/'];
        const mod = await loader();
        if (!active) return;
        page = mod.default;
      } catch {
        if (!active) return;
        page = null;
      } finally {
        if (active) pageLoading = false;
      }
    })();

    return () => {
      active = false;
    };
  });

  function handleLinkNavigation(e: MouseEvent) {
    const target = e.target as HTMLElement;
    const a = target.closest('a');
    if (a && a.href) {
      const url = new URL(a.href);
      if (url.origin === window.location.origin) {
        e.preventDefault();
        const nextPath = url.pathname as Route;
        const query = Object.fromEntries(url.searchParams.entries());
        router.navigate(nextPath, Object.keys(query).length > 0 ? query : undefined);
      }
    }
  }

  let isAuthenticated = $derived(
    auth.isEnabled() ? !!($auth.token && $auth.user) : !!$auth.user,
  );
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div onclick={handleLinkNavigation} class="h-full">
  <QueryClientProvider client={queryClient}>
    {#if !isAuthenticated}
      <Login />
    {:else}
      <AppLayout>
        {#if pageLoading}
          <Skeleton variant="page" />
        {:else if page}
          {@const Page = page}
          <Page />
        {:else}
          <div class="p-8 text-sm text-destructive">Failed to load this page.</div>
        {/if}
      </AppLayout>
    {/if}
  </QueryClientProvider>
</div>
