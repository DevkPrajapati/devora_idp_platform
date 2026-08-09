<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import { auth } from '$stores/auth';
  import { listProjects } from '$services/projects';
  import {
    listRegistryCredentials,
    saveRegistryCredential,
    deleteRegistryCredential,
    testRegistryConnection,
    REGISTRY_PRESETS,
    type RegistryCredential,
  } from '$services/registry';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { KeyRound, Plus, Trash2, X, RefreshCw, AlertCircle, CheckCircle2, ShieldCheck } from '@lucide/svelte';

  const queryClient = useQueryClient();

  let selectedProject = $state('');

  const projectsQuery = createQuery(() => ({
    queryKey: ['projects'],
    queryFn: () => listProjects(1, 100),
  }));

  $effect(() => {
    if (projectsQuery.data && projectsQuery.data.projects.length > 0 && !selectedProject) {
      selectedProject = projectsQuery.data.projects[0].slug;
    }
  });

  const credentialsQuery = createQuery(() => ({
    queryKey: ['registry-credentials', selectedProject],
    queryFn: () => listRegistryCredentials(selectedProject),
    enabled: !!selectedProject,
  }));

  const canWrite = $derived(
    $auth.user?.roles.includes('admin') || $auth.user?.roles.includes('developer') || false
  );

  // Form state
  let showModal = $state(false);
  let isSubmitting = $state(false);
  let isTesting = $state(false);
  let errorMsg = $state('');
  let noticeMsg = $state('');
  let testResult = $state<{ success: boolean; message: string } | null>(null);

  let credName = $state('');
  let registryUrl = $state('docker.io');
  let username = $state('');
  let password = $state('');
  let email = $state('');
  /** Set when editing: the stored password is never sent to the browser, so an
   *  untouched password field means "keep using the saved one". */
  let editingExisting = $state(false);

  let showDeleteModal = $state(false);
  let credentialToDelete = $state('');

  const selectedPreset = $derived(
    REGISTRY_PRESETS.find((p) => p.url && registryUrl.startsWith(p.url))
  );

  function resetForm() {
    credName = '';
    registryUrl = 'docker.io';
    username = '';
    password = '';
    email = '';
    editingExisting = false;
    errorMsg = '';
    testResult = null;
  }

  function openCreate() {
    resetForm();
    noticeMsg = '';
    showModal = true;
  }

  function openEdit(cred: RegistryCredential) {
    resetForm();
    credName = cred.name;
    registryUrl = cred.registryUrl;
    username = cred.username;
    email = cred.email;
    editingExisting = true;
    noticeMsg = '';
    showModal = true;
  }

  function applyPreset(url: string) {
    if (url) registryUrl = url;
    testResult = null;
  }

  async function handleTest() {
    if (!selectedProject) return;
    isTesting = true;
    errorMsg = '';
    testResult = null;

    try {
      const result = await testRegistryConnection({
        projectSlug: selectedProject,
        registryUrl,
        username,
        // An empty password on an existing credential asks the backend to use
        // the stored one, which the browser never receives.
        password,
        name: editingExisting ? credName : '',
      });
      testResult = { success: result.success, message: result.message };
    } catch (err: any) {
      errorMsg = err.message || 'Failed to test connection';
    } finally {
      isTesting = false;
    }
  }

  async function handleSave(e: Event) {
    e.preventDefault();
    if (!selectedProject) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      const result = await saveRegistryCredential({
        projectSlug: selectedProject,
        name: credName,
        registryUrl,
        username,
        password,
        email,
      });
      queryClient.invalidateQueries({ queryKey: ['registry-credentials', selectedProject] });
      // Existing deployments were re-synced server-side, so their pull secrets changed.
      queryClient.invalidateQueries({ queryKey: ['deployments'] });
      showModal = false;
      resetForm();

      noticeMsg =
        result.syncedNamespaces.length === 0
          ? 'Saved, but this project owns no namespaces yet — attach one to the project before the pull secret can be created.'
          : `Saved. Secret written to ${result.syncedNamespaces.join(', ')}` +
            (result.updatedDeployments > 0
              ? `; updated ${result.updatedDeployments} existing deployment(s).`
              : '.');
    } catch (err: any) {
      errorMsg = err.message || 'Failed to save credential';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDelete() {
    if (!selectedProject || !credentialToDelete) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      await deleteRegistryCredential(selectedProject, credentialToDelete);
      queryClient.invalidateQueries({ queryKey: ['registry-credentials', selectedProject] });
      queryClient.invalidateQueries({ queryKey: ['deployments'] });
      showDeleteModal = false;
      credentialToDelete = '';
    } catch (err: any) {
      errorMsg = err.message || 'Failed to delete credential';
    } finally {
      isSubmitting = false;
    }
  }
</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Registry Credentials</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Authenticate against private registries. Credentials are encrypted at rest and published as
        <span class="font-mono text-xs">dockerconfigjson</span> Secrets in every namespace the project owns.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex items-center gap-2">
        <span class="text-sm font-medium text-muted-foreground">Project:</span>
        <select
          bind:value={selectedProject}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
        >
          {#if projectsQuery.isPending}
            <option>Loading projects...</option>
          {:else if projectsQuery.data}
            {#each projectsQuery.data.projects as p}
              <option value={p.slug}>{p.name} ({p.slug})</option>
            {/each}
          {/if}
        </select>
      </div>

      <button
        onclick={() => credentialsQuery.refetch()}
        disabled={!selectedProject}
        aria-label="Refresh credentials"
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground disabled:opacity-50"
      >
        <RefreshCw class="h-4 w-4" />
      </button>

      {#if canWrite}
        <button
          onclick={openCreate}
          disabled={!selectedProject}
          class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          <Plus class="mr-2 h-4 w-4" />
          Add Credential
        </button>
      {/if}
    </div>
  </div>

  {#if noticeMsg}
    <Card class="border-amber-500/40 bg-amber-500/5">
      <CardContent class="py-4">
        <p class="text-sm text-amber-600 dark:text-amber-400">{noticeMsg}</p>
      </CardContent>
    </Card>
  {/if}

  {#if !selectedProject}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <AlertCircle class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">Select a project</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Registry credentials are scoped to a project and inherited by every namespace it owns.
      </p>
    </div>
  {:else if credentialsQuery.isPending}
    <div class="space-y-4">
      <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if credentialsQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive">Error loading credentials: {credentialsQuery.error.message}</p>
      </CardContent>
    </Card>
  {:else if !credentialsQuery.data || credentialsQuery.data.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <KeyRound class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No registry credentials</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-md">
        Deployments in this project can only pull public images. Add a credential to deploy from Docker Hub,
        GHCR, GitLab, ECR, ACR or GCR.
      </p>
      {#if canWrite}
        <button
          onclick={openCreate}
          class="mt-4 inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Add Credential
        </button>
      {/if}
    </div>
  {:else}
    <div class="border border-border rounded-lg bg-card overflow-x-auto">
      <table class="w-full min-w-[52rem] text-left border-collapse">
        <thead>
          <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
            <th class="px-5 py-3">Name</th>
            <th class="px-5 py-3">Registry</th>
            <th class="px-5 py-3">Username</th>
            <th class="px-5 py-3">Kubernetes Secret</th>
            <th class="px-5 py-3">Namespaces</th>
            {#if canWrite}
              <th class="px-5 py-3 text-right">Actions</th>
            {/if}
          </tr>
        </thead>
        <tbody class="divide-y divide-border text-sm">
          {#each credentialsQuery.data as c}
            <tr class="hover:bg-accent/20 transition-colors">
              <td class="px-5 py-3.5 font-medium text-foreground">{c.name}</td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                {c.registryUrl}
                {#if c.registryHost && c.registryHost !== c.registryUrl}
                  <p class="mt-0.5 text-[11px] text-muted-foreground/70">auth key: {c.registryHost}</p>
                {/if}
              </td>
              <td class="px-5 py-3.5 text-muted-foreground">{c.username}</td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">{c.secretName}</td>
              <td class="px-5 py-3.5">
                {#if c.namespaces.length === 0}
                  <span class="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-600 dark:text-amber-400">
                    <AlertCircle class="h-3 w-3" />
                    not deployed
                  </span>
                {:else}
                  <span class="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-500">
                    <ShieldCheck class="h-3 w-3" />
                    {c.namespaces.length} namespace{c.namespaces.length === 1 ? '' : 's'}
                  </span>
                  <p class="mt-1 font-mono text-[11px] text-muted-foreground">{c.namespaces.join(', ')}</p>
                {/if}
              </td>
              {#if canWrite}
                <td class="px-5 py-3.5 text-right">
                  <div class="flex items-center justify-end gap-2">
                    <button
                      onclick={() => openEdit(c)}
                      class="inline-flex h-8 items-center rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
                    >
                      Edit
                    </button>
                    <button
                      onclick={() => { credentialToDelete = c.name; showDeleteModal = true; }}
                      aria-label={`Delete ${c.name}`}
                      class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors"
                    >
                      <Trash2 class="h-4 w-4" />
                    </button>
                  </div>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- Add / Edit Credential Modal -->
{#if showModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-xl rounded-xl border border-border bg-card p-6 shadow-lg max-h-[90vh] overflow-y-auto">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <h2 class="text-lg font-semibold">
          {editingExisting ? `Edit credential — ${credName}` : 'Add Registry Credential'}
        </h2>
        <button
          onclick={() => (showModal = false)}
          aria-label="Close"
          class="rounded-md p-1 hover:bg-accent text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <form onsubmit={handleSave} class="mt-4 space-y-4">
        {#if errorMsg}
          <p class="text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
        {/if}

        {#if testResult}
          <p
            class="flex items-start gap-2 rounded-md p-3 text-sm {testResult.success
              ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
              : 'bg-destructive/10 text-destructive'}"
          >
            {#if testResult.success}
              <CheckCircle2 class="mt-0.5 h-4 w-4 shrink-0" />
            {:else}
              <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
            {/if}
            <span>{testResult.message}</span>
          </p>
        {/if}

        <div class="space-y-1.5">
          <span class="text-sm font-medium">Registry</span>
          <div class="flex flex-wrap gap-1.5">
            {#each REGISTRY_PRESETS as preset}
              <button
                type="button"
                onclick={() => applyPreset(preset.url)}
                class="rounded-full border border-input px-2.5 py-1 text-xs font-medium text-muted-foreground hover:bg-accent hover:text-accent-foreground"
              >
                {preset.label}
              </button>
            {/each}
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="credName" class="text-sm font-medium">Credential Name</label>
            <input
              id="credName"
              type="text"
              required
              readonly={editingExisting}
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              maxlength="48"
              placeholder="e.g. dockerhub"
              bind:value={credName}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring read-only:opacity-60"
            />
            <p class="text-xs text-muted-foreground">
              Becomes Secret <span class="font-mono">idp-registry-{credName || 'name'}</span>
            </p>
          </div>
          <div class="space-y-1.5">
            <label for="registryUrl" class="text-sm font-medium">Registry URL</label>
            <input
              id="registryUrl"
              type="text"
              required
              placeholder="ghcr.io"
              bind:value={registryUrl}
              oninput={() => (testResult = null)}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
            <p class="text-xs text-muted-foreground">Host or host:port. Scheme optional.</p>
          </div>
        </div>

        {#if selectedPreset}
          <p class="rounded-md bg-muted/50 p-2.5 text-xs text-muted-foreground">{selectedPreset.hint}</p>
        {/if}

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="username" class="text-sm font-medium">Username</label>
            <input
              id="username"
              type="text"
              required
              autocomplete="off"
              bind:value={username}
              oninput={() => (testResult = null)}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
          <div class="space-y-1.5">
            <label for="password" class="text-sm font-medium">
              Password / Access Token
              {#if editingExisting}
                <span class="font-normal text-muted-foreground">(blank keeps current)</span>
              {/if}
            </label>
            <input
              id="password"
              type="password"
              required={!editingExisting}
              autocomplete="new-password"
              bind:value={password}
              oninput={() => (testResult = null)}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
        </div>

        <div class="space-y-1.5">
          <label for="email" class="text-sm font-medium">
            Email <span class="font-normal text-muted-foreground">(optional)</span>
          </label>
          <input
            id="email"
            type="email"
            bind:value={email}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <div class="flex flex-wrap justify-between gap-3 pt-3 border-t border-border">
          <button
            type="button"
            onclick={handleTest}
            disabled={isTesting || !registryUrl || !username}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent disabled:opacity-50"
          >
            {isTesting ? 'Testing...' : 'Test Connection'}
          </button>

          <div class="flex gap-3">
            <button
              type="button"
              onclick={() => (showModal = false)}
              class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {isSubmitting ? 'Saving...' : 'Save'}
            </button>
          </div>
        </div>
      </form>
    </div>
  </div>
{/if}

<!-- Delete Confirmation -->
{#if showDeleteModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-md rounded-xl border border-border bg-card p-6 shadow-lg">
      <h2 class="text-lg font-semibold">Delete registry credential</h2>
      <p class="mt-2 text-sm text-muted-foreground">
        Removing <span class="font-mono font-semibold text-rose-500">{credentialToDelete}</span> deletes its
        Secret from every namespace in this project and drops it from every deployment's
        <span class="font-mono">imagePullSecrets</span>. Running pods keep running, but the next image pull
        will fail if the image is private.
      </p>

      {#if errorMsg}
        <p class="mt-3 text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
      {/if}

      <div class="mt-6 flex justify-end gap-3">
        <button
          type="button"
          onclick={() => { showDeleteModal = false; credentialToDelete = ''; }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
        >
          Cancel
        </button>
        <button
          type="button"
          disabled={isSubmitting}
          onclick={handleDelete}
          class="inline-flex h-9 items-center justify-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50"
        >
          {isSubmitting ? 'Deleting...' : 'Delete Credential'}
        </button>
      </div>
    </div>
  </div>
{/if}
