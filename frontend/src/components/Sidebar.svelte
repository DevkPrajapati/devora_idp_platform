<script lang="ts">
  import { cn } from '$lib/utils';
  import { router, type Route } from '$stores/router';
  import { sidebarOpen } from '$stores/ui';
  import { getOverview } from '$services/cluster';
  import { createQuery } from '@tanstack/svelte-query';
  import {
    Boxes,
    Container,
    Cylinder,
    FolderKanban,
    GitBranch,
    Globe,
    HardDrive,
    KeyRound,
    LayoutDashboard,
    Layers,
    ScrollText,
    Server,
    Settings,
    Shield,
    X,
  } from '@lucide/svelte';

  const overviewQuery = createQuery(() => ({
    queryKey: ['cluster-overview'],
    queryFn: getOverview,
    refetchInterval: 30000,
  }));

  interface NavItem {
    label: string;
    // Typed as Route so a nav entry pointing at a path the router does not
    // know fails at build time instead of silently falling back to Dashboard.
    href: Route;
    icon: typeof LayoutDashboard;
  }

  const navItems: NavItem[] = [
    { label: 'Dashboard', href: '/', icon: LayoutDashboard },
    { label: 'Projects', href: '/projects', icon: FolderKanban },
    { label: 'Namespaces', href: '/namespaces', icon: Layers },
    { label: 'Deployments', href: '/deployments', icon: Container },
    { label: 'Registry', href: '/registry', icon: KeyRound },
    { label: 'Builds', href: '/builds', icon: GitBranch },
    { label: 'Services', href: '/services', icon: Globe },
    { label: 'Workloads', href: '/workloads', icon: Boxes },
    { label: 'Databases', href: '/databases', icon: Cylinder },
    { label: 'Storage', href: '/storage', icon: HardDrive },
    { label: 'Monitoring', href: '/monitoring', icon: Server },
    { label: 'Audit Log', href: '/audit', icon: ScrollText },
    { label: 'RBAC', href: '/rbac', icon: Shield },
    { label: 'Settings', href: '/settings', icon: Settings },
  ];

  function navigate(e: MouseEvent, href: Route) {
    e.preventDefault();
    router.navigate(href);
  }
</script>

<!--
  Below `lg` this is a fixed off-canvas drawer translated out of view; at `lg`
  and above the transform is reset and it becomes a static column, so the same
  markup serves both without duplicating the nav list.
-->
<aside
  id="app-sidebar"
  aria-label="Main navigation"
  class={cn(
    'fixed inset-y-0 left-0 z-50 flex w-64 max-w-[85vw] flex-col border-r border-border bg-card',
    'transition-transform duration-200 ease-out motion-reduce:transition-none',
    'lg:static lg:z-auto lg:w-60 lg:max-w-none lg:translate-x-0',
    $sidebarOpen ? 'translate-x-0 shadow-2xl' : '-translate-x-full',
  )}
>
  <div class="flex h-14 shrink-0 items-center gap-2 border-b border-border px-4 lg:px-5">
    <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-primary">
      <Server class="h-4 w-4 text-primary-foreground" />
    </div>
    <div class="min-w-0 flex-1">
      <p class="truncate text-sm font-semibold leading-none">IDP Platform</p>
      <p class="truncate text-xs text-muted-foreground">Developer Console</p>
    </div>
    <button
      type="button"
      onclick={() => sidebarOpen.close()}
      aria-label="Close navigation"
      class="-mr-1 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground lg:hidden"
    >
      <X class="h-4 w-4" />
    </button>
  </div>

  <nav class="min-h-0 flex-1 space-y-1 overflow-y-auto p-3">
    {#each navItems as item (item.href)}
      <a
        href={item.href}
        onclick={(e) => navigate(e, item.href)}
        aria-current={$router === item.href ? 'page' : undefined}
        class={cn(
          'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
          $router === item.href
            ? 'bg-primary/10 text-primary'
            : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground',
        )}
      >
        <item.icon class="h-4 w-4 shrink-0" />
        <span class="truncate">{item.label}</span>
      </a>
    {/each}
  </nav>

  <div class="shrink-0 border-t border-border p-4">
    <div class="rounded-lg bg-muted/50 p-3">
      <p class="text-xs font-medium">Cluster</p>
      <p class="mt-0.5 truncate text-xs text-muted-foreground">
        {overviewQuery.data?.clusterName ?? 'disconnected'}
      </p>
    </div>
  </div>
</aside>
