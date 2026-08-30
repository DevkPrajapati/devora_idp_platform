<script lang="ts">
  import { formatLogTimestamp } from '$services/logstream';
  import {
    colorizeLogMessage,
    classifyLogLine,
    logLevelBarClass,
    formatClock,
  } from '$lib/log-style';

  interface Props {
    timestamp?: string;
    receivedAt?: number;
    source?: string;
    sourceClass?: string;
    message: string;
    isNew?: boolean;
  }

  let {
    timestamp = '',
    receivedAt,
    source = '',
    sourceClass = '',
    message,
    isNew = true,
  }: Props = $props();

  const level = $derived(classifyLogLine(message));
  const segments = $derived(colorizeLogMessage(message));
  const timeLabel = $derived(
    timestamp
      ? formatLogTimestamp(timestamp)
      : receivedAt
        ? formatClock(receivedAt)
        : '',
  );
</script>

<div class="log-line {isNew ? 'log-line-enter' : ''}">
  <span class="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full {logLevelBarClass(level)}"></span>
  {#if timeLabel}
    <span class="w-[5.6rem] shrink-0 select-none tabular-nums text-zinc-500">{timeLabel}</span>
  {/if}
  {#if source}
    <span class="w-20 shrink-0 truncate {sourceClass || 'text-zinc-500'}">{source}</span>
  {/if}
  <span class="min-w-0 flex-1 whitespace-pre-wrap break-all">
    {#each segments as seg, i (i)}
      <span class={seg.className}>{seg.text}</span>
    {/each}
  </span>
</div>
