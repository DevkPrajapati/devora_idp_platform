<script lang="ts">
  import { cn } from '$lib/utils';
  import type { Snippet } from 'svelte';

  interface Props {
    minWidth?: string;
    class?: string;
    children?: Snippet;
  }

  let { minWidth = '48rem', class: className = '', children }: Props = $props();
</script>

<div class={cn('overflow-x-auto rounded-xl border border-border bg-card', className)}>
  <table class="w-full text-left text-sm" style="min-width: {minWidth}">
    {@render children?.()}
  </table>
</div>
