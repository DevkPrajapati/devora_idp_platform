<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import {
    downloadBlob,
    ensureDatabasePersistence,
    exportDatabase,
    formatBytes,
    importDatabase,
    inspectDatabase,
    listDatabases,
    queryDocuments,
    type DatabaseInstance,
    type DatabaseTable,
  } from '$services/databases';
  import { auth } from '$stores/auth';
  import { createQuery } from '@tanstack/svelte-query';
  import {
    AlertCircle,
    CheckCircle2,
    ChevronLeft,
    Cylinder,
    Database,
    Download,
    HardDrive,
    RefreshCw,
    Table2,
    Upload,
  } from '@lucide/svelte';
  import { router } from '$stores/router';

  let selectedNamespace = $state('');
  let selectedInstance = $state<DatabaseInstance | null>(null);
  let selectedTable = $state<DatabaseTable | null>(null);
  let pageSkip = $state(0);
  const pageSize = 50;

  let actionBusy = $state('');
  let actionError = $state('');
  let actionMessage = $state('');
  let importInput: HTMLInputElement | null = $state(null);

  const canWrite = $derived(
    $auth.user?.roles.includes('admin') || $auth.user?.roles.includes('developer') || false,
  );

  // Deep-link from Deployments: /databases?ns=...&name=...
  let focusNamespace = $state('');
  let focusName = $state('');

  $effect(() => {
    // Re-read query whenever the router lands on this page (including
    // client-side navigations from Deployments).
    void $router;
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams(window.location.search);
    const ns = params.get('ns') ?? '';
    const name = params.get('name') ?? '';
    if (ns) selectedNamespace = ns;
    if (name) {
      focusNamespace = ns;
      focusName = name;
      selectedTable = null;
      pageSkip = 0;
    }
  });

  const listQuery = createQuery(() => ({
    queryKey: ['databases', selectedNamespace],
    queryFn: () => listDatabases(selectedNamespace),
    refetchInterval: 15000,
  }));

  const namespaceOptions = $derived(
    [...new Set((listQuery.data?.instances ?? []).map((i) => i.namespace))].sort(),
  );

  const visibleInstances = $derived(
    selectedNamespace === ''
      ? (listQuery.data?.instances ?? [])
      : (listQuery.data?.instances ?? []).filter((i) => i.namespace === selectedNamespace),
  );

  $effect(() => {
    if (!focusName || !listQuery.data) return;
    const match = (listQuery.data.instances ?? []).find(
      (i) => i.name === focusName && (!focusNamespace || i.namespace === focusNamespace),
    );
    if (match && (!selectedInstance || selectedInstance.name !== match.name)) {
      selectedInstance = match;
      selectedTable = null;
      pageSkip = 0;
      focusName = '';
    }
  });

  // Mongo without auth still resolves credentials; SQL engines need a password.
  // Allow inspect whenever the pod is ready so empty DBs still list tables.
  const inspectQuery = createQuery(() => ({
    queryKey: ['database-inspect', selectedInstance?.namespace, selectedInstance?.name],
    queryFn: () => inspectDatabase(selectedInstance!.namespace, selectedInstance!.name),
    enabled: !!selectedInstance?.ready,
  }));

  const docsQuery = createQuery(() => ({
    queryKey: [
      'database-docs',
      selectedInstance?.namespace,
      selectedInstance?.name,
      selectedTable?.schema,
      selectedTable?.name,
      pageSkip,
    ],
    queryFn: () =>
      queryDocuments({
        namespace: selectedInstance!.namespace,
        name: selectedInstance!.name,
        schema: selectedTable!.schema,
        table: selectedTable!.name,
        limit: pageSize,
        skip: pageSkip,
      }),
    enabled: !!selectedInstance && !!selectedTable,
  }));

  function openInstance(instance: DatabaseInstance) {
    selectedInstance = instance;
    selectedTable = null;
    pageSkip = 0;
  }

  function openTable(table: DatabaseTable) {
    selectedTable = table;
    pageSkip = 0;
  }

  function backToList() {
    selectedInstance = null;
    selectedTable = null;
    pageSkip = 0;
  }

  function backToTables() {
    selectedTable = null;
    pageSkip = 0;
  }

  function prettyJSON(raw: string): string {
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }

  function engineBadge(engine: string): string {
    switch (engine) {
      case 'mongodb':
        return 'bg-emerald-500/10 text-emerald-500';
      case 'postgres':
        return 'bg-sky-500/10 text-sky-500';
      case 'mysql':
        return 'bg-amber-500/10 text-amber-500';
      default:
        return 'bg-muted text-muted-foreground';
    }
  }

  async function handleExport() {
    if (!selectedInstance) return;
    actionBusy = 'export';
    actionError = '';
    actionMessage = '';
    try {
      const result = await exportDatabase(selectedInstance.namespace, selectedInstance.name);
      downloadBlob(result.blob, result.filename);
      actionMessage = `Exported ${formatBytes(result.sizeBytes)}`;
    } catch (err) {
      actionError = err instanceof Error ? err.message : 'Export failed';
    } finally {
      actionBusy = '';
    }
  }

  async function handleImport(file: File | undefined) {
    if (!selectedInstance || !file) return;
    actionBusy = 'import';
    actionError = '';
    actionMessage = '';
    try {
      const result = await importDatabase(selectedInstance.namespace, selectedInstance.name, file);
      actionMessage = result.message;
      await inspectQuery.refetch();
      if (selectedTable) await docsQuery.refetch();
    } catch (err) {
      actionError = err instanceof Error ? err.message : 'Import failed';
    } finally {
      actionBusy = '';
      if (importInput) importInput.value = '';
    }
  }

  async function handleEnablePersistence() {
    if (!selectedInstance) return;
    if (
      !confirm(
        'Enable persistence attaches a PVC and recreates the pod. Export your data first if you need to keep it. Continue?',
      )
    ) {
      return;
    }
    actionBusy = 'persist';
    actionError = '';
    actionMessage = '';
    try {
      const result = await ensureDatabasePersistence(
        selectedInstance.namespace,
        selectedInstance.name,
      );
      actionMessage = result.message;
      await listQuery.refetch();
      const refreshed = (listQuery.data?.instances ?? []).find(
        (i) => i.namespace === selectedInstance!.namespace && i.name === selectedInstance!.name,
      );
      if (refreshed) selectedInstance = refreshed;
    } catch (err) {
      actionError = err instanceof Error ? err.message : 'Could not enable persistence';
    } finally {
      actionBusy = '';
    }
  }
</script>

<div class="space-y-6">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Databases</h1>
      <p class="mt-1 text-sm text-muted-foreground">
        Browse MongoDB, PostgreSQL, and MySQL workloads discovered in the cluster — no CLI needed.
      </p>
    </div>

    <div class="flex flex-wrap items-center gap-3">
      <div class="flex min-w-0 items-center gap-2">
        <span class="shrink-0 text-sm font-medium text-muted-foreground">Namespace:</span>
        <select
          bind:value={selectedNamespace}
          class="h-9 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none"
          onchange={() => {
            selectedInstance = null;
            selectedTable = null;
          }}
        >
          <option value="">All namespaces</option>
          {#each namespaceOptions as ns}
            <option value={ns}>{ns}</option>
          {/each}
        </select>
      </div>

      <button
        type="button"
        onclick={() => {
          listQuery.refetch();
          if (selectedInstance) inspectQuery.refetch();
          if (selectedTable) docsQuery.refetch();
        }}
        class="inline-flex h-9 w-9 items-center justify-center rounded-md border border-input bg-background hover:bg-accent text-muted-foreground"
      >
        <RefreshCw class="h-4 w-4" />
      </button>
    </div>
  </div>

  {#if listQuery.isPending}
    <div class="flex items-center justify-center p-16 text-muted-foreground">
      <RefreshCw class="mr-2 h-5 w-5 animate-spin" />
      Discovering databases…
    </div>
  {:else if listQuery.isError}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <AlertCircle class="mb-4 h-12 w-12 text-destructive/60" />
      <h3 class="text-lg font-semibold">Could not list databases</h3>
      <p class="mt-2 max-w-md text-sm text-muted-foreground">{listQuery.error.message}</p>
    </div>
  {:else if listQuery.data && !listQuery.data.connected}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <AlertCircle class="mb-4 h-12 w-12 text-muted-foreground/40" />
      <h3 class="text-lg font-semibold">Cluster not connected</h3>
      <p class="mt-2 text-sm text-muted-foreground">
        Connect Kubernetes to discover database workloads.
      </p>
    </div>
  {:else if selectedTable && selectedInstance}
    <!-- Document / row browser -->
    <div class="space-y-4">
      <button
        type="button"
        onclick={backToTables}
        class="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ChevronLeft class="h-4 w-4" />
        Back to collections
      </button>

      <Card>
        <CardHeader class="flex flex-row items-center justify-between space-y-0 pb-2">
          <div>
            <CardTitle class="text-base font-semibold">
              {selectedTable.schema}.{selectedTable.name}
            </CardTitle>
            <p class="mt-1 text-xs text-muted-foreground">
              {selectedInstance.engineName} · {selectedInstance.namespace}/{selectedInstance.name}
            </p>
          </div>
          <Table2 class="h-5 w-5 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          {#if docsQuery.isPending}
            <div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
              <RefreshCw class="h-4 w-4 animate-spin" />
              Loading documents…
            </div>
          {:else if docsQuery.isError}
            <div class="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
              {docsQuery.error.message}
            </div>
          {:else if (docsQuery.data?.documents.length ?? 0) === 0}
            <div class="py-10 text-center text-sm text-muted-foreground">
              No documents in this collection yet.
            </div>
          {:else}
            <div class="space-y-3">
              {#each docsQuery.data?.documents ?? [] as doc}
                <pre
                  class="overflow-x-auto rounded-md border border-border bg-muted/40 p-3 text-xs leading-relaxed text-foreground"
                >{prettyJSON(doc)}</pre>
              {/each}
            </div>

            <div class="mt-4 flex items-center justify-between text-xs text-muted-foreground">
              <span>
                Showing {pageSkip + 1}–{pageSkip + (docsQuery.data?.returned ?? 0)}
                {#if docsQuery.data?.truncated}
                  (more available)
                {/if}
              </span>
              <div class="flex gap-2">
                <button
                  type="button"
                  disabled={pageSkip === 0}
                  onclick={() => {
                    pageSkip = Math.max(0, pageSkip - pageSize);
                  }}
                  class="rounded-md border border-input px-2.5 py-1 disabled:opacity-40"
                >
                  Previous
                </button>
                <button
                  type="button"
                  disabled={!docsQuery.data?.truncated}
                  onclick={() => {
                    pageSkip = pageSkip + pageSize;
                  }}
                  class="rounded-md border border-input px-2.5 py-1 disabled:opacity-40"
                >
                  Next
                </button>
              </div>
            </div>
          {/if}
        </CardContent>
      </Card>
    </div>
  {:else if selectedInstance}
    <!-- Collections / tables -->
    <div class="space-y-4">
      <button
        type="button"
        onclick={backToList}
        class="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ChevronLeft class="h-4 w-4" />
        Back to databases
      </button>

      <Card>
        <CardHeader class="flex flex-row items-start justify-between space-y-0">
          <div>
            <CardTitle class="text-base font-semibold">{selectedInstance.name}</CardTitle>
            <p class="mt-1 font-mono text-xs text-muted-foreground">{selectedInstance.image}</p>
            <div class="mt-2 flex flex-wrap gap-2 text-xs">
              <span class="inline-flex rounded-full px-2 py-0.5 font-medium {engineBadge(selectedInstance.engine)}">
                {selectedInstance.engineName}
              </span>
              <span class="text-muted-foreground">{selectedInstance.namespace}</span>
              {#if selectedInstance.serviceName}
                <span class="text-muted-foreground">svc/{selectedInstance.serviceName}</span>
              {/if}
              {#if selectedInstance.persistentVolumeClaims.length > 0}
                <span class="inline-flex items-center gap-1 text-emerald-500">
                  <HardDrive class="h-3 w-3" />
                  PVC: {selectedInstance.persistentVolumeClaims.join(', ')}
                </span>
              {:else}
                <span class="inline-flex items-center gap-1 text-amber-500">
                  <HardDrive class="h-3 w-3" />
                  Ephemeral (data lost on restart)
                </span>
              {/if}
            </div>
          </div>
          <Cylinder class="h-5 w-5 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          {#if canWrite}
            <div class="mb-4 flex flex-wrap items-center gap-2">
              <button
                type="button"
                disabled={!!actionBusy || !selectedInstance.ready}
                onclick={handleExport}
                class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent disabled:opacity-50"
              >
                <Download class="h-3.5 w-3.5" />
                {actionBusy === 'export' ? 'Exporting…' : 'Export'}
              </button>
              <button
                type="button"
                disabled={!!actionBusy || !selectedInstance.ready}
                onclick={() => importInput?.click()}
                class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent disabled:opacity-50"
              >
                <Upload class="h-3.5 w-3.5" />
                {actionBusy === 'import' ? 'Importing…' : 'Import'}
              </button>
              <input
                bind:this={importInput}
                type="file"
                class="hidden"
                onchange={(e) => handleImport(e.currentTarget.files?.[0])}
              />
              {#if selectedInstance.persistentVolumeClaims.length === 0}
                <button
                  type="button"
                  disabled={!!actionBusy}
                  onclick={handleEnablePersistence}
                  class="inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-2.5 text-xs font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
                >
                  <HardDrive class="h-3.5 w-3.5" />
                  {actionBusy === 'persist' ? 'Enabling…' : 'Enable persistence'}
                </button>
              {/if}
            </div>
            {#if actionError}
              <div class="mb-4 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-xs text-destructive">
                {actionError}
              </div>
            {/if}
            {#if actionMessage}
              <div class="mb-4 rounded-md border border-emerald-500/30 bg-emerald-500/5 p-3 text-xs text-emerald-600 dark:text-emerald-400">
                {actionMessage}
              </div>
            {/if}
          {/if}

          {#if !selectedInstance.ready}
            <div class="rounded-md border border-amber-500/30 bg-amber-500/5 p-4 text-sm text-amber-600 dark:text-amber-400">
              Pod is not ready yet — wait until it is Running before browsing data.
            </div>
          {:else if !selectedInstance.credentialsResolved}
            <div class="mb-4 rounded-md border border-amber-500/30 bg-amber-500/5 p-4 text-sm text-amber-600 dark:text-amber-400">
              Credentials could not be resolved from the pod environment — browse may fail.
              {#if selectedInstance.credentialsHint}
                <p class="mt-1 font-mono text-xs opacity-80">{selectedInstance.credentialsHint}</p>
              {/if}
            </div>
          {/if}

          {#if selectedInstance.ready && inspectQuery.isPending}
            <div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
              <RefreshCw class="h-4 w-4 animate-spin" />
              Inspecting schema…
            </div>
          {:else if selectedInstance.ready && inspectQuery.isError}
            <div class="rounded-md border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
              {inspectQuery.error.message}
            </div>
          {:else if selectedInstance.ready && inspectQuery.data}
            <div class="mb-4 grid gap-3 sm:grid-cols-4">
              <div class="rounded-md border border-border p-3">
                <p class="text-xs text-muted-foreground">Version</p>
                <p class="mt-1 text-sm font-medium">{inspectQuery.data.version || '—'}</p>
              </div>
              <div class="rounded-md border border-border p-3">
                <p class="text-xs text-muted-foreground">Collections / tables</p>
                <p class="mt-1 text-sm font-medium">{inspectQuery.data.tableCount}</p>
              </div>
              <div class="rounded-md border border-border p-3">
                <p class="text-xs text-muted-foreground">Size</p>
                <p class="mt-1 text-sm font-medium">{formatBytes(inspectQuery.data.sizeBytes)}</p>
              </div>
              <div class="rounded-md border border-border p-3">
                <p class="text-xs text-muted-foreground">Connections</p>
                <p class="mt-1 text-sm font-medium">
                  {inspectQuery.data.activeConnections < 0 ? '—' : inspectQuery.data.activeConnections}
                </p>
              </div>
            </div>

            {#if inspectQuery.data.tables.length === 0}
              <div class="py-8 text-center text-sm text-muted-foreground">
                No user collections / tables yet. Create data from your app, then refresh.
              </div>
            {:else}
              <div class="overflow-x-auto rounded-md border border-border">
                <table class="w-full text-sm">
                  <thead class="bg-muted/40 text-left text-xs text-muted-foreground">
                    <tr>
                      <th class="px-4 py-2.5 font-medium">Schema / DB</th>
                      <th class="px-4 py-2.5 font-medium">Name</th>
                      <th class="px-4 py-2.5 font-medium">Est. rows</th>
                      <th class="px-4 py-2.5 font-medium">Size</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-border">
                    {#each inspectQuery.data.tables as table}
                      <tr
                        class="cursor-pointer hover:bg-accent/30"
                        onclick={() => openTable(table)}
                        onkeydown={(e) => e.key === 'Enter' && openTable(table)}
                        tabindex="0"
                        role="button"
                      >
                        <td class="px-4 py-3 font-mono text-xs">{table.schema}</td>
                        <td class="px-4 py-3 font-medium">{table.name}</td>
                        <td class="px-4 py-3 text-muted-foreground">{table.rowEstimate}</td>
                        <td class="px-4 py-3 text-muted-foreground">{formatBytes(table.sizeBytes)}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
              <p class="mt-2 text-xs text-muted-foreground">
                Click a row to browse documents / rows.
                {#if inspectQuery.data.tablesTruncated}
                  Showing a capped list of the largest collections.
                {/if}
              </p>
            {/if}
          {/if}
        </CardContent>
      </Card>
    </div>
  {:else if visibleInstances.length === 0}
    <div class="flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-16 text-center">
      <Database class="mb-4 h-12 w-12 text-muted-foreground/30" />
      <h3 class="text-lg font-semibold">No databases found</h3>
      <p class="mt-2 max-w-md text-sm text-muted-foreground">
        Deploy MongoDB, PostgreSQL, or MySQL from Deployments and they will appear here automatically.
      </p>
    </div>
  {:else}
    <!-- Instance list -->
    <div class="overflow-hidden rounded-lg border border-border">
      <table class="w-full">
        <thead class="bg-muted/40 text-left text-xs text-muted-foreground">
          <tr>
            <th class="px-5 py-3 font-medium">Name</th>
            <th class="px-5 py-3 font-medium">Engine</th>
            <th class="px-5 py-3 font-medium">Namespace</th>
            <th class="px-5 py-3 font-medium">Status</th>
            <th class="px-5 py-3 font-medium">Credentials</th>
            <th class="px-5 py-3 font-medium">Storage</th>
            <th class="px-5 py-3 font-medium"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-border text-sm">
          {#each visibleInstances as instance}
            <tr class="hover:bg-accent/20">
              <td class="px-5 py-3.5">
                <p class="font-medium">{instance.name}</p>
                <p class="mt-0.5 font-mono text-[11px] text-muted-foreground">{instance.image}</p>
              </td>
              <td class="px-5 py-3.5">
                <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium {engineBadge(instance.engine)}">
                  {instance.engineName}
                </span>
              </td>
              <td class="px-5 py-3.5 text-muted-foreground">{instance.namespace}</td>
              <td class="px-5 py-3.5">
                {#if instance.ready}
                  <span class="inline-flex items-center gap-1 text-emerald-500">
                    <CheckCircle2 class="h-3.5 w-3.5" />
                    Ready
                  </span>
                {:else}
                  <span class="inline-flex items-center gap-1 text-amber-500">
                    <AlertCircle class="h-3.5 w-3.5" />
                    Not ready
                  </span>
                {/if}
              </td>
              <td class="px-5 py-3.5">
                {#if instance.credentialsResolved}
                  <span class="text-emerald-500">Resolved</span>
                {:else}
                  <span class="text-amber-500" title={instance.credentialsHint}>Missing</span>
                {/if}
              </td>
              <td class="px-5 py-3.5 text-xs">
                {#if instance.persistentVolumeClaims.length > 0}
                  <span class="inline-flex items-center gap-1 text-emerald-500">
                    <HardDrive class="h-3 w-3" />
                    {instance.persistentVolumeClaims.join(', ')}
                  </span>
                {:else}
                  <span class="inline-flex items-center gap-1 text-amber-500">
                    <HardDrive class="h-3 w-3" />
                    Ephemeral
                  </span>
                {/if}
              </td>
              <td class="px-5 py-3.5 text-right">
                <button
                  type="button"
                  onclick={() => openInstance(instance)}
                  class="rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                >
                  Open
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>
