import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: [],
    include: ['tests/**/*.test.js', 'tests/**/*.spec.js'],
    exclude: ['e2e/**'],
    coverage: {
      reporter: ['text', 'lcov']
    }
  }
});
