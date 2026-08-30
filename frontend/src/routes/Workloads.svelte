<script lang="ts">
  import PageHeader from '$components/ui/PageHeader.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import ClusterHierarchy from '$components/ClusterHierarchy.svelte';
  import {
    getOverview,
    listNodes,
    listPods,
    listClusterNamespaces,
    type PodInfo,
  } from '$services/cluster';
  import { createQuery } from '@tanstack/svelte-query';
  import LogViewer from '$components/LogViewer.svelte';
  import Modal from '$components/ui/Modal.svelte';
  import {
    Boxes,
    RefreshCw,
    Terminal,
    CheckCircle2,
    AlertTriangle,
    Network,
    Table2,
  } from '@lucide/svelte';
  import { statusBadgeClass } from '$lib/status';

  // Empty string = all namespaces (cluster-wide inventory).
  let selectedNamespace = $state('');
  let activeLogPod = $state<PodInfo | null>(null);
  let activeLogContainer = $state('');
  let showLogModal = $state(false);
  let view = $state<'hierarchy' | 'table'>('hierarchy');

  const namespacesQuery = createQuery(() => ({
    queryKey: ['cluster-namespaces'],
    queryFn: listClusterNamespaces,
    refetchInterval: 15000,
  }));

  const podsQuery = createQuery(() => ({
    queryKey: ['pods', selectedNamespace || 'all'],
    queryFn: () => listPods(selectedNamespace),
    refetchInterval: 10000,
  }));

  // Nodes back the hierarchy's top level. Pods are always fetched for the
  // selected namespace, but the nodes they sit on are cluster-wide, so this
  // query is not keyed on the namespace filter.
  const nodesQuery = createQuery(() => ({
    queryKey: ['nodes'],
    queryFn: listNodes,
    refetchInterval: 15000,
  }));

  const overviewQuery = createQuery(() => ({
    queryKey: ['cluster-overview'],
    queryFn: getOverview,
    refetchInterval: 30000,
  }));

  function openPodLogs(namespace: string, podName: string, container?: string) {
    const pod = podsQuery.data?.find((p) => p.namespace === namespace && p.name === podName);
    if (!pod) return;
    activeLogPod = pod;
    activeLogContainer = container ?? '';
    showLogModal = true;
  }

  function refreshAll() {
    podsQuery.refetch();
    nodesQuery.refetch();
  }
</script>

<div class="page-stack">
  <PageHeader
    title="Workloads"
    description="Track Kubernetes Pods, container resource cycles, liveness states, and diagnostics."
  >

    <div class="flex flex-wrap items-center gap-3">
      <div class="inline-flex shrink-0 rounded-md border border-input bg-background p-0.5">
        <button
          type="button"
          onclick={() => (view = 'hierarchy')}
          aria-pressed={view === 'hierarchy'}
          class="inline-flex h-8 items-center gap-1.5 rounded px-2.5 text-xs font-medium {view ===
          'hierarchy'
            ? 'bg-accent text-foreground'
            : 'text-muted-foreground hover:text-foreground'}"
        >
          <Network class="h-3.5 w-3.5" />
          Hierarchy
        </button>
        <button
          type="button"
          onclick={() => (view = 'table')}
          aria-pressed={view === 'table'}
          class="inline-flex h-8 items-center gap-1.5 rounded px-2.5 text-xs font-medium {view ===
          'table'
            ? 'bg-accent text-foreground'
            : 'text-muted-foreground hover:text-foreground'}"
        >
          <Table2 class="h-3.5 w-3.5" />
          Table
        </button>
      </div>

      <div class="flex min-w-0 items-center gap-2">
        <span class="shrink-0 text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none"
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
        onclick={refreshAll}
        aria-label="Refresh workloads"
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="h-4 w-4" />
      </button>
    </div>
  </PageHeader>

  {#if podsQuery.isPending}
    <Skeleton variant="table" rows={8} />
  {:else if podsQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive font-medium">
          Error loading workloads: {podsQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !podsQuery.data || podsQuery.data.length === 0}
    <EmptyState
      icon={Boxes}
      title="No active workloads"
      description="Launch deployments to instantiate pods and configure schedules."
    />
  {:else if view === 'hierarchy'}
    <ClusterHierarchy
      clusterName={overviewQuery.data?.clusterName ?? 'cluster'}
      nodes={nodesQuery.data ?? []}
      pods={podsQuery.data}
      namespace={selectedNamespace}
      onOpenPodLogs={openPodLogs}
    />
  {:else}
    <DataTable minWidth="52rem">
        <thead>
          <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
            <th class="px-5 py-3">Pod Name</th>
            <th class="px-5 py-3">Namespace</th>
            <th class="px-5 py-3">IP Address</th>
            <th class="px-5 py-3">Node</th>
            <th class="px-5 py-3">Status</th>
            <th class="px-5 py-3">Restarts</th>
            <th class="px-5 py-3">Age</th>
            <th class="px-5 py-3 text-right">Logs</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border text-sm">
          {#each podsQuery.data as p}
            <tr class="hover:bg-accent/20 transition-colors">
              <td class="px-5 py-3.5 font-medium text-foreground max-w-[200px] truncate">
                {p.name}
              </td>
              <td class="px-5 py-3.5 text-xs text-muted-foreground">{p.namespace}</td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                {p.ip || '—'}
              </td>
              <td class="px-5 py-3.5 text-xs text-muted-foreground">
                {p.node || '—'}
              </td>
              <td class="px-5 py-3.5">
                <span
                  class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(p.status)}"
                >
                  {#if p.status === 'Running'}
                    <CheckCircle2 class="h-3 w-3" />
                  {:else}
                    <AlertTriangle class="h-3 w-3" />
                  {/if}
                  {p.status}
                </span>
              </td>
              <td class="px-5 py-3.5 font-medium">
                {p.restartCount}
              </td>
              <td class="px-5 py-3.5 text-muted-foreground text-xs">
                {new Date(p.createdAt).toLocaleDateString()}
              </td>
              <td class="px-5 py-3.5 text-right">
                <button
                  onclick={() => { activeLogPod = p; showLogModal = true; }}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                >
                  <Terminal class="h-3 w-3" />
                  View Logs
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
    </DataTable>
  {/if}
</div>

<!-- Logs Viewer Modal -->
<Modal
  open={showLogModal && !!activeLogPod}
  title={activeLogPod ? `Container logs — ${activeLogPod.namespace}/${activeLogPod.name}` : 'Container logs'}
  description="Streaming container output as the pod runs."
  size="xl"
  onclose={() => { showLogModal = false; activeLogPod = null; }}
>
  {#if activeLogPod}
    <!-- Keyed on namespace+pod so remount starts a fresh stream. Must use the
         pod's own namespace — selectedNamespace is "" when viewing all. -->
    {#key `${activeLogPod.namespace}/${activeLogPod.name}/${activeLogContainer}`}
      <LogViewer
        namespace={activeLogPod.namespace}
        podName={activeLogPod.name}
        pods={[activeLogPod.name]}
        container={activeLogContainer}
      />
    {/key}
  {/if}
</Modal>
