<script lang="ts">
  import { ChevronLeft, ChevronRight } from '@lucide/svelte';

  interface Props {
    page?: number;
    totalPages?: number;
    totalCount?: number | string;
    summary?: string;
    hasPrev?: boolean;
    hasNext?: boolean;
    disabled?: boolean;
    onprev: () => void;
    onnext: () => void;
  }

  let {
    page,
    totalPages,
    totalCount,
    summary,
    hasPrev,
    hasNext,
    disabled = false,
    onprev,
    onnext,
  }: Props = $props();

  const prevEnabled = $derived(hasPrev ?? (page !== undefined && page > 1));
  const nextEnabled = $derived(
    hasNext ?? (page !== undefined && totalPages !== undefined && page < totalPages),
  );
</script>

<div class="flex flex-col gap-3 border-t border-border bg-muted/20 px-4 py-3 text-xs sm:flex-row sm:items-center sm:justify-between">
  <p class="text-muted-foreground">
    {#if summary}
      {summary}
    {:else if totalCount !== undefined}
      Total records: <span class="font-medium text-foreground">{totalCount}</span>
    {/if}
  </p>
  <div class="flex items-center gap-3">
    {#if page !== undefined && totalPages !== undefined}
      <span class="text-muted-foreground">
        Page <span class="font-medium text-foreground">{page}</span>
        of
        <span class="font-medium text-foreground">{totalPages}</span>
      </span>
    {/if}
    <div class="flex items-center gap-1">
      <button
        type="button"
        disabled={disabled || !prevEnabled}
        onclick={onprev}
        aria-label="Previous page"
        class="inline-flex h-8 items-center gap-1 rounded-md border border-input bg-background px-2.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-40"
      >
        <ChevronLeft class="h-4 w-4" />
        <span class="hidden sm:inline">Prev</span>
      </button>
      <button
        type="button"
        disabled={disabled || !nextEnabled}
        onclick={onnext}
        aria-label="Next page"
        class="inline-flex h-8 items-center gap-1 rounded-md border border-input bg-background px-2.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:opacity-40"
      >
        <span class="hidden sm:inline">Next</span>
        <ChevronRight class="h-4 w-4" />
      </button>
    </div>
  </div>
</div>
