<script lang="ts">
  import { cn } from '$lib/utils';

  interface Props {
    class?: string;
    rows?: number;
    variant?: 'lines' | 'table' | 'cards' | 'page';
  }

  let { class: className = '', rows = 4, variant = 'lines' }: Props = $props();
</script>

{#if variant === 'cards'}
  <div class={cn('grid gap-3 sm:grid-cols-2 xl:grid-cols-3', className)}>
    {#each Array.from({ length: rows }) as _, i (i)}
      <div class="rounded-xl border border-border bg-card p-5">
        <div class="h-3 w-20 animate-pulse rounded bg-muted"></div>
        <div class="mt-4 h-8 w-16 animate-pulse rounded bg-muted"></div>
        <div class="mt-3 h-3 w-28 animate-pulse rounded bg-muted"></div>
      </div>
    {/each}
  </div>
{:else if variant === 'table'}
  <div class={cn('overflow-hidden rounded-xl border border-border bg-card', className)}>
    <div class="border-b border-border bg-muted/40 px-4 py-3">
      <div class="h-3 w-40 animate-pulse rounded bg-muted"></div>
    </div>
    <div class="divide-y divide-border">
      {#each Array.from({ length: rows }) as _, i (i)}
        <div class="flex items-center gap-4 px-4 py-3">
          <div class="h-3 w-1/4 animate-pulse rounded bg-muted"></div>
          <div class="h-3 w-1/5 animate-pulse rounded bg-muted"></div>
          <div class="ml-auto h-3 w-16 animate-pulse rounded bg-muted"></div>
        </div>
      {/each}
    </div>
  </div>
{:else if variant === 'page'}
  <div class={cn('space-y-4', className)}>
    <div class="h-7 w-48 animate-pulse rounded bg-muted"></div>
    <div class="h-4 w-80 max-w-full animate-pulse rounded bg-muted"></div>
    <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {#each Array.from({ length: 6 }) as _, i (i)}
        <div class="h-24 animate-pulse rounded-xl bg-muted"></div>
      {/each}
    </div>
    <div class="h-48 animate-pulse rounded-xl bg-muted"></div>
  </div>
{:else}
  <div class={cn('space-y-3', className)} role="status" aria-label="Loading">
    {#each Array.from({ length: rows }) as _, i (i)}
      <div class="h-10 w-full animate-pulse rounded-lg bg-muted"></div>
    {/each}
  </div>
{/if}
