import { writable } from 'svelte/store';

export type Theme = 'light' | 'dark' | 'system';

function createThemeStore() {
  const stored = typeof localStorage !== 'undefined'
    ? (localStorage.getItem('idp-theme') as Theme | null)
    : null;

  const { subscribe, set, update } = writable<Theme>(stored ?? 'system');

  function applyTheme(theme: Theme) {
    const root = document.documentElement;
    const resolved = theme === 'system'
      ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : theme;

    root.classList.toggle('dark', resolved === 'dark');
    localStorage.setItem('idp-theme', theme);
  }

  return {
    subscribe,
    set: (theme: Theme) => {
      set(theme);
      applyTheme(theme);
    },
    toggle: () => {
      update((current) => {
        const resolved = current === 'system'
          ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
          : current;
        const next = resolved === 'dark' ? 'light' : 'dark';
        applyTheme(next);
        localStorage.setItem('idp-theme', next);
        return next;
      });
    },
    init: () => {
      const theme = stored ?? 'system';
      applyTheme(theme);
    },
  };
}

export const theme = createThemeStore();
