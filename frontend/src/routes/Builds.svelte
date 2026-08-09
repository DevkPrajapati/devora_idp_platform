<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import BuildLivePanel from '$components/BuildLivePanel.svelte';
  import { auth } from '$stores/auth';
  import { listProjects } from '$services/projects';
  import { listRegistryCredentials } from '$services/registry';
  import {
    listGitRepositories,
    saveGitRepository,
    deleteGitRepository,
    triggerBuild,
    retryBuild,
    listBuilds,
    isBuildActive,
    GIT_PROVIDERS,
    type GitRepository,
    type Build,
  } from '$services/builds';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    GitBranch, Plus, Trash2, X, RefreshCw, AlertCircle, CheckCircle2,
    Play, Undo2, Terminal, Copy, Loader2,
  } from '@lucide/svelte';

  const queryClient = useQueryClient();

  let selectedProject = $state('');
  let selectedRepo = $state('');

  const projectsQuery = createQuery(() => ({
    queryKey: ['projects'],
    queryFn: () => listProjects(1, 100),
  }));

  $effect(() => {
    if (projectsQuery.data && projectsQuery.data.projects.length > 0 && !selectedProject) {
      selectedProject = projectsQuery.data.projects[0].slug;
    }
  });

  const reposQuery = createQuery(() => ({
    queryKey: ['git-repositories', selectedProject],
    queryFn: () => listGitRepositories(selectedProject),
    enabled: !!selectedProject,
  }));

  $effect(() => {
    const repos = reposQuery.data ?? [];
    if (repos.length > 0 && !repos.some((r) => r.name === selectedRepo)) {
      selectedRepo = repos[0].name;
    }
  });

  const credentialsQuery = createQuery(() => ({
    queryKey: ['registry-credentials', selectedProject],
    queryFn: () => listRegistryCredentials(selectedProject),
    enabled: !!selectedProject,
  }));

  // Plain flag (not $state) so toggling it does not recreate the query options
  // object and thrash the router while a build is running.
  let forceBuildPoll = false;

  const buildsQuery = createQuery(() => ({
    queryKey: ['builds', selectedProject, selectedRepo],
    queryFn: () => listBuilds(selectedProject, selectedRepo),
    enabled: !!selectedProject && !!selectedRepo,
    // Builds finish asynchronously in the cluster, so the list is polled while
    // any of them is still moving (or we just kicked one off).
    refetchInterval: (query: any) => {
      const builds = (query.state.data?.builds ?? []) as Build[];
      if (forceBuildPoll || builds.some((b: Build) => isBuildActive(b.status))) return 2500;
      return false;
    },
  }));

  $effect(() => {
    const builds = buildsQuery.data?.builds ?? [];
    if (forceBuildPoll && builds.length > 0 && !builds.some((b) => isBuildActive(b.status))) {
      forceBuildPoll = false;
    }
  });

  const activeRepo = $derived((reposQuery.data ?? []).find((r) => r.name === selectedRepo) ?? null);
  const canWrite = $derived(
    $auth.user?.roles.includes('admin') || $auth.user?.roles.includes('developer') || false,
  );

  let errorMsg = $state('');
  let noticeMsg = $state('');
  let isSubmitting = $state(false);
  let busyBuild = $state<number | null>(null);

  // Repository form
  let showRepoModal = $state(false);
  let editingExisting = $state(false);
  let form = $state({
    name: '', provider: 'github', cloneUrl: '', defaultBranch: 'main',
    dockerfilePath: 'Dockerfile', buildContext: '.', imageRepository: '',
    registryCredential: '', token: '', webhookSecret: '',
    autoDeploy: false, targetNamespace: '', targetDeployment: '',
  });

  let showDeleteModal = $state(false);
  let repoToDelete = $state('');

  // Live build panel — opened by Build now / Logs. Closing it is sticky so
  // polling does not keep forcing the panel open (that blocked navigation).
  let watchedBuild = $state<Build | null>(null);
  let panelDismissedFor = $state<number | null>(null);

  function watchBuild(build: Build) {
    panelDismissedFor = null;
    watchedBuild = build;
  }

  function closeWatchPanel() {
    if (watchedBuild) panelDismissedFor = watchedBuild.number;
    watchedBuild = null;
  }

  $effect(() => {
    const builds = buildsQuery.data?.builds ?? [];
    if (watchedBuild) {
      const updated = builds.find((b) => b.number === watchedBuild!.number);
      // Only write when something visible changed — avoids re-rendering the
      // live panel on every poll tick.
      if (
        updated &&
        (updated.status !== watchedBuild.status ||
          updated.errorMessage !== watchedBuild.errorMessage ||
          updated.imageTag !== watchedBuild.imageTag ||
          updated.jobName !== watchedBuild.jobName)
      ) {
        watchedBuild = updated;
      }
      return;
    }
    const active = builds.find((b) => isBuildActive(b.status));
    if (active && panelDismissedFor !== active.number) {
      watchedBuild = active;
    }
  });

  const providerHint = $derived(
    GIT_PROVIDERS.find((p) => p.value === form.provider)?.secretHint ?? '',
  );

  function resetForm() {
    form = {
      name: '', provider: 'github', cloneUrl: '', defaultBranch: 'main',
      dockerfilePath: 'Dockerfile', buildContext: '.', imageRepository: '',
      registryCredential: (credentialsQuery.data ?? [])[0]?.name ?? '',
      token: '', webhookSecret: '',
      autoDeploy: false, targetNamespace: '', targetDeployment: '',
    };
    editingExisting = false;
    errorMsg = '';
  }

  function openCreate() {
    resetForm();
    noticeMsg = '';
    showRepoModal = true;
  }

  function openEdit(repo: GitRepository) {
    form = {
      name: repo.name, provider: repo.provider, cloneUrl: repo.cloneUrl,
      defaultBranch: repo.defaultBranch, dockerfilePath: repo.dockerfilePath,
      buildContext: repo.buildContext, imageRepository: repo.imageRepository,
      registryCredential: repo.registryCredential,
      // Blank: the server never sends stored secrets, and blank means "keep".
      token: '', webhookSecret: '',
      autoDeploy: repo.autoDeploy, targetNamespace: repo.targetNamespace,
      targetDeployment: repo.targetDeployment,
    };
    editingExisting = true;
    errorMsg = '';
    noticeMsg = '';
    showRepoModal = true;
  }

  async function handleSaveRepo(e: Event) {
    e.preventDefault();
    if (!selectedProject) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      const saved = await saveGitRepository({ projectSlug: selectedProject, ...form });
      queryClient.invalidateQueries({ queryKey: ['git-repositories', selectedProject] });
      selectedRepo = saved.name;
      showRepoModal = false;
      noticeMsg = saved.hasWebhookSecret
        ? `Saved. Add the webhook URL below to ${saved.provider} to build on push.`
        : 'Saved. Without a webhook secret, pushes will not trigger builds — only manual builds work.';
    } catch (err: any) {
      errorMsg = err.message || 'Failed to save repository';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDeleteRepo() {
    if (!selectedProject || !repoToDelete) return;
    isSubmitting = true;
    errorMsg = '';
    try {
      await deleteGitRepository(selectedProject, repoToDelete);
      queryClient.invalidateQueries({ queryKey: ['git-repositories', selectedProject] });
      selectedRepo = '';
      showDeleteModal = false;
      repoToDelete = '';
    } catch (err: any) {
      errorMsg = err.message || 'Failed to delete repository';
    } finally {
      isSubmitting = false;
    }
  }

  /** Show the new build in the table immediately; don't wait for a refetch. */
  function upsertBuildInCache(started: Build) {
    const key = ['builds', selectedProject, selectedRepo] as const;
    queryClient.setQueryData(key, (old: { builds: Build[]; totalCount: number; buildNamespace: string } | undefined) => {
      const prev = old?.builds ?? [];
      const builds = [started, ...prev.filter((b) => b.number !== started.number)];
      return {
        builds,
        totalCount: Math.max(old?.totalCount ?? 0, builds.length),
        buildNamespace: old?.buildNamespace ?? '',
      };
    });
  }

  function kickBuildPolling() {
    forceBuildPoll = true;
    // Do not await — a hung refetch used to keep the page feeling stuck and
    // blocked navigating away after "Build now".
    void queryClient.invalidateQueries({ queryKey: ['builds', selectedProject, selectedRepo] });
    void buildsQuery.refetch();
  }

  async function handleTrigger() {
    if (!activeRepo) return;
    if (!activeRepo.hasToken) {
      errorMsg = 'This repository needs a Git access token before it can clone. Open Edit, paste a GitHub PAT with repo read access, then save.';
      return;
    }
    isSubmitting = true;
    errorMsg = '';
    noticeMsg = '';
    try {
      const started = await triggerBuild(selectedProject, activeRepo.name);
      upsertBuildInCache(started);
      watchBuild(started);
      noticeMsg = `Build #${started.number} started on ${started.branch}.`;
      kickBuildPolling();
    } catch (err: any) {
      errorMsg = err.message || 'Failed to start build';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleRetry(build: Build) {
    if (!activeRepo) return;
    if (!activeRepo.hasToken) {
      errorMsg = 'Add a Git access token in Edit before retrying — clone is failing without it.';
      return;
    }
    busyBuild = build.number;
    errorMsg = '';
    noticeMsg = '';
    try {
      const started = await retryBuild(selectedProject, activeRepo.name, build.number);
      upsertBuildInCache(started);
      watchBuild(started);
      noticeMsg = `Retried #${build.number} as build #${started.number}.`;
      kickBuildPolling();
    } catch (err: any) {
      errorMsg = err.message || 'Failed to retry build';
    } finally {
      busyBuild = null;
    }
  }

  function copyWebhookUrl() {
    if (!activeRepo?.webhookUrl) return;
    navigator.clipboard?.writeText(activeRepo.webhookUrl);
    noticeMsg = 'Webhook URL copied.';
  }

  function statusClass(status: string): string {
    switch (status) {
      case 'succeeded': return 'bg-emerald-500/10 text-emerald-500';
      case 'failed': return 'bg-destructive/10 text-destructive';
      case 'running': return 'bg-indigo-500/10 text-indigo-500';
      case 'cancelled': return 'bg-muted text-muted-foreground';
      default: return 'bg-amber-500/10 text-amber-500';
    }
  }

  function duration(build: Build): string {
    if (!build.startedAt) return '—';
    const end = build.finishedAt ? new Date(build.finishedAt) : new Date();
    const seconds = Math.max(0, Math.round((end.getTime() - new Date(build.startedAt).getTime()) / 1000));
    return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
  }
</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Builds</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Clone, build with Kaniko, push to your registry, and optionally deploy — no local Docker required.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <select
        bind:value={selectedProject}
        class="h-9 rounded-md border border-input bg-background px-3 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
      >
        {#if projectsQuery.isPending}
          <option>Loading projects...</option>
        {:else if projectsQuery.data}
          {#each projectsQuery.data.projects as p}
            <option value={p.slug}>{p.name} ({p.slug})</option>
          {/each}
        {/if}
      </select>

      {#if (reposQuery.data ?? []).length > 0}
        <select
          bind:value={selectedRepo}
          class="h-9 rounded-md border border-input bg-background px-3 text-sm font-mono shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
        >
          {#each reposQuery.data ?? [] as repo}
            <option value={repo.name}>{repo.name}</option>
          {/each}
        </select>
      {/if}

      <button
        onclick={() => buildsQuery.refetch()}
        disabled={!selectedRepo}
        aria-label="Refresh builds"
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
          Add Repository
        </button>
      {/if}
    </div>
  </div>

  {#if errorMsg}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-4"><p class="text-sm text-destructive">{errorMsg}</p></CardContent>
    </Card>
  {/if}
  {#if noticeMsg}
    <Card class="border-emerald-500/40 bg-emerald-500/5">
      <CardContent class="py-4">
        <p class="text-sm text-emerald-600 dark:text-emerald-400">{noticeMsg}</p>
      </CardContent>
    </Card>
  {/if}

  {#if !selectedProject}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <AlertCircle class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">Select a project</h3>
    </div>
  {:else if reposQuery.isPending}
    <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
  {:else if (reposQuery.data ?? []).length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <GitBranch class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No repositories connected</h3>
      <p class="mt-2 max-w-md text-sm text-muted-foreground">
        Connect a GitHub, GitLab or Bitbucket repository to build images in-cluster and deploy them
        automatically. Requires a registry credential for the push.
      </p>
      {#if canWrite}
        <button
          onclick={openCreate}
          class="mt-4 inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Add Repository
        </button>
      {/if}
    </div>
  {:else if activeRepo}
    <!-- Repository summary -->
    <Card>
      <CardContent class="space-y-3 py-5">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="space-y-1">
            <p class="font-mono text-sm font-medium">{activeRepo.cloneUrl}</p>
            <p class="text-xs text-muted-foreground">
              Branch <span class="font-mono">{activeRepo.defaultBranch}</span> ·
              {activeRepo.dockerfilePath} ·
              pushes to <span class="font-mono">{activeRepo.imageRepository}</span>
              via credential <span class="font-mono">{activeRepo.registryCredential}</span>
            </p>
            <div class="flex flex-wrap gap-1.5 pt-1">
              {#if activeRepo.autoDeploy}
                <span class="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[11px] font-medium text-emerald-500">
                  auto-deploys to {activeRepo.targetNamespace}/{activeRepo.targetDeployment}
                </span>
              {:else}
                <span class="rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                  build only
                </span>
              {/if}
              {#if !activeRepo.hasToken}
                <span class="rounded-full bg-destructive/10 px-2 py-0.5 text-[11px] font-medium text-destructive">
                  git token required — clone will fail without it
                </span>
              {/if}
              {#if !activeRepo.hasWebhookSecret}
                <span class="rounded-full bg-amber-500/10 px-2 py-0.5 text-[11px] font-medium text-amber-600 dark:text-amber-400">
                  no webhook secret — pushes will not build
                </span>
              {/if}
            </div>
          </div>

          {#if canWrite}
            <div class="flex flex-wrap gap-2">
              <button
                onclick={handleTrigger}
                disabled={isSubmitting}
                class="inline-flex h-9 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
              >
                {#if isSubmitting}
                  <Loader2 class="h-3.5 w-3.5 animate-spin" />
                  Starting…
                {:else}
                  <Play class="h-3.5 w-3.5" />
                  Build now
                {/if}
              </button>
              <button
                onclick={() => openEdit(activeRepo)}
                class="inline-flex h-9 items-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent"
              >
                Edit
              </button>
              <button
                onclick={() => { repoToDelete = activeRepo.name; showDeleteModal = true; }}
                aria-label="Delete repository"
                class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
              >
                <Trash2 class="h-4 w-4" />
              </button>
            </div>
          {/if}
        </div>

        {#if activeRepo.webhookUrl}
          <div class="flex items-center gap-2 rounded-md bg-muted/50 p-2.5">
            <span class="shrink-0 text-xs font-medium text-muted-foreground">Webhook URL</span>
            <code class="flex-1 truncate text-xs">{activeRepo.webhookUrl}</code>
            <button
              onclick={copyWebhookUrl}
              aria-label="Copy webhook URL"
              class="rounded p-1 text-muted-foreground hover:bg-accent"
            >
              <Copy class="h-3.5 w-3.5" />
            </button>
          </div>
        {/if}
      </CardContent>
    </Card>

    <!-- Build history -->
    {#if buildsQuery.isPending}
      <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
    {:else if (buildsQuery.data?.builds ?? []).length === 0}
      <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-12 text-center">
        <GitBranch class="mb-3 h-10 w-10 text-muted-foreground/40" />
        <p class="text-sm font-medium">No builds yet</p>
        <p class="mt-1 text-xs text-muted-foreground">
          Start one with "Build now", or push to {activeRepo.defaultBranch}.
        </p>
      </div>
    {:else}
      <div class="overflow-x-auto rounded-lg border border-border bg-card">
        <table class="w-full min-w-[64rem] text-left border-collapse">
          <thead>
            <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
              <th class="px-4 py-3">Build</th>
              <th class="px-4 py-3">Branch / Commit</th>
              <th class="px-4 py-3">Image tag</th>
              <th class="px-4 py-3">Status</th>
              <th class="px-4 py-3">Trigger</th>
              <th class="px-4 py-3">Duration</th>
              <th class="px-4 py-3 text-right">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border text-sm">
            {#each buildsQuery.data?.builds ?? [] as build}
              <tr class="hover:bg-accent/20 transition-colors">
                <td class="px-4 py-3 font-medium">#{build.number}</td>
                <td class="px-4 py-3 text-xs">
                  <span class="font-mono">{build.branch}</span>
                  {#if build.commitSha}
                    <p class="mt-0.5 font-mono text-[11px] text-muted-foreground">
                      {build.commitSha.slice(0, 7)}
                    </p>
                  {/if}
                </td>
                <td class="px-4 py-3 font-mono text-xs text-muted-foreground">{build.imageTag || '—'}</td>
                <td class="px-4 py-3">
                  <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium {statusClass(build.status)}">
                    {#if isBuildActive(build.status)}
                      <Loader2 class="h-3 w-3 animate-spin" />
                    {:else if build.status === 'succeeded'}
                      <CheckCircle2 class="h-3 w-3" />
                    {:else if build.status === 'failed'}
                      <AlertCircle class="h-3 w-3" />
                    {/if}
                    {build.status}
                  </span>
                  {#if build.errorMessage}
                    <p class="mt-1 max-w-[18rem] break-words text-[11px] text-destructive">{build.errorMessage}</p>
                  {/if}
                </td>
                <td class="px-4 py-3 text-xs text-muted-foreground">
                  {build.triggerType}
                  {#if build.triggeredBy && build.triggeredBy !== 'webhook'}
                    <p class="mt-0.5 text-[11px]">{build.triggeredBy}</p>
                  {/if}
                </td>
                <td class="px-4 py-3 text-xs text-muted-foreground">{duration(build)}</td>
                <td class="px-4 py-3 text-right">
                  <div class="flex items-center justify-end gap-2">
                    <button
                      onclick={() => watchBuild(build)}
                      class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
                    >
                      <Terminal class="h-3 w-3" />
                      {isBuildActive(build.status) ? 'Live logs' : 'Logs'}
                    </button>
                    {#if canWrite && !isBuildActive(build.status)}
                      <button
                        onclick={() => handleRetry(build)}
                        disabled={busyBuild !== null}
                        class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent disabled:opacity-50"
                      >
                        <Undo2 class="h-3 w-3" />
                        {busyBuild === build.number ? 'Retrying...' : 'Retry'}
                      </button>
                    {/if}
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    {#if watchedBuild && buildsQuery.data?.buildNamespace}
      <div class="mt-4">
        <!-- Key only by build number. Keying on status remounted the panel on
             every pending→running flip, restarted log streams, and froze nav. -->
        {#key watchedBuild.number}
          <BuildLivePanel
            build={watchedBuild}
            namespace={buildsQuery.data.buildNamespace}
            onClose={closeWatchPanel}
          />
        {/key}
      </div>
    {/if}
  {/if}
</div>

<!-- Repository Modal -->
{#if showRepoModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-xl border border-border bg-card p-6 shadow-lg">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <h2 class="text-lg font-semibold">
          {editingExisting ? `Edit ${form.name}` : 'Connect a Git Repository'}
        </h2>
        <button onclick={() => (showRepoModal = false)} aria-label="Close" class="rounded-md p-1 text-muted-foreground hover:bg-accent">
          <X class="h-5 w-5" />
        </button>
      </div>

      <form onsubmit={handleSaveRepo} class="mt-4 space-y-4">
        {#if errorMsg}
          <p class="rounded-md bg-destructive/10 p-3 text-sm text-destructive">{errorMsg}</p>
        {/if}

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="repoName" class="text-sm font-medium">Name</label>
            <input id="repoName" type="text" required readonly={editingExisting}
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?" maxlength="48" placeholder="api"
              bind:value={form.name}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring read-only:opacity-60" />
          </div>
          <div class="space-y-1.5">
            <label for="provider" class="text-sm font-medium">Provider</label>
            <select id="provider" bind:value={form.provider}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring">
              {#each GIT_PROVIDERS as p}
                <option value={p.value}>{p.label}</option>
              {/each}
            </select>
          </div>
        </div>

        <div class="space-y-1.5">
          <label for="cloneUrl" class="text-sm font-medium">Clone URL (HTTPS)</label>
          <input id="cloneUrl" type="url" required placeholder="https://github.com/acme/api.git"
            bind:value={form.cloneUrl}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
          <p class="text-xs text-muted-foreground">SSH URLs are not supported — builds authenticate with a token.</p>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div class="space-y-1.5">
            <label for="branch" class="text-sm font-medium">Branch</label>
            <input id="branch" type="text" required bind:value={form.defaultBranch}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
          </div>
          <div class="space-y-1.5">
            <label for="dockerfile" class="text-sm font-medium">Dockerfile</label>
            <input id="dockerfile" type="text" bind:value={form.dockerfilePath}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
          </div>
          <div class="space-y-1.5">
            <label for="context" class="text-sm font-medium">Context</label>
            <input id="context" type="text" bind:value={form.buildContext}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="imageRepo" class="text-sm font-medium">Image repository</label>
            <input id="imageRepo" type="text" required placeholder="ghcr.io/acme/api"
              bind:value={form.imageRepository}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
            <p class="text-xs text-muted-foreground">No tag — one is generated per build.</p>
          </div>
          <div class="space-y-1.5">
            <label for="credential" class="text-sm font-medium">Registry credential (push)</label>
            <select id="credential" required bind:value={form.registryCredential}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring">
              <option value="">Select a credential...</option>
              {#each credentialsQuery.data ?? [] as cred}
                <option value={cred.name}>{cred.name} ({cred.registryHost})</option>
              {/each}
            </select>
            {#if (credentialsQuery.data ?? []).length === 0}
              <p class="text-xs text-amber-600 dark:text-amber-400">
                No credentials in this project — add one on the Registry page first.
              </p>
            {/if}
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="token" class="text-sm font-medium">
              Git access token
              <span class="font-normal text-muted-foreground">
                {editingExisting ? '(blank keeps current)' : '(private repos only)'}
              </span>
            </label>
            <input id="token" type="password" autocomplete="new-password" bind:value={form.token}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring" />
          </div>
          <div class="space-y-1.5">
            <label for="webhookSecret" class="text-sm font-medium">
              Webhook secret
              {#if editingExisting}<span class="font-normal text-muted-foreground">(blank keeps current)</span>{/if}
            </label>
            <input id="webhookSecret" type="password" autocomplete="new-password" bind:value={form.webhookSecret}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring" />
            <p class="text-xs text-muted-foreground">{providerHint}</p>
          </div>
        </div>

        <div class="space-y-2 rounded-md border border-border p-3">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input type="checkbox" bind:checked={form.autoDeploy} class="rounded border-input" />
            Deploy automatically after a successful build
          </label>
          {#if form.autoDeploy}
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <input type="text" required placeholder="Target namespace" bind:value={form.targetNamespace}
                class="rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
              <input type="text" required placeholder="Target deployment" bind:value={form.targetDeployment}
                class="rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
            </div>
            <p class="text-xs text-muted-foreground">
              The deployment's image is updated to the newly built tag, which rolls its pods.
            </p>
          {/if}
        </div>

        <div class="flex justify-end gap-3 border-t border-border pt-3">
          <button type="button" onclick={() => (showRepoModal = false)}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent">
            Cancel
          </button>
          <button type="submit" disabled={isSubmitting}
            class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50">
            {isSubmitting ? 'Saving...' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<!-- Delete confirmation -->
{#if showDeleteModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-md rounded-xl border border-border bg-card p-6 shadow-lg">
      <h2 class="text-lg font-semibold">Delete repository</h2>
      <p class="mt-2 text-sm text-muted-foreground">
        Removing <span class="font-mono font-semibold text-rose-500">{repoToDelete}</span> also deletes its
        build history. Images already pushed to the registry are not affected, and any running deployment
        keeps its current image.
      </p>
      {#if errorMsg}
        <p class="mt-3 rounded-md bg-destructive/10 p-3 text-sm text-destructive">{errorMsg}</p>
      {/if}
      <div class="mt-6 flex justify-end gap-3">
        <button type="button" onclick={() => { showDeleteModal = false; repoToDelete = ''; }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent">
          Cancel
        </button>
        <button type="button" disabled={isSubmitting} onclick={handleDeleteRepo}
          class="inline-flex h-9 items-center justify-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50">
          {isSubmitting ? 'Deleting...' : 'Delete Repository'}
        </button>
      </div>
    </div>
  </div>
{/if}
