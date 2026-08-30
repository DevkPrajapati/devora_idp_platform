<script lang="ts">
  import { onMount } from 'svelte';
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import Modal from '$components/ui/Modal.svelte';
  import NamespaceResourceTree from '$components/NamespaceResourceTree.svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import { auth } from '$stores/auth';
  import {
    listNamespaces,
    createNamespace,
    deleteNamespace,
    setNamespaceProject,
    isMissingFromCluster,
    type Namespace,
  } from '$services/namespaces';
  import {
    listClusterNamespaces,
    getNamespaceResources,
    type ClusterNamespace,
    type ClusterNamespaceKind,
  } from '$services/cluster';
  import { listProjects } from '$services/projects';
  import { formatAge } from '$lib/utils';
  import { toasts, toastError } from '$stores/toast';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    AlertTriangle,
    ArrowLeft,
    FolderGit2,
    Layers,
    Plus,
    RefreshCw,
    Search,
    Trash2,
    X,
  } from '@lucide/svelte';

  const queryClient = useQueryClient();
  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  function readNsParam(): string {
    if (typeof window === 'undefined') return '';
    return new URLSearchParams(window.location.search).get('ns') ?? '';
  }

  let selectedNs = $state(readNsParam());
  let search = $state('');
  let kindFilter = $state<'all' | ClusterNamespaceKind>('all');
  let logPod = $state('');

  onMount(() => {
    const onPop = () => {
      selectedNs = readNsParam();
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  });

  function openNamespace(name: string) {
    selectedNs = name;
    logPod = '';
    window.history.pushState(null, '', `/namespaces?ns=${encodeURIComponent(name)}`);
  }

  function closeNamespace() {
    selectedNs = '';
    logPod = '';
    window.history.pushState(null, '', '/namespaces');
  }

  const clusterQuery = createQuery(() => ({
    queryKey: ['cluster-namespaces'],
    queryFn: listClusterNamespaces,
    refetchInterval: 15000,
  }));

  const tenantQuery = createQuery(() => ({
    queryKey: ['namespaces'],
    queryFn: () => listNamespaces(1, 100),
  }));

  const resourcesQuery = createQuery(() => ({
    queryKey: ['namespace-resources', selectedNs],
    queryFn: () => getNamespaceResources(selectedNs),
    enabled: selectedNs.length > 0,
    refetchInterval: 12000,
  }));

  const projectsQuery = createQuery(() => ({
    queryKey: ['projects'],
    queryFn: () => listProjects(1, 100),
  }));

  const projects = $derived(projectsQuery.data?.projects ?? []);
  const tenantByName = $derived(
    Object.fromEntries((tenantQuery.data?.namespaces ?? []).map((ns) => [ns.name, ns])),
  );

  const filteredNamespaces = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return (clusterQuery.data ?? []).filter((ns) => {
      if (kindFilter !== 'all' && ns.kind !== kindFilter) return false;
      if (!q) return true;
      return ns.name.toLowerCase().includes(q) || ns.displayName.toLowerCase().includes(q);
    });
  });

  /**
   * Namespaces the platform has a record of that the cluster does not have.
   *
   * The table below lists what the cluster reports, so a registered namespace
   * that no longer exists there was invisible: it could not be inspected and it
   * could not be cleaned up, while still counting against projects and
   * credential syncs. Listing them separately makes the drift actionable
   * instead of silent.
   */
  const orphanedNamespaces = $derived(
    (tenantQuery.data?.namespaces ?? []).filter(isMissingFromCluster),
  );

  const selectedCluster = $derived(
    (clusterQuery.data ?? []).find((ns) => ns.name === selectedNs) ?? resourcesQuery.data?.namespace,
  );
  const selectedTenant = $derived(tenantByName[selectedNs] as Namespace | undefined);

  const groupCounts = $derived.by(() => {
    const counts: Record<string, number> = {};
    for (const group of resourcesQuery.data?.groups ?? []) {
      counts[group.name] = group.items.length;
    }
    return counts;
  });

  let assigningNamespace = $state('');
  let assignResult = $state<{ namespace: string; message: string; ok: boolean } | null>(null);

  function projectOf(ns: Namespace): string {
    return ns.projectSlug ?? '';
  }

  async function handleAssignProject(ns: Namespace, projectSlug: string) {
    if (projectSlug === projectOf(ns)) return;
    assigningNamespace = ns.name;
    assignResult = null;
    try {
      const result = await setNamespaceProject(ns.name, projectSlug);
      const synced = result.syncedRegistrySecrets ?? [];
      const message = projectSlug
        ? synced.length > 0
          ? `Moved to ${projectSlug}; synced ${synced.length} registry secret${synced.length === 1 ? '' : 's'}.`
          : `Moved to ${projectSlug}.`
        : 'Detached from its project.';
      assignResult = { namespace: ns.name, ok: true, message };
      toasts.success(`${ns.name}: ${message}`);
      await queryClient.invalidateQueries({ queryKey: ['namespaces'] });
    } catch (err) {
      assignResult = {
        namespace: ns.name,
        ok: false,
        message: err instanceof Error ? err.message : 'Could not update the project.',
      };
      toastError(err, `Could not change the project for ${ns.name}.`);
    } finally {
      assigningNamespace = '';
    }
  }

  let showCreateModal = $state(false);
  let showDeleteConfirm = $state(false);
  let namespaceToDelete = $state('');
  let deleteConfirmText = $state('');
  // Deleting a record with nothing behind it is not destructive, and warning
  // that it will terminate workloads would be false.
  const deletingOrphanRecord = $derived(
    namespaceToDelete !== '' &&
      orphanedNamespaces.some((ns) => ns.name === namespaceToDelete),
  );
  let isSubmitting = $state(false);
  let errorMsg = $state('');
  let name = $state('');
  let displayName = $state('');
  let description = $state('');
  let labelsInput = $state('');
  let annotationsInput = $state('');

  function resetForm() {
    name = '';
    displayName = '';
    description = '';
    labelsInput = '';
    annotationsInput = '';
    errorMsg = '';
  }

  function parseKeyValues(input: string): Record<string, string> {
    const result: Record<string, string> = {};
    if (!input.trim()) return result;
    for (const part of input.split(',')) {
      const idx = part.indexOf('=');
      if (idx !== -1) {
        const k = part.substring(0, idx).trim();
        const v = part.substring(idx + 1).trim();
        if (k) result[k] = v;
      }
    }
    return result;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    isSubmitting = true;
    errorMsg = '';
    try {
      await createNamespace(name, displayName, description, parseKeyValues(labelsInput), parseKeyValues(annotationsInput));
      queryClient.invalidateQueries({ queryKey: ['namespaces'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showCreateModal = false;
      resetForm();
      toasts.success(`Namespace ${name} created.`);
    } catch (err: any) {
      errorMsg = err.message || 'Failed to create namespace';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDelete() {
    if (!namespaceToDelete) return;
    isSubmitting = true;
    errorMsg = '';
    const deleted = namespaceToDelete;
    try {
      await deleteNamespace(deleted);
      queryClient.invalidateQueries({ queryKey: ['namespaces'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-namespaces'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showDeleteConfirm = false;
      namespaceToDelete = '';
      deleteConfirmText = '';
      if (selectedNs === deleted) closeNamespace();
      toasts.success(`Namespace ${deleted} deleted.`);
    } catch (err) {
      errorMsg = err instanceof Error ? err.message : 'Failed to delete namespace';
      toastError(err, `Could not delete namespace ${deleted}.`);
    } finally {
      isSubmitting = false;
    }
  }

  function kindBadge(kind: ClusterNamespaceKind): string {
    switch (kind) {
      case 'tenant':
        return 'bg-emerald-500/10 text-emerald-500';
      case 'system':
        return 'bg-slate-500/15 text-slate-400';
      default:
        return 'bg-sky-500/10 text-sky-500';
    }
  }

  function phaseBadge(phase: string): string {
    return phase === 'Active' ? 'bg-emerald-500/10 text-emerald-500' : 'bg-amber-500/10 text-amber-500';
  }

  function kindOf(ns: ClusterNamespace): ClusterNamespaceKind {
    return ns.kind || 'cluster';
  }
</script>

<div class="page-stack">
  {#if selectedNs}
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <button
          type="button"
          onclick={closeNamespace}
          class="mb-2 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft class="h-4 w-4" />
          All namespaces
        </button>
        <div class="flex flex-wrap items-center gap-2">
          <h1 class="font-mono text-2xl font-semibold tracking-tight">{selectedNs}</h1>
          {#if selectedCluster}
            <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium {phaseBadge(selectedCluster.phase)}">
              {selectedCluster.phase}
            </span>
            <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize {kindBadge(kindOf(selectedCluster))}">
              {kindOf(selectedCluster)}
            </span>
          {/if}
        </div>
        <p class="mt-1 text-sm text-muted-foreground">
          Live resources in this namespace. The tree refreshes every 5 seconds.
        </p>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <span class="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
          <span class="relative flex h-2 w-2">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-foreground opacity-40"></span>
            <span class="relative inline-flex h-2 w-2 rounded-full bg-foreground"></span>
          </span>
          Live
        </span>
        <button
          onclick={() => {
            resourcesQuery.refetch();
            clusterQuery.refetch();
          }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent"
        >
          <RefreshCw class="mr-2 h-4 w-4" />
          Refresh
        </button>
        {#if isAdmin && selectedTenant}
          <button
            onclick={() => {
              namespaceToDelete = selectedNs;
              showDeleteConfirm = true;
            }}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium text-destructive hover:bg-destructive/10"
          >
            <Trash2 class="mr-2 h-4 w-4" />
            Delete
          </button>
        {/if}
      </div>
    </div>

    {#if selectedTenant || selectedCluster}
      <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {#each [
          { name: 'Workloads', label: 'Workloads' },
          { name: 'Networking', label: 'Networking' },
          { name: 'Config', label: 'Config' },
          { name: 'Storage', label: 'Storage' },
        ] as stat (stat.name)}
          <Card class="px-5 py-4">
            <div class="flex flex-col items-start gap-1.5">
              <p class="text-xs font-medium leading-none text-muted-foreground">{stat.label}</p>
              <p class="text-2xl font-semibold leading-none tabular-nums tracking-tight">
                {groupCounts[stat.name] ?? 0}
              </p>
            </div>
          </Card>
        {/each}
      </div>
    {/if}

    {#if selectedTenant && isAdmin}
      <Card>
        <CardContent class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2 text-sm text-muted-foreground">
            <FolderGit2 class="h-4 w-4" />
            Project assignment
          </div>
          <select
            value={projectOf(selectedTenant)}
            disabled={assigningNamespace === selectedTenant.name || projectsQuery.isPending}
            onchange={(e) => handleAssignProject(selectedTenant, e.currentTarget.value)}
            aria-label="Project for namespace {selectedTenant.name}"
            class="h-9 min-w-48 rounded-md border border-input bg-background px-3 text-sm"
          >
            <option value="">Unassigned</option>
            {#each projects as project}
              <option value={project.slug}>{project.name}</option>
            {/each}
          </select>
          {#if assigningNamespace === selectedTenant.name}
            <p class="w-full text-right text-xs text-muted-foreground">Updating…</p>
          {:else if assignResult?.namespace === selectedTenant.name}
            <p class="w-full text-right text-xs {assignResult.ok ? 'text-emerald-500' : 'text-destructive'}">
              {assignResult.message}
            </p>
          {/if}
        </CardContent>
      </Card>
    {/if}

    {#if resourcesQuery.isPending}
      <div class="space-y-3">
        <div class="h-16 animate-pulse rounded-lg bg-muted"></div>
        <div class="h-40 animate-pulse rounded-lg bg-muted"></div>
      </div>
    {:else if resourcesQuery.error}
      <Card class="border-destructive bg-destructive/5">
        <CardContent class="py-6">
          <p class="text-sm text-destructive">
            Could not load resources for {selectedNs}: {resourcesQuery.error.message}
          </p>
        </CardContent>
      </Card>
    {:else if resourcesQuery.data}
      <div class="flex items-center justify-between text-sm text-muted-foreground">
        <p>{resourcesQuery.data.totalResources} objects · age {formatAge(resourcesQuery.data.namespace.createdAt)}</p>
        {#if selectedTenant}
          <p>Owner {selectedTenant.ownerEmail || '—'}</p>
        {/if}
      </div>
      <NamespaceResourceTree
        groups={resourcesQuery.data.groups}
        onOpenPodLogs={(podName) => {
          logPod = podName;
        }}
      />
    {/if}

    {#if logPod}
      <div class="rounded-xl border border-border bg-card p-4">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-sm font-semibold">Pod logs — {selectedNs}/{logPod}</h2>
          <button
            type="button"
            onclick={() => (logPod = '')}
            class="rounded-md p-1 text-muted-foreground hover:bg-accent"
            aria-label="Close logs"
          >
            <X class="h-4 w-4" />
          </button>
        </div>
        {#key `${selectedNs}/${logPod}`}
          <LogViewer namespace={selectedNs} podName={logPod} pods={[logPod]} />
        {/key}
      </div>
    {/if}
  {:else}
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <PageHeader
        title="Namespaces"
        description="Live Kubernetes namespaces from the cluster — the same list as kubectl get ns. Click one to inspect its resources."
      />
      <div class="flex flex-wrap items-center gap-3">
        <span class="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
          <span class="relative flex h-2 w-2">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-foreground opacity-40"></span>
            <span class="relative inline-flex h-2 w-2 rounded-full bg-foreground"></span>
          </span>
          Live · 5s
        </span>
        <button
          onclick={() => clusterQuery.refetch()}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent"
        >
          <RefreshCw class="mr-2 h-4 w-4" />
          Refresh
        </button>
        {#if isAdmin}
          <button
            onclick={() => {
              resetForm();
              showCreateModal = true;
            }}
            class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90"
          >
            <Plus class="mr-2 h-4 w-4" />
            Create Namespace
          </button>
        {/if}
      </div>
    </div>

    <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
      <label class="relative flex-1">
        <Search class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <input
          type="search"
          bind:value={search}
          placeholder="Filter namespaces…"
          class="h-9 w-full rounded-md border border-input bg-background pl-9 pr-3 text-sm"
        />
      </label>
      <div class="flex flex-wrap gap-2">
        {#each (['all', 'tenant', 'cluster', 'system'] as const) as kind}
          <button
            type="button"
            onclick={() => (kindFilter = kind)}
            class="h-9 rounded-md px-3 text-sm font-medium {kindFilter === kind
              ? 'bg-primary text-primary-foreground'
              : 'border border-input bg-background hover:bg-accent'}"
          >
            {kind === 'all' ? 'All' : kind[0].toUpperCase() + kind.slice(1)}
          </button>
        {/each}
      </div>
    </div>

    {#if clusterQuery.isPending}
      <div class="space-y-3">
        <div class="h-12 animate-pulse rounded bg-muted"></div>
        <div class="h-24 animate-pulse rounded bg-muted"></div>
      </div>
    {:else if clusterQuery.error}
      <Card class="border-destructive bg-destructive/5">
        <CardContent class="py-6">
          <p class="text-sm text-destructive">
            Error loading namespaces: {clusterQuery.error.message}
          </p>
        </CardContent>
      </Card>
    {:else if filteredNamespaces.length === 0}
      <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
        <Layers class="mb-4 h-12 w-12 text-muted-foreground/40" />
        <h3 class="text-lg font-semibold">No namespaces match</h3>
        <p class="mt-2 max-w-sm text-sm text-muted-foreground">
          {clusterQuery.data?.length
            ? 'Try a different search or filter.'
            : 'The cluster has no namespaces, or Kubernetes is unreachable.'}
        </p>
      </div>
    {:else}
      <div class="overflow-x-auto rounded-lg border border-border bg-card">
        <table class="w-full min-w-[52rem] text-left text-sm">
          <thead>
            <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
              <th class="px-5 py-3">Name</th>
              <th class="px-5 py-3">Status</th>
              <th class="px-5 py-3">Kind</th>
              <th class="px-5 py-3">Project</th>
              <th class="px-5 py-3">Owner</th>
              <th class="px-5 py-3">Age</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border">
            {#each filteredNamespaces as ns}
              {@const tenant = tenantByName[ns.name]}
              <tr
                class="cursor-pointer hover:bg-accent/30"
                onclick={() => openNamespace(ns.name)}
              >
                <td class="px-5 py-3.5">
                  <p class="font-mono text-sm font-medium">{ns.name}</p>
                  {#if ns.displayName && ns.displayName !== ns.name}
                    <p class="text-xs text-muted-foreground">{ns.displayName}</p>
                  {/if}
                </td>
                <td class="px-5 py-3.5">
                  <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium {phaseBadge(ns.phase)}">
                    {ns.phase}
                  </span>
                </td>
                <td class="px-5 py-3.5">
                  <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium capitalize {kindBadge(kindOf(ns))}">
                    {kindOf(ns)}
                  </span>
                </td>
                <td class="px-5 py-3.5 text-xs text-muted-foreground">
                  {tenant?.projectSlug || '—'}
                </td>
                <td class="px-5 py-3.5 text-xs text-muted-foreground">
                  {tenant?.ownerEmail || '—'}
                </td>
                <td class="px-5 py-3.5 text-xs text-muted-foreground">{formatAge(ns.createdAt)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="text-xs text-muted-foreground">
        Showing {filteredNamespaces.length} of {clusterQuery.data?.length ?? 0} cluster namespaces.
      </p>
    {/if}

    {#if orphanedNamespaces.length > 0}
      <div class="rounded-lg border border-destructive/40 bg-destructive/5">
        <div class="flex items-start gap-3 border-b border-destructive/20 px-5 py-3">
          <AlertTriangle class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
          <div>
            <h2 class="text-sm font-semibold text-destructive">
              Registered but missing from the cluster
            </h2>
            <p class="mt-1 text-xs text-muted-foreground">
              The platform has a record of {orphanedNamespaces.length === 1
                ? 'this namespace'
                : 'these namespaces'}, but the connected cluster does not.
              This happens when a namespace is deleted with kubectl, or when the
              cluster it lived in was rebuilt. Remove the record to clear it.
            </p>
          </div>
        </div>
        <table class="w-full min-w-[40rem] text-left text-sm">
          <thead>
            <tr class="border-b border-destructive/20 text-xs font-semibold uppercase text-muted-foreground">
              <th class="px-5 py-2">Name</th>
              <th class="px-5 py-2">Project</th>
              <th class="px-5 py-2">Owner</th>
              <th class="px-5 py-2">Registered</th>
              {#if isAdmin}<th class="px-5 py-2 text-right">Action</th>{/if}
            </tr>
          </thead>
          <tbody class="divide-y divide-destructive/10">
            {#each orphanedNamespaces as ns}
              <tr>
                <td class="px-5 py-3 font-mono text-sm font-medium">{ns.name}</td>
                <td class="px-5 py-3 text-xs text-muted-foreground">{ns.projectSlug || '—'}</td>
                <td class="px-5 py-3 text-xs text-muted-foreground">{ns.ownerEmail || '—'}</td>
                <td class="px-5 py-3 text-xs text-muted-foreground">{formatAge(ns.createdAt)}</td>
                {#if isAdmin}
                  <td class="px-5 py-3 text-right">
                    <button
                      type="button"
                      class="inline-flex items-center gap-1.5 rounded-md border border-destructive/40 px-2.5 py-1 text-xs font-medium text-destructive hover:bg-destructive/10"
                      onclick={() => {
                        namespaceToDelete = ns.name;
                        showDeleteConfirm = true;
                      }}
                    >
                      <Trash2 class="h-3.5 w-3.5" />
                      Remove record
                    </button>
                  </td>
                {/if}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}
</div>

<Modal
  open={showCreateModal && isAdmin}
  title="Create Tenant Namespace"
  size="lg"
  onclose={() => (showCreateModal = false)}
>
  <form id="create-namespace-form" onsubmit={handleCreate} class="space-y-4">
    {#if errorMsg}
      <p class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{errorMsg}</p>
    {/if}

    <div class="space-y-1.5">
      <label for="name" class="text-sm font-medium">Namespace Name (Kubernetes compatible)</label>
      <input
        id="name"
        type="text"
        required
        pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
        placeholder="e.g. billing-dev, frontend-prod"
        bind:value={name}
        class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
      />
    </div>

    <div class="space-y-1.5">
      <label for="displayName" class="text-sm font-medium">Display Name</label>
      <input
        id="displayName"
        type="text"
        required
        placeholder="e.g. Billing System Development"
        bind:value={displayName}
        class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
      />
    </div>

    <div class="space-y-1.5">
      <label for="description" class="text-sm font-medium">Description</label>
      <textarea
        id="description"
        rows="3"
        placeholder="Describe the environment purpose, target team, etc."
        bind:value={description}
        class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
      ></textarea>
    </div>

    <div class="space-y-1.5">
      <label for="labels" class="text-sm font-medium">Owner Labels (comma separated)</label>
      <input
        id="labels"
        type="text"
        placeholder="team=finance,tier=critical"
        bind:value={labelsInput}
        class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
      />
    </div>

    <div class="space-y-1.5">
      <label for="annotations" class="text-sm font-medium">Annotations (comma separated)</label>
      <input
        id="annotations"
        type="text"
        placeholder="contact=slack-channel,notify=email"
        bind:value={annotationsInput}
        class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
      />
    </div>
  </form>

  {#snippet footer()}
    <button
      type="button"
      onclick={() => (showCreateModal = false)}
      class="inline-flex h-9 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="submit"
      form="create-namespace-form"
      disabled={isSubmitting}
      class="inline-flex h-9 items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
    >
      {isSubmitting ? 'Creating...' : 'Create'}
    </button>
  {/snippet}
</Modal>

<Modal
  open={showDeleteConfirm && isAdmin}
  title="Delete namespace {namespaceToDelete}?"
  description={deletingOrphanRecord
    ? 'This namespace is not in the cluster, so only the platform record is removed. No workloads are affected.'
    : 'This terminates every workload, PVC, ConfigMap and Service inside it. It cannot be undone.'}
  onclose={() => {
    showDeleteConfirm = false;
    namespaceToDelete = '';
    errorMsg = '';
  }}
>
  <p class="text-sm text-muted-foreground">
    Type the namespace name to confirm you are deleting
    <span class="font-mono font-semibold text-destructive">{namespaceToDelete}</span>.
  </p>
  <input
    type="text"
    bind:value={deleteConfirmText}
    placeholder={namespaceToDelete}
    autocomplete="off"
    aria-label="Type the namespace name to confirm deletion"
    class="mt-3 h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm"
  />

  {#if errorMsg}
    <p role="alert" class="mt-3 rounded-md bg-destructive/10 p-3 text-sm text-destructive">
      {errorMsg}
    </p>
  {/if}

  {#snippet footer()}
    <button
      type="button"
      onclick={() => {
        showDeleteConfirm = false;
        namespaceToDelete = '';
        errorMsg = '';
      }}
      class="inline-flex h-9 items-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="button"
      disabled={isSubmitting || deleteConfirmText !== namespaceToDelete}
      onclick={handleDelete}
      class="inline-flex h-9 items-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:cursor-not-allowed disabled:opacity-50"
    >
      {isSubmitting ? 'Deleting…' : 'Delete namespace'}
    </button>
  {/snippet}
</Modal>
