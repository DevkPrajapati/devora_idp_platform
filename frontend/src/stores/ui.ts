import { writable } from 'svelte/store';

/**
 * Tracks whether the off-canvas navigation drawer is showing.
 *
 * Only meaningful below the `lg` breakpoint — at and above it the sidebar is a
 * static column and this value is forced back to false, so no consumer has to
 * branch on viewport width.
 */
function createSidebarStore() {
  const { subscribe, set, update } = writable(false);

  return {
    subscribe,
    open: () => set(true),
    close: () => set(false),
    toggle: () => update((isOpen) => !isOpen),
  };
}

export const sidebarOpen = createSidebarStore();
