<script lang="ts">
  import LogViewer from '$components/LogViewer.svelte';
  import { listPods, getPodLogs } from '$services/cluster';
  import { buildJobName, isBuildActive, type Build } from '$services/builds';
  import { Loader2, Terminal, X, CheckCircle2, AlertCircle, Radio } from '@lucide/svelte';

  interface Props {
    build: Build;
    namespace: string;
    onClose?: () => void;
  }

  let { build, namespace, onClose }: Props = $props();

  interface FeedLine {
    id: number;
    at: string;
    kind: 'info' | 'success' | 'error' | 'log';
    text: string;
  }

  let podName = $state('');
  let podError = $state('');
  let resolving = $state(true);
  let waitMessage = $state('Starting build…');
  let feed = $state<FeedLine[]>([]);
  let nextId = 0;
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let lastReportedStatus = $state('');

  function jobName(): string {
    return build.jobName || buildJobName(build.repositoryName, build.number);
  }

  function pushFeed(kind: FeedLine['kind'], text: string) {
    feed = [...feed, {
      id: ++nextId,
      at: new Date().toLocaleTimeString(undefined, { hour12: false }),
      kind,
      text,
    }].slice(-200);
  }

  async function resolvePod(): Promise<string> {
    const prefix = jobName() + '-';
    const pods = await listPods(namespace);
    const match = pods.find((p) => p.name === jobName() || p.name.startsWith(prefix));
    return match?.name ?? '';
  }

  async function loadHistoricalLogs(name: string) {
    try {
      const logs = await getPodLogs(namespace, name, 300);
      if (!logs.trim()) return;
      for (const line of logs.split('\n').slice(-80)) {
        if (line.trim()) pushFeed('log', line);
      }
    } catch {
      // Historical logs are best-effort for finished builds.
    }
  }

  let attachGeneration = 0;

  async function attach(myGeneration: number) {
    resolving = true;
    podError = '';
    podName = '';
    feed = [];
    pushFeed('info', `Build #${build.number} queued on branch ${build.branch}.`);

    const deadline = Date.now() + (isBuildActive(build.status) ? 180_000 : 10_000);
    while (Date.now() < deadline) {
      // Leaving the Builds page must stop this loop — otherwise ListPods keeps
      // firing and state updates fight the router after unmount.
      if (myGeneration !== attachGeneration) return;
      try {
        const found = await resolvePod();
        if (myGeneration !== attachGeneration) return;
        if (found) {
          podName = found;
          pushFeed('success', `Build pod ${found} is running — streaming logs below.`);
          if (!isBuildActive(build.status)) {
            await loadHistoricalLogs(found);
          }
          if (myGeneration !== attachGeneration) return;
          resolving = false;
          return;
        }
      } catch (err: any) {
        if (myGeneration !== attachGeneration) return;
        podError = err?.message || 'Could not reach the cluster';
        resolving = false;
        return;
      }

      if (!isBuildActive(build.status)) break;
      waitMessage = 'Waiting for Kaniko pod to start…';
      await new Promise((r) => setTimeout(r, 2000));
    }

    if (myGeneration !== attachGeneration) return;
    resolving = false;
    if (isBuildActive(build.status)) {
      podError = 'Build pod has not appeared yet. The panel keeps polling while the build is active.';
    } else {
      podError = 'Build pod not found. It may have been garbage-collected after the job finished.';
    }
  }

  $effect(() => {
    const myGeneration = ++attachGeneration;
    void attach(myGeneration);

    pollTimer = setInterval(async () => {
      if (myGeneration !== attachGeneration) return;
      if (podName || !isBuildActive(build.status)) return;
      const found = await resolvePod().catch(() => '');
      if (myGeneration !== attachGeneration || !found) return;
      podName = found;
      pushFeed('success', `Build pod ${found} is running — streaming logs below.`);
      resolving = false;
    }, 3000);

    return () => {
      attachGeneration += 1;
      if (pollTimer) {
        clearInterval(pollTimer);
        pollTimer = null;
      }
    };
  });

  $effect(() => {
    if (build.status === lastReportedStatus) return;
    lastReportedStatus = build.status;
    if (build.status === 'succeeded') {
      pushFeed('success', `Build #${build.number} succeeded — image ${build.imageTag || 'tagged'}.`);
    } else if (build.status === 'failed') {
      pushFeed('error', build.errorMessage || `Build #${build.number} failed.`);
    }
  });
</script>

<div class="rounded-lg border border-border bg-card">
  <div class="flex flex-wrap items-center justify-between gap-2 border-b border-border px-4 py-3">
    <div class="flex items-center gap-2">
      <Terminal class="h-4 w-4 text-primary" />
      <div>
        <p class="text-sm font-semibold">Build #{build.number} — {build.branch}</p>
        <p class="text-xs text-muted-foreground font-mono">{jobName()}</p>
      </div>
    </div>
    <div class="flex items-center gap-2">
      {#if isBuildActive(build.status)}
        <span class="inline-flex items-center gap-1 rounded-full bg-indigo-500/10 px-2 py-0.5 text-xs font-medium text-indigo-500">
          <Radio class="h-3 w-3 animate-pulse" />
          live
        </span>
      {:else if build.status === 'succeeded'}
        <span class="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-500">
          <CheckCircle2 class="h-3 w-3" />
          succeeded
        </span>
      {:else if build.status === 'failed'}
        <span class="inline-flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
          <AlertCircle class="h-3 w-3" />
          failed
        </span>
      {/if}
      {#if onClose}
        <button onclick={onClose} aria-label="Close" class="rounded-md p-1 text-muted-foreground hover:bg-accent">
          <X class="h-4 w-4" />
        </button>
      {/if}
    </div>
  </div>

  <div class="grid gap-0 lg:grid-cols-[minmax(16rem,22rem)_1fr]">
    <div class="max-h-80 overflow-y-auto border-b border-border p-3 lg:max-h-[28rem] lg:border-b-0 lg:border-r">
      <p class="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">Activity</p>
      <ul class="space-y-2">
        {#each feed as line (line.id)}
          <li class="text-xs">
            <span class="font-mono text-muted-foreground">{line.at}</span>
            <p class={
              line.kind === 'success' ? 'text-emerald-600 dark:text-emerald-400' :
              line.kind === 'error' ? 'text-destructive' :
              line.kind === 'log' ? 'font-mono text-[11px] text-muted-foreground break-all' :
              'text-foreground'
            }>{line.text}</p>
          </li>
        {/each}
      </ul>
    </div>

    <div class="p-3">
      {#if resolving}
        <div class="flex items-center gap-2 rounded-md border border-border bg-muted/40 px-3 py-8 text-sm text-muted-foreground">
          <Loader2 class="h-4 w-4 animate-spin" />
          {waitMessage}
        </div>
      {:else if podError && !podName}
        <div class="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-3 text-sm text-destructive">
          {podError}
        </div>
      {:else if podName}
        {#key podName}
          <LogViewer
            namespace={namespace}
            podName={podName}
            pods={[podName]}
            retryNotFound={isBuildActive(build.status)}
          />
        {/key}
      {/if}
    </div>
  </div>
</div>
