<script lang="ts">
  import type { NodeInfo, PodInfo, ServiceInfo } from '$services/cluster';
  import { Box, ChevronDown, ChevronRight, Globe, Server } from '@lucide/svelte';

  interface Props {
    clusterName: string;
    connected?: boolean;
    nodes: NodeInfo[];
    pods: PodInfo[];
    services: ServiceInfo[];
    compact?: boolean;
    loading?: boolean;
    error?: string;
    registeredNodeCount?: number;
  }

  let {
    clusterName,
    connected = true,
    nodes,
    pods,
    services,
    compact = false,
    loading = false,
    error = '',
    registeredNodeCount = 0,
  }: Props = $props();

  let expanded = $state<Record<string, boolean>>({});

  const grouped = $derived.by(() => {
    const known = new Set(nodes.map((n) => n.name));
    const groups = nodes.map((node) => ({
      node,
      pods: pods.filter((p) => p.node === node.name),
    }));
    const unscheduled = pods.filter((p) => !p.node || !known.has(p.node));
    return { groups, unscheduled };
  });

  const runningPods = $derived(pods.filter((p) => p.status === 'Running').length);
  const readyNodes = $derived(nodes.filter((n) => n.status === 'Ready').length);

  function toggle(name: string) {
    expanded[name] = !expanded[name];
  }

  function podDot(status: string) {
    if (status === 'Running') return 'bg-emerald-500';
    if (status === 'Pending') return 'bg-amber-500';
    if (status === 'Succeeded') return 'bg-sky-500';
    return 'bg-destructive';
  }

  function roleLabel(role: string) {
    const r = (role || '').toLowerCase();
    if (r.includes('control') || r === 'master') return 'control-plane';
    if (r) return r;
    return 'worker';
  }
</script>

{#if loading}
  <div class="h-56 animate-pulse rounded-xl bg-muted"></div>
{:else if error}
  <div class="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
    {error}
  </div>
{:else if !connected}
  <div class="rounded-xl border border-dashed border-border p-8 text-center">
    <p class="text-sm font-medium">Live architecture needs this cluster to be active</p>
    <p class="mt-2 text-sm text-muted-foreground">
      The platform talks to one cluster at a time. Activate “{clusterName}” to see its nodes, the pods on each node, and services.
      {#if registeredNodeCount > 0}
        This cluster last reported {registeredNodeCount} node{registeredNodeCount === 1 ? '' : 's'}.
      {/if}
    </p>
  </div>
{:else}
  <div class="space-y-4">
    <div class="grid gap-3 sm:grid-cols-3">
      <div class="rounded-lg border border-border bg-card px-4 py-3">
        <p class="text-xs text-muted-foreground">Nodes</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums">
          {readyNodes}<span class="text-sm font-normal text-muted-foreground"> / {nodes.length}</span>
        </p>
        <p class="mt-0.5 text-[11px] text-muted-foreground">Ready / total</p>
      </div>
      <div class="rounded-lg border border-border bg-card px-4 py-3">
        <p class="text-xs text-muted-foreground">Pods on nodes</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums">
          {runningPods}<span class="text-sm font-normal text-muted-foreground"> / {pods.length}</span>
        </p>
        <p class="mt-0.5 text-[11px] text-muted-foreground">Running / total</p>
      </div>
      <div class="rounded-lg border border-border bg-card px-4 py-3">
        <p class="text-xs text-muted-foreground">Services</p>
        <p class="mt-1 text-2xl font-semibold tabular-nums">{services.length}</p>
        <p class="mt-0.5 text-[11px] text-muted-foreground">Cluster networking</p>
      </div>
    </div>

    <div class="overflow-hidden rounded-xl border border-border bg-card">
      <div class="border-b border-border bg-muted/40 px-4 py-3">
        <p class="text-xs font-medium uppercase tracking-wide text-muted-foreground">How this cluster is built</p>
        <p class="mt-1 text-sm text-foreground">
          <span class="font-medium">{clusterName}</span>
          schedules pods onto worker nodes. Services keep a stable address in front of those pods.
        </p>
      </div>

      <div class="space-y-4 p-4">
        <div class="flex items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 px-4 py-3">
          <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Server class="h-5 w-5" />
          </div>
          <div>
            <p class="text-sm font-semibold">Control plane</p>
            <p class="text-xs text-muted-foreground">API server, scheduler, and controllers for {clusterName}</p>
          </div>
        </div>

        <div class="ml-5 border-l-2 border-dashed border-border pl-5">
          <p class="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Nodes · {nodes.length}
          </p>
          {#if grouped.groups.length === 0}
            <p class="text-sm text-muted-foreground">No nodes reported.</p>
          {:else}
            <div class="grid gap-3 {compact ? 'md:grid-cols-2' : 'lg:grid-cols-2'}">
              {#each grouped.groups as g (g.node.name)}
                {@const open = Boolean(expanded[g.node.name])}
                <div class="rounded-lg border border-border bg-background">
                  <button
                    type="button"
                    class="flex w-full items-start gap-3 px-3 py-3 text-left hover:bg-accent/40"
                    onclick={() => toggle(g.node.name)}
                  >
                    <div class="mt-0.5 text-muted-foreground">
                      {#if open}
                        <ChevronDown class="h-4 w-4" />
                      {:else}
                        <ChevronRight class="h-4 w-4" />
                      {/if}
                    </div>
                    <div class="min-w-0 flex-1">
                      <div class="flex items-center gap-2">
                        <span
                          class="inline-flex h-2 w-2 shrink-0 rounded-full {g.node.status === 'Ready'
                            ? 'bg-emerald-500'
                            : 'bg-destructive'}"
                        ></span>
                        <p class="truncate font-mono text-sm font-medium">{g.node.name}</p>
                      </div>
                      <p class="mt-1 text-[11px] text-muted-foreground">
                        {roleLabel(g.node.role)} · {g.node.status} · {g.pods.length} pod{g.pods.length === 1 ? '' : 's'}
                      </p>
                      {#if g.node.cpuCapacity || g.node.memoryCapacity}
                        <p class="mt-0.5 font-mono text-[11px] text-muted-foreground">
                          CPU {g.node.cpuCapacity || '—'} · Mem {g.node.memoryCapacity || '—'}
                        </p>
                      {/if}
                    </div>
                    <div class="flex flex-wrap justify-end gap-1">
                      {#each g.pods.slice(0, compact ? 8 : 16) as p (p.name)}
                        <span class="h-2 w-2 rounded-sm {podDot(p.status)}" title="{p.name} · {p.status}"></span>
                      {/each}
                      {#if g.pods.length > (compact ? 8 : 16)}
                        <span class="text-[10px] text-muted-foreground">+{g.pods.length - (compact ? 8 : 16)}</span>
                      {/if}
                    </div>
                  </button>
                  {#if open}
                    <div class="border-t border-border px-3 py-2">
                      {#if g.pods.length === 0}
                        <p class="px-1 py-2 text-xs text-muted-foreground">No pods scheduled on this node.</p>
                      {:else}
                        <ul class="divide-y divide-border">
                          {#each g.pods as p (p.namespace + '/' + p.name)}
                            <li class="flex items-center gap-2 py-1.5 text-xs">
                              <Box class="h-3 w-3 shrink-0 text-muted-foreground" />
                              <span class="min-w-0 flex-1 truncate font-mono" title={p.name}>{p.name}</span>
                              <span class="truncate text-muted-foreground">{p.namespace}</span>
                              <span class="shrink-0 text-muted-foreground">{p.status}</span>
                            </li>
                          {/each}
                        </ul>
                      {/if}
                    </div>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}

          {#if grouped.unscheduled.length > 0}
            <div class="mt-3 rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-xs">
              <p class="font-medium text-amber-700 dark:text-amber-400">
                {grouped.unscheduled.length} pod{grouped.unscheduled.length === 1 ? '' : 's'} not yet on a node
              </p>
              <p class="mt-1 text-muted-foreground">Usually Pending while the scheduler picks a node.</p>
            </div>
          {/if}
        </div>

        <div class="ml-5 border-l-2 border-dashed border-border pl-5">
          <p class="mb-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Services · {services.length}
          </p>
          {#if services.length === 0}
            <p class="text-sm text-muted-foreground">No services. Workloads are only reachable inside the cluster by pod IP.</p>
          {:else}
            <div class="grid gap-2 {compact ? '' : 'sm:grid-cols-2'}">
              {#each compact ? services.slice(0, 6) : services as s (`${s.namespace}/${s.name}`)}
                <div class="flex items-center gap-2 rounded-md border border-border px-3 py-2">
                  <Globe class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <div class="min-w-0 flex-1">
                    <p class="truncate font-mono text-xs font-medium">{s.name}</p>
                    <p class="truncate text-[11px] text-muted-foreground">
                      {s.namespace} · {s.type} · {s.clusterIp || '—'}
                    </p>
                  </div>
                </div>
              {/each}
            </div>
            {#if compact && services.length > 6}
              <p class="mt-2 text-[11px] text-muted-foreground">{services.length - 6} more services</p>
            {/if}
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}
