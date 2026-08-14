import { defineConfig, devices } from '@playwright/test'

// Some environments (CI images and sandboxes with a preinstalled browser)
// ship a Chromium build whose revision differs from the one this Playwright
// version downloads by default, which makes the default launch fail with
// "Executable doesn't exist". Pointing PLAYWRIGHT_CHROMIUM_EXECUTABLE at
// that browser runs the suite against it instead. Left unset — the normal
// case, including CI's own `npx playwright install --with-deps chromium` —
// Playwright's own managed browser is used exactly as before.
const chromiumExecutablePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
const chromium = {
  ...devices['Desktop Chrome'],
  ...(chromiumExecutablePath ? { launchOptions: { executablePath: chromiumExecutablePath } } : {}),
}

// Each fixture config needs its own panemux process: whether a pane has
// agent_board.enabled is read from the config file at startup, and the
// Agent Board dashboard UI is gated on it (GET /api/session-token's
// agent_board_enabled flag), so "enabled" and "not enabled" cannot be two
// states of one server.
const DEFAULT_BASE_URL = 'http://127.0.0.1:4173'
const AGENT_BOARD_BASE_URL = 'http://127.0.0.1:4174'
const AGENT_BOARD_AGMSG_BASE_URL = 'http://127.0.0.1:4175'

const AGENT_BOARD_SPEC = /agent-board\.spec\.ts$/
const AGENT_BOARD_AGMSG_SPEC = /agent-board-agmsg\.spec\.ts$/

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL: DEFAULT_BASE_URL,
    trace: 'on-first-retry',
  },
  webServer: [
    {
      command: 'sh ./e2e/run-panemux-e2e.sh',
      url: DEFAULT_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'sh ./e2e/run-panemux-e2e.sh agent-board.yml 4174',
      url: AGENT_BOARD_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'sh ./e2e/run-panemux-agent-board-e2e.sh',
      url: AGENT_BOARD_AGMSG_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: { ...chromium, baseURL: DEFAULT_BASE_URL },
      testIgnore: [AGENT_BOARD_SPEC, AGENT_BOARD_AGMSG_SPEC],
    },
    {
      name: 'chromium-agent-board',
      use: { ...chromium, baseURL: AGENT_BOARD_BASE_URL },
      testMatch: AGENT_BOARD_SPEC,
    },
    {
      name: 'chromium-agent-board-agmsg',
      use: { ...chromium, baseURL: AGENT_BOARD_AGMSG_BASE_URL },
      testMatch: AGENT_BOARD_AGMSG_SPEC,
    },
  ],
})
