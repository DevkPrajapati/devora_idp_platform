<script lang="ts">
  import { untrack } from 'svelte';
  import LogViewer from '$components/LogViewer.svelte';
  import LogLineRow from '$components/ui/LogLine.svelte';
  import { listPods, listEvents } from '$services/cluster';
  import { activityLog, matchesActivityScope, type ActivityLine } from '$stores/activitylog';
  import { Loader2 } from '@lucide/svelte';

  interface Props {
    namespace: string;
    app: string;
  }

  let { namespace, app }: Props = $props();

  let pods = $state<string[]>([]);
  let waitMsg = $state('Calling cluster… waiting for pods');
  let error = $state('');
  let feed = $state<{ id: number; at: string; text: string; kind: 'info' | 'error' | 'success' }[]>([]);
  let generation = 0;
  let seenActivity = new Set<number>();
  let seenEvents = new Set<string>();
  let nextFeed = 0;

  function pushFeed(kind: 'info' | 'error' | 'success', text: string) {
    feed = [...feed, {
      id: ++nextFeed,
      at: new Date().toISOString(),
      text,
      kind,
    }].slice(-200);
  }

  async function ingestEvents() {
    try {
      const events = await listEvents(namespace, 40);
      for (const event of events) {
        const object = event.object || '';
        if (app && !object.includes(app)) continue;
        const key = `${event.timestamp}|${event.reason}|${event.message}`;
        if (seenEvents.has(key)) continue;
        seenEvents.add(key);
        const prefix = event.reason ? `${event.reason}: ` : '';
        pushFeed(event.type === 'Warning' ? 'error' : 'info', `${prefix}${event.message}`.trim());
      }
    } catch (err: any) {
      pushFeed('error', err?.message || 'Could not list cluster events');
    }
  }

  async function resolve(myGen: number) {
    error = '';
    pods = [];
    waitMsg = `Deploy ${namespace}/${app} — waiting for pods (API + kubelet events stream here).`;
    pushFeed('info', waitMsg);
    const deadline = Date.now() + 180_000;
    while (Date.now() < deadline) {
      if (myGen !== generation) return;
      try {
        await ingestEvents();
        const found = await listPods(namespace, app);
        if (myGen !== generation) return;
        const names = found.map((p) => p.name);
        if (names.length > 0) {
          pods = names;
          pushFeed('success', `Pods ready: ${names.join(', ')} — streaming container logs.`);
          return;
        }
        waitMsg = 'No pods yet — image pull / schedule. Events above.';
      } catch (err: any) {
        if (myGen !== generation) return;
        error = err?.message || 'Could not list pods';
        pushFeed('error', error);
        await new Promise((r) => setTimeout(r, 800));
        continue;
      }
      await new Promise((r) => setTimeout(r, 400));
    }
    if (myGen !== generation) return;
    error = 'No pods appeared for this deployment yet. They may still be scheduling.';
    pushFeed('error', error);
  }

  $effect(() => {
    const myGen = ++generation;
    seenEvents = new Set();
    seenActivity = new Set();
    feed = [];
    nextFeed = 0;
    untrack(() => {
      void resolve(myGen);
    });
    const timer = setInterval(() => {
      if (myGen !== generation) return;
      listPods(namespace, app)
        .then((found) => {
          if (myGen !== generation) return;
          const names = found.map((p) => p.name);
          if (names.length) pods = names;
        })
        .catch((err: any) => {
          if (myGen !== generation) return;
          pushFeed('error', err?.message || 'ListPods failed');
        });
      void ingestEvents();
    }, 3000);
    return () => {
      generation += 1;
      clearInterval(timer);
    };
  });

  $effect(() => {
    const prefixes = [`deploy:${namespace}/${app}`, `deploy:${namespace}`];
    const unsub = activityLog.subscribe((rows: ActivityLine[]) => {
      for (const row of rows) {
        if (seenActivity.has(row.id)) continue;
        if (!matchesActivityScope(row.scope, prefixes) && row.scope !== prefixes[0]) continue;
        seenActivity.add(row.id);
        pushFeed(row.level, row.message);
      }
    });
    return unsub;
  });
</script>

<div class="space-y-3">
  {#if feed.length > 0}
    <div class="log-surface max-h-40 overflow-y-auto rounded-lg border p-2 font-mono text-xs">
      {#each feed as line (line.id)}
        <LogLineRow
          timestamp={line.at}
          source="idp"
          sourceClass={line.kind === 'error' ? 'text-red-400' : line.kind === 'success' ? 'text-emerald-400' : 'text-sky-400'}
          message={line.text}
        />
      {/each}
    </div>
  {/if}

  {#if pods.length === 0 && !error}
    <div class="log-surface log-console flex items-center justify-center gap-2 rounded-lg border px-3 font-mono text-xs text-zinc-500">
      <Loader2 class="h-4 w-4 animate-spin" />
      {waitMsg}
    </div>
  {:else if error && pods.length === 0}
    <div class="log-surface log-console flex items-center justify-center rounded-lg border border-destructive/30 px-3 text-sm text-destructive">
      {error}
    </div>
  {:else}
    {#key `${namespace}/${pods[0]}`}
      <LogViewer namespace={namespace} podName={pods[0]} pods={pods} retryNotFound={true} />
    {/key}
  {/if}
</div>
