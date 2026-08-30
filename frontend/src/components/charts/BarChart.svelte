<script lang="ts">
  import { chartBarClass } from '$lib/status';
  /**
   * Horizontal bars for magnitude across nominal categories.
   *
   * Status-like labels (Running, Failed, Pending) get semantic colours so a
   * healthy bar is green and a failed bar is red. Other categories stay neutral.
   */
  interface Datum {
    label: string;
    value: number;
  }

  interface Props {
    data: Datum[];
    /** Describes the chart for screen readers. */
    ariaLabel: string;
    /** Unit appended to the value labels, e.g. "events". */
    unit?: string;
  }

  let { data, ariaLabel, unit = '' }: Props = $props();

  const max = $derived(Math.max(1, ...data.map((d) => d.value)));
</script>

{#if data.length === 0}
  <p class="py-6 text-center text-sm text-muted-foreground">No data to chart.</p>
{:else}
  <!-- A plain list rather than an SVG: horizontal bars need to grow with their
       own text, and HTML wraps long category names for free. -->
  <ul class="space-y-2.5" aria-label={ariaLabel}>
    {#each data as d (d.label)}
      <li class="space-y-1">
        <div class="flex items-baseline justify-between gap-3 text-xs">
          <span class="min-w-0 truncate font-medium text-foreground" title={d.label}>
            {d.label}
          </span>
          <!-- Direct-labelled, so the value never lives only in a tooltip. -->
          <span class="shrink-0 tabular-nums text-muted-foreground">
            {d.value}{unit ? ` ${unit}` : ''}
          </span>
        </div>
        <div class="h-1.5 w-full rounded-full bg-muted">
          <div
            class="h-full rounded-full {chartBarClass(d.label)}"
            style="width: {(d.value / max) * 100}%"
          ></div>
        </div>
      </li>
    {/each}
  </ul>
{/if}
