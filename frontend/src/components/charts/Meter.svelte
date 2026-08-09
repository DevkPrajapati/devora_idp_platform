<script lang="ts">
  /**
   * A single ratio against a limit — CPU requested vs allocatable, pods ready
   * vs desired. Deliberately a meter and not a two-slice donut: with one
   * quantity and its cap there is nothing to compare between slices.
   *
   * The value is always rendered as text, so the reading never depends on a
   * hover state.
   */
  interface Props {
    label: string;
    /** Portion consumed, in the same unit as `max`. */
    value: number;
    max: number;
    /** Pre-formatted detail, e.g. "1200m / 4000m". Falls back to value/max. */
    detail?: string;
    hint?: string;
  }

  let { label, value, max, detail, hint }: Props = $props();

  // Guards a divide-by-zero on a disconnected cluster, where capacity reads 0
  // and an unguarded ratio would render the bar as NaN% wide.
  const percent = $derived(max > 0 ? Math.round((value / max) * 100) : 0);
  const clamped = $derived(Math.max(0, Math.min(100, percent)));
</script>

<div class="space-y-1.5">
  <div class="flex items-baseline justify-between gap-3">
    <span class="truncate text-xs font-medium text-foreground">{label}</span>
    <span class="shrink-0 text-xs text-muted-foreground">
      {detail ?? `${value} / ${max}`}
    </span>
  </div>

  <div
    class="h-2 w-full overflow-hidden rounded-full bg-muted"
    role="meter"
    aria-valuenow={clamped}
    aria-valuemin={0}
    aria-valuemax={100}
    aria-label="{label}: {clamped}%"
  >
    <!-- Rounded data-end anchored to the baseline; the track is the same ramp
         one step down rather than a contrasting hue. -->
    <div
      class="h-full rounded-full bg-primary transition-[width] duration-500 ease-out motion-reduce:transition-none"
      style="width: {clamped}%"
    ></div>
  </div>

  <div class="flex items-baseline justify-between gap-3">
    <span class="text-xs tabular-nums text-muted-foreground">{clamped}%</span>
    {#if hint}
      <span class="truncate text-[11px] text-muted-foreground">{hint}</span>
    {/if}
  </div>
</div>
