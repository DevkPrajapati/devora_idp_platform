<script lang="ts">
  /**
   * Shown when the platform is running but the workload cluster is not.
   * Start lives here so the user does not have to open a terminal: the backend
   * does not need Kubernetes to be up, and RestartCluster talks to minikube/kind
   * on the host.
   */
  import { listClusters, restartCluster, type ManagedCluster } from '$services/cluster';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { auth } from '$stores/auth';
  import { router } from '$stores/router';
  import { toastError, toasts } from '$stores/toast';
  import { AlertTriangle, Loader2, Power } from '@lucide/svelte';

  const queryClient = useQueryClient();
  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  const fleetQuery = createQuery(() => ({
    queryKey: ['clusters'],
    queryFn: listClusters,
    refetchInterval: (query) => {
      const rows = query.state.data?.clusters ?? [];
      return rows.some((c) =>
        c.status === 'provisioning' ||
        c.status === 'starting' ||
        c.status === 'stopping' ||
        c.status === 'deleting',
      )
        ? 2000
        : 15000;
    },
  }));

  const overview = createQuery(() => ({
    queryKey: ['cluster-overview'],
    queryFn: async () => {
      const { getOverview } = await import('$services/cluster');
      return getOverview();
    },
    refetchInterval: 15000,
  }));

  let pending = $state(false);

  const target = $derived.by((): ManagedCluster | null => {
    const rows = fleetQuery.data?.clusters ?? [];
    const active = rows.find((c) => c.active);
    if (active && (active.status === 'stopped' || active.status === 'error' || active.status === 'starting')) {
      return active;
    }
    const stopped = rows.find((c) => c.status === 'stopped' || c.status === 'error' || c.status === 'starting');
    return stopped ?? null;
  });

  const connected = $derived(overview.data?.connected === true);
  const show = $derived(!connected && target !== null);

  async function start() {
    if (!target) return;
    pending = true;
    try {
      await restartCluster(target.id);
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      toasts.success(`${target.displayName} is starting`);
    } catch (err) {
      toastError(err, 'Could not start the cluster');
    } finally {
      pending = false;
    }
  }
</script>

{#if show && target}
  <div class="mb-4 flex flex-col gap-3 rounded-lg border border-amber-500/40 bg-amber-500/10 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
    <div class="flex items-start gap-2 text-sm">
      {#if target.status === 'starting'}
        <Loader2 class="mt-0.5 h-4 w-4 shrink-0 animate-spin text-amber-600" />
      {:else}
        <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
      {/if}
      <div>
        <p class="font-medium text-foreground">
          {#if target.status === 'starting'}
            Starting cluster {target.displayName}…
          {:else}
            Kubernetes cluster {target.displayName} is {target.status === 'error' ? 'unreachable' : 'stopped'}
          {/if}
        </p>
        <p class="mt-0.5 text-xs text-muted-foreground">
          The platform itself is running. Workloads, namespaces and services will appear once the cluster is Ready.
          {#if target.lastError}
            {target.lastError}
          {/if}
        </p>
      </div>
    </div>
    <div class="flex shrink-0 items-center gap-2">
      <button
        type="button"
        class="inline-flex h-8 items-center rounded-md border border-input bg-background px-3 text-xs font-medium hover:bg-accent"
        onclick={() => router.navigate('/clusters')}
      >
        Clusters
      </button>
      {#if isAdmin && target.status !== 'starting'}
        <button
          type="button"
          class="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-xs font-medium text-primary-foreground disabled:opacity-50"
          disabled={pending}
          onclick={start}
        >
          {#if pending}
            <Loader2 class="h-3.5 w-3.5 animate-spin" />
          {:else}
            <Power class="h-3.5 w-3.5" />
          {/if}
          Start cluster
        </button>
      {/if}
    </div>
  </div>
{/if}
