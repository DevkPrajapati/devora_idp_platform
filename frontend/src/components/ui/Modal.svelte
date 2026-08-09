<script lang="ts">
  import { X } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  interface Props {
    open: boolean;
    title: string;
    description?: string;
    /** Widens the panel for forms; confirmations stay narrow. */
    size?: 'sm' | 'lg';
    onclose: () => void;
    children?: Snippet;
    footer?: Snippet;
  }

  let { open, title, description, size = 'sm', onclose, children, footer }: Props = $props();

  let panel = $state<HTMLElement | null>(null);
  /** The element focused before opening, so it can be restored on close. */
  let previouslyFocused: HTMLElement | null = null;

  const uid = Math.random().toString(36).slice(2, 9);
  const titleId = `modal-title-${uid}`;
  const descId = `modal-desc-${uid}`;

  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

  function focusableItems(): HTMLElement[] {
    if (!panel) return [];
    return Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
      (el) => el.offsetParent !== null || el === document.activeElement,
    );
  }

  // Moving focus into the dialog is what makes it a dialog for a keyboard or
  // screen-reader user. Without it, focus stays on the page behind and Tab
  // walks the content the dialog is supposed to be blocking.
  $effect(() => {
    if (!open) return;

    previouslyFocused = document.activeElement as HTMLElement | null;

    // Deferred a frame: the panel's children are not in the DOM yet on the
    // tick this effect first runs.
    const raf = requestAnimationFrame(() => {
      const items = focusableItems();
      (items[0] ?? panel)?.focus();
    });

    return () => {
      cancelAnimationFrame(raf);
      // Returning focus to the trigger stops a keyboard user from being dumped
      // back at the top of the document on every close.
      previouslyFocused?.focus?.();
    };
  });

  // The page behind would otherwise scroll under the backdrop.
  $effect(() => {
    if (!open) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = previous;
    };
  });

  function handleKeydown(event: KeyboardEvent) {
    if (!open) return;

    if (event.key === 'Escape') {
      event.stopPropagation();
      onclose();
      return;
    }

    if (event.key !== 'Tab') return;

    // Cycle focus within the dialog. Browsers do not scope Tab to a container
    // unless the element is a native <dialog> opened modally.
    const items = focusableItems();
    if (items.length === 0) {
      event.preventDefault();
      return;
    }

    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;

    if (event.shiftKey && (active === first || active === panel)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-background/80 p-4 backdrop-blur-sm"
    onclick={(e) => {
      // Only a click that both starts and ends on the backdrop dismisses.
      // Checking the target alone closes the dialog when a drag inside a text
      // field happens to release over the backdrop.
      if (e.target === e.currentTarget) onclose();
    }}
  >
    <div
      bind:this={panel}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-describedby={description ? descId : undefined}
      tabindex="-1"
      class="w-full {size === 'lg'
        ? 'max-w-2xl'
        : 'max-w-md'} max-h-[calc(100dvh-2rem)] overflow-y-auto rounded-xl border border-border bg-card shadow-xl focus:outline-none"
    >
      <div class="flex items-start justify-between gap-4 border-b border-border px-5 py-4">
        <div class="min-w-0">
          <h2 id={titleId} class="text-base font-semibold">{title}</h2>
          {#if description}
            <p id={descId} class="mt-1 text-sm text-muted-foreground">{description}</p>
          {/if}
        </div>
        <button
          type="button"
          onclick={onclose}
          aria-label="Close dialog"
          class="-mr-1 shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
        >
          <X class="h-4 w-4" />
        </button>
      </div>

      {#if children}
        <div class="px-5 py-4">{@render children()}</div>
      {/if}

      {#if footer}
        <div class="flex justify-end gap-2 border-t border-border px-5 py-4">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}
