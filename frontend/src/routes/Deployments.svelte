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
    emptyAutoscaling,
    probeOrUndefined,
    type Rollout,
    type ResourceLimits,
    type Autoscaling,
    type DeploymentTemplate,
    type Deployment,
    type EnvVar,
    type SecretVar,
    type Probe
  } from '$services/deployments';
  import { openWorkload } from '$services/client';
  import { activityLog } from '$stores/activitylog';
  import { isDatabaseImage } from '$services/databases';
  import { router } from '$stores/router';
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import Modal from '$components/ui/Modal.svelte';
  import DeploymentLogPanel from '$components/DeploymentLogPanel.svelte';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import Skeleton from '$components/ui/Skeleton.svelte';
  import EmptyState from '$components/ui/EmptyState.svelte';
  import DataTable from '$components/ui/DataTable.svelte';
  import { statusBadgeClass } from '$lib/status';
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
      activityLog.push(`deploy:${namespace}/${name}`, 'info', `Opening ${namespace}/${name} on localhost…`);
      await openWorkload(namespace, name);
    } catch (err) {
      openAppError = err instanceof Error ? err.message : 'Could not open this app.';
      activityLog.push(`deploy:${namespace}/${name}`, 'error', openAppError);
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
    refetchInterval: 12000,
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
  let appAutoscaling = $state<Autoscaling>(emptyAutoscaling());
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
    appAutoscaling = tpl.autoscaling ? { ...tpl.autoscaling } : emptyAutoscaling();
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
      appAutoscaling = emptyAutoscaling();
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
    appAutoscaling = emptyAutoscaling();
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
  let scaleAutoscaling = $state<Autoscaling>(emptyAutoscaling());

  // Delete state
  let deploymentToDelete = $state('');

  // Live logs — opened automatically after deploy, or from View logs.
  let logTarget = $state<{ namespace: string; name: string } | null>(null);

  function openLogs(namespace: string, name: string) {
    logTarget = { namespace, name };
  }

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
    appAutoscaling = emptyAutoscaling();
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

    const deployedNs = selectedNamespace;
    const deployedName = appName;
    openLogs(deployedNs, deployedName);
    activityLog.push(`deploy:${deployedNs}/${deployedName}`, 'info', `CreateDeployment API → ${appImage} (${appReplicas} replica${appReplicas === 1 ? '' : 's'})`);
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
        autoscaling: appAutoscaling.maxReplicas > 1 ? appAutoscaling : null,
      });
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      queryClient.invalidateQueries({ queryKey: ['cluster-overview'] });
      activityLog.push(`deploy:${deployedNs}/${deployedName}`, 'success', 'API accepted. Waiting for pods / image pull.');
      showCreateModal = false;
      resetForm();
    } catch (err: any) {
      errorMsg = err.message || 'Failed to create deployment';
      activityLog.push(`deploy:${deployedNs}/${deployedName}`, 'error', errorMsg);
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
      await scaleDeployment(
        selectedNamespace,
        activeDeployment.name,
        targetReplicas,
        scaleAutoscaling.maxReplicas > 1 ? scaleAutoscaling : { minReplicas: 1, maxReplicas: 0, cpuAverageUtilization: 0, memoryAverageUtilization: 0 },
      );
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
    openLogs(selectedNamespace, dName);
    activityLog.push(`deploy:${selectedNamespace}/${dName}`, 'info', 'RestartDeployment API…');
    try {
      await restartDeployment(selectedNamespace, dName);
      queryClient.invalidateQueries({ queryKey: ['deployments', selectedNamespace] });
      activityLog.push(`deploy:${selectedNamespace}/${dName}`, 'success', 'Rolling restart accepted.');
    } catch (err: any) {
      const msg = err.message || 'Failed to trigger rolling restart';
      activityLog.push(`deploy:${selectedNamespace}/${dName}`, 'error', msg);
      alert(msg);
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

<div class="page-stack">
  <PageHeader
    title="Deployments"
    description="Manage stateless workloads, microservices, container configurations, and scaling."
  >

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
  </PageHeader>

  {#if !selectedNamespace}
    <EmptyState
      icon={AlertCircle}
      title="Select namespace"
      description="Select a tenant namespace from the dropdown above to manage deployments."
    />
  {:else if deploymentsQuery.isPending}
    <Skeleton variant="table" rows={6} />
  {:else if deploymentsQuery.error}
    <Card class="border-destructive bg-destructive/5">
      <CardContent class="py-6">
        <p class="text-sm text-destructive">
          Error loading deployments: {deploymentsQuery.error.message}
        </p>
      </CardContent>
    </Card>
  {:else if !deploymentsQuery.data || deploymentsQuery.data.deployments.length === 0}
    <EmptyState
      icon={Container}
      title="No deployments in this namespace"
      description="Launch your first containerized application in namespace {selectedNamespace}."
    >
      {#if canWrite}
        <button
          onclick={() => showCreateModal = true}
          class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Deploy Container
        </button>
      {/if}
    </EmptyState>
  {:else}
    <DataTable minWidth="64rem">
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
                  class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(d.status)}"
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
                  Opens on localhost (auto port-forward). No 127.0.0.1:18xxx.
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
                  <button
                    type="button"
                    onclick={() => openLogs(d.namespace, d.name)}
                    class="inline-flex items-center gap-1 rounded-md border border-input bg-background px-2 py-0.5 text-[11px] font-medium text-foreground hover:bg-accent"
                  >
                    <Terminal class="h-3 w-3" />
                    View logs
                  </button>
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
                </div>
              </td>
              <td class="px-5 py-3.5 text-muted-foreground text-xs">
                {new Date(d.createdAt).toLocaleDateString()}
              </td>
              {#if canWrite}
                <td class="px-5 py-3.5 text-right flex items-center justify-end gap-2">
                  <button
                    type="button"
                    onclick={() => openLogs(d.namespace, d.name)}
                    class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium text-foreground hover:bg-accent"
                  >
                    <Terminal class="h-3 w-3" />
                    Logs
                  </button>
                  <button
                    onclick={() => {
                      activeDeployment = d;
                      targetReplicas = d.replicas;
                      scaleAutoscaling = d.autoscaling ? { ...d.autoscaling } : emptyAutoscaling();
                      showScaleModal = true;
                    }}
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
    </DataTable>
  {/if}
</div>

<!-- Deploy Modal -->
<Modal
  open={showCreateModal && canWrite}
  title="Deploy Containerized Workload"
  size="lg"
  onclose={() => (showCreateModal = false)}
>
  <form id="deploy-workload-form" onsubmit={handleDeploy} class="space-y-4">
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

          <div class="space-y-2">
            <div>
              <span class="text-sm font-medium">Autoscaling</span>
              <p class="text-xs text-muted-foreground">
                Horizontal Pod Autoscaler. Max replicas above 1 creates an HPA on CPU utilisation.
                Requires resource requests (filled automatically if blank) and metrics-server.
              </p>
            </div>
            <label class="flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={appAutoscaling.maxReplicas > 1}
                onchange={(e) => {
                  const on = (e.currentTarget as HTMLInputElement).checked;
                  appAutoscaling = on
                    ? { minReplicas: Math.max(appReplicas, 1), maxReplicas: Math.max(appReplicas * 3, 4), cpuAverageUtilization: 70, memoryAverageUtilization: 0 }
                    : emptyAutoscaling();
                }}
                class="rounded border-input"
              />
              Scale pods automatically with traffic
            </label>
            {#if appAutoscaling.maxReplicas > 1}
              <div class="grid grid-cols-3 gap-2">
                <label class="text-xs">
                  Min
                  <input type="number" min="1" max="50" bind:value={appAutoscaling.minReplicas}
                    class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
                </label>
                <label class="text-xs">
                  Max
                  <input type="number" min="2" max="50" bind:value={appAutoscaling.maxReplicas}
                    class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
                </label>
                <label class="text-xs">
                  CPU target %
                  <input type="number" min="10" max="100" bind:value={appAutoscaling.cpuAverageUtilization}
                    class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
                </label>
              </div>
            {/if}
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

  </form>

  {#snippet footer()}
    <button
      type="button"
      onclick={() => (showCreateModal = false)}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="submit"
      form="deploy-workload-form"
      disabled={isSubmitting}
      class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
    >
      {isSubmitting ? 'Deploying...' : 'Deploy'}
    </button>
  {/snippet}
</Modal>

<!-- Rollout History Modal -->
<Modal
  open={showHistoryModal && !!historyTarget}
  title={historyTarget ? `Deployment History — ${historyTarget.name}` : 'Deployment History'}
  description="Revisions Kubernetes retains for this Deployment, newest first."
  size="xl"
  onclose={() => { showHistoryModal = false; historyTarget = null; }}
>
  {#if errorMsg}
    <p class="mb-4 text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
  {/if}
  {#if historyNotice}
    <p class="mb-4 text-sm text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 p-3 rounded-md">
      {historyNotice}
    </p>
  {/if}

  {#if isLoadingHistory}
    <div class="space-y-3">
      <div class="h-10 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-10 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else if rollouts.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-10 text-center">
      <History class="mb-3 h-10 w-10 text-muted-foreground/40" />
      <p class="text-sm font-medium">No revision history</p>
      <p class="mt-1 text-xs text-muted-foreground max-w-sm">
        Kubernetes records a revision each time the pod template changes. This deployment has not
        been updated since it was created.
      </p>
    </div>
  {:else}
    <div class="overflow-x-auto rounded-lg border border-border">
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

  {#snippet footer()}
    <button
      type="button"
      onclick={() => { showHistoryModal = false; historyTarget = null; }}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Close
    </button>
  {/snippet}
</Modal>

<!-- Configuration Modal -->
<Modal
  open={showConfigModal && !!configTarget && canWrite}
  title={configTarget ? `Configuration — ${configTarget.name}` : 'Configuration'}
  description={configTarget ? `${configTarget.configMapName} · ${configTarget.secretName}` : undefined}
  size="lg"
  onclose={() => { showConfigModal = false; configTarget = null; }}
>
  {#if isLoadingConfig}
    <div class="space-y-3">
      <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
      <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
    </div>
  {:else}
    <form id="deployment-config-form" onsubmit={handleSaveConfig} class="space-y-5">
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

    </form>
  {/if}

  {#snippet footer()}
    <button
      type="button"
      onclick={() => { showConfigModal = false; configTarget = null; }}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Close
    </button>
    <button
      type="submit"
      form="deployment-config-form"
      disabled={isSubmitting}
      class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
    >
      {isSubmitting ? 'Saving...' : 'Save & Roll'}
    </button>
  {/snippet}
</Modal>

<!-- Scale Modal -->
<Modal
  open={showScaleModal && !!activeDeployment && canWrite}
  title="Scale Workload"
  onclose={() => (showScaleModal = false)}
>
  {#if activeDeployment}
    <form id="scale-workload-form" onsubmit={handleScale} class="space-y-4">
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

      <div class="space-y-2">
        <label class="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={scaleAutoscaling.maxReplicas > 1}
            onchange={(e) => {
              const on = (e.currentTarget as HTMLInputElement).checked;
              scaleAutoscaling = on
                ? { minReplicas: Math.max(targetReplicas, 1), maxReplicas: Math.max(targetReplicas * 3, 4), cpuAverageUtilization: 70, memoryAverageUtilization: 0 }
                : emptyAutoscaling();
            }}
            class="rounded border-input"
          />
          Autoscale (HPA)
        </label>
        {#if scaleAutoscaling.maxReplicas > 1}
          <div class="grid grid-cols-3 gap-2">
            <label class="text-xs">Min
              <input type="number" min="1" bind:value={scaleAutoscaling.minReplicas}
                class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
            </label>
            <label class="text-xs">Max
              <input type="number" min="2" bind:value={scaleAutoscaling.maxReplicas}
                class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
            </label>
            <label class="text-xs">CPU %
              <input type="number" min="10" max="100" bind:value={scaleAutoscaling.cpuAverageUtilization}
                class="mt-1 w-full rounded-md border border-input bg-background px-2 py-1.5 text-xs" />
            </label>
          </div>
        {/if}
      </div>
    </form>
  {/if}

  {#snippet footer()}
    <button
      type="button"
      onclick={() => (showScaleModal = false)}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-4 text-sm font-medium hover:bg-accent"
    >
      Cancel
    </button>
    <button
      type="submit"
      form="scale-workload-form"
      disabled={isSubmitting}
      class="inline-flex h-9 items-center justify-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
    >
      {isSubmitting ? 'Scaling...' : 'Apply Scale'}
    </button>
  {/snippet}
</Modal>

<!-- Delete Confirmation Modal -->
<Modal
  open={showDeleteModal && canWrite}
  title="Confirm Workload Deletion"
  onclose={() => { showDeleteModal = false; deploymentToDelete = ''; }}
>
  <p class="text-sm text-muted-foreground">
    Are you sure you want to delete deployment <span class="font-mono font-semibold text-rose-500">{deploymentToDelete}</span>?
    All active containers running under this deployment will be terminated. This action is permanent.
  </p>

  {#if errorMsg}
    <p class="mt-3 text-sm text-destructive bg-destructive/10 p-3 rounded-md">{errorMsg}</p>
  {/if}

  {#snippet footer()}
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
  {/snippet}
</Modal>

<Modal
  open={!!logTarget}
  title={logTarget ? `Live logs — ${logTarget.namespace}/${logTarget.name}` : 'Live logs'}
  description="Streaming container output as pods start and run."
  size="xl"
  onclose={() => (logTarget = null)}
>
  {#if logTarget}
    {#key `${logTarget.namespace}/${logTarget.name}`}
      <DeploymentLogPanel namespace={logTarget.namespace} app={logTarget.name} />
    {/key}
  {/if}
</Modal>
