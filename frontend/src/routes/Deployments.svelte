<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { auth } from '$stores/auth';
  import { listNamespaces } from '$services/namespaces';
  import {
    listDeployments,
    createDeployment,
    scaleDeployment,
    restartDeployment,
    deleteDeployment,
    getDeploymentConfig,
    updateDeploymentConfig,
    listRollouts,
    rollbackDeployment,
    listDeploymentTemplates,
    emptyProbe,
    emptyResources,
    probeOrUndefined,
    type Rollout,
    type ResourceLimits,
    type DeploymentTemplate,
    type Deployment,
    type EnvVar,
    type SecretVar,
    type Probe
  } from '$services/deployments';
  import { openWorkload } from '$services/client';
  import { isDatabaseImage } from '$services/databases';
  import { router } from '$stores/router';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Container, Plus, Trash2, X, RefreshCw, Scale, Play, AlertCircle, Settings2, KeyRound, ExternalLink, HeartPulse, History, Undo2, Layers, Cylinder, Terminal } from '@lucide/svelte';

  const queryClient = useQueryClient();
  /** Workload currently being opened, so only its own button shows a spinner. */
  let openingApp = $state('');
  /** Surfaced inline rather than swallowed — a failed open used to look like a dead link. */
  let openAppError = $state('');

  /**
   * Always open via sticky localhost port-forward. Ingress hostnames
   * (*.idp.local) need /etc/hosts + minikube tunnel and otherwise show NXDOMAIN.
   */
  async function handleOpenApp(namespace: string, name: string) {
    openingApp = `${namespace}/${name}`;
    openAppError = '';
    try {
      await openWorkload(namespace, name);
    } catch (err) {
      openAppError = err instanceof Error ? err.message : 'Could not open this app.';
    } finally {
      openingApp = '';
    }
  }

  // Selected namespace state
  let selectedNamespace = $state('');

  // Fetch Namespaces for dropdown
  const namespacesQuery = createQuery(() => ({
    queryKey: ['namespaces'],
    queryFn: () => listNamespaces(1, 100),
  }));

  // Automatically select first namespace once loaded
  $effect(() => {
    if (namespacesQuery.data && namespacesQuery.data.namespaces.length > 0 && !selectedNamespace) {
      selectedNamespace = namespacesQuery.data.namespaces[0].name;
    }
  });

  // Fetch Deployments in selected namespace
  const deploymentsQuery = createQuery(() => ({
    queryKey: ['deployments', selectedNamespace],
    queryFn: () => listDeployments(selectedNamespace),
    enabled: !!selectedNamespace,
    refetchInterval: 10000,
  }));

  // Reactive role state from the auth store
  const canWrite = $derived(
    $auth.user?.roles.includes('admin') || $auth.user?.roles.includes('developer') || false
  );

  // Modals & form state
  let showCreateModal = $state(false);
  let showScaleModal = $state(false);
  let showDeleteModal = $state(false);
  let isSubmitting = $state(false);
  let errorMsg = $state('');

  // Deploy Form state
  let appName = $state('');
  let appImage = $state('');
  let appReplicas = $state(1);
  let appPort = $state(80);
  let appServiceType = $state('NodePort');
  let configVars = $state<EnvVar[]>([{ key: '', value: '' }]);
  let secretVars = $state<EnvVar[]>([]);
  let appHostname = $state('');
  let appIngressDisabled = $state(false);
  let readinessProbe = $state<Probe>(emptyProbe());
  let livenessProbe = $state<Probe>(emptyProbe());
  let showAdvanced = $state(false);
  let appResources = $state<ResourceLimits>(emptyResources());
  let selectedTemplateId = $state('');

  const templatesQuery = createQuery(() => ({
    queryKey: ['deployment-templates'],
    // The catalogue is static server-side data, so it never needs refetching.
    queryFn: listDeploymentTemplates,
    staleTime: Infinity,
  }));

  const activeTemplate = $derived(
    (templatesQuery.data ?? []).find((t) => t.id === selectedTemplateId) ?? null,
  );

  /**
   * Applies a template's defaults. Name and image are left alone — those are
   * the two things the developer supplies, and clobbering a half-typed name
   * when someone browses templates would be hostile.
   */
  function applyTemplate(tpl: DeploymentTemplate) {
    selectedTemplateId = tpl.id;
    appReplicas = tpl.replicas;
    appPort = tpl.port;
    appResources = { ...tpl.resources };
    readinessProbe = tpl.readinessProbe ? { ...tpl.readinessProbe } : emptyProbe();
    livenessProbe = tpl.livenessProbe ? { ...tpl.livenessProbe } : emptyProbe();
    configVars = tpl.configVars.length > 0
      ? tpl.configVars.map((v) => ({ ...v }))
      : [{ key: '', value: '' }];
    // Secret *names* are prefilled with blank values. The template deliberately
    // ships no values — a default credential would get deployed unchanged.
    secretVars = tpl.suggestedSecretKeys.map((key) => ({ key, value: '' }));
    // Database templates stay internal and get a PVC server-side.
    if (tpl.category === 'Database') {
      appIngressDisabled = true;
      appReplicas = 1;
      if (tpl.exampleImage && !appImage.trim()) {
        appImage = tpl.exampleImage;
      }
    }
    // Probes and resources come from the template, so show what was applied
    // rather than hiding it behind a collapsed section.
    showAdvanced = true;
  }

  function clearTemplate() {
    selectedTemplateId = '';
    appResources = emptyResources();
    readinessProbe = emptyProbe();
    livenessProbe = emptyProbe();
    configVars = [{ key: '', value: '' }];
    secretVars = [];
  }

  // Config editor state
  let showConfigModal = $state(false);
  let configTarget = $state<Deployment | null>(null);
  let isLoadingConfig = $state(false);
  let editConfigVars = $state<EnvVar[]>([]);
  let editSecretVars = $state<SecretVar[]>([]);
  let removedSecretKeys = $state<string[]>([]);
  let configNotice = $state('');

  // Rollout history state
  let showHistoryModal = $state(false);
  let historyTarget = $state<Deployment | null>(null);
  let rollouts = $state<Rollout[]>([]);
  let isLoadingHistory = $state(false);
  let historyNotice = $state('');
  let rollingBackTo = $state<number | null>(null);

  // Scale state
  let activeDeployment = $state<Deployment | null>(null);
  let targetReplicas = $state(1);

  // Delete state
  let deploymentToDelete = $state('');

  function resetForm() {
    appName = '';
    appImage = '';
    appReplicas = 1;
    appPort = 80;
    appServiceType = 'NodePort';
    configVars = [{ key: '', value: '' }];
    secretVars = [];
    appHostname = '';
    appIngressDisabled = false;
    readinessProbe = emptyProbe();
    livenessProbe = emptyProbe();
    showAdvanced = false;
    appResources = emptyResources();
    selectedTemplateId = '';
    errorMsg = '';
  }

  const nonEmpty = (vars: EnvVar[]) => vars.filter((v) => v.key.trim() !== '');

  async function openConfig(d: Deployment) {
    configTarget = d;
    showConfigModal = true;
    isLoadingConfig = true;
    errorMsg = '';
    configNotice = '';
    removedSecretKeys = [];
    editConfigVars = [];
    editSecretVars = [];

    try {
      const config = await getDeploymentConfig(selectedNamespace, d.name);
      editConfigVars = config.configVars.map((v) => ({ ...v }));
      // Existing secrets arrive as names only — there is no API that returns
      // their values, so the value box stays blank until the user retypes one.
      editSecretVars = config.secretKeys.map((key) => ({ key, value: '', isExisting: true }));
    } catch (err: any) {
      errorMsg = err.message || 'Failed to load configuration';
    } finally {
      isLoadingConfig = false;
    }
  }

  function removeSecretRow(index: number) {
    const row = editSecretVars[index];
    // Only pre-existing keys need an explicit removal instruction; a row the
    // user just added was never sent to the server.
    if (row.isExisting) {
      removedSecretKeys = [...removedSecretKeys, row.key];
    }
    editSecretVars = editSecretVars.filter((_, i) => i !== index);
  }

  async function openHistory(d: Deployment) {
    historyTarget = d;
    showHistoryModal = true;
    isLoadingHistory = true;
    errorMsg = '';
    historyNotice = '';
    rollouts = [];

    const ns = d.namespace || selectedNamespace;
    try {
      rollouts = await listRollouts(ns, d.name);
    } catch (err: any) {
      errorMsg = err.message || 'Failed to load rollout history';
    } finally {
      isLoadingHistory = false;
    }
  }

  async function handleRollback(revision: number) {
    if (!historyTarget) return;
    const ns = historyTarget.namespace || selectedNamespace;
    if (!ns) return;
    rollingBackTo = revision;
    errorMsg = '';
    historyNotice = '';

    try {
      const result = await rollbackDeployment(ns, historyTarget.name, revision);
      queryClient.invalidateQueries({ queryKey: ['deployments', ns] });
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      historyNotice = `Rolled back to revision ${result.revision}. A new revision is rolling out — use Open App again if the old localhost tab stopped responding.`;
      // The rollback creates a *new* revision, so the list the user is looking
      // at is already stale — refetch rather than patch it locally.
      rollouts = await listRollouts(ns, historyTarget.name);
    } catch (err: any) {
      errorMsg = err.message || 'Failed to roll back';
    } finally {
      rollingBackTo = null;
    }
  }

  async function handleSaveConfig(e: Event) {
    e.preventDefault();
    if (!configTarget || !selectedNamespace) return;
    isSubmitting = true;
    errorMsg = '';
    configNotice = '';

    try {
      // Only secrets with a typed value are sent. An untouched existing key is
      // omitted so the server keeps its current value.
      const changedSecrets = editSecretVars
        .filter((s) => s.key.trim() !== '' && s.value !== '')
        .map((s) => ({ key: s.key, value: s.value }));

      const result = await updateDeploymentConfig({
        namespace: selectedNamespace,
        name: configTarget.name,
        configVars: nonEmpty(editConfigVars),
        secretVars: changedSecrets,
        removedSecretKeys,
      });

      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      configNotice = result.restarted
        ? 'Configuration saved. Pods are rolling to pick up the new values.'
        : 'Configuration saved. No values changed, so pods were left running.';
      removedSecretKeys = [];
      editSecretVars = editSecretVars.map((s) =>
        s.value !== '' ? { ...s, value: '', isExisting: true } : s
      );
    } catch (err: any) {
      errorMsg = err.message || 'Failed to save configuration';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleDeploy(e: Event) {
    e.preventDefault();
    if (!selectedNamespace) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      await createDeployment({
        namespace: selectedNamespace,
        name: appName,
        image: appImage,
        replicas: appReplicas,
        port: appPort,
        serviceType: appServiceType,
        configVars: nonEmpty(configVars),
        secretVars: nonEmpty(secretVars),
        // Blank probes are dropped, not sent as empty messages, so the backend
        // omits them from the pod spec entirely.
        readinessProbe: probeOrUndefined(readinessProbe),
        livenessProbe: probeOrUndefined(livenessProbe),
        hostname: appHostname.trim(),
        ingressDisabled: appIngressDisabled || isDatabaseImage(appImage),
        resources: appResources,
        templateId: selectedTemplateId,
        // Database images get a 5Gi PVC automatically unless the operator opts out.
        persistent: isDatabaseImage(appImage),
        storageSize: isDatabaseImage(appImage) ? '5Gi' : '',
      });
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showCreateModal = false;
      resetForm();
    } catch (err: any) {
      errorMsg = err.message || 'Failed to create deployment';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleScale(e: Event) {
    e.preventDefault();
    if (!selectedNamespace || !activeDeployment) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      await scaleDeployment(selectedNamespace, activeDeployment.name, targetReplicas);
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      showScaleModal = false;
      activeDeployment = null;
    } catch (err: any) {
      errorMsg = err.message || 'Failed to scale deployment';
    } finally {
      isSubmitting = false;
    }
  }

  async function handleRestart(dName: string) {
    if (!selectedNamespace) return;
    try {
      await restartDeployment(selectedNamespace, dName);
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
    } catch (err: any) {
      alert(err.message || 'Failed to trigger rolling restart');
    }
  }

  async function handleDelete() {
    if (!selectedNamespace || !deploymentToDelete) return;
    isSubmitting = true;
    errorMsg = '';

    try {
      await deleteDeployment(selectedNamespace, deploymentToDelete);
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      showDeleteModal = false;
      deploymentToDelete = '';
    } catch (err: any) {
      errorMsg = err.message || 'Failed to delete deployment';
    } finally {
      isSubmitting = false;
    }
  }
</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Deployments</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Manage stateless workloads, microservices, container configurations, and scaling.
      </p>
    </div>

    {#if openAppError}
      <div
        role="alert"
        class="flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      >
        <AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
        <span>{openAppError}</span>
        <button
          type="button"
          onclick={() => (openAppError = '')}
          aria-label="Dismiss"
          class="ml-1 rounded p-0.5 hover:bg-destructive/10"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
    {/if}

    <!-- Namespace Selector -->
    <div class="flex flex-wrap items-center gap-3">
      <div class="flex items-center gap-2">
        <span class="text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2"
        >
          {#if namespacesQuery.isPending}
            <option>Loading namespaces...</option>
          {:else if namespacesQuery.data}
            {#each namespacesQuery.data.namespaces as ns}
              <option value={ns.name}>{ns.displayName} ({ns.name})</option>
            {/each}
          {/if}
        </select>
      </div>

      <button
        onclick={() => deploymentsQuery.refetch()}
        disabled={!selectedNamespace}
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="h-4 w-4" />
      </button>

      {#if canWrite}
        <button
          onclick={() => { resetForm(); showCreateModal = true; }}
          disabled={!selectedNamespace}
          class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
        >
          <Plus class="mr-2 h-4 w-4" />
          New Deployment
        </button>
      {/if}
    </div>
  </div>

  {#if !selectedNamespace}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <AlertCircle class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">Select namespace</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Select a tenant namespace from the dropdown above to manage deployments.
      </p>
    </div>
  {:else if deploymentsQuery.isPending}
    <div class="space-y-4">
      <div class="h-12 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if deploymentsQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive">
          Error loading deployments: {deploymentsQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !deploymentsQuery.data || deploymentsQuery.data.deployments.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <Container class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">No deployments in this namespace</h3>
      <p class="mt-2 text-sm text-muted-foreground max-w-sm">
        Launch your first containerized application in namespace <span class="font-mono">{selectedNamespace}</span>.
      </p>
      {#if canWrite}
        <button
          onclick={() => showCreateModal = true}
          class="mt-4 inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Deploy Container
        </button>
      {/if}
    </div>
  {:else}
    <div class="border border-border rounded-lg bg-card overflow-x-auto">
      <table class="w-full min-w-[72rem] text-left border-collapse">
        <thead>
          <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
            <th class="px-5 py-3">Deployment Name</th>
            <th class="px-5 py-3">Docker Image</th>
            <th class="px-5 py-3">Replicas</th>
            <th class="px-5 py-3">Status</th>
            <th class="px-5 py-3">Access</th>
            <th class="px-5 py-3">Age</th>
            {#if canWrite}
              <th class="px-5 py-3 text-right">Actions</th>
            {/if}
          </tr>
        </thead>
        <tbody class="divide-y divide-border text-sm">
          {#each deploymentsQuery.data.deployments as d}
            <tr class="hover:bg-accent/20 transition-colors">
              <td class="px-5 py-3.5 font-medium text-foreground">
                {d.name}
              </td>
              <td class="px-5 py-3.5 font-mono text-xs text-muted-foreground">
                {d.image}
              </td>
              <td class="px-5 py-3.5">
                <span class="font-medium">{d.readyReplicas} / {d.replicas}</span>
                <span class="text-xs text-muted-foreground ml-1">available</span>
              </td>
              <td class="px-5 py-3.5">
                <span
                  class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium
                  {d.status === 'Running' ? 'bg-emerald-500/10 text-emerald-500' :
                   d.status === 'Progressing' ? 'bg-indigo-500/10 text-indigo-500' :
                   d.status === 'ScaledToZero' ? 'bg-muted text-muted-foreground' : 'bg-amber-500/10 text-amber-500'}"
                >
                  {d.status}
                </span>
                {#if d.statusReason}
                  <p class="mt-1 text-xs text-amber-500 max-w-[16rem] break-words">
                    {d.statusReason}
                  </p>
                {/if}
              </td>
              <td class="px-5 py-3.5 text-xs">
                <button
                  type="button"
                  onclick={() => handleOpenApp(d.namespace, d.name)}
                  disabled={openingApp === `${d.namespace}/${d.name}`}
                  class="inline-flex items-center gap-1.5 rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {#if openingApp === `${d.namespace}/${d.name}`}
                    <RefreshCw class="h-3 w-3 animate-spin" />
                    Opening…
                  {:else}
                    Open App
                    <ExternalLink class="h-3 w-3" />
                  {/if}
                </button>
                <p class="mt-1 text-muted-foreground">
                  Open App → localhost (works now). Ingress needs hosts setup.
                </p>
                <div class="mt-1.5 space-y-0.5 font-mono text-[11px] text-muted-foreground">
                  {#if d.clusterIp}
                    <p>
                      <span class="text-muted-foreground/80">ClusterIP</span>
                      {d.clusterIp}{d.port ? `:${d.port}` : ''}
                    </p>
                  {/if}
                  {#if d.serviceType}
                    <p>
                      <span class="text-muted-foreground/80">Type</span>
                      {d.serviceType}
                      {#if d.nodePort > 0}
                        · NodePort {d.nodePort}
                      {/if}
                    </p>
                  {:else if d.nodePort > 0}
                    <p>NodePort {d.nodePort}</p>
                  {/if}
                  {#if d.clusterAddress}
                    <p class="break-all" title={d.clusterAddress}>{d.clusterAddress}</p>
                  {/if}
                  {#if d.url}
                    <p class="break-all" title="Requires /etc/hosts + minikube tunnel">{d.url}</p>
                  {/if}
                </div>

                {#if d.readinessProbe || d.livenessProbe}
                  <p class="mt-1 inline-flex items-center gap-1 text-[11px] text-emerald-500">
                    <HeartPulse class="h-3 w-3" />
                    {[d.readinessProbe ? 'readiness' : '', d.livenessProbe ? 'liveness' : '']
                      .filter(Boolean)
                      .join(' + ')}
                  </p>
                {/if}

                <div class="mt-2 flex flex-wrap gap-1.5">
                  {#if isDatabaseImage(d.image)}
                    <button
                      type="button"
                      onclick={() => router.navigate('/databases', { ns: d.namespace, name: d.name })}
                      class="inline-flex items-center gap-1 rounded-md border border-input bg-background px-2 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent"
                    >
                      <Cylinder class="h-3 w-3" />
                      Browse data
                    </button>
                  {/if}
                  <button
                    type="button"
                    onclick={() => router.navigate('/workloads')}
                    class="inline-flex items-center gap-1 rounded-md border border-input bg-background px-2 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent"
                  >
                    <Terminal class="h-3 w-3" />
                    View logs
                  </button>
                </div>
              </td>
              <td class="px-5 py-3.5 text-muted-foreground text-xs">
                {new Date(d.createdAt).toLocaleDateString()}
              </td>
              {#if canWrite}
                <td class="px-5 py-3.5 text-right flex items-center justify-end gap-2">
                  <button
                    onclick={() => { activeDeployment = d; targetReplicas = d.replicas; showScaleModal = true; }}
                    class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    <Scale class="h-3 w-3" />
                    Scale
                  </button>
                  <button
                    onclick={() => openConfig(d)}
                    class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    <Settings2 class="h-3 w-3" />
                    Config
                    {#if d.secretKeys.length > 0}
                      <span class="ml-0.5 rounded-full bg-amber-500/15 px-1.5 text-[10px] font-semibold text-amber-600 dark:text-amber-400">
                        {d.secretKeys.length}
                      </span>
                    {/if}
                  </button>
                  <button
                    type="button"
                    title="View revisions and roll back"
                    onclick={() => openHistory(d)}
                    class="inline-flex h-8 items-center gap-1.5 rounded-md border border-primary/40 bg-primary/5 px-2.5 text-xs font-medium text-foreground hover:bg-primary/10"
                  >
                    <History class="h-3 w-3" />
                    History / Rollback
                  </button>
                  <button
                    onclick={() => handleRestart(d.name)}
                    class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    <Play class="h-3 w-3" />
                    Restart
                  </button>
                  <button
                    onclick={() => { deploymentToDelete = d.name; showDeleteModal = true; }}
                    class="inline-flex h-8 w-8 items-center justify-center rounded-md border border-input bg-background text-muted-foreground hover:bg-destructive/10 hover:text-destructive transition-colors"
                  >
                    <Trash2 class="h-4 w-4" />
                  </button>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- Deploy Modal -->
{#if showCreateModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="flex max-h-[90vh] w-full max-w-xl flex-col rounded-xl border border-border bg-card shadow-lg">
      <div class="flex items-center justify-between border-b border-border p-6 pb-3">
        <h2 class="text-lg font-semibold">Deploy Containerized Workload</h2>
        <button
          onclick={() => showCreateModal = false}
          class="rounded-md p-1 hover:bg-accent hover:text-accent-foreground text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <form onsubmit={handleDeploy} class="min-h-0 flex-1 space-y-4 overflow-y-auto p-6 pt-4">
        {#if errorMsg}
          <p class="text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
        {/if}

        <!-- Golden paths -->
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <div>
              <span class="text-sm font-medium">Start from a template</span>
              <p class="text-xs text-muted-foreground">
                Reviewed defaults for replicas, resources, ports, probes and config.
                You supply the image, name and version.
              </p>
            </div>
            {#if selectedTemplateId}
              <button type="button" onclick={clearTemplate} class="text-xs font-semibold text-primary hover:underline">
                Clear
              </button>
            {/if}
          </div>

          {#if templatesQuery.isPending}
            <div class="h-16 w-full animate-pulse rounded bg-muted"></div>
          {:else if templatesQuery.data}
            <div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
              {#each templatesQuery.data as tpl}
                <button
                  type="button"
                  onclick={() => applyTemplate(tpl)}
                  class="rounded-md border p-2.5 text-left transition-colors
                  {selectedTemplateId === tpl.id
                    ? 'border-primary bg-primary/5'
                    : 'border-input hover:bg-accent/40'}"
                >
                  <span class="flex items-center gap-1.5 text-xs font-semibold">
                    <Layers class="h-3 w-3 text-primary" />
                    {tpl.name}
                  </span>
                  <p class="mt-0.5 text-[11px] leading-snug text-muted-foreground">{tpl.description}</p>
                </button>
              {/each}
            </div>
          {/if}

          {#if activeTemplate}
            <div class="rounded-md bg-muted/50 p-2.5 text-xs text-muted-foreground">
              <p>{activeTemplate.rationale}</p>
              {#if activeTemplate.exampleImage}
                <p class="mt-1.5">
                  Example image: <span class="font-mono text-foreground">{activeTemplate.exampleImage}</span>
                </p>
              {/if}
              {#if activeTemplate.suggestedSecretKeys.length > 0}
                <p class="mt-1">
                  Secrets to fill in: <span class="font-mono">{activeTemplate.suggestedSecretKeys.join(', ')}</span>
                  — names only, no values are supplied.
                </p>
              {/if}
            </div>
          {/if}
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="appName" class="text-sm font-medium">Deployment Name</label>
            <input
              id="appName"
              type="text"
              required
              pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?"
              placeholder="e.g. backend-api"
              bind:value={appName}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
          <div class="space-y-1.5">
            <label for="appReplicas" class="text-sm font-medium">Initial Replicas</label>
            <input
              id="appReplicas"
              type="number"
              min="1"
              max="10"
              required
              bind:value={appReplicas}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
            />
          </div>
        </div>

        <div class="space-y-1.5">
          <label for="appImage" class="text-sm font-medium">Container Image (Docker Registry)</label>
          <input
            id="appImage"
            type="text"
            required
            placeholder="e.g. nginx:alpine, redis:7-alpine"
            bind:value={appImage}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-1.5">
            <label for="appPort" class="text-sm font-medium">Container Port</label>
            <input
              id="appPort"
              type="number"
              min="1"
              max="65535"
              required
              bind:value={appPort}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
            />
            <p class="text-xs text-muted-foreground">
              Port your image listens on — nginx 80, redis 6379.
            </p>
          </div>
          <div class="space-y-1.5">
            <label for="appServiceType" class="text-sm font-medium">Exposure</label>
            <select
              id="appServiceType"
              bind:value={appServiceType}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="NodePort">NodePort — reachable outside the cluster</option>
              <option value="ClusterIP">ClusterIP — in-cluster only</option>
            </select>
          </div>
        </div>

        <!-- Config variables → ConfigMap -->
        <div class="space-y-2">
          <div class="flex items-center justify-between">
            <div>
              <span class="text-sm font-medium">Config Variables</span>
              <p class="text-xs text-muted-foreground">
                Non-sensitive — NODE_ENV, LOG_LEVEL. Stored in a ConfigMap.
              </p>
            </div>
            <button
              type="button"
              onclick={() => (configVars = [...configVars, { key: '', value: '' }])}
              class="text-xs font-semibold text-primary hover:underline"
            >
              + Add
            </button>
          </div>

          <div class="space-y-2">
            {#each configVars as ev, idx}
              <div class="flex items-center gap-2">
                <input
                  type="text"
                  placeholder="Key (e.g. NODE_ENV)"
                  bind:value={ev.key}
                  class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <input
                  type="text"
                  placeholder="Value"
                  bind:value={ev.value}
                  class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <button
                  type="button"
                  onclick={() => (configVars = configVars.filter((_, i) => i !== idx))}
                  aria-label="Remove config variable"
                  class="text-muted-foreground hover:text-destructive p-1"
                >
                  <X class="h-4 w-4" />
                </button>
              </div>
            {/each}
          </div>
        </div>

        <!-- Secret variables → Secret -->
        <div class="space-y-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
          <div class="flex items-center justify-between">
            <div>
              <span class="flex items-center gap-1.5 text-sm font-medium">
                <KeyRound class="h-3.5 w-3.5 text-amber-500" />
                Secret Variables
              </span>
              <p class="text-xs text-muted-foreground">
                DB_PASSWORD, API_KEY, JWT_SECRET. Stored in a Secret — never shown again.
              </p>
            </div>
            <button
              type="button"
              onclick={() => (secretVars = [...secretVars, { key: '', value: '' }])}
              class="text-xs font-semibold text-primary hover:underline"
            >
              + Add
            </button>
          </div>

          <div class="space-y-2">
            {#each secretVars as sv, idx}
              <div class="flex items-center gap-2">
                <input
                  type="text"
                  placeholder="Key (e.g. DB_PASSWORD)"
                  bind:value={sv.key}
                  class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <input
                  type="password"
                  autocomplete="new-password"
                  placeholder="Value"
                  bind:value={sv.value}
                  class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
                />
                <button
                  type="button"
                  onclick={() => (secretVars = secretVars.filter((_, i) => i !== idx))}
                  aria-label="Remove secret variable"
                  class="text-muted-foreground hover:text-destructive p-1"
                >
                  <X class="h-4 w-4" />
                </button>
              </div>
            {/each}
            {#if secretVars.length === 0}
              <p class="text-xs text-muted-foreground">No secret variables.</p>
            {/if}
          </div>
        </div>

        <!-- Routing and health checks -->
        <div class="rounded-md border border-border">
          <button
            type="button"
            onclick={() => (showAdvanced = !showAdvanced)}
            class="flex w-full items-center justify-between px-3 py-2 text-sm font-medium hover:bg-accent/40"
          >
            <span>Routing &amp; Health Checks</span>
            <span class="text-xs text-muted-foreground">{showAdvanced ? 'Hide' : 'Show'}</span>
          </button>

          {#if showAdvanced}
            <div class="space-y-4 border-t border-border p-3">
              <!-- Ingress -->
              <div class="space-y-1.5">
                <label for="appHostname" class="text-sm font-medium">Custom Hostname</label>
                <input
                  id="appHostname"
                  type="text"
                  placeholder="leave blank for {appName || '<name>'}.<project>.idp.local"
                  bind:value={appHostname}
                  disabled={appIngressDisabled}
                  class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-ring disabled:opacity-50"
                />
                <label class="flex items-center gap-2 text-xs text-muted-foreground">
                  <input type="checkbox" bind:checked={appIngressDisabled} class="rounded border-input" />
                  Internal only — do not create an Ingress
                </label>
              </div>

              <!-- Resources -->
              <div class="space-y-2">
                <div>
                  <span class="text-sm font-medium">Resources</span>
                  <p class="text-xs text-muted-foreground">
                    Kubernetes quantities — 250m, 1, 512Mi, 1Gi. Blank leaves the field unset so a
                    namespace LimitRange still applies.
                  </p>
                </div>
                <div class="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  <input type="text" placeholder="CPU request" bind:value={appResources.cpuRequest}
                    class="rounded-md border border-input bg-background px-2 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
                  <input type="text" placeholder="CPU limit" bind:value={appResources.cpuLimit}
                    class="rounded-md border border-input bg-background px-2 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
                  <input type="text" placeholder="Memory request" bind:value={appResources.memoryRequest}
                    class="rounded-md border border-input bg-background px-2 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
                  <input type="text" placeholder="Memory limit" bind:value={appResources.memoryLimit}
                    class="rounded-md border border-input bg-background px-2 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring" />
                </div>
              </div>

              <!-- Probes -->
              {#each [{ label: 'Readiness Probe', probe: readinessProbe, hint: 'Removes the pod from the Service until it passes.' }, { label: 'Liveness Probe', probe: livenessProbe, hint: 'Restarts the container when it fails. Leave blank unless the app has a real health endpoint.' }] as section}
                <div class="space-y-2">
                  <div>
                    <span class="text-sm font-medium">{section.label}</span>
                    <p class="text-xs text-muted-foreground">{section.hint}</p>
                  </div>
                  <div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
                    <input
                      type="text"
                      placeholder="Path (e.g. /healthz)"
                      bind:value={section.probe.path}
                      class="col-span-2 rounded-md border border-input bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring sm:col-span-3"
                    />
                    <input type="number" min="0" max="65535" placeholder="Port (container port)" bind:value={section.probe.port}
                      class="rounded-md border border-input bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring" />
                    <input type="number" min="0" placeholder="Initial delay (s)" bind:value={section.probe.initialDelaySeconds}
                      class="rounded-md border border-input bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring" />
                    <input type="number" min="0" placeholder="Timeout (1s)" bind:value={section.probe.timeoutSeconds}
                      class="rounded-md border border-input bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring" />
                    <input type="number" min="0" placeholder="Period (10s)" bind:value={section.probe.periodSeconds}
                      class="rounded-md border border-input bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring" />
                    <input type="number" min="0" placeholder="Failure threshold (3)" bind:value={section.probe.failureThreshold}
                      class="rounded-md border border-input bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring" />
                  </div>
                </div>
              {/each}

              <p class="text-xs text-muted-foreground">
                Leave the path blank to skip a probe entirely. Blank numeric fields use the Kubernetes defaults shown.
              </p>
            </div>
          {/if}
        </div>

        <div class="sticky bottom-0 -mx-6 -mb-6 flex justify-end gap-3 border-t border-border bg-card px-6 py-3">
          <button
            type="button"
            onclick={() => showCreateModal = false}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isSubmitting}
            class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {isSubmitting ? 'Deploying...' : 'Deploy'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<!-- Rollout History Modal -->
{#if showHistoryModal && historyTarget}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-3xl rounded-xl border border-border bg-card p-6 shadow-lg max-h-[90vh] overflow-y-auto">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <div>
          <h2 class="text-lg font-semibold">Deployment History — {historyTarget.name}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground">
            Revisions Kubernetes retains for this Deployment, newest first.
          </p>
        </div>
        <button
          onclick={() => { showHistoryModal = false; historyTarget = null; }}
          aria-label="Close"
          class="rounded-md p-1 hover:bg-accent text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      {#if errorMsg}
        <p class="mt-4 text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
      {/if}
      {#if historyNotice}
        <p class="mt-4 text-sm text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 p-3 rounded-md">
          {historyNotice}
        </p>
      {/if}

      {#if isLoadingHistory}
        <div class="mt-6 space-y-3">
          <div class="h-10 w-full animate-pulse rounded bg-muted"></div>
          <div class="h-10 w-full animate-pulse rounded bg-muted"></div>
        </div>
      {:else if rollouts.length === 0}
        <div class="mt-6 flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-10 text-center">
          <History class="mb-3 h-10 w-10 text-muted-foreground/40" />
          <p class="text-sm font-medium">No revision history</p>
          <p class="mt-1 text-xs text-muted-foreground max-w-sm">
            Kubernetes records a revision each time the pod template changes. This deployment has not
            been updated since it was created.
          </p>
        </div>
      {:else}
        <div class="mt-4 overflow-x-auto rounded-lg border border-border">
          <table class="w-full min-w-[48rem] text-left border-collapse">
            <thead>
              <tr class="border-b border-border bg-muted/40 text-xs font-semibold text-muted-foreground uppercase">
                <th class="px-4 py-2.5">Revision</th>
                <th class="px-4 py-2.5">Timestamp</th>
                <th class="px-4 py-2.5">Image</th>
                <th class="px-4 py-2.5">Replicas</th>
                <th class="px-4 py-2.5">Status</th>
                {#if canWrite}
                  <th class="px-4 py-2.5 text-right">Action</th>
                {/if}
              </tr>
            </thead>
            <tbody class="divide-y divide-border text-sm">
              {#each rollouts as r}
                <tr class="hover:bg-accent/20 transition-colors">
                  <td class="px-4 py-3 font-medium">#{r.revision}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">
                    {r.createdAt ? new Date(r.createdAt).toLocaleString() : '—'}
                  </td>
                  <td class="px-4 py-3 font-mono text-xs text-muted-foreground max-w-[16rem] truncate">
                    {r.image || '—'}
                    {#if r.changeCause}
                      <p class="mt-0.5 text-[11px] italic text-muted-foreground/70">{r.changeCause}</p>
                    {/if}
                  </td>
                  <td class="px-4 py-3 text-xs">
                    {r.readyReplicas} / {r.replicas}
                  </td>
                  <td class="px-4 py-3">
                    <span
                      class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium
                      {r.current ? 'bg-emerald-500/10 text-emerald-500' : 'bg-muted text-muted-foreground'}"
                    >
                      {r.status}
                    </span>
                  </td>
                  {#if canWrite}
                    <td class="px-4 py-3 text-right">
                      {#if r.current}
                        <span class="text-xs text-muted-foreground">current</span>
                      {:else}
                        <button
                          onclick={() => handleRollback(r.revision)}
                          disabled={rollingBackTo !== null}
                          class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent disabled:opacity-50"
                        >
                          <Undo2 class="h-3 w-3" />
                          {rollingBackTo === r.revision ? 'Rolling back...' : 'Rollback'}
                        </button>
                      {/if}
                    </td>
                  {/if}
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        <p class="mt-3 text-xs text-muted-foreground">
          Rolling back restores that revision's pod template and creates a new revision — the history
          is never rewritten. How far back this list goes is set by the Deployment's
          <span class="font-mono">revisionHistoryLimit</span> (default 10).
        </p>
      {/if}

      <div class="mt-6 flex justify-end">
        <button
          type="button"
          onclick={() => { showHistoryModal = false; historyTarget = null; }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
        >
          Close
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- Configuration Modal -->
{#if showConfigModal && configTarget && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-2xl rounded-xl border border-border bg-card p-6 shadow-lg max-h-[90vh] overflow-y-auto">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <div>
          <h2 class="text-lg font-semibold">Configuration — {configTarget.name}</h2>
          <p class="mt-0.5 text-xs text-muted-foreground font-mono">
            {configTarget.configMapName} · {configTarget.secretName}
          </p>
        </div>
        <button
          onclick={() => { showConfigModal = false; configTarget = null; }}
          aria-label="Close"
          class="rounded-md p-1 hover:bg-accent text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      {#if isLoadingConfig}
        <div class="mt-6 space-y-3">
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
        </div>
      {:else}
        <form onsubmit={handleSaveConfig} class="mt-4 space-y-5">
          {#if errorMsg}
            <p class="text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
          {/if}
          {#if configNotice}
            <p class="text-sm text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 p-3 rounded-md">
              {configNotice}
            </p>
          {/if}

          <!-- Config variables -->
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <div>
                <span class="text-sm font-medium">Config Variables</span>
                <p class="text-xs text-muted-foreground">Visible in the ConfigMap. Safe for non-sensitive values.</p>
              </div>
              <button
                type="button"
                onclick={() => (editConfigVars = [...editConfigVars, { key: '', value: '' }])}
                class="text-xs font-semibold text-primary hover:underline"
              >
                + Add
              </button>
            </div>

            <div class="space-y-2">
              {#each editConfigVars as ev, idx}
                <div class="flex items-center gap-2">
                  <input
                    type="text"
                    placeholder="Key"
                    bind:value={ev.key}
                    class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                  <input
                    type="text"
                    placeholder="Value"
                    bind:value={ev.value}
                    class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                  <button
                    type="button"
                    onclick={() => (editConfigVars = editConfigVars.filter((_, i) => i !== idx))}
                    aria-label="Remove config variable"
                    class="text-muted-foreground hover:text-destructive p-1"
                  >
                    <X class="h-4 w-4" />
                  </button>
                </div>
              {/each}
              {#if editConfigVars.length === 0}
                <p class="text-xs text-muted-foreground">No config variables.</p>
              {/if}
            </div>
          </div>

          <!-- Secret variables -->
          <div class="space-y-2 rounded-md border border-amber-500/30 bg-amber-500/5 p-3">
            <div class="flex items-center justify-between">
              <div>
                <span class="flex items-center gap-1.5 text-sm font-medium">
                  <KeyRound class="h-3.5 w-3.5 text-amber-500" />
                  Secret Variables
                </span>
                <p class="text-xs text-muted-foreground">
                  Values are write-only. Leave blank to keep the stored value.
                </p>
              </div>
              <button
                type="button"
                onclick={() => (editSecretVars = [...editSecretVars, { key: '', value: '', isExisting: false }])}
                class="text-xs font-semibold text-primary hover:underline"
              >
                + Add
              </button>
            </div>

            <div class="space-y-2">
              {#each editSecretVars as sv, idx}
                <div class="flex items-center gap-2">
                  <input
                    type="text"
                    placeholder="Key"
                    readonly={sv.isExisting}
                    bind:value={sv.key}
                    class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring read-only:opacity-70"
                  />
                  <input
                    type="password"
                    autocomplete="new-password"
                    placeholder={sv.isExisting ? '•••••••• (unchanged)' : 'Value'}
                    bind:value={sv.value}
                    class="flex-1 rounded-md border border-input bg-background px-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
                  />
                  <button
                    type="button"
                    onclick={() => removeSecretRow(idx)}
                    aria-label="Remove secret variable"
                    class="text-muted-foreground hover:text-destructive p-1"
                  >
                    <X class="h-4 w-4" />
                  </button>
                </div>
              {/each}
              {#if editSecretVars.length === 0}
                <p class="text-xs text-muted-foreground">No secret variables.</p>
              {/if}
              {#if removedSecretKeys.length > 0}
                <p class="text-xs text-destructive">
                  Will delete on save: <span class="font-mono">{removedSecretKeys.join(', ')}</span>
                </p>
              {/if}
            </div>
          </div>

          <p class="text-xs text-muted-foreground">
            Saving rolls the pods — containers only read environment variables at start.
          </p>

          <div class="flex justify-end gap-3 pt-3 border-t border-border">
            <button
              type="button"
              onclick={() => { showConfigModal = false; configTarget = null; }}
              class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
            >
              Close
            </button>
            <button
              type="submit"
              disabled={isSubmitting}
              class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            >
              {isSubmitting ? 'Saving...' : 'Save & Roll'}
            </button>
          </div>
        </form>
      {/if}
    </div>
  </div>
{/if}

<!-- Scale Modal -->
{#if showScaleModal && activeDeployment && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-sm rounded-xl border border-border bg-card p-6 shadow-lg">
      <div class="flex items-center justify-between border-b border-border pb-3">
        <h2 class="text-lg font-semibold">Scale Workload</h2>
        <button
          onclick={() => showScaleModal = false}
          class="rounded-md p-1 hover:bg-accent hover:text-accent-foreground text-muted-foreground"
        >
          <X class="h-5 w-5" />
        </button>
      </div>

      <form onsubmit={handleScale} class="mt-4 space-y-4">
        {#if errorMsg}
          <p class="text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
        {/if}

        <p class="text-sm text-muted-foreground">
          Scale replicas for deployment <span class="font-mono text-foreground font-semibold">{activeDeployment.name}</span>.
        </p>

        <div class="space-y-1.5">
          <label for="replicas" class="text-sm font-medium">Replica Count</label>
          <input
            id="replicas"
            type="number"
            min="0"
            max="100"
            required
            bind:value={targetReplicas}
            class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus:outline-none focus:ring-2 focus:ring-ring"
          />
        </div>

        <div class="flex justify-end gap-3 pt-3 border-t border-border">
          <button
            type="button"
            onclick={() => showScaleModal = false}
            class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isSubmitting}
            class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {isSubmitting ? 'Scaling...' : 'Apply Scale'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<!-- Delete Confirmation Modal -->
{#if showDeleteModal && canWrite}
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm p-4">
    <div class="w-full max-w-md rounded-xl border border-border bg-card p-6 shadow-lg">
      <h2 class="text-lg font-semibold text-foreground">Confirm Workload Deletion</h2>
      <p class="mt-2 text-sm text-muted-foreground">
        Are you sure you want to delete deployment <span class="font-mono font-semibold text-rose-500">{deploymentToDelete}</span>?
        All active containers running under this deployment will be terminated. This action is permanent.
      </p>

      {#if errorMsg}
        <p class="mt-3 text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
      {/if}

      <div class="mt-6 flex justify-end gap-3">
        <button
          type="button"
          onclick={() => { showDeleteModal = false; deploymentToDelete = ''; }}
          class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent hover:text-accent-foreground"
        >
          Cancel
        </button>
        <button
          type="button"
          disabled={isSubmitting}
          onclick={handleDelete}
          class="inline-flex h-9 items-center justify-center rounded-md bg-destructive px-4 text-sm font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50"
        >
          {isSubmitting ? 'Deleting...' : 'Delete Deployment'}
        </button>
      </div>
    </div>
  </div>
{/if}
