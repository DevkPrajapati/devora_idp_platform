<script lang="ts">
  import { cn } from '$lib/utils';
  import type { Snippet } from 'svelte';

  interface Props {
    title: string;
    description?: string;
    class?: string;
    children?: Snippet;
  }

  let { title, description, class: className = '', children }: Props = $props();
</script>

<div class={cn('flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between', className)}>
  <div class="min-w-0">
    <h1 class="text-xl font-semibold tracking-tight md:text-2xl">{title}</h1>
    {#if description}
      <p class="mt-1 max-w-2xl text-sm text-muted-foreground">{description}</p>
    {/if}
  </div>
  {#if children}
    <div class="flex shrink-0 flex-wrap items-center gap-2">
      {@render children()}
    </div>
  {/if}
</div>
