import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['test/**/*.test.ts?(x)'],
    // Pin NODE_ENV: an ambient NODE_ENV=production (sandbox shells) makes
    // React resolve its production build where act() is unavailable.
    env: { NODE_ENV: 'test' },
  },
  esbuild: { jsx: 'automatic' },
});
