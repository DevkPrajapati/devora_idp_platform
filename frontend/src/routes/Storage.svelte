<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import BarChart from '$components/charts/BarChart.svelte';
  import Meter from '$components/charts/Meter.svelte';
  import {
    getStorageOverview,
    listNodeStorage,
    listPersistentVolumeClaims,
    listPersistentVolumes,
    listStorageClasses,
  } from '$services/storage';
  import { createQuery } from '@tanstack/svelte-query';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import {
    Database,
    HardDrive,
    Container,
    Layers,
    RefreshCw,
    AlertCircle,
    CheckCircle2,
    AlertTriangle,
    Cpu,
  } from '@lucide/svelte';

  const overviewQuery = createQuery(() => ({
    queryKey: ['storage-overview'],
    queryFn: getStorageOverview,
    refetchInterval: 30000,
  }));

  // Claims are fetched across all namespaces once and filtered in the browser.
  // The namespace list on this page is derived from the claims themselves, so a
  // second round trip for namespaces would buy nothing.
  const claimsQuery = createQuery(() => ({
    queryKey: ['storage-pvcs'],
    queryFn: () => listPersistentVolumeClaims(''),
    refetchInterval: 30000,
  }));

  const volumesQuery = createQuery(() => ({
    queryKey: ['storage-pvs'],
    queryFn: listPersistentVolumes,
    refetchInterval: 30000,
  }));

  const classesQuery = createQuery(() => ({
    queryKey: ['storage-classes'],
    queryFn: listStorageClasses,
    staleTime: 60000,
  }));

  const nodesQuery = createQuery(() => ({
    queryKey: ['storage-node-runtimes'],
    queryFn: listNodeStorage,
    refetchInterval: 60000,
  }));

  let selectedNamespace = $state('');

  const namespaceOptions = $derived(
    [...new Set((claimsQuery.data ?? []).map((c) => c.namespace))].sort(),
  );

  const visibleClaims = $derived(
    selectedNamespace === ''
      ? (claimsQuery.data ?? [])
      : (claimsQuery.data ?? []).filter((c) => c.namespace === selectedNamespace),
  );

  // Image footprint per node — nominal categories, so one hue for every bar.
  const imageFootprint = $derived(
    (nodesQuery.data ?? [])
      .map((n) => ({ label: n.name, value: Math.round(n.imageBytes / (1024 * 1024)) }))
      .sort((a, b) => b.value - a.value),
  );

  function phaseClass(phase: string): string {
    switch (phase) {
      case 'Bound':
      case 'Available':
        return 'bg-emerald-500/10 text-emerald-500';
      case 'Pending':
        return 'bg-amber-500/10 text-amber-500';
      case 'Released':
        return 'bg-blue-500/10 text-blue-500';
      default:
        return 'bg-destructive/10 text-destructive';
    }
  }

  function refreshAll() {
    overviewQuery.refetch();
    claimsQuery.refetch();
    volumesQuery.refetch();
    classesQuery.refetch();
    nodesQuery.refetch();
  }

  function shortDate(value: string): string {
    if (!value) return '—';
    const parsed = new Date(value);
    return Number.isNaN(parsed.getTime()) ? '—' : parsed.toLocaleDateString();
  }
</script>

<div class="page-stack">
  <PageHeader
    title="PVC Storage"
    description="Persistent volume claims, backing volumes, provisioner classes, and per-node container runtime and image footprint."
  >

    <button
      onclick={refreshAll}
      aria-label="Refresh storage data"
      class="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-md border border-input bg-background text-muted-foreground hover:bg-accent"
    >
      <RefreshCw class="h-4 w-4" />
    </button>
  </PageHeader>

  {#if overviewQuery.data && !overviewQuery.data.connected}
    <Card class="border-amber-500/30 bg-amber-500/5">
      <CardContent class="flex items-center gap-3 py-5">
        <AlertTriangle class="h-5 w-5 shrink-0 text-amber-500" />
        <div>
          <h3 class="text-sm font-semibold">Cluster not connected</h3>
          <p class="mt-0.5 text-xs text-muted-foreground">
            Storage data is unavailable until the platform can reach the Kubernetes API server.
          </p>
        </div>
      </CardContent>
    </Card>
  {/if}

  <!-- Headline counts: a KPI row of stat tiles, not a grouped bar chart. -->
  <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-medium text-muted-foreground">Volume Claims</CardTitle>
          <Database class="h-4 w-4 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent>
        <p class="text-3xl font-semibold tracking-tight">{overviewQuery.data?.pvcCount ?? '—'}</p>
        <p class="mt-1 text-xs text-muted-foreground">
          {overviewQuery.data?.boundPvcCount ?? 0} bound · {overviewQuery.data?.pendingPvcCount ?? 0} pending
        </p>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-medium text-muted-foreground">Persistent Volumes</CardTitle>
          <HardDrive class="h-4 w-4 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent>
        <p class="text-3xl font-semibold tracking-tight">{overviewQuery.data?.pvCount ?? '—'}</p>
        <p class="mt-1 text-xs text-muted-foreground">
          {overviewQuery.data?.availablePvCount ?? 0} available to bind
        </p>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-medium text-muted-foreground">Storage Classes</CardTitle>
          <Layers class="h-4 w-4 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent>
        <p class="text-3xl font-semibold tracking-tight">
          {overviewQuery.data?.storageClassCount ?? '—'}
        </p>
        <p class="mt-1 text-xs text-muted-foreground">Provisioners available</p>
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-medium text-muted-foreground">Claimed Capacity</CardTitle>
          <Container class="h-4 w-4 text-muted-foreground" />
        </div>
      </CardHeader>
      <CardContent>
        <p class="text-3xl font-semibold tracking-tight">
          {overviewQuery.data?.totalRequested ?? '—'}
        </p>
        <p class="mt-1 text-xs text-muted-foreground">
          of {overviewQuery.data?.totalCapacity ?? '—'} provisioned
        </p>
      </CardContent>
    </Card>
  </div>

  <div class="grid gap-4 lg:grid-cols-2">
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <HardDrive class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">Capacity utilisation</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        {#if overviewQuery.isPending}
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
        {:else if overviewQuery.data}
          <Meter
            label="Claimed vs provisioned"
            value={overviewQuery.data.totalRequestedBytes}
            max={overviewQuery.data.totalCapacityBytes}
            detail="{overviewQuery.data.totalRequested} / {overviewQuery.data.totalCapacity}"
            hint="claims against volume capacity"
          />
          <Meter
            label="Claims bound"
            value={overviewQuery.data.boundPvcCount}
            max={overviewQuery.data.pvcCount}
            detail="{overviewQuery.data.boundPvcCount} / {overviewQuery.data.pvcCount}"
            hint="a pending claim has no volume yet"
          />
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Container class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">Container image footprint per node</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {#if nodesQuery.isPending}
          <div class="h-32 w-full animate-pulse rounded bg-muted"></div>
        {:else if nodesQuery.error}
          <p class="text-sm text-destructive">Unable to load node storage.</p>
        {:else}
          <BarChart
            data={imageFootprint}
            ariaLabel="Cached container image size per node in mebibytes"
            unit="MiB"
          />
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- One filter row above everything it scopes. -->
  <div class="flex flex-wrap items-center gap-3">
    <span class="text-sm font-medium text-muted-foreground">Namespace:</span>
    <select
      bind:value={selectedNamespace}
      aria-label="Filter claims by namespace"
      class="h-9 min-w-0 max-w-full rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm focus:outline-none"
    >
      <option value="">All namespaces</option>
      {#each namespaceOptions as ns}
        <option value={ns}>{ns}</option>
      {/each}
    </select>
    <span class="text-xs text-muted-foreground">
      {visibleClaims.length} claim{visibleClaims.length === 1 ? '' : 's'}
    </span>
  </div>

  <!-- Persistent Volume Claims -->
  <Card>
    <CardHeader>
      <CardTitle class="text-sm font-semibold">Persistent Volume Claims</CardTitle>
    </CardHeader>
    <CardContent>
      {#if claimsQuery.isPending}
        <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
      {:else if claimsQuery.error}
        <p class="text-sm text-destructive">{claimsQuery.error.message}</p>
      {:else if visibleClaims.length === 0}
        <div class="flex flex-col items-center justify-center py-10 text-center">
          <Database class="mb-3 h-10 w-10 text-muted-foreground/30" />
          <p class="text-sm font-medium">No volume claims</p>
          <p class="mt-1 text-xs text-muted-foreground">
            Workloads requesting persistent storage will appear here.
          </p>
        </div>
      {:else}
        <div class="overflow-x-auto rounded-lg border border-border">
          <table class="w-full min-w-[68rem] border-collapse text-left">
            <thead>
              <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
                <th class="px-4 py-3">Claim</th>
                <th class="px-4 py-3">Namespace</th>
                <th class="px-4 py-3">Status</th>
                <th class="px-4 py-3">Requested</th>
                <th class="px-4 py-3">Class</th>
                <th class="px-4 py-3">Access</th>
                <th class="px-4 py-3">Volume</th>
                <th class="px-4 py-3">Used by</th>
                <th class="px-4 py-3">Created</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border text-sm">
              {#each visibleClaims as c (c.namespace + '/' + c.name)}
                <tr class="transition-colors hover:bg-accent/20">
                  <td class="px-4 py-3 font-medium">{c.name}</td>
                  <td class="px-4 py-3 font-mono text-xs text-muted-foreground">{c.namespace}</td>
                  <td class="px-4 py-3">
                    <span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium {phaseClass(c.phase)}">
                      {#if c.phase === 'Bound'}
                        <CheckCircle2 class="h-3 w-3" />
                      {:else}
                        <AlertTriangle class="h-3 w-3" />
                      {/if}
                      {c.phase}
                    </span>
                  </td>
                  <td class="px-4 py-3 tabular-nums">
                    {c.requested || '—'}
                    {#if c.capacity && c.capacity !== c.requested}
                      <span class="block text-[11px] text-muted-foreground">bound: {c.capacity}</span>
                    {/if}
                  </td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{c.storageClass || '—'}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">
                    {c.accessModes.join(', ') || '—'}
                  </td>
                  <td class="px-4 py-3 font-mono text-[11px] text-muted-foreground">
                    {c.volumeName || '—'}
                  </td>
                  <td class="px-4 py-3 text-xs">
                    {#if c.usedBy.length === 0}
                      <!-- Worth calling out: an unmounted claim still holds its
                           volume, and nothing else on this page shows that. -->
                      <span class="text-amber-500">unmounted</span>
                    {:else}
                      <span class="text-muted-foreground">{c.usedBy.join(', ')}</span>
                    {/if}
                  </td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{shortDate(c.createdAt)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </CardContent>
  </Card>

  <!-- Persistent Volumes -->
  <Card>
    <CardHeader>
      <CardTitle class="text-sm font-semibold">Persistent Volumes</CardTitle>
    </CardHeader>
    <CardContent>
      {#if volumesQuery.isPending}
        <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
      {:else if volumesQuery.error}
        <p class="text-sm text-destructive">{volumesQuery.error.message}</p>
      {:else if (volumesQuery.data ?? []).length === 0}
        <p class="py-6 text-center text-sm text-muted-foreground">No persistent volumes.</p>
      {:else}
        <div class="overflow-x-auto rounded-lg border border-border">
          <table class="w-full min-w-[60rem] border-collapse text-left">
            <thead>
              <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
                <th class="px-4 py-3">Volume</th>
                <th class="px-4 py-3">Status</th>
                <th class="px-4 py-3">Capacity</th>
                <th class="px-4 py-3">Class</th>
                <th class="px-4 py-3">Driver</th>
                <th class="px-4 py-3">Reclaim</th>
                <th class="px-4 py-3">Bound claim</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border text-sm">
              {#each volumesQuery.data ?? [] as v (v.name)}
                <tr class="transition-colors hover:bg-accent/20">
                  <td class="px-4 py-3 font-mono text-xs font-medium">{v.name}</td>
                  <td class="px-4 py-3">
                    <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium {phaseClass(v.phase)}">
                      {v.phase}
                    </span>
                  </td>
                  <td class="px-4 py-3 tabular-nums">{v.capacity || '—'}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{v.storageClass || '—'}</td>
                  <td class="px-4 py-3 font-mono text-[11px] text-muted-foreground">{v.driver}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{v.reclaimPolicy || '—'}</td>
                  <td class="px-4 py-3 font-mono text-[11px] text-muted-foreground">
                    {v.claim || '—'}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </CardContent>
  </Card>

  <!-- Storage Classes -->
  <Card>
    <CardHeader>
      <CardTitle class="text-sm font-semibold">Storage Classes</CardTitle>
    </CardHeader>
    <CardContent>
      {#if classesQuery.isPending}
        <div class="h-20 w-full animate-pulse rounded bg-muted"></div>
      {:else if classesQuery.error}
        <p class="text-sm text-destructive">{classesQuery.error.message}</p>
      {:else if (classesQuery.data ?? []).length === 0}
        <p class="py-6 text-center text-sm text-muted-foreground">No storage classes.</p>
      {:else}
        <div class="overflow-x-auto rounded-lg border border-border">
          <table class="w-full min-w-[54rem] border-collapse text-left">
            <thead>
              <tr class="border-b border-border bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
                <th class="px-4 py-3">Name</th>
                <th class="px-4 py-3">Provisioner</th>
                <th class="px-4 py-3">Binding mode</th>
                <th class="px-4 py-3">Reclaim</th>
                <th class="px-4 py-3">Expandable</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border text-sm">
              {#each classesQuery.data ?? [] as sc (sc.name)}
                <tr class="transition-colors hover:bg-accent/20">
                  <td class="px-4 py-3 font-medium">
                    {sc.name}
                    {#if sc.isDefault}
                      <span class="ml-2 rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-semibold text-primary">
                        default
                      </span>
                    {/if}
                  </td>
                  <td class="px-4 py-3 font-mono text-xs text-muted-foreground">{sc.provisioner}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{sc.volumeBindingMode || '—'}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">{sc.reclaimPolicy || '—'}</td>
                  <td class="px-4 py-3 text-xs text-muted-foreground">
                    {sc.allowVolumeExpansion ? 'yes' : 'no'}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </CardContent>
  </Card>

  <!-- Container runtime / Docker detail per node -->
  <Card>
    <CardHeader>
      <div class="flex items-center gap-2">
        <Cpu class="h-4 w-4 text-muted-foreground" />
        <CardTitle class="text-sm font-semibold">Container runtime &amp; node detail</CardTitle>
      </div>
    </CardHeader>
    <CardContent>
      {#if nodesQuery.isPending}
        <div class="h-24 w-full animate-pulse rounded bg-muted"></div>
      {:else if nodesQuery.error}
        <p class="text-sm text-destructive">{nodesQuery.error.message}</p>
      {:else if (nodesQuery.data ?? []).length === 0}
        <div class="flex flex-col items-center justify-center py-10 text-center">
          <AlertCircle class="mb-3 h-10 w-10 text-muted-foreground/30" />
          <p class="text-sm font-medium">No nodes reported</p>
        </div>
      {:else}
        <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {#each nodesQuery.data ?? [] as node (node.name)}
            <div class="rounded-lg border border-border p-4">
              <div class="flex items-start justify-between gap-2">
                <div class="min-w-0">
                  <p class="truncate text-sm font-semibold" title={node.name}>{node.name}</p>
                  <p class="mt-0.5 font-mono text-[11px] text-muted-foreground">
                    {node.runtimeName || 'unknown'}
                    {node.runtimeVersion}
                  </p>
                </div>
                <span
                  class="inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium
                  {node.ready ? 'bg-emerald-500/10 text-emerald-500' : 'bg-destructive/10 text-destructive'}"
                >
                  {node.ready ? 'Ready' : 'Not ready'}
                </span>
              </div>

              <dl class="mt-3 space-y-1.5 text-xs">
                <div class="flex justify-between gap-2">
                  <dt class="text-muted-foreground">Kubelet</dt>
                  <dd class="truncate font-mono">{node.kubeletVersion || '—'}</dd>
                </div>
                <div class="flex justify-between gap-2">
                  <dt class="text-muted-foreground">OS</dt>
                  <dd class="truncate text-right" title={node.osImage}>{node.osImage || '—'}</dd>
                </div>
                <div class="flex justify-between gap-2">
                  <dt class="text-muted-foreground">Kernel / arch</dt>
                  <dd class="truncate text-right font-mono">
                    {node.kernelVersion || '—'} · {node.architecture || '—'}
                  </dd>
                </div>
                <div class="flex justify-between gap-2">
                  <dt class="text-muted-foreground">Cached images</dt>
                  <dd class="tabular-nums">{node.imageCount} · {node.imageSize}</dd>
                </div>
                <div class="flex justify-between gap-2">
                  <dt class="text-muted-foreground">Ephemeral disk</dt>
                  <dd class="tabular-nums">
                    {node.ephemeralStorageAllocatable || '—'} / {node.ephemeralStorageCapacity || '—'}
                  </dd>
                </div>
              </dl>

              {#if node.diskPressure}
                <p class="mt-3 inline-flex items-center gap-1.5 rounded-md bg-destructive/10 px-2 py-1 text-[11px] font-medium text-destructive">
                  <AlertTriangle class="h-3 w-3" />
                  Disk pressure — kubelet may evict pods
                </p>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </CardContent>
  </Card>
</div>
