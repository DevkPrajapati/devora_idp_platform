<script lang="ts">
  /**
   * The Cluster -> Node -> Pod -> Container tree.
   *
   * Pods are grouped by the node the scheduler placed them on, because the
   * questions this view answers are per-node: which node is full, which node
   * is under pressure, and which pods went down with it. Pods with no node are
   * collected separately — an unscheduled pod is a capacity problem, and
   * hiding it under a node it never reached would conceal that.
   */
  import Meter from '$components/charts/Meter.svelte';
  import { formatAge } from '$lib/utils';
  import { meterTextClass, statusBadgeClass } from '$lib/status';
  import type { ContainerInfo, NodeInfo, PodInfo } from '$services/cluster';
  import {
    AlertTriangle,
    Box,
    ChevronDown,
    ChevronRight,
    Container as ContainerIcon,
    HeartPulse,
    Server,
    Terminal,
  } from '@lucide/svelte';

  interface Props {
    clusterName: string;
    nodes: NodeInfo[];
    pods: PodInfo[];
    /** Namespace filter; empty shows every namespace. */
    namespace?: string;
    onOpenPodLogs?: (namespace: string, podName: string, container?: string) => void;
  }

  let { clusterName, nodes, pods, namespace = '', onOpenPodLogs }: Props = $props();

  const UNSCHEDULED = '__unscheduled__';

  let expandedNodes = $state<Record<string, boolean>>({});
  let expandedPods = $state<Record<string, boolean>>({});

  const visiblePods = $derived(
    namespace ? pods.filter((p) => p.namespace === namespace) : pods,
  );

  const podsByNode = $derived.by(() => {
    const map = new Map<string, PodInfo[]>();
    for (const pod of visiblePods) {
      const key = pod.node || UNSCHEDULED;
      const list = map.get(key);
      if (list) list.push(pod);
      else map.set(key, [pod]);
    }
    for (const list of map.values()) {
      list.sort((a, b) => a.namespace.localeCompare(b.namespace) || a.name.localeCompare(b.name));
    }
    return map;
  });

  const unscheduled = $derived(podsByNode.get(UNSCHEDULED) ?? []);

  // Nodes start expanded: a collapsed tree would hide the health of a
  // single-node cluster, which is the common local case.
  function nodeOpen(name: string): boolean {
    return expandedNodes[name] !== false;
  }

  function podKey(pod: PodInfo): string {
    return `${pod.namespace}/${pod.name}`;
  }

  function toggleNode(name: string) {
    expandedNodes = { ...expandedNodes, [name]: !nodeOpen(name) };
  }

  function togglePod(pod: PodInfo) {
    const key = podKey(pod);
    expandedPods = { ...expandedPods, [key]: !expandedPods[key] };
  }

  /** A pod worth attention: not ready, restarting, or unschedulable. */
  function podDegraded(pod: PodInfo): boolean {
    if (pod.phase === 'Succeeded') return false;
    return !pod.ready || pod.restartCount > 0 || pod.schedulingMessage !== '';
  }

  function readyContainers(pod: PodInfo): number {
    return pod.containers.filter((c) => c.ready).length;
  }

  function containerResources(c: ContainerInfo): string {
    const req = c.cpuRequest || c.memoryRequest
      ? `requests ${c.cpuRequest || '—'} CPU / ${c.memoryRequest || '—'}`
      : 'no requests set';
    const lim = c.cpuLimit || c.memoryLimit
      ? `limits ${c.cpuLimit || '—'} CPU / ${c.memoryLimit || '—'}`
      : 'no limits set';
    return `${req} · ${lim}`;
  }

  function probeSummary(c: ContainerInfo): string {
    const on: string[] = [];
    if (c.hasStartupProbe) on.push('startup');
    if (c.hasReadinessProbe) on.push('readiness');
    if (c.hasLivenessProbe) on.push('liveness');
    return on.length ? on.join(' · ') : 'none';
  }

  function containerLabel(c: ContainerInfo): string {
    return c.reason ? `${c.state}: ${c.reason}` : c.state;
  }
</script>

<div class="space-y-3">
  <div class="flex flex-wrap items-center gap-2 text-sm">
    <Server class="h-4 w-4 text-primary" />
    <span class="font-semibold">{clusterName}</span>
    <span class="text-muted-foreground">
      {nodes.length} node{nodes.length === 1 ? '' : 's'} ·
      {visiblePods.length} pod{visiblePods.length === 1 ? '' : 's'}
      {#if namespace}<span class="font-mono">in {namespace}</span>{/if}
    </span>
  </div>

  {#each nodes as node (node.name)}
    {@const nodePods = podsByNode.get(node.name) ?? []}
    {@const open = nodeOpen(node.name)}
    <div class="overflow-hidden rounded-lg border border-border bg-card">
      <button
        type="button"
        onclick={() => toggleNode(node.name)}
        aria-expanded={open}
        class="flex w-full flex-wrap items-center justify-between gap-3 px-4 py-3 text-left hover:bg-accent/30"
      >
        <span class="flex min-w-0 items-center gap-2">
          {#if open}
            <ChevronDown class="h-4 w-4 shrink-0 text-muted-foreground" />
          {:else}
            <ChevronRight class="h-4 w-4 shrink-0 text-muted-foreground" />
          {/if}
          <Server class="h-4 w-4 shrink-0 text-primary" />
          <span class="truncate font-mono text-sm font-semibold">{node.name}</span>
          <span class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(node.status)}">
            {node.status}
          </span>
          {#if node.role}
            <span class="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
              {node.role}
            </span>
          {/if}
          {#if node.unschedulable}
            <span class="shrink-0 rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-500">
              scheduling disabled
            </span>
          {/if}
          {#each node.pressureConditions as cond}
            <span class="shrink-0 rounded-full bg-red-500/15 px-2 py-0.5 text-xs font-medium text-red-500">
              {cond}
            </span>
          {/each}
        </span>

        <span class="flex shrink-0 items-center gap-3 text-xs text-muted-foreground">
          <span class={meterTextClass(node.podsPercent)}>
            {node.podCount}/{node.podCapacity} pods
          </span>
          {#if node.kubeletVersion}<span class="hidden sm:inline">{node.kubeletVersion}</span>{/if}
        </span>
      </button>

      {#if node.statusMessage}
        <p class="flex items-start gap-2 border-t border-border bg-red-500/5 px-4 py-2 text-xs text-red-500">
          <AlertTriangle class="mt-0.5 h-3.5 w-3.5 shrink-0" />
          {node.statusMessage}
        </p>
      {/if}

      {#if open}
        <div class="grid gap-4 border-t border-border bg-muted/20 px-4 py-3 sm:grid-cols-3">
          <Meter
            label="Pods"
            value={node.podCount}
            max={node.podCapacity}
            detail={`${node.podCount} / ${node.podCapacity}`}
            hint="Pods scheduled against this node's ceiling. New pods stay Pending once it is reached."
          />
          <Meter
            label="CPU requested"
            value={node.cpuRequestsPercent}
            max={100}
            detail={`${node.cpuRequests} / ${node.cpuAllocatable}`}
            hint="Reserved by requests, not live usage. The scheduler places pods against this."
          />
          <Meter
            label="Memory requested"
            value={node.memoryRequestsPercent}
            max={100}
            detail={`${node.memoryRequests} / ${node.memoryAllocatable}`}
            hint="Reserved by requests, not live usage."
          />
        </div>

        {#if nodePods.length === 0}
          <p class="border-t border-border px-4 py-6 text-center text-sm text-muted-foreground">
            No pods on this node{namespace ? ` in ${namespace}` : ''}.
          </p>
        {:else}
          <ul class="divide-y divide-border border-t border-border">
            {#each nodePods as pod (podKey(pod))}
              {@const podExpanded = expandedPods[podKey(pod)] === true}
              <li>
                <button
                  type="button"
                  onclick={() => togglePod(pod)}
                  aria-expanded={podExpanded}
                  class="flex w-full flex-wrap items-center justify-between gap-3 px-4 py-2.5 pl-8 text-left hover:bg-accent/20"
                >
                  <span class="flex min-w-0 items-center gap-2">
                    {#if podExpanded}
                      <ChevronDown class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    {:else}
                      <ChevronRight class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    {/if}
                    <Box class="h-3.5 w-3.5 shrink-0 {podDegraded(pod) ? 'text-amber-500' : 'text-muted-foreground'}" />
                    <span class="truncate font-mono text-xs font-medium">{pod.name}</span>
                    <span class="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                      {pod.namespace}
                    </span>
                    <span class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(pod.status)}">
                      {pod.status}
                    </span>
                  </span>

                  <span class="flex shrink-0 items-center gap-3 text-xs text-muted-foreground">
                    <span class={pod.ready ? '' : 'text-amber-500'}>
                      {readyContainers(pod)}/{pod.containers.length} ready
                    </span>
                    {#if pod.restartCount > 0}
                      <span class="text-amber-500">{pod.restartCount} restarts</span>
                    {/if}
                    <span class="hidden sm:inline">{formatAge(pod.createdAt)}</span>
                  </span>
                </button>

                {#if pod.schedulingMessage}
                  <p class="flex items-start gap-2 bg-amber-500/5 px-4 py-2 pl-14 text-xs text-amber-500">
                    <AlertTriangle class="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    Cannot be scheduled: {pod.schedulingMessage}
                  </p>
                {/if}
                {#if pod.reason || pod.message}
                  <p class="bg-red-500/5 px-4 py-2 pl-14 text-xs text-red-500">
                    {pod.reason}{pod.reason && pod.message ? ' — ' : ''}{pod.message}
                  </p>
                {/if}

                {#if podExpanded}
                  <div class="bg-muted/10 px-4 py-2 pl-14 text-xs text-muted-foreground">
                    <span class="font-medium text-foreground">Pod IP</span> {pod.ip || '—'} ·
                    <span class="font-medium text-foreground">QoS</span> {pod.qosClass || '—'} ·
                    <span class="font-medium text-foreground">Phase</span> {pod.phase || '—'}
                  </div>

                  {#if pod.containers.length === 0}
                    <p class="px-4 py-3 pl-14 text-xs text-muted-foreground">
                      No container status reported yet.
                    </p>
                  {:else}
                    <ul class="space-y-2 px-4 pb-3 pl-14">
                      {#each pod.containers as c (c.name)}
                        <li class="rounded-md border border-border bg-background p-3">
                          <div class="flex flex-wrap items-center justify-between gap-2">
                            <span class="flex min-w-0 items-center gap-2">
                              <ContainerIcon class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                              <span class="truncate font-mono text-xs font-semibold">{c.name}</span>
                              <span class="shrink-0 rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(c.reason || c.state)}">
                                {containerLabel(c)}
                              </span>
                              {#if !c.ready && c.state === 'Running'}
                                <span class="shrink-0 rounded-full bg-amber-500/15 px-2 py-0.5 text-xs font-medium text-amber-500">
                                  not receiving traffic
                                </span>
                              {/if}
                            </span>

                            {#if onOpenPodLogs}
                              <button
                                type="button"
                                onclick={() => onOpenPodLogs(pod.namespace, pod.name, c.name)}
                                class="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border border-input bg-background px-2 text-xs font-medium hover:bg-accent"
                              >
                                <Terminal class="h-3 w-3" />
                                Logs
                              </button>
                            {/if}
                          </div>

                          <p class="mt-2 break-all font-mono text-xs text-muted-foreground">{c.image}</p>

                          {#if c.message}
                            <p class="mt-2 text-xs text-red-500">{c.message}</p>
                          {/if}

                          <dl class="mt-2 grid gap-x-4 gap-y-1 text-xs sm:grid-cols-2">
                            <div class="flex gap-1.5">
                              <dt class="text-muted-foreground">Resources</dt>
                              <dd class={c.cpuRequest && c.memoryRequest ? '' : 'text-amber-500'}>
                                {containerResources(c)}
                              </dd>
                            </div>
                            <div class="flex gap-1.5">
                              <dt class="flex items-center gap-1 text-muted-foreground">
                                <HeartPulse class="h-3 w-3" />Probes
                              </dt>
                              <dd class={c.hasReadinessProbe ? '' : 'text-amber-500'}>
                                {probeSummary(c)}
                              </dd>
                            </div>
                            <div class="flex gap-1.5">
                              <dt class="text-muted-foreground">Restarts</dt>
                              <dd class={c.restartCount > 0 ? 'text-amber-500' : ''}>{c.restartCount}</dd>
                            </div>
                            {#if c.lastTerminationReason}
                              <div class="flex gap-1.5">
                                <dt class="text-muted-foreground">Last exit</dt>
                                <dd class="text-red-500">
                                  {c.lastTerminationReason} (code {c.lastExitCode})
                                </dd>
                              </div>
                            {/if}
                            {#if c.startedAt}
                              <div class="flex gap-1.5">
                                <dt class="text-muted-foreground">Started</dt>
                                <dd>{formatAge(c.startedAt)} ago</dd>
                              </div>
                            {/if}
                          </dl>
                        </li>
                      {/each}
                    </ul>
                  {/if}
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    </div>
  {/each}

  {#if unscheduled.length > 0}
    <div class="overflow-hidden rounded-lg border border-amber-500/40 bg-amber-500/5">
      <div class="flex items-center gap-2 px-4 py-3">
        <AlertTriangle class="h-4 w-4 shrink-0 text-amber-500" />
        <span class="text-sm font-semibold text-amber-500">
          {unscheduled.length} pod{unscheduled.length === 1 ? '' : 's'} not placed on any node
        </span>
      </div>
      <ul class="divide-y divide-amber-500/20 border-t border-amber-500/20">
        {#each unscheduled as pod (podKey(pod))}
          <li class="px-4 py-2.5">
            <div class="flex flex-wrap items-center gap-2">
              <Box class="h-3.5 w-3.5 shrink-0 text-amber-500" />
              <span class="font-mono text-xs font-medium">{pod.name}</span>
              <span class="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{pod.namespace}</span>
              <span class="rounded-full px-2 py-0.5 text-xs font-medium {statusBadgeClass(pod.status)}">
                {pod.status}
              </span>
            </div>
            {#if pod.schedulingMessage}
              <p class="mt-1 text-xs text-amber-500">{pod.schedulingMessage}</p>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if nodes.length === 0}
    <p class="rounded-lg border border-dashed border-border px-4 py-10 text-center text-sm text-muted-foreground">
      No nodes reported. The cluster may be starting or unreachable.
    </p>
  {/if}
</div>
