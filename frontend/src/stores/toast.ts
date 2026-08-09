import { writable } from 'svelte/store';

export type ToastKind = 'success' | 'error' | 'info';

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

/**
 * Errors stay until dismissed. An error that vanishes after four seconds is
 * worse than no error at all: the user sees something red flash, cannot read
 * it, and has no way to get it back.
 */
const AUTO_DISMISS_MS: Record<ToastKind, number | null> = {
  success: 4000,
  info: 6000,
  error: null,
};

/** Bounds the stack so a retry loop cannot paper over the whole viewport. */
const MAX_VISIBLE = 4;

function createToastStore() {
  const { subscribe, update } = writable<Toast[]>([]);

  // Monotonic rather than random: ids only need to be unique within a session,
  // and a counter keeps them stable and readable in tests.
  let nextId = 1;
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

  function dismiss(id: number) {
    const timer = timers.get(id);
    if (timer !== undefined) {
      clearTimeout(timer);
      timers.delete(id);
    }
    update((list) => list.filter((t) => t.id !== id));
  }

  function push(kind: ToastKind, message: string): number {
    const id = nextId++;

    update((list) => {
      const next = [...list, { id, kind, message }];
      // Drop from the front: the oldest toast is the one the user has had the
      // most time to read.
      return next.length > MAX_VISIBLE ? next.slice(next.length - MAX_VISIBLE) : next;
    });

    const ttl = AUTO_DISMISS_MS[kind];
    if (ttl !== null) {
      timers.set(
        id,
        setTimeout(() => dismiss(id), ttl),
      );
    }

    return id;
  }

  return {
    subscribe,
    success: (message: string) => push('success', message),
    error: (message: string) => push('error', message),
    info: (message: string) => push('info', message),
    dismiss,
    /** Clears everything, including pending timers. Used on unmount and in tests. */
    clear() {
      timers.forEach((timer) => clearTimeout(timer));
      timers.clear();
      update(() => []);
    },
  };
}

export const toasts = createToastStore();

/**
 * Narrows an unknown thrown value to a message.
 *
 * Callers catch `unknown`, and `String(err)` on a non-Error yields
 * "[object Object]" — which tells the user nothing.
 */
export function toastError(err: unknown, fallback: string): number {
  return toasts.error(err instanceof Error && err.message ? err.message : fallback);
}
