<script lang="ts">
  /**
   * Horizontal bars for magnitude across nominal categories.
   *
   * Every bar carries the same hue on purpose. Shading each bar
   * darker-where-bigger would double-encode length as lightness and burn the
   * only free channel on information the bar length already shows.
   *
   * Horizontal rather than vertical because the categories here are long
   * strings (Kubernetes event reasons), which would otherwise need rotated
   * tick labels.
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
            class="h-full rounded-full bg-primary"
            style="width: {(d.value / max) * 100}%"
          ></div>
        </div>
      </li>
    {/each}
  </ul>
{/if}
