<script lang="ts">
  import { untrack } from 'svelte';
  import { streamPodLogs, StreamError, type LogLine } from '$services/logstream';
  import { getPodLogs } from '$services/cluster';
  import LogLineRow from '$components/ui/LogLine.svelte';
  import { liveClock } from '$stores/clock';
  import { formatAgo, formatClock } from '$lib/log-style';
  import { Play, Pause, Trash2, RefreshCw, AlertCircle, Search } from '@lucide/svelte';

  interface Props {
    namespace: string;
    /** Pods available to tail. The viewer follows one at a time. */
    pods: string[];
    /** Initially selected pod. */
    podName: string;
    /** Container to read. Empty selects the pod's first container. */
    container?: string;
    /** Keep reconnecting while the pod is still starting. */
    retryNotFound?: boolean;
  }

  interface DisplayLine extends LogLine {
    receivedAt: number;
    seq: number;
  }

  let { namespace, pods, podName, container = '', retryNotFound = false }: Props = $props();

  const MAX_LINES = 5000;
  const RECONNECT_DELAYS = [1000, 2000, 5000, 10000];

  let selectedPod = $state(untrack(() => podName));
  let lines = $state<DisplayLine[]>([]);
  let paused = $state(false);
  let filter = $state('');
  let status = $state<'connecting' | 'streaming' | 'reconnecting' | 'ended' | 'error'>('connecting');
  let errorMsg = $state('');
  let autoScroll = $state(true);
  let pausedCount = $state(0);
  let seq = 0;

  let logEl = $state<HTMLDivElement | null>(null);
  let controller: AbortController | null = null;
  let pausedBuffer: DisplayLine[] = [];
  let pending: DisplayLine[] = [];
  let flushHandle = 0;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;
  let generation = 0;

  const visibleLines = $derived(
    filter.trim() === ''
      ? lines
      : lines.filter((l) => l.message.toLowerCase().includes(filter.toLowerCase())),
  );

  const lastReceivedAt = $derived(lines.length > 0 ? lines[lines.length - 1].receivedAt : 0);

  function toDisplay(line: LogLine): DisplayLine {
    seq += 1;
    return {
      ...line,
      receivedAt: Date.now(),
      seq,
    };
  }

  function flushPending() {
    flushHandle = 0;
    if (pending.length === 0) return;
    const batch = pending;
    pending = [];
    lines.push(...batch);
    if (lines.length > MAX_LINES) lines.splice(0, lines.length - MAX_LINES);
  }

  function append(line: LogLine) {
    const row = toDisplay(line);
    if (paused) {
      pausedBuffer.push(row);
      if (pausedBuffer.length > MAX_LINES) pausedBuffer.shift();
      pausedCount = pausedBuffer.length;
      return;
    }
    pending.push(row);
    if (flushHandle === 0) {
      flushHandle = requestAnimationFrame(flushPending);
    }
  }

  function scrollToBottom() {
    if (autoScroll && logEl) logEl.scrollTop = logEl.scrollHeight;
  }

  $effect(() => {
    void visibleLines.length;
    scrollToBottom();
  });

  function onScroll() {
    if (!logEl) return;
    autoScroll = logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 40;
  }

  async function connect() {
    const myGeneration = ++generation;
    controller?.abort();
    controller = new AbortController();
    status = attempt > 0 ? 'reconnecting' : 'connecting';
    errorMsg = '';

    try {
      for await (const line of streamPodLogs({
        namespace,
        podName: selectedPod,
        container,
        follow: true,
        tailLines: 200,
        signal: controller.signal,
        onOpen: () => {
          if (myGeneration !== generation) return;
          status = 'streaming';
          attempt = 0;
        },
      })) {
        if (myGeneration !== generation) return;
        status = 'streaming';
        attempt = 0;
        append(line);
      }

      if (myGeneration !== generation) return;
      if (retryNotFound) {
        await seedSnapshot(myGeneration);
        scheduleReconnect(true);
        return;
      }
      status = 'ended';
    } catch (err: any) {
      if (myGeneration !== generation) return;

      errorMsg = err?.message || 'Log stream failed';

      if (
        err instanceof StreamError &&
        (err.code === 'unauthenticated' || err.code === 'permission_denied')
      ) {
        status = 'error';
        return;
      }

      if (err instanceof StreamError && err.code === 'not_found' && !retryNotFound) {
        status = 'error';
        return;
      }
      await seedSnapshot(myGeneration);
      scheduleReconnect(false);
    }
  }

  function scheduleReconnect(keepLive = false) {
    if (!keepLive) status = 'reconnecting';
    const delay = keepLive ? 250 : RECONNECT_DELAYS[Math.min(attempt, RECONNECT_DELAYS.length - 1)];
    attempt += 1;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, delay);
  }

  async function seedSnapshot(myGeneration: number) {
    if (lines.length > 0 || pausedBuffer.length > 0) return;
    try {
      const text = await getPodLogs(namespace, selectedPod, 200);
      if (myGeneration !== generation) return;
      if (lines.length > 0) return;
      for (const row of text.split('\n')) {
        if (row !== '') {
          append({ podName: selectedPod, timestamp: '', message: row });
        }
      }
    } catch {
      // Live follow is still retrying; a snapshot miss is not fatal.
    }
  }

  function resume() {
    paused = false;
    if (pausedBuffer.length > 0) {
      lines = [...lines, ...pausedBuffer].slice(-MAX_LINES);
      pausedBuffer = [];
      pausedCount = 0;
    }
  }

  function clearLines() {
    lines = [];
    pausedBuffer = [];
    pending = [];
    pausedCount = 0;
    if (flushHandle) {
      cancelAnimationFrame(flushHandle);
      flushHandle = 0;
    }
  }

  function restart() {
    attempt = 0;
    clearLines();
    connect();
  }

  function selectPod(pod: string) {
    if (pod === selectedPod) return;
    selectedPod = pod;
    attempt = 0;
    clearLines();
  }

  $effect(() => {
    const ns = namespace;
    const pod = selectedPod;
    void ns;
    void pod;
    untrack(() => {
      connect();
    });
    return () => {
      generation += 1;
      controller?.abort();
      if (reconnectTimer) clearTimeout(reconnectTimer);
      if (flushHandle) {
        cancelAnimationFrame(flushHandle);
        flushPending();
      }
    };
  });
</script>

<div class="flex flex-col gap-3">
  <div class="flex flex-wrap items-center gap-2">
    {#if pods.length > 1}
      <select
        value={selectedPod}
        onchange={(e) => selectPod((e.currentTarget as HTMLSelectElement).value)}
        class="h-8 rounded-md border border-input bg-background px-2 font-mono text-xs focus:outline-none focus:ring-2 focus:ring-ring"
      >
        {#each pods as pod}
          <option value={pod}>{pod}</option>
        {/each}
      </select>
    {:else}
      <span class="font-mono text-xs text-muted-foreground">{selectedPod}</span>
    {/if}

    <div class="relative">
      <Search class="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
      <input
        type="text"
        placeholder="Filter lines..."
        bind:value={filter}
        class="h-8 w-40 rounded-md border border-input bg-background pl-7 pr-2 text-xs focus:outline-none focus:ring-2 focus:ring-ring md:w-52"
      />
    </div>

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
      onclick={clearLines}
      class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
    >
      <Trash2 class="h-3 w-3" />
      Clear
    </button>

    <button
      type="button"
      onclick={restart}
      class="inline-flex h-8 items-center gap-1.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium hover:bg-accent"
    >
      <RefreshCw class="h-3 w-3" />
      Reconnect
    </button>

    <div class="ml-auto flex flex-wrap items-center gap-3 text-xs">
      <span class="tabular-nums text-muted-foreground">{formatClock($liveClock)}</span>
      <div class="flex items-center gap-1.5">
        <span
          class="live-dot {status === 'streaming'
            ? 'text-emerald-500'
            : status === 'error'
              ? 'text-destructive'
              : status === 'ended'
                ? 'text-muted-foreground'
                : 'text-amber-500'}"
        ></span>
        <span class="text-muted-foreground">
          {#if status === 'streaming'}
            {paused ? 'streaming (paused)' : 'live'}
            {#if lastReceivedAt}
              · {formatAgo(lastReceivedAt, $liveClock)}
            {/if}
          {:else if status === 'reconnecting'}
            reconnecting...
          {:else if status === 'ended'}
            container exited
          {:else if status === 'error'}
            disconnected
          {:else}
            connecting...
          {/if}
        </span>
      </div>
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
    {#if visibleLines.length === 0}
      <p class="px-1 py-2 text-zinc-500">
        {#if filter.trim() !== '' && lines.length > 0}
          No lines match "{filter}".
        {:else if status === 'connecting'}
          Connecting to {selectedPod}...
        {:else if status === 'reconnecting'}
          Waiting for container output from {selectedPod}...
        {:else}
          No output yet.
        {/if}
      </p>
    {:else}
      {#each visibleLines as line (line.seq)}
        <LogLineRow
          timestamp={line.timestamp}
          receivedAt={line.receivedAt}
          message={line.message}
        />
      {/each}
    {/if}
  </div>

  <div class="flex items-center justify-between text-[11px] text-muted-foreground">
    <span>
      {visibleLines.length} line{visibleLines.length === 1 ? '' : 's'}
      {#if filter.trim() !== ''}(filtered from {lines.length}){/if}
      {#if lines.length >= MAX_LINES}· oldest trimmed at {MAX_LINES}{/if}
      · updates live
    </span>
    {#if !autoScroll}
      <button
        type="button"
        onclick={() => { autoScroll = true; scrollToBottom(); }}
        class="font-medium text-foreground hover:underline"
      >
        Jump to latest
      </button>
    {/if}
  </div>
</div>
