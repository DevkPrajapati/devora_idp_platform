<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { listNamespaces } from '$services/namespaces';
  import { listServices } from '$services/cluster';
  import { createQuery } from '@tanstack/svelte-query';
  import { Globe, RefreshCw } from '@lucide/svelte';

  // Empty string = all namespaces.
  let selectedNamespace = $state('');

  const namespacesQuery = createQuery(() => ({
    queryKey: ['namespaces'],
    queryFn: () => listNamespaces(1, 100),
  }));

  const servicesQuery = createQuery(() => ({
    queryKey: ['services', selectedNamespace || 'all'],
    queryFn: () => listServices(selectedNamespace),
    refetchInterval: 10000,
  }));
</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Services</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Exposed network endpoints, load balancers, and cluster routing configurations.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex items-center gap-2">
        <span class="text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
        >
          <option value="">All namespaces</option>
          {#if namespacesQuery.data}
            {#each namespacesQuery.data.namespaces as ns}
              <option value={ns.name}>{ns.displayName} ({ns.name})</option>
            {/each}
          {/if}
        </select>
      </div>

      <button
        onclick={() => servicesQuery.refetch()}
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="h-4 w-4" />
      </button>
    </div>
  </div>

  {#if servicesQuery.isPending}
    <div class="space-y-4">
      <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if servicesQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive font-medium">
          Error loading services: {servicesQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !servicesQuery.data || servicesQuery.data.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <Globe class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No services found</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Expose your deployments to generate network endpoints.
      </p>
    </div>
  {:else}
    <div class="border border-border rounded-lg bg-card overflow-x-auto">
      <table class="w-full min-w-[48rem] text-left border-collapse">
        <thead>
          <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
            <th class="px-5 py-3">Service Name</th>
            <th class="px-5 py-3">Namespace</th>
            <th class="px-5 py-3">Type</th>
            <th class="px-5 py-3">Cluster IP</th>
            <th class="px-5 py-3">External IP</th>
            <th class="px-5 py-3">Ports</th>
            <th class="px-5 py-3">Age</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border text-sm">
          {#each servicesQuery.data as s}
            <tr class="hover:bg-accent/20 transition-colors">
              <td class="px-5 py-3.5 font-medium text-foreground">
                {s.name}
              </td>
              <td class="px-5 py-3.5 text-xs text-muted-foreground">{s.namespace}</td>
              <td class="px-5 py-3.5 text-xs font-medium">
                <span class="inline-flex items-center rounded-md bg-secondary px-2 py-1">
                  {s.type}
                </span>
              </td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                {s.clusterIp}
              </td>
              <td class="px-5 py-3.5 font-mono text-xs">
                {s.externalIp || '—'}
              </td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                {s.ports.join(', ')}
              </td>
              <td class="px-5 py-3.5 text-muted-foreground text-xs">
                {new Date(s.createdAt).toLocaleDateString()}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
