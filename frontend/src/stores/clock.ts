import { readable } from 'svelte/store';

/** Wall clock that ticks once a second so live log views can show "now" and "Ns ago". */
export const liveClock = readable(Date.now(), (set) => {
  if (typeof window === 'undefined') return;
  set(Date.now());
  const id = window.setInterval(() => set(Date.now()), 1000);
  return () => window.clearInterval(id);
});
