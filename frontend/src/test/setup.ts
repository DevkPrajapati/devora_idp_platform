import '@testing-library/jest-dom/vitest';
import { afterEach, vi } from 'vitest';

// Each test gets a clean slate. The auth store reads localStorage at module
// load and caches state, so leaking a token between tests would make results
// depend on file ordering.
afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
});
