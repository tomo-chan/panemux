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
const COMMAND_CENTER_BASE_URL = 'http://127.0.0.1:4176'
// core-multiplexer.spec.ts mutates the layout (split, close, resize, workspace
// CRUD), so it cannot share a server with specs that assert on the default
// fixture's panes and tabs.
const CORE_MULTIPLEXER_BASE_URL = 'http://127.0.0.1:4177'
// a11y.spec.ts is the same coupling seen from the other side: it counts axe
// violation *nodes*, so any spec that leaves an extra pane behind changes its
// numbers. See e2e/a11y.yml for what that measured.
const A11Y_BASE_URL = 'http://127.0.0.1:4178'

const AGENT_BOARD_SPEC = /agent-board\.spec\.ts$/
const AGENT_BOARD_AGMSG_SPEC = /agent-board-agmsg\.spec\.ts$/
const COMMAND_CENTER_SPEC = /command-center\.spec\.ts$/
const CORE_MULTIPLEXER_SPEC = /core-multiplexer\.spec\.ts$/
const A11Y_SPEC = /a11y\.spec\.ts$/

export default defineConfig({
  testDir: './e2e',
  // Playwright's default testMatch takes `*.test.ts` as well as `*.spec.ts`,
  // and e2e/ now holds e2e/a11y-ceiling.test.ts — a vitest unit test for the
  // a11y comparator, which loads @vitest/expect and dies here with "Cannot
  // redefine property: Symbol($$jest-matchers-object)". The two runners split
  // this directory by suffix: `.spec.ts` is Playwright's, `.test.ts` is
  // vitest's (see vite.config.ts, which draws the same line from its side).
  testMatch: /\.spec\.ts$/,
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
    {
      command: 'sh ./e2e/run-panemux-command-center-e2e.sh',
      url: COMMAND_CENTER_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'sh ./e2e/run-panemux-e2e.sh core-multiplexer.yml 4177',
      url: CORE_MULTIPLEXER_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
    {
      command: 'sh ./e2e/run-panemux-e2e.sh a11y.yml 4178',
      url: A11Y_BASE_URL,
      cwd: '.',
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
    },
  ],
  projects: [
    {
      name: 'chromium',
      use: { ...chromium, baseURL: DEFAULT_BASE_URL },
      testIgnore: [
        AGENT_BOARD_SPEC,
        AGENT_BOARD_AGMSG_SPEC,
        COMMAND_CENTER_SPEC,
        CORE_MULTIPLEXER_SPEC,
        A11Y_SPEC,
      ],
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
    {
      name: 'chromium-command-center',
      use: { ...chromium, baseURL: COMMAND_CENTER_BASE_URL },
      testMatch: COMMAND_CENTER_SPEC,
    },
    {
      name: 'chromium-core-multiplexer',
      use: { ...chromium, baseURL: CORE_MULTIPLEXER_BASE_URL },
      testMatch: CORE_MULTIPLEXER_SPEC,
    },
    {
      name: 'chromium-a11y',
      use: { ...chromium, baseURL: A11Y_BASE_URL },
      testMatch: A11Y_SPEC,
    },
  ],
})
