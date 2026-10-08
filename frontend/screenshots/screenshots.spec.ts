import { fileURLToPath } from 'node:url'
import { expect, test, type Locator, type Page } from '@playwright/test'

// Captures the images README.md and docs/ show (`make screenshots`). Each
// test writes one file under docs/images/. See playwright.config.ts in this
// directory for why this is not part of the e2e suite, and
// run-panemux-screenshots.sh for what the server is given to show.

const IMAGES_DIR = fileURLToPath(new URL('../../docs/images/', import.meta.url))

function imagePath(name: string): string {
  return `${IMAGES_DIR}${name}`
}

function pane(page: Page, id: string): Locator {
  return page.locator(`[data-pane-id="${id}"]`)
}

function terminalText(target: Locator): Promise<string> {
  return target.locator('.xterm-rows').textContent().then((text) => text ?? '')
}

// Types a command into a pane once its shell has printed a prompt: keys sent
// before the terminal stream is open are dropped.
async function run(page: Page, id: string, command: string, expectOutput: RegExp) {
  const target = pane(page, id)
  await expect.poll(() => terminalText(target)).toContain('demo@panemux')
  await target.locator('.xterm-helper-textarea').focus()
  await page.keyboard.type(command)
  await page.keyboard.press('Enter')
  await expect.poll(() => terminalText(target)).toMatch(expectOutput)
}

// One test, not one per image: the server keeps every pane's scrollback, so
// commands typed by a second test would show up twice in its capture.
test('documentation screenshots', async ({ page }) => {
  // The Agent Board relay reads agmsg on a 5s poll and the panel reads the
  // board API on another.
  test.setTimeout(120_000)
  await serveTasks(page)
  await page.goto('/')
  await expect(pane(page, 'editor')).toBeVisible()

  await test.step('workspace', async () => {
    await run(page, 'editor', 'git log --oneline --graph --all', /initial commit/)
    await run(page, 'tests', './scripts/test', /ok\s+sample-project\/internal\/search/)
    await expect.poll(() => terminalText(pane(page, 'server'))).toContain('/api/search?q=tmux')
    // Leave no pane focused, so no cursor or focus ring depends on timing.
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
    await page.screenshot({ path: imagePath('workspace.png') })
  })

  await test.step('pane settings', async () => {
    await pane(page, 'editor').getByTitle('Pane settings').click()
    const dialog = page.getByRole('dialog', { name: 'Pane settings' })
    await expect(dialog).toBeVisible()
    await page.screenshot({ path: imagePath('pane-settings.png') })
    await dialog.getByRole('button', { name: 'Cancel' }).click()
    await expect(dialog).toHaveCount(0)
  })

  await test.step('agent board', async () => {
    await page.getByRole('button', { name: 'Open agent board' }).click()
    const panel = page.getByRole('dialog', { name: 'Agent board' })
    await expect(panel).toBeVisible()
    await expect(panel.getByText(/One flaky timeout/)).toBeVisible({ timeout: 30_000 })
    await expect(panel.getByText(/Adding pagination/)).toBeVisible()
    await page.screenshot({ path: imagePath('agent-board.png') })
    await panel.getByRole('button', { name: 'Close agent board panel' }).click()
    await expect(panel).toHaveCount(0)
  })

  await test.step('task dashboard', async () => {
    await page.getByRole('button', { name: /^Tasks/ }).click()
    const dashboard = page.getByRole('region', { name: 'Task dashboard' })
    await expect(dashboard).toBeVisible()
    await dashboard.getByTestId(`task-card-${WAITING_TASK_ID}`).click()
    await expect(
      dashboard.getByRole('complementary', { name: 'Task details' }).getByText('Add pagination to the search endpoint'),
    ).toBeVisible()
    await page.screenshot({ path: imagePath('task-dashboard.png') })
  })
})

// The real collection lists every claude process of the user generating the
// images, so the dashboard is given a fixed set of placeholder tasks
// instead, and the task event stream is kept from replacing their states.
const WAITING_TASK_ID = 'local:claude:0b6a7c1e-2d3f-4a5b-8c9d-0e1f2a3b4c5d'

async function serveTasks(page: Page) {
  const now = Date.now()
  const ago = (minutes: number) => new Date(now - minutes * 60_000).toISOString()
  const repo = (name: string, branch: string, pr?: number) => ({
    repo: `example/${name}`,
    repo_url: `https://github.com/example/${name}`,
    branch,
    ...(pr ? { pr_url: `https://github.com/example/${name}/pull/${pr}`, pr_number: pr } : {}),
  })
  const tasks = [
    {
      id: WAITING_TASK_ID,
      host: '',
      agent: 'claude',
      session_id: '0b6a7c1e-2d3f-4a5b-8c9d-0e1f2a3b4c5d',
      cwd: '/workspace/user/sample-project',
      state: 'wait',
      waiting_for: 'approve Bash: make test',
      status_since: ago(2),
      started_at: ago(45),
      pid: 4101,
      location: { kind: 'tmux', tmux_session: 'task-0b6a7c1e', attachable: true },
      git: repo('sample-project', 'feature/search', 128),
      labels: ['api'],
    },
    {
      id: 'local:claude:1c7b8d2f-3e4a-4b5c-9d0e-1f2a3b4c5d6e',
      host: '',
      agent: 'claude',
      session_id: '1c7b8d2f-3e4a-4b5c-9d0e-1f2a3b4c5d6e',
      cwd: '/workspace/user/sample-project',
      state: 'busy',
      status_since: ago(6),
      started_at: ago(30),
      pid: 4102,
      location: { kind: 'tmux', tmux_session: 'task-1c7b8d2f', attachable: true },
      git: repo('sample-project', 'feature/pagination'),
      labels: ['api'],
    },
    {
      id: 'build-box:codex:01a0e2b9-d054-7cc2-9278-a5e25ebcc524',
      host: 'build-box',
      agent: 'codex',
      session_id: '01a0e2b9-d054-7cc2-9278-a5e25ebcc524',
      cwd: '/remote/home/demo/sample-web',
      state: 'busy',
      status_since: ago(12),
      started_at: ago(50),
      pid: 2201,
      location: { kind: 'tmux', tmux_session: 'task-5ebcc524', attachable: true },
      git: repo('sample-web', 'fix/login-redirect', 131),
    },
    {
      id: 'local:claude:2d8c9e3a-4f5b-4c6d-8e1f-2a3b4c5d6e7f',
      host: '',
      agent: 'claude',
      session_id: '2d8c9e3a-4f5b-4c6d-8e1f-2a3b4c5d6e7f',
      cwd: '/workspace/user/sample-docs',
      state: 'idle',
      status_since: ago(20),
      started_at: ago(90),
      pid: 4103,
      location: { kind: 'outside', attachable: false },
      git: repo('sample-docs', 'docs/getting-started', 77),
      labels: ['docs'],
    },
    {
      id: 'local:claude:3e9d0f4b-5a6c-4d7e-9f2a-3b4c5d6e7f80',
      host: '',
      agent: 'claude',
      session_id: '3e9d0f4b-5a6c-4d7e-9f2a-3b4c5d6e7f80',
      cwd: '/workspace/user/sample-project',
      state: 'stop',
      status_since: ago(180),
      location: { kind: 'none', attachable: false },
      git: repo('sample-project', 'main'),
      done: true,
    },
    {
      id: 'build-box:claude:4fa01a5c-6b7d-4e8f-8a3b-4c5d6e7f8091',
      host: 'build-box',
      agent: 'claude',
      session_id: '4fa01a5c-6b7d-4e8f-8a3b-4c5d6e7f8091',
      cwd: '/remote/home/demo/sample-infra',
      state: 'stop',
      status_since: ago(240),
      location: { kind: 'none', attachable: false },
      git: repo('sample-infra', 'chore/upgrade-runtime'),
    },
  ]
  // Summaries are shown only for the first task: they come from the server's
  // own `claude -p` run, which this fixture does not have.
  Object.assign(tasks[0], {
    summary: {
      state: 'ready',
      text: 'Add pagination to the search endpoint',
      remaining: ['Run the handler tests', 'Update the API docs'],
      summarized_at: ago(3),
    },
  })
  await page.route('**/api/tasks', (route) =>
    route.fulfill({
      json: {
        hosts: [
          { name: '', status: 'ok', collected_at: ago(0) },
          { name: 'build-box', status: 'ok', collected_at: ago(0) },
        ],
        tasks,
        // Labels used before (issue #310), offered under Add a label.
        known_labels: [
          'api', 'backend', 'bug', 'ci', 'Docs', 'docs', 'e2e', 'frontend', 'infra', 'perf',
          'refactor', 'release-1.4', 'release-1.5', 'research', 'review', 'security', 'spike', 'tests', 'ui', 'ux',
        ],
        summaries_enabled: true,
      },
    }),
  )
  await page.routeWebSocket(/\/ws\/tasks\/events$/, () => {})
}
