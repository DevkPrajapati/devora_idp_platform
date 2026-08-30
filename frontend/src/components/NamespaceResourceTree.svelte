<script lang="ts">
  import { formatAge } from '$lib/utils';
  import type { NamespaceResource, ResourceGroup } from '$services/cluster';
  import {
    Box,
    ChevronDown,
    ChevronRight,
    Database,
    Globe,
    HardDrive,
    Layers,
    Lock,
    Network,
    Server,
    Settings2,
    Terminal,
  } from '@lucide/svelte';

  interface Props {
    groups: ResourceGroup[];
    onOpenPodLogs?: (podName: string) => void;
  }

  let { groups, onOpenPodLogs }: Props = $props();

  let collapsed = $state<Record<string, boolean>>({});

  function toggle(name: string) {
    collapsed = { ...collapsed, [name]: !collapsed[name] };
  }

  function statusClass(status: string): string {
    const s = status.toLowerCase();
    if (['running', 'ready', 'active', 'bound', 'complete', 'available'].includes(s)) {
      return 'bg-emerald-500/10 text-emerald-500';
    }
    if (['pending', 'progressing', 'scheduled'].includes(s)) {
      return 'bg-amber-500/10 text-amber-500';
    }
    if (['failed', 'error', 'terminating', 'crashloopbackoff'].includes(s)) {
      return 'bg-destructive/10 text-destructive';
    }
    return 'bg-muted text-muted-foreground';
  }

  function kindIcon(kind: string) {
    switch (kind) {
      case 'Pod':
        return Box;
      case 'Deployment':
      case 'ReplicaSet':
      case 'StatefulSet':
      case 'DaemonSet':
        return Layers;
      case 'Job':
      case 'CronJob':
        return Server;
      case 'Service':
      case 'Ingress':
        return Globe;
      case 'ConfigMap':
        return Settings2;
      case 'Secret':
        return Lock;
      case 'PersistentVolumeClaim':
        return HardDrive;
      default:
        return Database;
    }
  }

  function groupIcon(name: string) {
    switch (name) {
      case 'Workloads':
        return Layers;
      case 'Networking':
        return Network;
      case 'Config':
        return Settings2;
      case 'Storage':
        return HardDrive;
      default:
        return Box;
    }
  }
</script>

<div class="space-y-3">
  {#each groups as group}
    {@const Icon = groupIcon(group.name)}
    {@const isCollapsed = collapsed[group.name] === true}
    <div class="overflow-hidden rounded-lg border border-border bg-card">
      <button
        type="button"
        onclick={() => toggle(group.name)}
        class="flex w-full items-center justify-between gap-3 px-4 py-3 text-left hover:bg-accent/30"
      >
        <span class="flex items-center gap-2 text-sm font-semibold">
          {#if isCollapsed}
            <ChevronRight class="h-4 w-4 text-muted-foreground" />
          {:else}
            <ChevronDown class="h-4 w-4 text-muted-foreground" />
          {/if}
          <Icon class="h-4 w-4 text-primary" />
          {group.name}
        </span>
        <span class="rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground">
          {group.items.length}
        </span>
      </button>

      {#if !isCollapsed}
        {#if group.items.length === 0}
          <p class="border-t border-border px-4 py-6 text-center text-sm text-muted-foreground">
            No {group.name.toLowerCase()} in this namespace.
          </p>
        {:else}
          <div class="overflow-x-auto border-t border-border">
            <table class="w-full min-w-[40rem] text-left text-sm">
              <thead>
                <tr class="bg-muted/40 text-xs font-semibold uppercase text-muted-foreground">
                  <th class="px-4 py-2">Kind</th>
                  <th class="px-4 py-2">Name</th>
                  <th class="px-4 py-2">Status</th>
                  <th class="px-4 py-2">Detail</th>
                  <th class="px-4 py-2">Age</th>
                  {#if onOpenPodLogs}
                    <th class="px-4 py-2 text-right">Actions</th>
                  {/if}
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#each group.items as item}
                  {@const KindIcon = kindIcon(item.kind)}
                  <tr class="hover:bg-accent/20">
                    <td class="px-4 py-2.5">
                      <span class="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                        <KindIcon class="h-3.5 w-3.5" />
                        {item.kind}
                      </span>
                    </td>
                    <td class="px-4 py-2.5 font-mono text-xs font-medium">{item.name}</td>
                    <td class="px-4 py-2.5">
                      <span class="inline-flex rounded-full px-2 py-0.5 text-xs font-medium {statusClass(item.status)}">
                        {item.status || '—'}
                      </span>
                    </td>
                    <td class="max-w-[22rem] truncate px-4 py-2.5 text-xs text-muted-foreground" title={item.detail}>
                      {item.detail || '—'}
                    </td>
                    <td class="px-4 py-2.5 text-xs text-muted-foreground">{formatAge(item.createdAt)}</td>
                    {#if onOpenPodLogs}
                      <td class="px-4 py-2.5 text-right">
                        {#if item.kind === 'Pod'}
                          <button
                            type="button"
                            onclick={() => onOpenPodLogs(item.name)}
                            class="inline-flex h-7 items-center gap-1 rounded-md border border-input bg-background px-2 text-xs font-medium hover:bg-accent"
                          >
                            <Terminal class="h-3 w-3" />
                            Logs
                          </button>
                        {/if}
                      </td>
                    {/if}
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      {/if}
    </div>
  {/each}
</div>
