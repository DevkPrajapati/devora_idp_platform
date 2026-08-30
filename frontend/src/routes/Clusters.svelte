<script lang="ts">
  import Modal from '$components/ui/Modal.svelte';
  import ClusterArchitecture from '$components/ClusterArchitecture.svelte';
  import ClusterHierarchy from '$components/ClusterHierarchy.svelte';
  import ClusterLogPanel from '$components/ClusterLogPanel.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import Button from '$components/ui/Button.svelte';
  import { statusBadgeClass } from '$lib/status';
  import { auth } from '$stores/auth';
  import { toasts, toastError } from '$stores/toast';
  import { formatAge } from '$lib/utils';
  import { router } from '$stores/router';
  import {
    listClusters,
    createCluster,
    activateCluster,
    stopCluster,
    restartCluster,
    deleteCluster,
    listNodes,
    listPods,
    listServices,
    type ClusterProvider,
    type ManagedCluster,
  } from '$services/cluster';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    ArrowLeft,
    AlertCircle,
    Loader2,
    Network,
    Play,
    Plus,
    Power,
    ScrollText,
    Square,
    Star,
    Trash2,
    Workflow,
  } from '@lucide/svelte';

  const queryClient = useQueryClient();
  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  const fleetQuery = createQuery(() => ({
    queryKey: ['clusters'],
    queryFn: listClusters,
    refetchInterval: (query) => {
      const rows = query.state.data?.clusters ?? [];
      return rows.some((c) => c.status === 'provisioning' || c.status === 'starting' || c.status === 'stopping' || c.status === 'deleting') ? 2000 : 10000;
    },
  }));

  let showCreate = $state(false);
  let isSubmitting = $state(false);
  let errorMsg = $state('');
  let pendingId = $state('');

  let formName = $state('');
  let formDisplay = $state('');
  let formProvider = $state<ClusterProvider>('kind');
  let formKubeconfig = $state('');
  let formVersion = $state('');
  let formWorkers = $state(0);
  let formActivate = $state(true);

  let deleteTarget = $state<ManagedCluster | null>(null);
  let deleteConfirm = $state('');
  let logTarget = $state<ManagedCluster | null>(null);
  let podLog = $state<{ namespace: string; pod: string; container?: string } | null>(null);
  let selectedId = $state(
    typeof window !== 'undefined'
      ? (new URLSearchParams(window.location.search).get('id') ?? '')
      : '',
  );

  const logLive = $derived(
    logTarget
      ? (fleetQuery.data?.clusters.find((c) => c.id === logTarget?.id) ?? logTarget)
      : null,
  );

  const selected = $derived(fleetQuery.data?.clusters.find((c) => c.id === selectedId) ?? null);
  const inspectLive = $derived(Boolean(selected?.active && selected.status === 'running'));

  const nodesQuery = createQuery(() => ({
    queryKey: ['cluster-nodes'],
    queryFn: listNodes,
    enabled: inspectLive,
    refetchInterval: 15000,
  }));
  const podsQuery = createQuery(() => ({
    queryKey: ['pods', 'all'],
    queryFn: () => listPods(''),
    enabled: inspectLive,
    refetchInterval: 15000,
  }));
  const servicesQuery = createQuery(() => ({
    queryKey: ['services', 'all'],
    queryFn: () => listServices(''),
    enabled: inspectLive,
    refetchInterval: 20000,
  }));

  $effect(() => {
    const unsub = router.subscribe(() => {
      if (typeof window === 'undefined') return;
      selectedId = new URLSearchParams(window.location.search).get('id') ?? '';
    });
    return unsub;
  });

  function openArchitecture(c: ManagedCluster) {
    selectedId = c.id;
    router.navigate('/clusters', { id: c.id });
  }

  function closeArchitecture() {
    selectedId = '';
    router.navigate('/clusters');
  }

  function resetForm() {
    formName = '';
    formDisplay = '';
    formProvider = fleetQuery.data?.kindAvailable ? 'kind' : fleetQuery.data?.minikubeAvailable ? 'minikube' : 'imported';
    formKubeconfig = '';
    formVersion = '';
    formWorkers = 0;
    formActivate = true;
    errorMsg = '';
  }

  function openCreate() {
    resetForm();
    showCreate = true;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    isSubmitting = true;
    errorMsg = '';
    try {
      const created = await createCluster({
        name: formName.trim(),
        displayName: formDisplay.trim(),
        provider: formProvider,
        kubeconfig: formKubeconfig,
        kubernetesVersion: formVersion.trim(),
        workerCount: formWorkers,
        activate: formActivate,
      });
      showCreate = false;
      resetForm();
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      toasts.success(
        created.status === 'provisioning'
          ? 'Cluster create started. It will finish in the background.'
          : 'Cluster registered.',
      );
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to create cluster';
    } finally {
      isSubmitting = false;
    }
  }

  async function runAction(id: string, fn: () => Promise<unknown>, ok: string) {
    pendingId = id;
    try {
      await fn();
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      await queryClient.invalidateQueries({ queryKey: ['pods'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-nodes'] });
      await queryClient.invalidateQueries({ queryKey: ['namespaces'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-namespaces'] });
      toasts.success(ok);
    } catch (err) {
      toastError(err, 'Cluster action failed');
    } finally {
      pendingId = '';
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    pendingId = deleteTarget.id;
    try {
      await deleteCluster(deleteTarget.id);
      deleteTarget = null;
      deleteConfirm = '';
      await queryClient.invalidateQueries({ queryKey: ['clusters'] });
      await queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      toasts.success('Cluster deletion started. The profile will be destroyed in the background.');
    } catch (err) {
      toastError(err, 'Failed to delete cluster');
    } finally {
      pendingId = '';
    }
  }

  function clusterStatusLabel(c: ManagedCluster) {
    if (!c.status) return 'Unknown';
    return c.status.charAt(0).toUpperCase() + c.status.slice(1);
  }

  function hostFromUrl(url: string) {
    return url.replace(/^https?:\/\//, '');
  }
</script>

<div class="page-stack">
  <PageHeader
    title="Clusters"
    description="Create, stop, restart, and delete Kubernetes clusters from the platform. The active cluster is the one every other page talks to."
  >
    {#if isAdmin}
      <Button variant="primary" onclick={openCreate}>
        <Plus class="h-4 w-4" />
        Create cluster
      </Button>
    {/if}
  </PageHeader>

  {#if selected}
    <div class="space-y-4">
      <button
        type="button"
        onclick={closeArchitecture}
        class="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft class="h-4 w-4" />
        All clusters
      </button>

      <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div class="flex items-center gap-2">
            {#if selected.active}
              <Star class="h-4 w-4 fill-amber-400 text-amber-400" />
            {/if}
            <h2 class="text-xl font-semibold">{selected.displayName}</h2>
            <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(selected.status)}">
              {clusterStatusLabel(selected)}
            </span>
            {#if selected.active}
              <span class="inline-flex items-center rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-500">
                Active
              </span>
            {/if}
          </div>
          <p class="mt-1 font-mono text-xs text-muted-foreground">
            {selected.provider} · {selected.kubernetesVersion || 'version unknown'} · {selected.nodeCount || 0} registered nodes
          </p>
        </div>
        <Button size="sm" onclick={() => (logTarget = selected)}>
          <ScrollText class="h-3.5 w-3.5" />
          View logs
        </Button>
      </div>

      <ClusterHierarchy
        clusterName={selected.name}
        nodes={inspectLive ? (nodesQuery.data ?? []) : []}
        pods={inspectLive ? (podsQuery.data ?? []) : []}
        onOpenPodLogs={(namespace, podName, container) => {
          podLog = { namespace, pod: podName, container };
        }}
      />

      {#if inspectLive && (nodesQuery.isPending || podsQuery.isPending)}
        <p class="text-xs text-muted-foreground">Refreshing live inventory…</p>
      {:else if inspectLive && (nodesQuery.error || podsQuery.error)}
        <p class="text-sm text-destructive">
          {nodesQuery.error?.message || podsQuery.error?.message}
        </p>
      {:else if !inspectLive}
        <p class="text-sm text-muted-foreground">
          {#if selected.status === 'starting' || selected.status === 'provisioning'}
            The hierarchy appears once the cluster is Ready.
          {:else if selected.status === 'stopped' || selected.status === 'error'}
            Start this cluster to inspect nodes, pods and containers.
          {:else}
            Activate this cluster to inspect its live architecture.
          {/if}
        </p>
      {/if}

      {#if inspectLive}
        <ClusterArchitecture
          clusterName={selected.name}
          connected={inspectLive}
          nodes={nodesQuery.data ?? []}
          pods={podsQuery.data ?? []}
          services={servicesQuery.data ?? []}
          compact
          registeredNodeCount={selected.nodeCount}
        />
      {/if}
    </div>
  {:else if fleetQuery.isPending}
    <Skeleton variant="table" rows={3} />
  {:else if fleetQuery.error}
    <div class="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
      <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
      <span>{fleetQuery.error.message}</span>
    </div>
  {:else if !fleetQuery.data?.clusters.length}
    <EmptyState
      icon={Network}
      title="No clusters registered"
      description={isAdmin
        ? 'Provision a local Kind or Minikube cluster, or import an existing kubeconfig. You do not need the CLI.'
        : 'An admin has not registered a cluster yet.'}
    >
      {#if isAdmin}
        <Button variant="primary" onclick={openCreate}>
          <Plus class="h-4 w-4" />
          Create cluster
        </Button>
      {/if}
    </EmptyState>
  {:else}
    <DataTable minWidth="48rem">
      <thead>
        <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <th class="px-4 py-3">Cluster</th>
          <th class="px-4 py-3">Provider</th>
          <th class="px-4 py-3">Status</th>
          <th class="px-4 py-3">Version</th>
          <th class="px-4 py-3">Nodes</th>
          <th class="px-4 py-3">Age</th>
          <th class="px-4 py-3 text-right">Actions</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-border text-sm">
        {#each fleetQuery.data.clusters as c (c.id)}
          <tr class="transition-colors hover:bg-accent/20">
            <td class="px-4 py-3 align-middle">
              <button
                type="button"
                onclick={() => openArchitecture(c)}
                class="flex max-w-sm items-center gap-2.5 text-left"
              >
                {#if c.active}
                  <Star class="h-3.5 w-3.5 shrink-0 fill-amber-400 text-amber-400" />
                {:else}
                  <span class="inline-block h-3.5 w-3.5 shrink-0"></span>
                {/if}
                <span class="min-w-0">
                  <span class="block truncate font-medium hover:underline">{c.displayName}</span>
                  {#if c.name !== c.displayName}
                    <span class="block truncate font-mono text-[11px] text-muted-foreground">{c.name}</span>
                  {/if}
                  {#if c.serverUrl}
                    <span class="block truncate font-mono text-[11px] text-muted-foreground" title={c.serverUrl}>
                      {hostFromUrl(c.serverUrl)}
                    </span>
                  {/if}
                  {#if c.lastError}
                    <span class="mt-0.5 block text-xs text-destructive">{c.lastError}</span>
                  {/if}
                </span>
              </button>
            </td>
            <td class="px-4 py-3 align-middle capitalize text-muted-foreground">{c.provider}</td>
            <td class="px-4 py-3 align-middle">
              <span class="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-medium {statusBadgeClass(c.status)}">
                {#if c.status === 'provisioning' || c.status === 'deleting' || c.status === 'starting' || c.status === 'stopping'}
                  <Loader2 class="h-3 w-3 animate-spin" />
                {/if}
                {clusterStatusLabel(c)}
              </span>
            </td>
            <td class="px-4 py-3 align-middle font-mono text-xs text-muted-foreground">{c.kubernetesVersion || '—'}</td>
            <td class="px-4 py-3 align-middle tabular-nums">{c.nodeCount || '—'}</td>
            <td class="px-4 py-3 align-middle text-xs text-muted-foreground">{formatAge(c.createdAt)}</td>
            <td class="px-4 py-3 align-middle">
              <div class="flex items-center justify-end gap-0.5 whitespace-nowrap">
                <Button
                  variant="ghost"
                  class="h-8 w-8 px-0"
                  title="Architecture"
                  aria-label="Architecture"
                  onclick={() => openArchitecture(c)}
                >
                  <Workflow class="h-4 w-4" />
                </Button>
                <Button
                  variant="ghost"
                  class="h-8 w-8 px-0"
                  title="Logs"
                  aria-label="Logs"
                  onclick={() => (logTarget = c)}
                >
                  <ScrollText class="h-4 w-4" />
                </Button>
                {#if isAdmin}
                  <span class="mx-1 h-4 w-px bg-border"></span>
                  <Button
                    variant="ghost"
                    class="h-8 w-8 px-0"
                    title={c.active ? 'Already active' : 'Activate'}
                    aria-label="Activate"
                    disabled={pendingId === c.id || c.active || c.status === 'provisioning'}
                    onclick={() => runAction(c.id, () => activateCluster(c.id), `${c.name} is now the active cluster`)}
                  >
                    <Play class="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    class="h-8 w-8 px-0"
                    title="Stop"
                    aria-label="Stop"
                    disabled={pendingId === c.id || c.status !== 'running'}
                    onclick={() => runAction(c.id, () => stopCluster(c.id), `${c.name} is stopping`)}
                  >
                    <Square class="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    class="h-8 w-8 px-0"
                    title={c.status === 'stopped' || c.status === 'error' ? 'Start' : 'Restart'}
                    aria-label={c.status === 'stopped' || c.status === 'error' ? 'Start' : 'Restart'}
                    disabled={pendingId === c.id || c.status === 'provisioning' || c.status === 'deleting' || c.status === 'starting' || c.status === 'stopping'}
                    onclick={() => runAction(c.id, () => restartCluster(c.id), `${c.name} is starting`)}
                  >
                    <Power class="h-4 w-4" />
                  </Button>
                  <Button
                    variant="ghost"
                    class="h-8 w-8 px-0 hover:bg-destructive/10 hover:text-destructive"
                    title="Delete"
                    aria-label="Delete"
                    disabled={pendingId === c.id}
                    onclick={() => { deleteTarget = c; deleteConfirm = ''; }}
                  >
                    <Trash2 class="h-4 w-4" />
                  </Button>
                {/if}
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </DataTable>
  {/if}
</div>

<Modal
  open={showCreate}
  title="Create cluster"
  description="Provision a local cluster or import an existing kubeconfig. No CLI required."
  size="lg"
  onclose={() => { showCreate = false; }}
>
  <form id="create-cluster-form" onsubmit={handleCreate} class="space-y-4">
    {#if errorMsg}
      <p class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{errorMsg}</p>
    {/if}

    <div class="grid gap-3 sm:grid-cols-3">
      {#each [
        { id: 'kind', label: 'Kind', hint: 'Local Docker Kubernetes', disabled: !fleetQuery.data?.kindAvailable },
        { id: 'minikube', label: 'Minikube', hint: 'Local minikube profile', disabled: !fleetQuery.data?.minikubeAvailable },
        { id: 'imported', label: 'Import', hint: 'Existing kubeconfig', disabled: false },
      ] as opt}
        <button
          type="button"
          disabled={opt.disabled}
          onclick={() => (formProvider = opt.id as ClusterProvider)}
          class="rounded-lg border px-3 py-3 text-left text-sm transition-colors disabled:opacity-40 {formProvider === opt.id
            ? 'border-primary bg-primary/5'
            : 'border-border hover:bg-accent'}"
        >
          <p class="font-medium">{opt.label}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">{opt.hint}</p>
          {#if opt.disabled}
            <p class="mt-1 text-[11px] text-amber-600">Not installed on this host</p>
          {/if}
        </button>
      {/each}
    </div>

    <div class="grid gap-3 sm:grid-cols-2">
      <label class="block text-sm">
        <span class="mb-1 block font-medium">Name</span>
        <input
          bind:value={formName}
          required
          pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
          maxlength="48"
          placeholder="team-prod"
          class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
        />
      </label>
      <label class="block text-sm">
        <span class="mb-1 block font-medium">Display name</span>
        <input
          bind:value={formDisplay}
          placeholder="Production"
          class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
        />
      </label>
    </div>

    {#if formProvider !== 'imported'}
      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block text-sm">
          <span class="mb-1 block font-medium">Kubernetes version (optional)</span>
          <input
            bind:value={formVersion}
            placeholder="v1.31.0"
            class="h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm"
          />
        </label>
        {#if formProvider === 'kind'}
          <label class="block text-sm">
            <span class="mb-1 block font-medium">Worker nodes</span>
            <input
              type="number"
              min="0"
              max="3"
              bind:value={formWorkers}
              class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm"
            />
          </label>
        {/if}
      </div>
    {:else}
      <label class="block text-sm">
        <span class="mb-1 block font-medium">Kubeconfig YAML</span>
        <textarea
          bind:value={formKubeconfig}
          required
          rows="8"
          placeholder="apiVersion: v1&#10;kind: Config&#10;..."
          class="w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs"
        ></textarea>
        <span class="mt-1 block text-xs text-muted-foreground">Stored encrypted. Never shown again.</span>
      </label>
    {/if}

    <label class="flex items-center gap-2 text-sm">
      <input type="checkbox" bind:checked={formActivate} class="rounded border-input" />
      Activate this cluster when it is ready
    </label>
  </form>

  {#snippet footer()}
    <button
      type="button"
      onclick={() => (showCreate = false)}
      class="inline-flex h-9 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="submit"
      form="create-cluster-form"
      disabled={isSubmitting}
      class="inline-flex h-9 items-center gap-2 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-50"
    >
      {#if isSubmitting}
        <Loader2 class="h-3.5 w-3.5 animate-spin" />
      {/if}
      Create
    </button>
  {/snippet}
</Modal>

<Modal
  open={!!deleteTarget}
  title="Delete cluster"
  description={deleteTarget
    ? deleteTarget.provider === 'imported'
      ? `Unregister “${deleteTarget.name}”. The remote cluster is not destroyed.`
      : `Destroy local cluster “${deleteTarget.name}” and remove it from the platform. This cannot be undone.`
    : ''}
  onclose={() => { deleteTarget = null; deleteConfirm = ''; }}
>
  {#if deleteTarget}
    <p class="text-sm text-muted-foreground">
      Type <span class="font-mono font-medium text-foreground">{deleteTarget.name}</span> to confirm.
    </p>
    <input
      bind:value={deleteConfirm}
      class="mt-3 h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm"
    />
  {/if}
  {#snippet footer()}
    <button
      type="button"
      onclick={() => { deleteTarget = null; deleteConfirm = ''; }}
      class="inline-flex h-9 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="button"
      disabled={!deleteTarget || deleteConfirm !== deleteTarget.name || pendingId === deleteTarget.id}
      onclick={handleDelete}
      class="inline-flex h-9 items-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground disabled:opacity-50"
    >
      Delete
    </button>
  {/snippet}
</Modal>

<Modal
  open={!!podLog}
  title={podLog ? `Container logs — ${podLog.namespace}/${podLog.pod}` : 'Container logs'}
  size="xl"
  onclose={() => (podLog = null)}
>
  {#if podLog}
    {#key `${podLog.namespace}/${podLog.pod}/${podLog.container ?? ''}`}
      <LogViewer
        namespace={podLog.namespace}
        podName={podLog.pod}
        pods={[podLog.pod]}
        container={podLog.container}
      />
    {/key}
  {/if}
</Modal>

<Modal
  open={!!logLive}
  title={logLive ? `Cluster logs — ${logLive.displayName}` : 'Cluster logs'}
  description={logLive?.status === 'provisioning' || logLive?.status === 'starting'
    ? 'Live output from kind/minikube. Node logs appear once the cluster is running.'
    : 'Provisioner output, node logs, and Kubernetes events for this cluster.'}
  size="xl"
  onclose={() => (logTarget = null)}
>
  {#if logLive}
    {#key logLive.id}
      <ClusterLogPanel clusterId={logLive.id} clusterName={logLive.name} status={logLive.status} />
    {/key}
  {/if}
</Modal>
