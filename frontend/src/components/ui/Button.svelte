<script lang="ts">
  import { cn } from '$lib/utils';
  import type { Snippet } from 'svelte';

  interface Props {
    variant?: 'primary' | 'secondary' | 'ghost' | 'destructive' | 'icon';
    size?: 'sm' | 'md';
    type?: 'button' | 'submit';
    disabled?: boolean;
    class?: string;
    onclick?: (e: MouseEvent) => void;
    children?: Snippet;
    'aria-label'?: string;
    title?: string;
  }

  let {
    variant = 'secondary',
    size = 'md',
    type = 'button',
    disabled = false,
    class: className = '',
    onclick,
    children,
    'aria-label': ariaLabel,
    title,
  }: Props = $props();

  const base =
    'inline-flex items-center justify-center gap-1.5 rounded-lg font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50';

  const variants: Record<string, string> = {
    primary: 'bg-primary text-primary-foreground hover:bg-primary/90',
    secondary: 'border border-input bg-background text-foreground hover:bg-accent',
    ghost: 'text-muted-foreground hover:bg-accent hover:text-foreground',
    destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
    icon: 'border border-input bg-background text-muted-foreground hover:bg-accent hover:text-foreground',
  };

  const sizes: Record<string, string> = {
    sm: 'h-8 px-2.5 text-xs',
    md: 'h-9 px-3 text-sm',
  };
</script>

<button
  {type}
  {disabled}
  {onclick}
  aria-label={ariaLabel}
  {title}
  class={cn(base, variants[variant], variant === 'icon' ? 'h-9 w-9 px-0' : sizes[size], className)}
>
  {@render children?.()}
</button>
