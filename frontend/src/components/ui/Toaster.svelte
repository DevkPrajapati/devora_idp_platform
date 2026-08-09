<script lang="ts">
  import { toasts, type ToastKind } from '$stores/toast';
  import { CheckCircle2, AlertCircle, Info, X } from '@lucide/svelte';

  const STYLES: Record<ToastKind, string> = {
    success: 'border-success/30',
    error: 'border-destructive/40',
    info: 'border-border',
  };

  const ICON_STYLES: Record<ToastKind, string> = {
    success: 'text-success',
    error: 'text-destructive',
    info: 'text-primary',
  };

  const ICONS = { success: CheckCircle2, error: AlertCircle, info: Info };
</script>

<!--
  aria-live="polite" announces new toasts without interrupting whatever the
  screen reader is currently saying. The region is always present rather than
  conditionally rendered — assistive tech only watches live regions that existed
  when the page loaded, so mounting one alongside its first message announces
  nothing.
-->
<div
  aria-live="polite"
  aria-atomic="false"
  class="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2"
>
  {#each $toasts as toast (toast.id)}
    {@const Icon = ICONS[toast.kind]}
    <div
      role={toast.kind === 'error' ? 'alert' : 'status'}
      class="pointer-events-auto flex items-start gap-2.5 rounded-lg border bg-card px-3.5 py-3 text-foreground shadow-lg {STYLES[
        toast.kind
      ]}"
    >
      <Icon class="mt-0.5 h-4 w-4 shrink-0 {ICON_STYLES[toast.kind]}" />
      <p class="min-w-0 flex-1 break-words text-sm">{toast.message}</p>
      <button
        type="button"
        onclick={() => toasts.dismiss(toast.id)}
        aria-label="Dismiss notification"
        class="-mr-1 rounded p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
  {/each}
</div>
