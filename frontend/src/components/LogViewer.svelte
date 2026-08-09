<script lang="ts">
  import { untrack } from 'svelte';
  import { streamPodLogs, formatLogTimestamp, StreamError, type LogLine } from '$services/logstream';
  import { Play, Pause, Trash2, RefreshCw, AlertCircle, Search } from '@lucide/svelte';

  interface Props {
    namespace: string;
    /** Pods available to tail. The viewer follows one at a time. */
    pods: string[];
    /** Initially selected pod. */
    podName: string;
    /** Keep reconnecting while the pod is still starting. */
    retryNotFound?: boolean;
  }

  let { namespace, pods, podName, retryNotFound = false }: Props = $props();

  /**
   * Lines are capped so a chatty container cannot grow the DOM without bound —
   * a pod logging a few hundred lines a second would otherwise lock the tab up
   * within minutes.
   */
  const MAX_LINES = 5000;
  /** Backoff between reconnect attempts, in ms. Caps rather than giving up. */
  const RECONNECT_DELAYS = [1000, 2000, 5000, 10000];

  // Seeded from the prop, then owned locally so the in-viewer pod picker can
  // change it. untrack makes the "initial value only" intent explicit; callers
  // that need to switch pods from outside remount via {#key}.
  let selectedPod = $state(untrack(() => podName));
  let lines = $state<LogLine[]>([]);
  let paused = $state(false);
  let filter = $state('');
  let status = $state<'connecting' | 'streaming' | 'reconnecting' | 'ended' | 'error'>('connecting');
  let errorMsg = $state('');
  let autoScroll = $state(true);
  let pausedCount = $state(0);

  let logEl = $state<HTMLDivElement | null>(null);
  let controller: AbortController | null = null;
  /**
   * While paused the stream stays open and lines land here instead of in the
   * view. Closing it would lose everything written during the pause, which is
   * usually the exact window the user paused to read about.
   */
  let pausedBuffer: LogLine[] = [];
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let attempt = 0;
  /** Guards against a stale stream appending after the pod changed. */
  let generation = 0;

  const visibleLines = $derived(
    filter.trim() === ''
      ? lines
      : lines.filter((l) => l.message.toLowerCase().includes(filter.toLowerCase())),
  );

  function append(line: LogLine) {
    if (paused) {
      pausedBuffer.push(line);
      if (pausedBuffer.length > MAX_LINES) pausedBuffer.shift();
      pausedCount = pausedBuffer.length;
      return;
    }
    lines.push(line);
    if (lines.length > MAX_LINES) lines.splice(0, lines.length - MAX_LINES);
  }

  function scrollToBottom() {
    if (autoScroll && logEl) logEl.scrollTop = logEl.scrollHeight;
  }

  // Re-pins to the bottom whenever new lines render, unless the user has
  // scrolled up to read something.
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
        follow: true,
        tailLines: 200,
        signal: controller.signal,
      })) {
        if (myGeneration !== generation) return;
        if (status !== 'streaming') status = 'streaming';
        // A successful line proves the connection works, so the next drop
        // starts its backoff from zero again.
        attempt = 0;
        append(line);
      }

      if (myGeneration !== generation) return;
      // The server closed cleanly: the container exited. Reconnecting would
      // spin against a pod that is gone, so stop and say so.
      status = 'ended';
    } catch (err: any) {
      if (myGeneration !== generation) return;

      errorMsg = err?.message || 'Log stream failed';

      // A missing pod will not come back unless we're waiting for a build pod
      // that has not been scheduled yet.
      if (err instanceof StreamError && err.code === 'not_found' && !retryNotFound) {
        status = 'error';
        return;
      }
      scheduleReconnect();
    }
  }

  function scheduleReconnect() {
    status = 'reconnecting';
    const delay = RECONNECT_DELAYS[Math.min(attempt, RECONNECT_DELAYS.length - 1)];
    attempt += 1;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, delay);
  }

  function resume() {
    paused = false;
    // Buffered lines are flushed rather than dropped, so a pause reads as a
    // freeze of the view, not a gap in the logs.
    if (pausedBuffer.length > 0) {
      lines = [...lines, ...pausedBuffer].slice(-MAX_LINES);
      pausedBuffer = [];
      pausedCount = 0;
    }
  }

  function clearLines() {
    lines = [];
    pausedBuffer = [];
    pausedCount = 0;
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
    connect();
  }

  $effect(() => {
    connect();
    return () => {
      generation += 1;
      controller?.abort();
      if (reconnectTimer) clearTimeout(reconnectTimer);
    };
  });
</script>

<div class="flex flex-col gap-3">
  <!-- Controls -->
  <div class="flex flex-wrap items-center gap-2">
    {#if pods.length > 1}
      <select
        value={selectedPod}
        onchange={(e) => selectPod((e.currentTarget as HTMLSelectElement).value)}
        class="h-8 rounded-md border border-input bg-background px-2 text-xs font-mono focus:outline-none focus:ring-2 focus:ring-ring"
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
        class="h-8 w-44 rounded-md border border-input bg-background pl-7 pr-2 text-xs focus:outline-none focus:ring-2 focus:ring-ring"
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

    <div class="ml-auto flex items-center gap-1.5 text-xs">
      <span
        class="h-2 w-2 rounded-full {status === 'streaming'
          ? 'bg-emerald-500 animate-pulse'
          : status === 'error'
            ? 'bg-destructive'
            : status === 'ended'
              ? 'bg-muted-foreground'
              : 'bg-amber-500 animate-pulse'}"
      ></span>
      <span class="text-muted-foreground">
        {#if status === 'streaming'}
          {paused ? 'streaming (paused)' : 'live'}
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

  {#if errorMsg && status === 'error'}
    <p class="flex items-start gap-2 rounded-md bg-destructive/10 p-2.5 text-xs text-destructive">
      <AlertCircle class="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span>{errorMsg}</span>
    </p>
  {/if}

  <!-- Log surface -->
  <div
    bind:this={logEl}
    onscroll={onScroll}
    class="h-[50vh] overflow-y-auto rounded-lg border border-zinc-800 bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-300"
  >
    {#if visibleLines.length === 0}
      <p class="text-zinc-500">
        {#if filter.trim() !== '' && lines.length > 0}
          No lines match "{filter}".
        {:else if status === 'connecting'}
          Connecting to {selectedPod}...
        {:else}
          No output yet.
        {/if}
      </p>
    {:else}
      {#each visibleLines as line, i (i)}
        <div class="flex gap-2 hover:bg-zinc-900/60">
          {#if line.timestamp}
            <span class="shrink-0 select-none text-zinc-600">{formatLogTimestamp(line.timestamp)}</span>
          {/if}
          <span class="whitespace-pre-wrap break-all">{line.message}</span>
        </div>
      {/each}
    {/if}
  </div>

  <div class="flex items-center justify-between text-[11px] text-muted-foreground">
    <span>
      {visibleLines.length} line{visibleLines.length === 1 ? '' : 's'}
      {#if filter.trim() !== ''}(filtered from {lines.length}){/if}
      {#if lines.length >= MAX_LINES}· oldest trimmed at {MAX_LINES}{/if}
    </span>
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
