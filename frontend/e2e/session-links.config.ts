import { defineConfig, devices } from '@playwright/test'

// Isolated local run; the normal suite also runs these with the task dashboard.
const baseURL = 'http://127.0.0.1:4289'
export default defineConfig({
  testDir: '.',
  testMatch: /session-links\.spec\.ts$/,
  workers: 1,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'], baseURL,
    ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ? { launchOptions: { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE } } : {}),
  },
  webServer: {
    command: 'sh ./e2e/run-panemux-task-dashboard-e2e.sh 4289',
    cwd: '..', url: baseURL, reuseExistingServer: false, timeout: 180_000,
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5000 },
  },
})
