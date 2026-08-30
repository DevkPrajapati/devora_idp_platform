<script lang="ts">
  import { untrack } from 'svelte';
  import {
    streamPodLogs,
    StreamError,
    type LogLine,
  } from '$services/logstream';
  import LogLineRow from '$components/ui/LogLine.svelte';
  import { liveClock } from '$stores/clock';
  import { formatClock, stripAnsi } from '$lib/log-style';
  import { listPods, getPodLogs, listEvents } from '$services/cluster';
  import { activityLog, matchesActivityScope } from '$stores/activitylog';
  import {
    buildJobName,
    isBuildActive,
    matchBuildPodName,
    type Build,
  } from '$services/builds';
  import {
    AlertCircle,
    CheckCircle2,
    Pause,
    Play,
    Radio,
    RefreshCw,
    Terminal,
  } from '@lucide/svelte';

  interface Props {
    build: Build;
    namespace: string;
  }

  let { build, namespace }: Props = $props();

  const MAX_LINES = 5000;
  const RECONNECT_DELAYS = [1000, 2000, 3000, 5000];

  let lines = $state<LogLine[]>([]);
  let paused = $state(false);
  let autoScroll = $state(true);
  let pausedCount = $state(0);
  let status = $state<'connecting' | 'streaming' | 'reconnecting' | 'ended' | 'error'>('connecting');
  let statusHint = $state('Opening live log stream…');
  let podName = $state('');
  let errorMsg = $state('');

  let logEl = $state<HTMLDivElement | null>(null);
  let pausedBuffer: LogLine[] = [];
  let seenKeys = new Set<string>();
  let seenActivity = new Set<number>();
  let controller: AbortController | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let lastReportedStatus = '';
  let lastErrorMessage = '';
  let streamAttempt = 0;
  let findGeneration = 0;
  let streamGeneration = 0;
  let startedAt = $state(Date.now());
  const elapsedSec = $derived(Math.max(0, Math.round(($liveClock - startedAt) / 1000)));

  function jobName(): string {
    return build.jobName || buildJobName(build.repositoryName, build.number);
  }

  function lineKey(line: LogLine): string {
    return `${line.timestamp}|${line.podName}|${line.message}`;
  }

  function append(line: LogLine) {
    line = { ...line, message: stripAnsi(line.message) };
    const key = lineKey(line);
    if (seenKeys.has(key)) return;
    seenKeys.add(key);

    if (paused) {
      pausedBuffer.push(line);
      if (pausedBuffer.length > MAX_LINES) pausedBuffer.shift();
      pausedCount = pausedBuffer.length;
      return;
    }
    lines.push(line);
    if (lines.length > MAX_LINES) {
      const dropped = lines.splice(0, lines.length - MAX_LINES);
      for (const old of dropped) seenKeys.delete(lineKey(old));
    }
  }

  function appendSystem(message: string) {
    append({
      podName: 'idp',
      timestamp: new Date().toISOString(),
      message,
    });
  }

  function scrollToBottom() {
    if (autoScroll && logEl) logEl.scrollTop = logEl.scrollHeight;
  }

  $effect(() => {
    void lines.length;
    scrollToBottom();
  });

  function onScroll() {
    if (!logEl) return;
    autoScroll = logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 40;
  }

  async function resolvePod(): Promise<string> {
    const pods = await listPods(namespace, 'idp-build');
    return matchBuildPodName(pods.map((p) => p.name), jobName());
  }

  async function ingestEvents() {
    if (!namespace) return;
    const job = jobName();
    const pod = podName;
    try {
      const events = await listEvents(namespace, 40);
      for (const event of events) {
        const object = event.object || '';
        if (job && !object.includes(job) && !(pod && object.includes(pod))) continue;
        const prefix = event.reason ? `${event.reason}: ` : '';
        append({
          podName: 'event',
          timestamp: event.timestamp || new Date().toISOString(),
          message: `${prefix}${event.message}`.trim(),
        });
      }
    } catch {
      // Events are extra context; the Kaniko stream is the source of truth.
    }
  }

  async function seedPodLogs(name: string) {
    try {
      const text = await getPodLogs(namespace, name, 400);
      if (!text.trim()) return;
      for (const row of text.split('\n')) {
        if (row === '') continue;
        append({ podName: name, timestamp: '', message: row });
      }
    } catch {
      // Follow is still retrying.
    }
  }

  async function findPodLoop(myGeneration: number) {
    if (!namespace) {
      status = 'error';
      errorMsg = 'Build namespace is not configured, so job logs cannot be streamed yet.';
      appendSystem(errorMsg);
      return;
    }

    status = 'connecting';
    statusHint = 'Waiting for the build pod… clone, compile, and push output will appear here.';
    appendSystem(`Build #${build.number} ${build.status} on ${build.branch} — watching ${jobName()}.`);
    if (build.errorMessage) appendSystem(build.errorMessage);

    while (myGeneration === findGeneration) {
      try {
        const found = await resolvePod();
        if (myGeneration !== findGeneration) return;
        if (found) {
          if (podName !== found) {
            podName = found;
            appendSystem(`Build pod ${found} — attaching live stream.`);
          }
          statusHint = `Streaming ${found}`;
          return;
        }
      } catch (err: any) {
        if (myGeneration !== findGeneration) return;
        errorMsg = err?.message || 'Could not list build pods';
        statusHint = errorMsg;
        appendSystem(errorMsg);
        await new Promise((r) => setTimeout(r, 800));
        continue;
      }

      await ingestEvents();
      if (!isBuildActive(build.status) && Date.now() - startedAt > 8_000) {
        statusHint = 'Build finished before a pod was found. Showing job events and error above.';
        if (build.errorMessage) {
          status = 'ended';
          return;
        }
      }
      await new Promise((r) => setTimeout(r, 400));
    }
  }

  async function followLogs(myGeneration: number, ns: string, pod: string) {
    controller?.abort();
    controller = new AbortController();
    if (streamAttempt > 0) {
      status = 'reconnecting';
      statusHint = `Reconnecting to ${pod}…`;
    } else if (status !== 'streaming') {
      status = 'connecting';
      statusHint = `Connecting to ${pod}…`;
    }
    errorMsg = '';

    try {
      for await (const line of streamPodLogs({
        namespace: ns,
        podName: pod,
        container: 'kaniko',
        follow: true,
        tailLines: 200,
        signal: controller.signal,
        onOpen: () => {
          if (myGeneration !== streamGeneration) return;
          status = 'streaming';
          statusHint = `Live · ${pod}`;
          streamAttempt = 0;
          errorMsg = '';
        },
      })) {
        if (myGeneration !== streamGeneration) return;
        status = 'streaming';
        statusHint = `Live · ${pod}`;
        streamAttempt = 0;
        errorMsg = '';
        append(line);
      }

      if (myGeneration !== streamGeneration) return;
      if (lines.length === 0) await seedPodLogs(pod);
      if (isBuildActive(build.status)) {
        await new Promise((r) => setTimeout(r, 250));
        if (myGeneration !== streamGeneration) return;
        void followLogs(myGeneration, ns, pod);
        return;
      }
      status = 'ended';
      statusHint = `Container exited · ${pod}`;
      appendSystem(`Log stream ended (${build.status}).`);
    } catch (err: any) {
      if (myGeneration !== streamGeneration) return;
      if (err?.name === 'AbortError') return;

      errorMsg = err?.message || 'Log stream failed';
      if (err instanceof StreamError && (err.code === 'unauthenticated' || err.code === 'permission_denied')) {
        status = 'error';
        statusHint = errorMsg;
        appendSystem(errorMsg);
        return;
      }

      if (lines.length === 0) await seedPodLogs(pod);
      if (isBuildActive(build.status)) {
        scheduleReconnect(myGeneration, ns, pod);
        return;
      }
      status = 'ended';
      statusHint = errorMsg;
      appendSystem(errorMsg);
    }
  }

  function scheduleReconnect(myGeneration: number, ns: string, pod: string) {
    status = 'reconnecting';
    statusHint = `Waiting for more output from ${pod}…`;
    const delay = RECONNECT_DELAYS[Math.min(streamAttempt, RECONNECT_DELAYS.length - 1)];
    streamAttempt += 1;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      if (myGeneration !== streamGeneration) return;
      void followLogs(myGeneration, ns, pod);
    }, delay);
  }

  // Find the Job pod once per job identity. Status polls must not tear this down
  // or the viewer resets to 0 lines every 2.5s.
  $effect(() => {
    const ns = namespace;
    const job = jobName();
    void ns;
    void job;
    startedAt = Date.now();
    lines = [];
    seenKeys = new Set();
    seenActivity = new Set();
    pausedBuffer = [];
    podName = '';
    lastReportedStatus = '';
    lastErrorMessage = '';
    const myGeneration = ++findGeneration;
    untrack(() => {
      void findPodLoop(myGeneration);
    });

    return () => {
      findGeneration += 1;
    };
  });

  $effect(() => {
    const ns = namespace;
    const pod = podName;
    if (!ns || !pod) return;
    const myGeneration = ++streamGeneration;
    streamAttempt = 0;
    untrack(() => {
      void followLogs(myGeneration, ns, pod);
    });
    return () => {
      streamGeneration += 1;
      controller?.abort();
      if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
      }
    };
  });

  $effect(() => {
    const prefixes = [`build:${build.repositoryName}`, `build:${build.repositoryName}#${build.number}`];
    const unsub = activityLog.subscribe((rows) => {
      for (const row of rows) {
        if (seenActivity.has(row.id)) continue;
        if (!matchesActivityScope(row.scope, prefixes) && row.scope !== prefixes[0]) continue;
        seenActivity.add(row.id);
        const tag = row.level === 'error' ? 'ERROR ' : row.level === 'success' ? 'OK ' : '';
        append({ podName: 'idp', timestamp: row.at, message: tag + row.message });
      }
    });
    return unsub;
  });

  $effect(() => {
    const nextStatus = build.status;
    const nextError = build.errorMessage;
    untrack(() => {
      if (nextStatus !== lastReportedStatus) {
        lastReportedStatus = nextStatus;
        appendSystem(`Build status → ${nextStatus}.`);
        if (nextStatus === 'succeeded') {
          appendSystem(`Image ${build.imageTag || 'tagged'} pushed.`);
          status = 'ended';
        } else if (nextStatus === 'failed') {
          statusHint = nextError || 'Build failed';
        }
      }
      if (nextError && nextError !== lastErrorMessage) {
        lastErrorMessage = nextError;
        appendSystem(nextError);
      }
    });
  });

  function resume() {
    paused = false;
    if (pausedBuffer.length > 0) {
      for (const line of pausedBuffer) {
        lines.push(line);
      }
      if (lines.length > MAX_LINES) lines.splice(0, lines.length - MAX_LINES);
      pausedBuffer = [];
      pausedCount = 0;
    }
  }

  function reconnect() {
    streamAttempt = 0;
    errorMsg = '';
    if (podName && namespace) {
      controller?.abort();
      void followLogs(streamGeneration, namespace, podName);
      return;
    }
    void findPodLoop(findGeneration);
  }
</script>

<div class="space-y-3">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div class="flex min-w-0 items-center gap-2">
      <Terminal class="h-4 w-4 shrink-0 text-primary" />
      <div class="min-w-0">
        <p class="text-sm font-semibold">Build #{build.number} — {build.branch}</p>
        <p class="truncate font-mono text-[11px] text-muted-foreground">{jobName()}{podName ? ` · ${podName}` : ''}</p>
      </div>
    </div>
    <div class="flex items-center gap-2">
      {#if isBuildActive(build.status)}
        <span class="inline-flex items-center gap-1 rounded-full bg-indigo-500/10 px-2 py-0.5 text-xs font-medium text-indigo-500">
          <Radio class="h-3 w-3 animate-pulse" />
          live · {elapsedSec}s
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
    </div>
  </div>

  <div class="flex flex-wrap items-center gap-2">
    <button
      type="button"
      onclick={() => (paused ? resume() : (paused = true))}
      class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
    >
      {#if paused}
        <Play class="h-3 w-3" />
        Resume{pausedCount > 0 ? ` (${pausedCount})` : ''}
      {:else}
        <Pause class="h-3 w-3" />
        Pause
      {/if}
    </button>
    <button
      type="button"
      onclick={reconnect}
      class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
    >
      <RefreshCw class="h-3 w-3" />
      Reconnect
    </button>
    <div class="ml-auto flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <span class="tabular-nums">{formatClock($liveClock)}</span>
      <span
        class="live-dot {status === 'streaming'
          ? 'text-emerald-500'
          : status === 'error'
            ? 'text-destructive'
            : status === 'ended'
              ? 'text-muted-foreground'
              : 'text-amber-500'}"
      ></span>
      <span>{statusHint}</span>
    </div>
  </div>

  {#if errorMsg && (status === 'error' || status === 'reconnecting')}
    <p class="flex items-start gap-2 rounded-md bg-destructive/10 p-2.5 text-xs text-destructive">
      <AlertCircle class="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span>{errorMsg}</span>
    </p>
  {/if}

  <div
    bind:this={logEl}
    onscroll={onScroll}
    class="log-surface log-console overflow-y-auto rounded-lg border p-2 font-mono text-xs leading-relaxed"
  >
    {#if lines.length === 0}
      <p class="px-1 py-2 text-zinc-500">{statusHint}</p>
    {:else}
      {#each lines as line, i (i)}
        <LogLineRow
          timestamp={line.timestamp}
          source={line.podName === 'idp' || line.podName === 'event' ? line.podName : ''}
          sourceClass={line.podName === 'idp' ? 'text-sky-400' : line.podName === 'event' ? 'text-violet-400' : ''}
          message={line.message}
        />
      {/each}
    {/if}
  </div>

  <div class="flex items-center justify-between text-[11px] text-muted-foreground">
    <span>{lines.length} line{lines.length === 1 ? '' : 's'} · live stream</span>
    {#if !autoScroll}
      <button
        type="button"
        onclick={() => { autoScroll = true; scrollToBottom(); }}
        class="font-medium text-primary hover:underline"
      >
        Jump to latest
      </button>
    {/if}
  </div>
</div>
