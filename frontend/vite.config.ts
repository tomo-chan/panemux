import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    // `e2e/**` holds Playwright specs, which vitest must never run — but it
    // also holds e2e/a11y-ceiling.ts, a pure comparator with no browser in it
    // whose branches a passing Playwright run never reaches. Splitting the
    // exclusion by suffix rather than by directory lets that one be unit
    // tested here: `.spec.ts` stays Playwright's, `.test.ts` is vitest's.
    include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'e2e/**/*.test.ts'],
    exclude: ['**/node_modules/**', 'e2e/**/*.spec.ts'],
    coverage: {
      provider: 'v8',
      // Measure hooks, schemas and utils — the modules that hold decisions.
      // UI components and entry points (App.tsx, main.tsx, components/)
      // require browser rendering and are covered by integration/E2E tests,
      // not unit tests.
      include: ['src/hooks/**', 'src/schemas/**', 'src/utils/**'],
      exclude: ['src/test/**'],
      reporter: ['text', 'json-summary'],
      thresholds: {
        lines: 80,
        functions: 80,
        branches: 80,
        statements: 80,
      },
    },
  },
})
