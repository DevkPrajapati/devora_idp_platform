<script lang="ts">
  /**
   * Single-series trend over time.
   *
   * One series, so there is no legend — the card title names it. Scales with
   * its container through the viewBox; the bottom band is inside the viewBox
   * so the x tick labels can never be clipped by the card's height.
   */
  interface Datum {
    label: string;
    value: number;
  }

  interface Props {
    data: Datum[];
    ariaLabel: string;
    unit?: string;
  }

  let { data, ariaLabel, unit = '' }: Props = $props();

  // Fixed internal coordinate system; the rendered size comes from the parent.
  const W = 600;
  const H = 200;
  const PAD = { top: 12, right: 14, bottom: 30, left: 34 };
  const plotW = W - PAD.left - PAD.right;
  const plotH = H - PAD.top - PAD.bottom;
  const baseline = PAD.top + plotH;

  // A flat all-zero series would collapse the scale, so the axis keeps a
  // minimum top of 1 and the line sits on the baseline instead of vanishing.
  const max = $derived(Math.max(1, ...data.map((d) => d.value)));

  const points = $derived(
    data.map((d, i) => ({
      ...d,
      // A single sample has no interval to spread across, so it is centred
      // rather than pinned to the left edge.
      x: data.length === 1 ? PAD.left + plotW / 2 : PAD.left + (i / (data.length - 1)) * plotW,
      y: baseline - (d.value / max) * plotH,
    })),
  );

  const linePath = $derived(
    points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' '),
  );

  const areaPath = $derived(
    points.length === 0
      ? ''
      : `M${points[0].x.toFixed(2)},${baseline} ` +
        points.map((p) => `L${p.x.toFixed(2)},${p.y.toFixed(2)}`).join(' ') +
        ` L${points[points.length - 1].x.toFixed(2)},${baseline} Z`,
  );

  // Three solid hairlines. Dashed rules read as a threshold or a projection
  // when they are only a grid.
  const gridLines = $derived([0, 0.5, 1].map((t) => ({
    y: baseline - t * plotH,
    value: Math.round(t * max),
  })));

  let hoverIndex = $state<number | null>(null);
  let svgEl = $state<SVGSVGElement | null>(null);

  const active = $derived(hoverIndex === null ? null : points[hoverIndex] ?? null);

  /** Maps a pointer position onto the nearest sample, so the hit target is a
      full column rather than the 8px marker itself. */
  function handleMove(event: PointerEvent) {
    if (!svgEl || points.length === 0) return;
    const rect = svgEl.getBoundingClientRect();
    if (rect.width === 0) return;
    const xInView = ((event.clientX - rect.left) / rect.width) * W;
    let nearest = 0;
    let best = Infinity;
    for (let i = 0; i < points.length; i++) {
      const dist = Math.abs(points[i].x - xInView);
      if (dist < best) {
        best = dist;
        nearest = i;
      }
    }
    hoverIndex = nearest;
  }

  // Only a few ticks are labelled; one label per sample would collide.
  const tickEvery = $derived(Math.max(1, Math.ceil(data.length / 6)));
</script>

{#if data.length === 0}
  <p class="py-6 text-center text-sm text-muted-foreground">No data to chart.</p>
{:else}
  <div class="relative">
    <svg
      bind:this={svgEl}
      viewBox="0 0 {W} {H}"
      class="h-auto w-full touch-none"
      role="img"
      aria-label={ariaLabel}
      onpointermove={handleMove}
      onpointerleave={() => (hoverIndex = null)}
    >
      <defs>
        <linearGradient id="idp-area-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="var(--primary)" stop-opacity="0.28" />
          <stop offset="100%" stop-color="var(--primary)" stop-opacity="0.02" />
        </linearGradient>
      </defs>

      {#each gridLines as line}
        <line
          x1={PAD.left}
          y1={line.y}
          x2={W - PAD.right}
          y2={line.y}
          stroke="var(--border)"
          stroke-width="1"
        />
        <text
          x={PAD.left - 8}
          y={line.y + 3.5}
          text-anchor="end"
          font-size="10"
          fill="var(--muted-fg)"
        >
          {line.value}
        </text>
      {/each}

      <path d={areaPath} fill="url(#idp-area-fill)" />
      <path
        d={linePath}
        fill="none"
        stroke="var(--primary)"
        stroke-width="2"
        stroke-linejoin="round"
        stroke-linecap="round"
      />

      {#each points as p, i}
        {#if i % tickEvery === 0 || i === points.length - 1}
          <text x={p.x} y={H - 10} text-anchor="middle" font-size="10" fill="var(--muted-fg)">
            {p.label}
          </text>
        {/if}
      {/each}

      <!-- Endpoint is direct-labelled; the rest are left to the axis and the
           hover layer rather than printing a number on every point. -->
      {#if points.length > 0}
        <circle
          cx={points[points.length - 1].x}
          cy={points[points.length - 1].y}
          r="4"
          fill="var(--primary)"
          stroke="var(--card)"
          stroke-width="2"
        />
      {/if}

      {#if active}
        <line
          x1={active.x}
          y1={PAD.top}
          x2={active.x}
          y2={baseline}
          stroke="var(--muted-fg)"
          stroke-width="1"
        />
        <circle
          cx={active.x}
          cy={active.y}
          r="4.5"
          fill="var(--primary)"
          stroke="var(--card)"
          stroke-width="2"
        />
      {/if}
    </svg>

    {#if active}
      <div
        class="pointer-events-none absolute -translate-x-1/2 -translate-y-full rounded-md border border-border bg-card px-2 py-1 text-xs shadow-md"
        style="left: {(active.x / W) * 100}%; top: {(active.y / H) * 100}%"
      >
        <span class="font-medium text-foreground">{active.value}{unit ? ` ${unit}` : ''}</span>
        <span class="ml-1 text-muted-foreground">{active.label}</span>
      </div>
    {/if}
  </div>

  <!-- The table twin: every value stays reachable without a pointer. -->
  <details class="mt-3">
    <summary class="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
      View as table
    </summary>
    <div class="mt-2 max-h-40 overflow-y-auto rounded-md border border-border">
      <table class="w-full text-xs">
        <thead class="bg-muted/50 text-left text-muted-foreground">
          <tr>
            <th class="px-2 py-1 font-medium">Period</th>
            <th class="px-2 py-1 text-right font-medium">Value</th>
          </tr>
        </thead>
        <tbody>
          {#each data as d (d.label)}
            <tr class="border-t border-border">
              <td class="px-2 py-1">{d.label}</td>
              <td class="px-2 py-1 text-right tabular-nums">{d.value}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </details>
{/if}
