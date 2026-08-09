<script lang="ts">
  import type { Component } from 'svelte';
  import AppLayout from '$layouts/AppLayout.svelte';
  import Login from '$routes/Login.svelte';
  import { queryClient } from '$lib/query-client';
  import { theme } from '$stores/theme';
  import { router } from '$stores/router';
  import { auth } from '$stores/auth';
  import { QueryClientProvider } from '@tanstack/svelte-query';
  import { onMount } from 'svelte';

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
    const loader = routeLoaders[path] ?? routeLoaders['/'];
    pageLoading = true;
    let cancelled = false;
    loader()
      .then((mod) => {
        if (!cancelled) {
          page = mod.default;
          pageLoading = false;
        }
      })
      .catch(() => {
        if (!cancelled) {
          page = null;
          pageLoading = false;
        }
      });
    return () => {
      cancelled = true;
    };
  });

  function handleLinkNavigation(e: MouseEvent) {
    const target = e.target as HTMLElement;
    const a = target.closest('a');
    if (a && a.href) {
      const url = new URL(a.href);
      if (url.origin === window.location.origin) {
        e.preventDefault();
        router.navigate(url.pathname as any);
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
          <div class="flex h-48 items-center justify-center text-sm text-muted-foreground">Loading page…</div>
        {:else if page}
          <svelte:component this={page} />
        {:else}
          <div class="p-8 text-sm text-destructive">Failed to load this page.</div>
        {/if}
      </AppLayout>
    {/if}
  </QueryClientProvider>
</div>
