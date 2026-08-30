<script lang="ts">
  import { cn } from '$lib/utils';
  import { healthStatusColor, healthStatusLabel } from '$types/health';
  import type { HealthStatus } from '$types/health';

  interface Props {
    status: HealthStatus;
    class?: string;
  }

  let { status, class: className = '' }: Props = $props();

  const dotColor = $derived(
    status === 1 ? 'bg-success' :
    status === 2 ? 'bg-warning' :
    status === 3 ? 'bg-destructive' :
    'bg-muted-foreground',
  );
</script>

<span class={cn('inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-sm font-medium', healthStatusColor(status), className)}>
  <span class={cn('h-2 w-2 rounded-full', dotColor)}></span>
  {healthStatusLabel(status)}
</span>
