import { get } from 'svelte/store';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { toasts, toastError } from './toast';

beforeEach(() => {
  toasts.clear();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  toasts.clear();
});

describe('toast store', () => {
  it('queues messages with their kind', () => {
    toasts.success('saved');
    toasts.error('boom');

    const list = get(toasts);
    expect(list).toHaveLength(2);
    expect(list[0]).toMatchObject({ kind: 'success', message: 'saved' });
    expect(list[1]).toMatchObject({ kind: 'error', message: 'boom' });
  });

  it('auto-dismisses success toasts', () => {
    toasts.success('saved');
    expect(get(toasts)).toHaveLength(1);

    vi.advanceTimersByTime(4000);
    expect(get(toasts)).toHaveLength(0);
  });

  // An error that vanishes before it can be read is worse than none: the user
  // sees a red flash and has no way to recover the text.
  it('keeps error toasts until dismissed', () => {
    toasts.error('deployment failed');

    vi.advanceTimersByTime(60_000);
    expect(get(toasts)).toHaveLength(1);

    toasts.dismiss(get(toasts)[0].id);
    expect(get(toasts)).toHaveLength(0);
  });

  it('caps the stack and drops the oldest first', () => {
    for (let i = 1; i <= 6; i++) {
      toasts.error(`error ${i}`);
    }

    const list = get(toasts);
    expect(list).toHaveLength(4);
    // 1 and 2 were pushed out; the most recent four survive.
    expect(list.map((t) => t.message)).toEqual(['error 3', 'error 4', 'error 5', 'error 6']);
  });

  it('assigns unique ids', () => {
    const ids = [toasts.error('a'), toasts.error('b'), toasts.error('c')];
    expect(new Set(ids).size).toBe(3);
  });

  it('dismissing one leaves the others', () => {
    const first = toasts.error('first');
    toasts.error('second');

    toasts.dismiss(first);

    const list = get(toasts);
    expect(list).toHaveLength(1);
    expect(list[0].message).toBe('second');
  });

  // Dismissing by hand must also cancel the pending timer, or the timer fires
  // later against an id that no longer exists.
  it('dismissing cancels the pending auto-dismiss', () => {
    const id = toasts.success('saved');
    toasts.dismiss(id);

    expect(() => vi.advanceTimersByTime(10_000)).not.toThrow();
    expect(get(toasts)).toHaveLength(0);
  });
});

describe('toastError', () => {
  it('uses the Error message when there is one', () => {
    toastError(new Error('namespace not found'), 'fallback');
    expect(get(toasts)[0].message).toBe('namespace not found');
  });

  // String(err) on a plain object yields "[object Object]", which tells the
  // user nothing — the fallback has to win.
  it('falls back for non-Error throws', () => {
    toastError({ weird: true }, 'Could not delete namespace.');
    expect(get(toasts)[0].message).toBe('Could not delete namespace.');
  });

  it('falls back for an Error with an empty message', () => {
    toastError(new Error(''), 'Something went wrong.');
    expect(get(toasts)[0].message).toBe('Something went wrong.');
  });
});
