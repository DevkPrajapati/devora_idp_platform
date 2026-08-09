<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { listNamespaces } from '$services/namespaces';
  import { listPods, type PodInfo } from '$services/cluster';
  import { createQuery } from '@tanstack/svelte-query';
  import LogViewer from '$components/LogViewer.svelte';
  import { Boxes, RefreshCw, AlertCircle, Terminal, X, CheckCircle2, AlertTriangle } from '@lucide/svelte';

  // Empty string = all namespaces (cluster-wide inventory).
  let selectedNamespace = $state('');
  let activeLogPod = $state<PodInfo | null>(null);
  let showLogModal = $state(false);

  const namespacesQuery = createQuery(() => ({
    queryKey: ['namespaces'],
    queryFn: () => listNamespaces(1, 100),
  }));

  const podsQuery = createQuery(() => ({
    queryKey: ['pods', selectedNamespace || 'all'],
    queryFn: () => listPods(selectedNamespace),
    refetchInterval: 5000,
  }));

</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Workloads</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Track Kubernetes Pods, container resource cycles, liveness states, and diagnostics.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex min-w-0 items-center gap-2">
        <span class="shrink-0 text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none"
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
        onclick={() => podsQuery.refetch()}
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="h-4 w-4" />
      </button>
    </div>
  </div>

  {#if podsQuery.isPending}
    <div class="space-y-4">
      <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if podsQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive font-medium">
          Error loading workloads: {podsQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !podsQuery.data || podsQuery.data.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <Boxes class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No active workloads</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Launch deployments to instantiate pods and configure schedules.
      </p>
    </div>
  {:else}
    <!-- `overflow-x-auto` plus a table min-width: without the min-width the
         table just compresses its columns to the viewport and the scroll
         container never engages, which is what made this unreadable on a
         phone. -->
    <div class="border border-border rounded-lg bg-card overflow-x-auto">
      <table class="w-full min-w-[52rem] text-left border-collapse">
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
                  class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium
                  {p.status === 'Running' ? 'bg-emerald-500/10 text-emerald-500' :
                   p.status === 'Succeeded' ? 'bg-blue-500/10 text-blue-500' : 'bg-amber-500/10 text-amber-500'}"
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
      </table>
    </div>
  {/if}
</div>

<!-- Logs Viewer Modal -->
{#if showLogModal && activeLogPod}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
    <div class="max-h-[90vh] w-full max-w-4xl overflow-y-auto rounded-xl border border-border bg-card p-4 shadow-lg sm:p-6">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <div class="flex items-center gap-2">
          <Terminal class="h-5 w-5 text-primary" />
          <h2 class="text-sm font-semibold">
            Container Logs — {activeLogPod.namespace}/{activeLogPod.name}
          </h2>
        </div>
        <button
          onclick={() => { showLogModal = false; activeLogPod = null; }}
          aria-label="Close logs"
          class="rounded-md p-1 hover:bg-accent text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <div class="mt-4">
        <!-- Keyed on namespace+pod so remount starts a fresh stream. Must use the
             pod's own namespace — selectedNamespace is "" when viewing all. -->
        {#key `${activeLogPod.namespace}/${activeLogPod.name}`}
          <LogViewer
            namespace={activeLogPod.namespace}
            podName={activeLogPod.name}
            pods={[activeLogPod.name]}
          />
        {/key}
      </div>

      <div class="mt-4 flex justify-end gap-3">
        <button
          type="button"
          onclick={() => { showLogModal = false; activeLogPod = null; }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
        >
          Close
        </button>
      </div>
    </div>
  </div>
{/if}
