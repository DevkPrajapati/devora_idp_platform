<script lang="ts">
  import PageHeader from '$components/ui/PageHeader.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import { listServices, listClusterNamespaces } from '$services/cluster';
  import { createQuery } from '@tanstack/svelte-query';
  import { Globe, RefreshCw } from '@lucide/svelte';

  // Empty string = all namespaces.
  let selectedNamespace = $state('');

  const namespacesQuery = createQuery(() => ({
    queryKey: ['cluster-namespaces'],
    queryFn: listClusterNamespaces,
    refetchInterval: 20000,
  }));

  const servicesQuery = createQuery(() => ({
    queryKey: ['services', selectedNamespace || 'all'],
    queryFn: () => listServices(selectedNamespace),
    refetchInterval: 15000,
  }));
</script>

<div class="page-stack">
  <PageHeader
    title="Services"
    description="Exposed network endpoints, load balancers, and cluster routing configurations."
  >

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex items-center gap-2">
        <span class="text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
        >
          <option value="">All namespaces</option>
          {#if namespacesQuery.data}
            {#each namespacesQuery.data as ns}
              <option value={ns.name}>{ns.name}</option>
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
  </PageHeader>

  {#if servicesQuery.isPending}
    <Skeleton variant="table" rows={8} />
  {:else if servicesQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive font-medium">
          Error loading services: {servicesQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !servicesQuery.data || servicesQuery.data.length === 0}
    <EmptyState
      icon={Globe}
      title="No services found"
      description="Expose your deployments to generate network endpoints."
    />
  {:else}
    <DataTable minWidth="48rem">
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
    </DataTable>
  {/if}
</div>
