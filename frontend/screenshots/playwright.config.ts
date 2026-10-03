import { defineConfig, devices } from '@playwright/test'

// Generates the documentation screenshots under docs/images/ (`make
// screenshots`). Kept apart from playwright.config.ts on purpose: these are
// not tests and assert nothing beyond what the capture needs, and the e2e
// suite (`make test-e2e`, CI) must not start a server or write into docs/.
//
// PLAYWRIGHT_CHROMIUM_EXECUTABLE works as it does for the e2e suite.
const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
const BASE_URL = 'http://127.0.0.1:4180'

export default defineConfig({
  testDir: '.',
  testMatch: /screenshots\.spec\.ts$/,
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  outputDir: '../../test-results/screenshots',
  use: {
    ...devices['Desktop Chrome'],
    ...(chromiumExecutablePath ? { launchOptions: { executablePath: chromiumExecutablePath } } : {}),
    baseURL: BASE_URL,
    viewport: { width: 1440, height: 860 },
    deviceScaleFactor: 1,
    colorScheme: 'dark',
  },
  webServer: {
    command: 'sh ./run-panemux-screenshots.sh',
    url: BASE_URL,
    cwd: '.',
    // A server left over from an earlier run holds layout changes the
    // captures made (an opened pane, a dialog's edits), so always start one.
    reuseExistingServer: false,
    timeout: 180_000,
  },
})
