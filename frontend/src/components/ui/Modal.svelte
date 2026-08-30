<script lang="ts">
  import { X } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  interface Props {
    open: boolean;
    title: string;
    description?: string;
    /** Widens the panel for forms; confirmations stay narrow. */
    size?: 'sm' | 'lg' | 'xl';
    onclose: () => void;
    children?: Snippet;
    footer?: Snippet;
  }

  let { open, title, description, size = 'sm', onclose, children, footer }: Props = $props();

  let dialogEl = $state<HTMLDialogElement | null>(null);
  let panel = $state<HTMLElement | null>(null);
  /** The element focused before opening, so it can be restored on close. */
  let previouslyFocused: HTMLElement | null = null;
  /** Avoid firing onclose twice when we close the dialog from the open prop. */
  let closingFromProp = false;

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

  $effect(() => {
    if (!dialogEl) return;

    if (open) {
      if (!dialogEl.open) {
        previouslyFocused = document.activeElement as HTMLElement | null;
        dialogEl.showModal();
      }
    } else if (dialogEl.open) {
      closingFromProp = true;
      dialogEl.close();
      closingFromProp = false;
    }
  });

  $effect(() => {
    if (!open) return;

    const raf = requestAnimationFrame(() => {
      const items = focusableItems();
      (items[0] ?? panel)?.focus();
    });

    return () => {
      cancelAnimationFrame(raf);
      previouslyFocused?.focus?.();
    };
  });

  function handleDialogClose() {
    if (closingFromProp) return;
    onclose();
  }

  function handleCancel(event: Event) {
    event.preventDefault();
    onclose();
  }

  function handleDialogClick(event: MouseEvent) {
    // Clicks on the dialog backdrop (the dialog element itself) dismiss.
    if (event.target === dialogEl) {
      onclose();
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    if (!open) return;
    if (event.key !== 'Tab') return;

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

<!-- Native dialog keeps position:fixed anchored to the viewport and handles Escape. -->
<dialog
  bind:this={dialogEl}
  onclose={handleDialogClose}
  oncancel={handleCancel}
  onclick={handleDialogClick}
  aria-labelledby={titleId}
  aria-describedby={description ? descId : undefined}
  class="modal-dialog"
>
  <div
    bind:this={panel}
    role="document"
    tabindex="-1"
    class="modal-panel flex w-full max-h-[calc(100dvh-2rem)] shrink-0 flex-col overflow-hidden rounded-xl border border-border bg-card text-card-foreground shadow-xl focus:outline-none {size === 'xl'
      ? 'max-w-4xl'
      : size === 'lg'
        ? 'max-w-2xl'
        : 'max-w-md'}"
    onclick={(e) => e.stopPropagation()}
  >
    <div class="flex shrink-0 items-start justify-between gap-4 border-b border-border px-5 py-4">
      <div class="min-w-0">
        <h2 id={titleId} class="text-base font-semibold text-card-foreground">{title}</h2>
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
      <div class="modal-body min-h-0 flex-1 overflow-y-auto px-5 py-4 text-card-foreground">{@render children()}</div>
    {/if}

    {#if footer}
      <div class="modal-footer flex shrink-0 justify-end gap-2 border-t border-border px-5 py-4">
        {@render footer()}
      </div>
    {/if}
  </div>
</dialog>

<style>
  .modal-dialog {
    position: fixed;
    inset: 0;
    margin: 0;
    width: 100%;
    height: 100%;
    max-width: none;
    max-height: none;
    border: 0;
    padding: 1rem;
    background: transparent;
    overflow: hidden;
  }

  .modal-dialog[open] {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--card-fg);
  }

  .modal-dialog::backdrop {
    background: color-mix(in oklab, var(--bg) 80%, transparent);
    backdrop-filter: blur(4px);
  }

  /* Top-layer dialog resets color to browser default (black). Re-apply theme tokens. */
  .modal-panel :global(label) {
    color: var(--card-fg);
  }

  .modal-panel :global(input),
  .modal-panel :global(select),
  .modal-panel :global(textarea) {
    color: var(--fg);
    background-color: var(--bg);
  }

  .modal-panel :global(
    button:not(.text-primary-foreground):not(.text-destructive-foreground)
  ),
  .modal-footer :global(
    button:not(.text-primary-foreground):not(.text-destructive-foreground)
  ) {
    color: var(--fg);
  }
</style>
