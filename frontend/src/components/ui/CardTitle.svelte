<script lang="ts">
  import { cn } from '$lib/utils';
  import type { Snippet } from 'svelte';

  interface Props {
    class?: string;
    children?: Snippet;
  }

  let { class: className = '', children }: Props = $props();
</script>

<h3 class={cn('text-sm font-medium text-muted-foreground', className)}>
  {@render children?.()}
</h3>
