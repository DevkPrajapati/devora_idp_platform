<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { auth } from '$stores/auth';
  import {
    listNamespaces,
    createNamespace,
    deleteNamespace,
    setNamespaceProject,
    type Namespace,
  } from '$services/namespaces';
  import { listProjects } from '$services/projects';
  import Modal from '$components/ui/Modal.svelte';
  import { toasts, toastError } from '$stores/toast';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Layers, Plus, Trash2, X, RefreshCw, FolderGit2 } from '@lucide/svelte';

  const queryClient = useQueryClient();

  // Queries
  const namespacesQuery = createQuery(() => ({
    queryKey: ['namespaces'],
    queryFn: () => listNamespaces(1, 100),
  }));

  // Populates the project picker. SetNamespaceProject has existed on the
  // backend all along but had no UI, so namespaces could never be attached to
  // a project from the console.
  const projectsQuery = createQuery(() => ({
    queryKey: ['projects'],
    queryFn: () => listProjects(1, 100),
  }));

  const projects = $derived(projectsQuery.data?.projects ?? []);

  /** Namespace whose assignment is in flight, so only its own row shows a spinner. */
  let assigningNamespace = $state('');
  /** Transient per-namespace confirmation of what the move actually did. */
  let assignResult = $state<{ namespace: string; message: string; ok: boolean } | null>(null);

  /**
   * project_slug is a first-class field on the Namespace message, backed by a
   * foreign key. Reading it from the labels map would only reflect whatever the
   * registry-secret sync happened to stamp there.
   */
  function projectOf(ns: Namespace): string {
    return ns.projectSlug ?? '';
  }

  async function handleAssignProject(ns: Namespace, projectSlug: string) {
    if (projectSlug === projectOf(ns)) return;

    assigningNamespace = ns.name;
    assignResult = null;
    try {
      const result = await setNamespaceProject(ns.name, projectSlug);

      // Registry secrets are re-synced as a side effect of the move. Saying so
      // explicitly beats a silent success, because it explains why new pull
      // secrets appeared in the namespace.
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

  // Reactive role state from the auth store
  const isAdmin = $derived($auth.user?.roles.includes('admin') ?? false);

  // State
  let showCreateModal = $state(false);
  let showDeleteConfirm = $state(false);
  let namespaceToDelete = $state('');
  /** Typed confirmation guarding an irreversible delete. */
  let deleteConfirmText = $state('');
  let isSubmitting = $state(false);
  let errorMsg = $state('');

  // Form State
  let name = $state('');
  let displayName = $state('');
  let description = $state('');
  let labelsInput = $state(''); // format: k1=v1,k2=v2
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
    const parts = input.split(',');
    for (const part of parts) {
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
      const labels = parseKeyValues(labelsInput);
      const annotations = parseKeyValues(annotationsInput);
      await createNamespace(name, displayName, description, labels, annotations);
      queryClient.invalidateQueries({ queryKey: ['namespaces'] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showCreateModal = false;
      resetForm();
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
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showDeleteConfirm = false;
      namespaceToDelete = '';
      deleteConfirmText = '';
      // Confirmed by toast rather than inline: the dialog that would have
      // shown the message is gone by the time the delete succeeds.
      toasts.success(`Namespace ${deleted} deleted.`);
    } catch (err) {
      // Kept inline as well as toasted — the dialog stays open on failure, and
      // the reason belongs next to the button that failed.
      errorMsg = err instanceof Error ? err.message : 'Failed to delete namespace';
      toastError(err, `Could not delete namespace ${deleted}.`);
    } finally {
      isSubmitting = false;
    }
  }
</script>

<div class="space-y-6">
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Namespaces</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Manage isolated virtual clusters (namespaces) for your apps and teams.
      </p>
    </div>
    <div class="flex flex-wrap items-center gap-3">
      <button
        onclick={() => namespacesQuery.refetch()}
        class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent hover:text-accent-foreground"
      >
        <RefreshCw class="mr-2 h-4 w-4" />
        Refresh
      </button>
      {#if isAdmin}
        <button
          onclick={() => { resetForm(); showCreateModal = true; }}
          class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          <Plus class="mr-2 h-4 w-4" />
          Create Namespace
        </button>
      {/if}
    </div>
  </div>

  {#if namespacesQuery.isPending}
    <div class="space-y-4">
      <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if namespacesQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive">
          Error loading namespaces: {namespacesQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !namespacesQuery.data || namespacesQuery.data.namespaces.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <Layers class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No namespaces found</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Create your first Kubernetes tenant environment to deploy applications.
      </p>
      {#if isAdmin}
        <button
          onclick={() => showCreateModal = true}
          class="mt-4 inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Create Namespace
        </button>
      {/if}
    </div>
  {:else}
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {#each namespacesQuery.data.namespaces as ns}
        <Card>
          <CardHeader>
            <div class="flex items-start justify-between">
              <div>
                <CardTitle class="text-lg">{ns.displayName}</CardTitle>
                <p class="text-xs font-mono text-muted-foreground mt-0.5">{ns.name}</p>
              </div>
              <span
                class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize
                {ns.status === 'active' ? 'bg-emerald-500/10 text-emerald-500' : 'bg-amber-500/10 text-amber-500'}"
              >
                {ns.status}
              </span>
            </div>
          </CardHeader>
          <CardContent class="space-y-4">
            <p class="text-sm text-muted-foreground line-clamp-2 min-h-10">
              {ns.description || 'No description provided.'}
            </p>

            <div class="space-y-1.5 border-t border-border pt-3 text-xs">
              <div class="flex items-center justify-between gap-2">
                <span class="flex items-center gap-1.5 text-muted-foreground">
                  <FolderGit2 class="h-3.5 w-3.5" /> Project:
                </span>
                {#if isAdmin}
                  <select
                    value={projectOf(ns)}
                    disabled={assigningNamespace === ns.name || projectsQuery.isPending}
                    onchange={(e) => handleAssignProject(ns, e.currentTarget.value)}
                    aria-label="Project for namespace {ns.name}"
                    class="h-7 min-w-0 flex-1 rounded-md border border-input bg-background px-2 text-right text-xs font-medium focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-primary disabled:opacity-60"
                  >
                    <option value="">Unassigned</option>
                    {#each projects as project}
                      <option value={project.slug}>{project.name}</option>
                    {/each}
                  </select>
                {:else}
                  <span class="font-medium text-foreground">{projectOf(ns) || 'Unassigned'}</span>
                {/if}
              </div>

              {#if assigningNamespace === ns.name}
                <p class="flex items-center justify-end gap-1.5 text-muted-foreground">
                  <RefreshCw class="h-3 w-3 animate-spin" /> Updating…
                </p>
              {:else if assignResult?.namespace === ns.name}
                <p
                  role="status"
                  class="text-right {assignResult.ok ? 'text-success' : 'text-destructive'}"
                >
                  {assignResult.message}
                </p>
              {/if}

              <div class="flex justify-between">
                <span class="text-muted-foreground">Owner:</span>
                <span class="font-medium text-foreground">{ns.ownerEmail}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-muted-foreground">Created:</span>
                <span class="font-medium text-foreground">
                  {new Date(ns.createdAt).toLocaleDateString()}
                </span>
              </div>
            </div>

            {#if isAdmin}
              <div class="flex items-center justify-end gap-2 border-t border-border pt-3">
                <button
                  onclick={() => { namespaceToDelete = ns.name; showDeleteConfirm = true; }}
                  class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background hover:bg-destructive/10 hover:text-destructive text-muted-foreground transition-colors"
                  title="Delete Namespace"
                >
                  <Trash2 class="h-4 w-4" />
                </button>
              </div>
            {/if}
          </CardContent>
        </Card>
      {/each}
    </div>
  {/if}
</div>

<!-- Create Namespace Modal -->
{#if showCreateModal && isAdmin}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm">
    <div class="max-h-[90vh] overflow-y-auto w-full max-w-lg rounded-xl border border-border bg-card p-6 shadow-lg">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <h2 class="text-lg font-semibold">Create Tenant Namespace</h2>
        <button
          onclick={() => showCreateModal = false}
          class="rounded-md p-1 hover:bg-accent hover:text-accent-foreground text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <form onsubmit={handleCreate} class="mt-4 space-y-4">
        {#if errorMsg}
          <p class="text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
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
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
          />
          <p class="text-xs text-muted-foreground">lowercase alphanumeric, dashes allowed.</p>
        </div>

        <div class="space-y-1.5">
          <label for="displayName" class="text-sm font-medium">Display Name (User friendly)</label>
          <input
            id="displayName"
            type="text"
            required
            placeholder="e.g. Billing System Development"
            bind:value={displayName}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
          />
        </div>

        <div class="space-y-1.5">
          <label for="description" class="text-sm font-medium">Description</label>
          <textarea
            id="description"
            rows="3"
            placeholder="Describe the environment purpose, target team, etc."
            bind:value={description}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
          ></textarea>
        </div>

        <div class="space-y-1.5">
          <label for="labels" class="text-sm font-medium">Owner Labels (comma separated)</label>
          <input
            id="labels"
            type="text"
            placeholder="team=finance,tier=critical"
            bind:value={labelsInput}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
          />
        </div>

        <div class="space-y-1.5">
          <label for="annotations" class="text-sm font-medium">Annotations (comma separated)</label>
          <input
            id="annotations"
            type="text"
            placeholder="contact=slack-channel,notify=email"
            bind:value={annotationsInput}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
          />
        </div>

        <div class="flex justify-end gap-3 pt-3 border-t border-border">
          <button
            type="button"
            onclick={() => showCreateModal = false}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent hover:text-accent-foreground"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isSubmitting}
            class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {isSubmitting ? 'Creating...' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<!-- Delete Confirmation Modal -->
<Modal
  open={showDeleteConfirm && isAdmin}
  title="Delete namespace {namespaceToDelete}?"
  description="This terminates every workload, PVC, ConfigMap and Service inside it. It cannot be undone."
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
  <!-- A typed confirmation, because the button alone is one stray click away
       from destroying a tenant's entire namespace. -->
  <input
    type="text"
    bind:value={deleteConfirmText}
    placeholder={namespaceToDelete}
    autocomplete="off"
    aria-label="Type the namespace name to confirm deletion"
    class="mt-3 h-9 w-full rounded-md border border-input bg-background px-3 font-mono text-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
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
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
    >
      Cancel
    </button>
    <button
      type="button"
      disabled={isSubmitting || deleteConfirmText !== namespaceToDelete}
      onclick={handleDelete}
      class="inline-flex h-9 items-center justify-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-destructive disabled:cursor-not-allowed disabled:opacity-50"
    >
      {isSubmitting ? 'Deleting…' : 'Delete namespace'}
    </button>
  {/snippet}
</Modal>
