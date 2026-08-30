<script lang="ts">
  import type { Component, Snippet } from 'svelte';

  interface Props {
    icon?: Component;
    title: string;
    description?: string;
    children?: Snippet;
  }

  let { icon: Icon, title, description, children }: Props = $props();
</script>

<div class="flex flex-col items-center justify-center rounded-xl border border-dashed border-border bg-card/40 px-6 py-14 text-center sm:py-16">
  {#if Icon}
    <Icon class="mb-4 h-10 w-10 text-muted-foreground/40" />
  {/if}
  <h3 class="text-base font-semibold sm:text-lg">{title}</h3>
  {#if description}
    <p class="mt-2 max-w-md text-sm text-muted-foreground">{description}</p>
  {/if}
  {#if children}
    <div class="mt-4">{@render children()}</div>
  {/if}
</div>
