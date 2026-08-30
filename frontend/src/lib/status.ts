export type StatusTone = 'success' | 'danger' | 'warn' | 'info' | 'neutral';

/**
 * Maps a Kubernetes / platform status string onto a semantic tone so badges
 * and charts stay consistent: healthy work is green, failures are red.
 */
export function statusTone(label: string): StatusTone {
  const l = label.trim().toLowerCase();
  if (
    /^(running|ready|healthy|succeeded|success|connected|active|ok|available|live)$/.test(l) ||
    /\b(success|succeeded|healthy|running|ready)\b/.test(l)
  ) {
    return 'success';
  }
  if (
    /^(failed|error|unhealthy|disconnected|unknown|crashloopbackoff|crashloop)$/.test(l) ||
    /\b(fail|error|unhealthy|crash|denied)\b/.test(l)
  ) {
    return 'danger';
  }
  if (
    /^(pending|progressing|warning|degraded|waiting|scaledtozero)$/.test(l) ||
    /\b(warn|pending|backoff|evict)\b/.test(l)
  ) {
    return 'warn';
  }
  if (/^(clusterip|nodeport|loadbalancer|externalname)$/.test(l)) {
    return 'info';
  }
  return 'neutral';
}

export function statusBadgeClass(label: string): string {
  switch (statusTone(label)) {
    case 'success':
      return 'bg-emerald-500/15 text-emerald-500';
    case 'danger':
      return 'bg-red-500/15 text-red-500';
    case 'warn':
      return 'bg-amber-500/15 text-amber-500';
    case 'info':
      return 'bg-sky-500/15 text-sky-400';
    default:
      return 'bg-muted text-muted-foreground';
  }
}

export function chartBarClass(label: string): string {
  switch (statusTone(label)) {
    case 'success':
      return 'bg-emerald-500';
    case 'danger':
      return 'bg-red-500';
    case 'warn':
      return 'bg-amber-500';
    case 'info':
      return 'bg-sky-500';
    default:
      return 'bg-zinc-400 dark:bg-zinc-500';
  }
}

/** Load meters: green while healthy, amber under pressure, red near the limit. */
export function meterBarClass(percent: number): string {
  if (percent >= 90) return 'bg-red-500';
  if (percent >= 70) return 'bg-amber-500';
  return 'bg-emerald-500';
}

export function meterTextClass(percent: number): string {
  if (percent >= 90) return 'text-red-500';
  if (percent >= 70) return 'text-amber-500';
  return 'text-emerald-500';
}
