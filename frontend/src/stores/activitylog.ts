import { writable } from 'svelte/store';

export type ActivityLevel = 'info' | 'success' | 'error';

export interface ActivityLine {
  id: number;
  scope: string;
  level: ActivityLevel;
  message: string;
  at: string;
}

let nextId = 0;
const { subscribe, update } = writable<ActivityLine[]>([]);

/** Ring buffer of platform API / lifecycle lines shown in live log panels. */
export const activityLog = {
  subscribe,
  push(scope: string, level: ActivityLevel, message: string): ActivityLine {
    const line: ActivityLine = {
      id: ++nextId,
      scope,
      level,
      message,
      at: new Date().toISOString(),
    };
    update((rows) => [...rows, line].slice(-300));
    return line;
  },
};

export function matchesActivityScope(scope: string, prefixes: string[]): boolean {
  return prefixes.some((prefix) => scope === prefix || scope.startsWith(prefix + '#') || scope.startsWith(prefix + '/'));
}
