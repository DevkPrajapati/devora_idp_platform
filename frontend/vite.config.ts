import tailwindcss from '@tailwindcss/vite';
// vitest/config re-exports Vite's defineConfig with the `test` block added to
// the type. Importing it from 'vite' typechecks everything except `test`.
import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import path from 'path';

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  resolve: {
    alias: {
      '$lib': path.resolve('./src/lib'),
      '$components': path.resolve('./src/components'),
      '$stores': path.resolve('./src/stores'),
      '$services': path.resolve('./src/services'),
      '$types': path.resolve('./src/types'),
      '$layouts': path.resolve('./src/layouts'),
      '$routes': path.resolve('./src/routes'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/rpc': {
        target: 'http://localhost:8090',
        changeOrigin: true,
        rewrite: (p) => p.replace(/^\/rpc/, ''),
        // Live log streams (Connect server-streaming) must not be cut by the
        // default proxy idle timeout.
        timeout: 24 * 60 * 60 * 1000,
        proxyTimeout: 24 * 60 * 60 * 1000,
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            // Stop intermediaries from buffering Connect server-streams (live logs).
            proxyRes.headers['cache-control'] = 'no-cache, no-transform';
            proxyRes.headers['x-accel-buffering'] = 'no';
          });
        },
      },
    },
  },
  test: {
    // The units under test are stores and service helpers that touch
    // localStorage, fetch, and window.open, so a DOM is required.
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.{test,spec}.{ts,js}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'lcov'],
      // Generated and purely presentational files would inflate the number
      // without saying anything about correctness.
      exclude: ['src/main.ts', 'src/**/*.svelte', 'src/test/**', '**/*.d.ts'],
    },
  },
});
