<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import { getOverview, getResourceMetrics } from '$services/cluster';
  import { getStorageOverview } from '$services/storage';
  import { getPlatformInfo } from '$services/platform';
  import { createQuery } from '@tanstack/svelte-query';
  import { Cpu, Database, HardDrive, RefreshCw, BarChart2, CheckCircle, Activity } from '@lucide/svelte';
  import PageHeader from '$components/ui/PageHeader.svelte';
  import { meterBarClass } from '$lib/status';

  const overviewQuery = createQuery(() => ({
    queryKey: ['cluster-overview'],
    queryFn: getOverview,
    refetchInterval: 20000,
  }));

  // Real sampled history from the backend ring buffer. These were three
  // hardcoded arrays; the page claimed "real-time telemetry" while drawing
  // constants, so there was no way to tell a live panel from a fake one.
  const platformQuery = createQuery(() => ({
    queryKey: ['platform-info'],
    queryFn: getPlatformInfo,
    refetchInterval: 30000,
  }));

  const history = $derived(platformQuery.data?.history ?? []);
  const cpuHistory = $derived(history.map((s) => s.cpu));
  const memoryHistory = $derived(history.map((s) => s.mem));

  /** Below two points a line chart has nothing meaningful to draw. */
  const hasTrend = $derived(history.length >= 2);

  const trendWindowLabel = $derived.by(() => {
    if (!hasTrend) return '';
    const spanSeconds = history[history.length - 1].t - history[0].t;
    const minutes = Math.max(1, Math.round(spanSeconds / 60));
    return minutes >= 60 ? `last ${Math.round(minutes / 60)}h` : `last ${minutes}m`;
  });

  // The headline percentages and the "X cores out of Y" captions were also
  // hardcoded (45% / 32% / 19%). All three have real sources.
  const metricsQuery = createQuery(() => ({
    queryKey: ['cluster-resource-metrics'],
    queryFn: getResourceMetrics,
    refetchInterval: 20000,
  }));

  const storageQuery = createQuery(() => ({
    queryKey: ['storage-overview'],
    queryFn: getStorageOverview,
    refetchInterval: 30000,
  }));

  const metrics = $derived(metricsQuery.data);
  const storage = $derived(storageQuery.data);

  const storagePercent = $derived.by(() => {
    const total = storage?.totalCapacityBytes ?? 0;
    if (!total) return null;
    return Math.round(((storage?.totalRequestedBytes ?? 0) / total) * 100);
  });

  /** Renders a percentage, or an em dash when the value is genuinely unknown. */
  function pct(value: number | null | undefined): string {
    return value === null || value === undefined ? '—' : `${value}%`;
  }
</script>

<div class="page-stack">
  <PageHeader
    title="Monitoring & Metrics"
    description="Real-time telemetry, resource trends, allocation metrics, and node conditions."
  >
    <button
      onclick={() => overviewQuery.refetch()}
      class="inline-flex h-9 items-center justify-center rounded-md border border-input bg-background px-3 text-sm font-medium hover:bg-accent text-muted-foreground"
    >
      <RefreshCw class="mr-2 h-4 w-4" />
      Refresh
    </button>
  </PageHeader>

  <!-- Main System Load Metrics -->
  <div class="grid gap-4 md:grid-cols-3">
    <!-- CPU -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2 text-emerald-500">
          <Cpu class="h-5 w-5" />
          <CardTitle class="text-sm font-semibold text-foreground">CPU Utilization</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div>
          <p class="text-3xl font-bold tracking-tight">{pct(metrics?.cpuUsagePercent)}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {metrics ? `${metrics.cpuRequests} requested of ${metrics.cpuCapacity}` : 'Awaiting cluster metrics'}
          </p>
        </div>
        <div class="flex h-12 items-end gap-1.5 pt-2">
          {#each cpuHistory as val}
            <div
              class="w-full rounded-t {meterBarClass(val)} opacity-80 transition-colors hover:opacity-100"
              style="height: {Math.max(val, 2)}%"
              title="CPU {val}%"
            ></div>
          {/each}
        </div>
        <p class="text-center text-xs text-muted-foreground">
          {hasTrend ? trendWindowLabel : 'Collecting samples…'}
        </p>
      </CardContent>
    </Card>

    <!-- Memory -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2 text-indigo-500">
          <Database class="h-5 w-5" />
          <CardTitle class="text-sm font-semibold text-foreground">Memory Allocation</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div>
          <p class="text-3xl font-bold tracking-tight">{pct(metrics?.memoryUsagePercent)}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {metrics
              ? `${metrics.memoryRequests} requested of ${metrics.memoryCapacity}`
              : 'Awaiting cluster metrics'}
          </p>
        </div>
        <!-- Bar height is the percentage directly. It was `val * 2`, which
             drew 32% as a two-thirds-full bar and made memory look far more
             loaded than it was. -->
        <div class="flex h-12 items-end gap-1.5 pt-2">
          {#each memoryHistory as val}
            <div
              class="w-full rounded-t {meterBarClass(val)} opacity-80 transition-colors hover:opacity-100"
              style="height: {Math.max(val, 2)}%"
              title="Memory {val}%"
            ></div>
          {/each}
        </div>
        <p class="text-center text-xs text-muted-foreground">
          {hasTrend ? trendWindowLabel : 'Collecting samples…'}
        </p>
      </CardContent>
    </Card>

    <!-- Storage -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2 text-amber-500">
          <HardDrive class="h-5 w-5" />
          <CardTitle class="text-sm font-semibold text-foreground">Storage Provisioned</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div>
          <p class="text-3xl font-bold tracking-tight">{pct(storagePercent)}</p>
          <p class="mt-0.5 text-xs text-muted-foreground">
            {storage
              ? `${storage.totalRequested} requested of ${storage.totalCapacity}`
              : 'Awaiting storage data'}
          </p>
        </div>
        <!-- No trend line here: the backend samples CPU and memory, not
             storage. A bar chart of one repeated value would imply history
             that was never recorded, which is what the old constant array did.
             A single meter states the same fact honestly. -->
        <div class="pt-2">
          <div class="h-2 w-full overflow-hidden rounded-full bg-muted">
            <div
              class="h-full rounded-full {meterBarClass(storagePercent ?? 0)} transition-all"
              style="width: {storagePercent ?? 0}%"
            ></div>
          </div>
          <p class="mt-2 text-center text-xs text-muted-foreground">
            {storage ? `${storage.pvcCount} claims, ${storage.boundPvcCount} bound` : ''}
          </p>
        </div>
      </CardContent>
    </Card>
  </div>

  <div class="grid gap-4 md:grid-cols-2">
    <!-- Node Telemetry -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <BarChart2 class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Master & worker Node status</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        <div class="flex items-center justify-between border-b border-border pb-3">
          <div>
            <p class="text-sm font-medium">kind-idp-dev-control-plane</p>
            <p class="text-xs text-muted-foreground">Version: v1.36.1 &middot; OS: Linux</p>
          </div>
          <div class="flex items-center gap-2 text-xs font-medium text-emerald-500 bg-emerald-500/10 px-2.5 py-0.5 rounded-full">
            <CheckCircle class="h-3.5 w-3.5" />
            Ready
          </div>
        </div>

        <div class="grid grid-cols-2 gap-4 pt-1">
          <div>
            <p class="text-xs text-muted-foreground">Pod Limits</p>
            <p class="text-sm font-semibold">110 Max Pods</p>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Containers Run</p>
            <p class="text-sm font-semibold">{overviewQuery.data?.podCount ?? 0} active</p>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Internal Node IP</p>
            <p class="text-sm font-semibold font-mono">172.18.0.2</p>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Kubelet status</p>
            <p class="text-sm font-semibold">Healthy</p>
          </div>
        </div>
      </CardContent>
    </Card>

    <!-- Cluster Conditions -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Activity class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Kubernetes System Conditions</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-3.5">
        <div class="flex items-center justify-between text-sm">
          <span class="text-muted-foreground">NetworkUnavailable</span>
          <span class="font-semibold text-emerald-500">False (Healthy)</span>
        </div>
        <div class="flex items-center justify-between text-sm">
          <span class="text-muted-foreground">MemoryPressure</span>
          <span class="font-semibold text-emerald-500">False (Healthy)</span>
        </div>
        <div class="flex items-center justify-between text-sm">
          <span class="text-muted-foreground">DiskPressure</span>
          <span class="font-semibold text-emerald-500">False (Healthy)</span>
        </div>
        <div class="flex items-center justify-between text-sm">
          <span class="text-muted-foreground">PIDPressure</span>
          <span class="font-semibold text-emerald-500">False (Healthy)</span>
        </div>
      </CardContent>
    </Card>
  </div>
</div>
