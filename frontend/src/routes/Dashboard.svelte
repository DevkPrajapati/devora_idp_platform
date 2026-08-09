<script lang="ts">
  import Card from '$components/ui/Card.svelte';
  import CardContent from '$components/ui/CardContent.svelte';
  import CardHeader from '$components/ui/CardHeader.svelte';
  import CardTitle from '$components/ui/CardTitle.svelte';
  import StatusBadge from '$components/ui/StatusBadge.svelte';
  import AreaChart from '$components/charts/AreaChart.svelte';
  import BarChart from '$components/charts/BarChart.svelte';
  import Meter from '$components/charts/Meter.svelte';
  import {
    getOverview,
    listEvents,
    getResourceMetrics,
    listNodes,
    listPods,
    listServices,
  } from '$services/cluster';
  import { listAuditLogs } from '$services/audit';
  import { HealthStatus } from '$types/health';
  import { router } from '$stores/router';
  import { createQuery } from '@tanstack/svelte-query';
  import { Activity, Box, Container, Globe, Layers, Server, ScrollText, Cpu, ArrowRight } from '@lucide/svelte';

  const overviewQuery = createQuery(() => ({
    queryKey: ['cluster-overview'],
    queryFn: getOverview,
    refetchInterval: 10000,
  }));

  // Cluster-wide (all namespaces) — same mental model as minikube dashboard.
  const podsQuery = createQuery(() => ({
    queryKey: ['dashboard-pods'],
    queryFn: () => listPods(''),
    refetchInterval: 10000,
  }));

  const servicesQuery = createQuery(() => ({
    queryKey: ['dashboard-services'],
    queryFn: () => listServices(''),
    refetchInterval: 15000,
  }));

  const podStatusChart = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const p of podsQuery.data ?? []) {
      const status = p.status || 'Unknown';
      counts.set(status, (counts.get(status) ?? 0) + 1);
    }
    return [...counts.entries()]
      .map(([label, value]) => ({ label, value }))
      .sort((a, b) => b.value - a.value);
  });

  const serviceTypeChart = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const s of servicesQuery.data ?? []) {
      const type = s.type || 'Unknown';
      counts.set(type, (counts.get(type) ?? 0) + 1);
    }
    return [...counts.entries()]
      .map(([label, value]) => ({ label, value }))
      .sort((a, b) => b.value - a.value);
  });

  const recentPods = $derived(
    [...(podsQuery.data ?? [])]
      .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
      .slice(0, 12),
  );

  const recentServices = $derived(
    [...(servicesQuery.data ?? [])]
      .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
      .slice(0, 12),
  );

  function podStatusClass(status: string): string {
    switch (status) {
      case 'Running':
        return 'bg-emerald-500/10 text-emerald-500';
      case 'Pending':
        return 'bg-amber-500/10 text-amber-500';
      case 'Failed':
      case 'Unknown':
        return 'bg-destructive/10 text-destructive';
      case 'Succeeded':
        return 'bg-sky-500/10 text-sky-500';
      default:
        return 'bg-muted text-muted-foreground';
    }
  }

  // Widened from 10 to 50 so the by-reason distribution below is drawn from a
  // representative sample rather than whatever ten events came back first.
  const eventsQuery = createQuery(() => ({
    queryKey: ['cluster-events'],
    queryFn: () => listEvents('', 50),
    refetchInterval: 15000,
  }));

  // Widened from 5 to 100 to give the activity trend enough history to plot.
  // The card below still shows only the newest few.
  const auditQuery = createQuery(() => ({
    queryKey: ['recent-audit-logs'],
    queryFn: () => listAuditLogs(1, 100).then(res => res.logs),
    refetchInterval: 15000,
  }));

  const recentAudit = $derived((auditQuery.data ?? []).slice(0, 5));
  const recentEvents = $derived((eventsQuery.data ?? []).slice(0, 8));

  const metricsQuery = createQuery(() => ({
    queryKey: ['cluster-resource-metrics'],
    queryFn: getResourceMetrics,
    refetchInterval: 15000,
  }));

  const nodesQuery = createQuery(() => ({
    queryKey: ['cluster-nodes'],
    queryFn: listNodes,
    refetchInterval: 30000,
  }));

  function getHealthStatus(connected: boolean): HealthStatus {
    return connected ? HealthStatus.HEALTHY : HealthStatus.UNHEALTHY;
  }

  const ACTIVITY_DAYS = 14;

  /**
   * Buckets by *local* calendar day. Using the UTC date instead would file an
   * evening action under tomorrow for anyone east of UTC, so the trend would
   * disagree with the timestamps shown in the audit card beside it.
   */
  function localDayKey(date: Date): string {
    return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
  }

  const activitySeries = $derived.by(() => {
    const buckets = new Map<string, number>();
    const labels: { key: string; label: string }[] = [];

    const today = new Date();
    today.setHours(0, 0, 0, 0);

    // Every day in the window is seeded, so quiet days plot as a real zero
    // instead of being dropped and compressing the time axis.
    for (let offset = ACTIVITY_DAYS - 1; offset >= 0; offset--) {
      const day = new Date(today);
      day.setDate(day.getDate() - offset);
      const key = localDayKey(day);
      labels.push({ key, label: day.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) });
      buckets.set(key, 0);
    }

    for (const log of auditQuery.data ?? []) {
      const at = new Date(log.createdAt);
      if (Number.isNaN(at.getTime())) continue;
      const key = localDayKey(at);
      // Anything older than the window is simply outside the chart.
      if (buckets.has(key)) buckets.set(key, (buckets.get(key) ?? 0) + 1);
    }

    return labels.map((l) => ({ label: l.label, value: buckets.get(l.key) ?? 0 }));
  });

  const eventsByReason = $derived.by(() => {
    const counts = new Map<string, number>();
    for (const ev of eventsQuery.data ?? []) {
      if (!ev.reason) continue;
      counts.set(ev.reason, (counts.get(ev.reason) ?? 0) + 1);
    }
    // Capped at six: past that the bars stop being comparable at a glance and
    // the card wants a table instead.
    return [...counts.entries()]
      .map(([label, value]) => ({ label, value }))
      .sort((a, b) => b.value - a.value)
      .slice(0, 6);
  });
</script>

<div class="space-y-6">
  <div>
    <h1 class="text-2xl font-semibold tracking-tight">Dashboard</h1>
    <p class="mt-1 text-sm text-muted-foreground">
      Cluster inventory like a Kubernetes dashboard — pods, services, charts, no CLI.
    </p>
  </div>

  <!-- Cluster Health Banner -->
  <Card class="border-primary/20 bg-primary/5">
    <CardHeader>
      <div class="flex items-center justify-between">
        <CardTitle class="text-base font-semibold">Cluster Connection Status</CardTitle>
        {#if overviewQuery.isPending}
          <span class="text-sm text-muted-foreground animate-pulse">Checking status...</span>
        {:else if overviewQuery.error}
          <StatusBadge status={HealthStatus.UNHEALTHY} />
        {:else if overviewQuery.data}
          <StatusBadge status={getHealthStatus(overviewQuery.data.connected)} />
        {/if}
      </div>
    </CardHeader>
    <CardContent>
      {#if overviewQuery.isPending}
        <div class="space-y-2">
          <div class="h-4 w-48 animate-pulse rounded bg-muted"></div>
          <div class="h-4 w-32 animate-pulse rounded bg-muted"></div>
        </div>
      {:else if overviewQuery.error}
        <p class="text-sm text-destructive">
          Unable to contact API server: {overviewQuery.error.message}
        </p>
      {:else if overviewQuery.data}
        <div class="flex flex-wrap gap-x-8 gap-y-4">
          <div>
            <p class="text-xs text-muted-foreground">Cluster Name</p>
            <p class="text-sm font-semibold text-foreground">{overviewQuery.data.clusterName}</p>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Control Plane Nodes</p>
            <p class="text-sm font-semibold text-foreground">
              {overviewQuery.data.readyNodes} / {overviewQuery.data.nodeCount} Ready
            </p>
          </div>
          <div>
            <p class="text-xs text-muted-foreground">Kubernetes Status</p>
            <span class="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-500">
              Active
            </span>
          </div>
        </div>
      {/if}
    </CardContent>
  </Card>

  <!-- Metric Count Cards — click through to full lists -->
  <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
    <button
      type="button"
      onclick={() => router.navigate('/namespaces')}
      class="text-left transition-colors hover:opacity-90"
    >
      <Card class="h-full">
        <CardHeader>
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium text-muted-foreground">Namespaces</CardTitle>
            <Layers class="h-4 w-4 text-muted-foreground" />
          </div>
        </CardHeader>
        <CardContent>
          <p class="text-3xl font-semibold tracking-tight">
            {overviewQuery.data?.namespaceCount ?? '—'}
          </p>
          <p class="mt-1 text-xs text-muted-foreground">Click to open</p>
        </CardContent>
      </Card>
    </button>

    <button
      type="button"
      onclick={() => router.navigate('/deployments')}
      class="text-left transition-colors hover:opacity-90"
    >
      <Card class="h-full">
        <CardHeader>
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium text-muted-foreground">Deployments</CardTitle>
            <Container class="h-4 w-4 text-muted-foreground" />
          </div>
        </CardHeader>
        <CardContent>
          <p class="text-3xl font-semibold tracking-tight">
            {overviewQuery.data?.deploymentCount ?? '—'}
          </p>
          <p class="mt-1 text-xs text-muted-foreground">Click to open</p>
        </CardContent>
      </Card>
    </button>

    <button
      type="button"
      onclick={() => router.navigate('/services')}
      class="text-left transition-colors hover:opacity-90"
    >
      <Card class="h-full">
        <CardHeader>
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium text-muted-foreground">Services</CardTitle>
            <Globe class="h-4 w-4 text-muted-foreground" />
          </div>
        </CardHeader>
        <CardContent>
          <p class="text-3xl font-semibold tracking-tight">
            {servicesQuery.data?.length ?? overviewQuery.data?.serviceCount ?? '—'}
          </p>
          <p class="mt-1 text-xs text-muted-foreground">All namespaces · click to open</p>
        </CardContent>
      </Card>
    </button>

    <button
      type="button"
      onclick={() => router.navigate('/workloads')}
      class="text-left transition-colors hover:opacity-90"
    >
      <Card class="h-full">
        <CardHeader>
          <div class="flex items-center justify-between">
            <CardTitle class="text-sm font-medium text-muted-foreground">Pods</CardTitle>
            <Box class="h-4 w-4 text-muted-foreground" />
          </div>
        </CardHeader>
        <CardContent>
          <p class="text-3xl font-semibold tracking-tight">
            {overviewQuery.data?.runningPods ?? 0}
            <span class="text-sm font-normal text-muted-foreground">
              / {podsQuery.data?.length ?? overviewQuery.data?.podCount ?? 0}
            </span>
          </p>
          <p class="mt-1 text-xs text-muted-foreground">Running / total · click to open</p>
        </CardContent>
      </Card>
    </button>
  </div>

  <!-- Workload charts (minikube-dashboard style) -->
  <div class="grid gap-4 md:grid-cols-2">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <Box class="h-4 w-4 text-muted-foreground" />
            <CardTitle class="text-sm font-semibold">Pods by status</CardTitle>
          </div>
          <button
            type="button"
            onclick={() => router.navigate('/workloads')}
            class="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            All pods <ArrowRight class="h-3 w-3" />
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {#if podsQuery.isPending}
          <div class="h-40 w-full animate-pulse rounded bg-muted"></div>
        {:else if podsQuery.error}
          <p class="text-sm text-destructive">{podsQuery.error.message}</p>
        {:else}
          <BarChart data={podStatusChart} ariaLabel="Pods grouped by phase" unit="pods" />
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <div class="flex items-center gap-2">
            <Globe class="h-4 w-4 text-muted-foreground" />
            <CardTitle class="text-sm font-semibold">Services by type</CardTitle>
          </div>
          <button
            type="button"
            onclick={() => router.navigate('/services')}
            class="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            All services <ArrowRight class="h-3 w-3" />
          </button>
        </div>
      </CardHeader>
      <CardContent>
        {#if servicesQuery.isPending}
          <div class="h-40 w-full animate-pulse rounded bg-muted"></div>
        {:else if servicesQuery.error}
          <p class="text-sm text-destructive">{servicesQuery.error.message}</p>
        {:else}
          <BarChart data={serviceTypeChart} ariaLabel="Services grouped by type" unit="services" />
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- Live cluster inventory -->
  <div class="grid gap-4 lg:grid-cols-2">
    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-semibold">
            Pods ({podsQuery.data?.length ?? 0})
          </CardTitle>
          <button
            type="button"
            onclick={() => router.navigate('/workloads')}
            class="text-xs text-primary hover:underline"
          >
            View all
          </button>
        </div>
      </CardHeader>
      <CardContent class="p-0">
        {#if podsQuery.isPending}
          <div class="space-y-2 p-4">
            <div class="h-8 animate-pulse rounded bg-muted"></div>
            <div class="h-8 animate-pulse rounded bg-muted"></div>
          </div>
        {:else if recentPods.length === 0}
          <p class="p-6 text-center text-sm text-muted-foreground">No pods in the cluster.</p>
        {:else}
          <div class="max-h-80 overflow-auto">
            <table class="w-full text-left text-sm">
              <thead class="sticky top-0 bg-muted/80 text-xs text-muted-foreground">
                <tr>
                  <th class="px-4 py-2 font-medium">Name</th>
                  <th class="px-4 py-2 font-medium">Namespace</th>
                  <th class="px-4 py-2 font-medium">Status</th>
                  <th class="px-4 py-2 font-medium">Node</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#each recentPods as p}
                  <tr class="hover:bg-accent/20">
                    <td class="max-w-[10rem] truncate px-4 py-2 font-medium" title={p.name}>{p.name}</td>
                    <td class="px-4 py-2 text-xs text-muted-foreground">{p.namespace}</td>
                    <td class="px-4 py-2">
                      <span class="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium {podStatusClass(p.status)}">
                        {p.status}
                      </span>
                    </td>
                    <td class="px-4 py-2 font-mono text-[11px] text-muted-foreground">{p.node || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center justify-between">
          <CardTitle class="text-sm font-semibold">
            Services ({servicesQuery.data?.length ?? 0})
          </CardTitle>
          <button
            type="button"
            onclick={() => router.navigate('/services')}
            class="text-xs text-primary hover:underline"
          >
            View all
          </button>
        </div>
      </CardHeader>
      <CardContent class="p-0">
        {#if servicesQuery.isPending}
          <div class="space-y-2 p-4">
            <div class="h-8 animate-pulse rounded bg-muted"></div>
            <div class="h-8 animate-pulse rounded bg-muted"></div>
          </div>
        {:else if recentServices.length === 0}
          <p class="p-6 text-center text-sm text-muted-foreground">No services in the cluster.</p>
        {:else}
          <div class="max-h-80 overflow-auto">
            <table class="w-full text-left text-sm">
              <thead class="sticky top-0 bg-muted/80 text-xs text-muted-foreground">
                <tr>
                  <th class="px-4 py-2 font-medium">Name</th>
                  <th class="px-4 py-2 font-medium">Namespace</th>
                  <th class="px-4 py-2 font-medium">Type</th>
                  <th class="px-4 py-2 font-medium">Cluster IP</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#each recentServices as s}
                  <tr class="hover:bg-accent/20">
                    <td class="max-w-[10rem] truncate px-4 py-2 font-medium" title={s.name}>{s.name}</td>
                    <td class="px-4 py-2 text-xs text-muted-foreground">{s.namespace}</td>
                    <td class="px-4 py-2 text-xs">{s.type}</td>
                    <td class="px-4 py-2 font-mono text-[11px] text-muted-foreground">{s.clusterIp || '—'}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- Resource Metrics Visualizer -->
  <div class="grid gap-4 md:grid-cols-2">
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Cpu class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">Cluster Resource Load</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-4">
        {#if metricsQuery.isPending}
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
        {:else if metricsQuery.error}
          <p class="text-sm text-destructive">Unable to load resource metrics: {metricsQuery.error.message}</p>
        {:else if metricsQuery.data}
          <!-- Four independent ratios, each against its own limit, so each gets
               its own meter and its own scale. Plotting CPU and memory together
               would need two y-scales on one plot, which invents a relationship
               the data does not contain. -->
          <Meter
            label="CPU allocation (requests)"
            value={metricsQuery.data.cpuUsagePercent}
            max={100}
            detail="{metricsQuery.data.cpuRequests} / {metricsQuery.data.cpuCapacity}"
          />
          <Meter
            label="Memory allocation (requests)"
            value={metricsQuery.data.memoryUsagePercent}
            max={100}
            detail="{metricsQuery.data.memoryRequests} / {metricsQuery.data.memoryCapacity}"
          />
          {#if overviewQuery.data}
            <Meter
              label="Pods running"
              value={overviewQuery.data.runningPods}
              max={overviewQuery.data.podCount}
              detail="{overviewQuery.data.runningPods} / {overviewQuery.data.podCount}"
            />
            <Meter
              label="Nodes ready"
              value={overviewQuery.data.readyNodes}
              max={overviewQuery.data.nodeCount}
              detail="{overviewQuery.data.readyNodes} / {overviewQuery.data.nodeCount}"
            />
          {/if}
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Server class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">Node Status Overview</CardTitle>
        </div>
      </CardHeader>
      <CardContent class="space-y-2 py-2">
        {#if nodesQuery.isPending}
          <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
        {:else if nodesQuery.error}
          <p class="text-sm text-destructive">Unable to load nodes: {nodesQuery.error.message}</p>
        {:else if nodesQuery.data && nodesQuery.data.length > 0}
          {#each nodesQuery.data as node (node.name)}
            <div class="flex items-center justify-between">
              <div class="flex flex-col">
                <span class="text-sm font-medium">{node.name}</span>
                <span class="text-xs text-muted-foreground">Role: {node.role || 'worker'}</span>
              </div>
              <div class="flex items-center gap-2">
                <span
                  class="inline-flex h-2.5 w-2.5 rounded-full {node.status === 'Ready' ? 'bg-emerald-500' : 'bg-red-500'}"
                ></span>
                <span class="text-sm font-medium">{node.status}</span>
              </div>
            </div>
          {/each}
        {:else}
          <p class="text-sm text-muted-foreground">No nodes reported.</p>
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- Trends -->
  <div class="grid gap-4 lg:grid-cols-3">
    <Card class="lg:col-span-2">
      <CardHeader>
        <div class="flex items-center gap-2">
          <ScrollText class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">
            Platform activity — last {ACTIVITY_DAYS} days
          </CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {#if auditQuery.isPending}
          <div class="h-44 w-full animate-pulse rounded bg-muted"></div>
        {:else if auditQuery.error}
          <p class="text-sm text-destructive">Unable to load activity history.</p>
        {:else}
          <AreaChart
            data={activitySeries}
            ariaLabel="Audited platform actions per day over the last {ACTIVITY_DAYS} days"
            unit="actions"
          />
        {/if}
      </CardContent>
    </Card>

    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Activity class="h-4 w-4 text-muted-foreground" />
          <CardTitle class="text-sm font-semibold">Warning events by reason</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {#if eventsQuery.isPending}
          <div class="h-44 w-full animate-pulse rounded bg-muted"></div>
        {:else if eventsQuery.error}
          <p class="text-sm text-destructive">Unable to load events.</p>
        {:else}
          <BarChart
            data={eventsByReason}
            ariaLabel="Cluster warning events grouped by reason"
            unit="events"
          />
        {/if}
      </CardContent>
    </Card>
  </div>

  <!-- Recent Activity & Cluster Events -->
  <div class="grid gap-4 lg:grid-cols-2">
    <!-- Audit / Activity Logs -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <ScrollText class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Recent Audit Trail</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {#if auditQuery.isPending}
          <div class="space-y-4">
            <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
            <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
          </div>
        {:else if auditQuery.error}
          <p class="text-sm text-destructive">Failed to fetch activities.</p>
        {:else if recentAudit.length === 0}
          <div class="flex flex-col items-center justify-center py-8 text-center">
            <Server class="mb-2 h-8 w-8 text-muted-foreground/30" />
            <p class="text-sm font-medium">No recent actions</p>
            <p class="text-xs text-muted-foreground">Platform events will stream here</p>
          </div>
        {:else}
          <div class="divide-y divide-border">
            {#each recentAudit as log}
              <div class="py-2.5 text-sm">
                <div class="flex items-center justify-between">
                  <span class="font-medium text-foreground">{log.action}</span>
                  <span class="text-xs text-muted-foreground">
                    {new Date(log.createdAt).toLocaleTimeString()}
                  </span>
                </div>
                <div class="mt-0.5 flex items-center justify-between text-xs text-muted-foreground">
                  <span>{log.userEmail} &middot; {log.resourceType}/{log.resource}</span>
                  <span class={log.result === 'success' ? 'text-emerald-500' : 'text-rose-500'}>
                    {log.result}
                  </span>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </CardContent>
    </Card>

    <!-- Cluster Events -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-2">
          <Activity class="h-4 w-4 text-muted-foreground" />
          <CardTitle>Kubernetes Warning Events</CardTitle>
        </div>
      </CardHeader>
      <CardContent>
        {#if eventsQuery.isPending}
          <div class="space-y-4">
            <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
            <div class="h-8 w-full animate-pulse rounded bg-muted"></div>
          </div>
        {:else if eventsQuery.error}
          <p class="text-sm text-destructive">Failed to retrieve events.</p>
        {:else if recentEvents.length === 0}
          <div class="flex flex-col items-center justify-center py-8 text-center">
            <Box class="mb-2 h-8 w-8 text-muted-foreground/30" />
            <p class="text-sm font-medium">No warning events</p>
            <p class="text-xs text-muted-foreground">No cluster errors detected</p>
          </div>
        {:else}
          <div class="divide-y divide-border">
            {#each recentEvents as ev}
              <div class="py-2 text-xs">
                <div class="flex items-center justify-between">
                  <span class="font-semibold text-rose-500">{ev.reason}</span>
                  <span class="text-muted-foreground">
                    {new Date(ev.timestamp).toLocaleTimeString()}
                  </span>
                </div>
                <p class="mt-0.5 text-foreground">{ev.message}</p>
                <div class="mt-0.5 text-muted-foreground">
                  ns: {ev.namespace} &middot; obj: {ev.object}
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </CardContent>
    </Card>
  </div>
</div>
