import { existsSync } from 'node:fs'
import AxeBuilder from '@axe-core/playwright'
import { expect, test, type APIRequestContext, type Page, type WebSocketRoute } from '@playwright/test'

// The task dashboard against a real panemux: the real collection script, the
// real `ps` and `tmux` of this machine, and a HOME that
// run-panemux-task-dashboard-e2e.sh filled with what agents would have
// written. See that script for what is fake and why.

async function openDashboard(page: Page) {
  await page.goto('/')
  await page.getByRole('button', { name: /^Tasks/ }).click()
  await expect(page.getByRole('region', { name: 'Task dashboard' })).toBeVisible()
}

test('lists running and stopped sessions and returns to the workspaces', async ({ page }) => {
  await openDashboard(page)

  await expect(page.getByRole('list', { name: 'Hosts' })).toContainText('Local')
  const working = page.getByRole('region', { name: 'Working' })
  const outside = working.getByTestId('task-card-local:claude:e2e-outside')
  await expect(outside).toContainText('outside tmux')
  await expect(outside.getByRole('button', { name: /^(Open|Go to pane)/ })).toHaveCount(0)
  await expect(
    page.getByRole('region', { name: 'Stopped' }).getByTestId('task-card-local:claude:e2e-stopped'),
  ).toContainText('/tmp/e2e-stopped')

  await page.getByRole('button', { name: 'Workspaces' }).click()
  await expect(page.getByRole('region', { name: 'Task dashboard' })).toHaveCount(0)
  await expect(page.locator('[data-pane-id="task-dashboard-main"]')).toBeVisible()
})

test('switches layers with Ctrl+Shift+S while a terminal pane holds focus', async ({ page }) => {
  await page.goto('/')
  await page.locator('[data-pane-id="task-dashboard-main"] .xterm-helper-textarea').focus()

  await page.keyboard.press('Control+Shift+S')
  await expect(page.getByRole('region', { name: 'Task dashboard' })).toBeVisible()

  await page.keyboard.press('Control+Shift+S')
  await expect(page.getByRole('region', { name: 'Task dashboard' })).toHaveCount(0)
})

test('opens a session running in tmux as a pane attached to it', async ({ page, request }) => {
  const tasks = await (await request.get('/api/tasks')).json()
  test.skip(
    !tasks.tasks.some((task: { id: string }) => task.id === 'local:claude:e2e-in-tmux'),
    'tmux is not installed here, so the fixture has no session running inside it',
  )

  await openDashboard(page)
  const card = page.getByRole('region', { name: 'Waiting for input' }).getByTestId('task-card-local:claude:e2e-in-tmux')
  await expect(card).toContainText('input needed')
  await expect(card).toContainText('tmux e2e-task-dashboard · no pane')

  await card.getByRole('button', { name: /^Open/ }).click()
  const picker = page.getByRole('dialog', { name: 'Open in workspace' })
  await expect(picker.getByRole('radio', { name: 'Default' })).toBeChecked()
  await picker.getByRole('button', { name: 'Open here' }).click()

  await expect(page.getByRole('region', { name: 'Task dashboard' })).toHaveCount(0)
  const pane = page.locator('[data-pane-id]').filter({ hasText: 'e2e-task-dashboard' })
  await expect(pane).toHaveCount(1)

  await page.getByRole('button', { name: /^Tasks/ }).click()
  await expect(card).toContainText('pane e2e-task-dashboard · Default')
  await expect(card.getByRole('button', { name: /^Go to pane/ })).toBeVisible()
})

// Issue #254: an agent started from a local pane's shell, outside tmux, is
// found through the PANEMUX_PANE_ID it inherited. The command is typed into
// the pane, so the variable reaches the agent the way it does for a person.
test('goes to the local pane an agent outside tmux was started from', async ({ page, request }) => {
  test.skip(process.platform !== 'linux', 'process environments are read only on Linux hosts so far')

  await page.goto('/')
  // Keys typed before the pane's terminal stream is open are dropped, so wait
  // for the shell's prompt: output arriving means the stream is open.
  const main = page.locator('[data-pane-id="task-dashboard-main"]')
  await expect.poll(async () => (await main.locator('.xterm-rows').textContent()) ?? '').toMatch(/\S/)
  await main.locator('.xterm-helper-textarea').focus()
  // In the background, so the agent stays a child of the pane's shell.
  await page.keyboard.type('"$HOME/bin/start-agent" e2e-in-pane &')
  await page.keyboard.press('Enter')
  await expect.poll(async () => {
    const tasks = await (await request.get('/api/tasks')).json()
    const task = tasks.tasks.find((t: { id: string }) => t.id === 'local:claude:e2e-in-pane')
    return task?.location
  }).toEqual({ kind: 'outside', pane_id: 'task-dashboard-main', attachable: false })

  await page.getByRole('button', { name: /^Tasks/ }).click()
  const card = page.getByRole('region', { name: 'Working' }).getByTestId('task-card-local:claude:e2e-in-pane')
  await expect(card).toContainText('pane Shell · Default')
  await card.getByRole('button', { name: /^Go to pane/ }).click()

  await expect(page.getByRole('region', { name: 'Task dashboard' })).toHaveCount(0)
  await expect(page.locator('[data-pane-id="task-dashboard-main"]')).toBeVisible()
})

// Done and labels are written to ~/.config/panemux/tasks.json under the
// fixture's HOME, so a reload reads them back from the file rather than from
// the page's memory. The test clears its record at the end so the other
// tests keep seeing e2e-stopped in the Stopped column.
test('labels a task and marks it done, and both survive a reload', async ({ page, request }) => {
  const clear = () =>
    request.put('/api/tasks/records', {
      data: { host: '', agent: 'claude', session_id: 'e2e-stopped', done: false, labels: [] },
    })
  try {
    await openDashboard(page)
    const stopped = page.getByRole('region', { name: 'Stopped' })
    const card = stopped.getByTestId('task-card-local:claude:e2e-stopped')
    await card.click()

    const detail = page.getByRole('complementary', { name: 'Task details' })
    await detail.getByRole('textbox', { name: 'Add a label' }).fill('e2e-label')
    await detail.getByRole('button', { name: 'Add' }).click()
    await expect(card.getByRole('list', { name: 'Labels' })).toHaveText('e2e-label')

    await detail.getByRole('button', { name: 'Mark done' }).click()
    await detail.getByRole('button', { name: 'Confirm: mark done' }).click()
    await expect(card).toHaveCount(0)

    await page.reload()
    await page.getByRole('button', { name: /^Tasks/ }).click()
    await page.getByRole('checkbox', { name: 'Show done' }).check()
    const done = page.getByRole('region', { name: 'Done' }).getByTestId('task-card-local:claude:e2e-stopped')
    await expect(done.getByRole('list', { name: 'Labels' })).toHaveText('e2e-label')

    await page.getByRole('combobox', { name: 'Group tasks by' }).selectOption('label')
    await expect(page.getByRole('region', { name: 'e2e-label — Done' }).getByTestId('task-card-local:claude:e2e-stopped')).toBeVisible()
  } finally {
    expect((await clear()).ok()).toBe(true)
  }
})

// Labels used before (issue #310): a label on a record whose task is not
// listed is still offered, in the detail panel and in New task, and a tag is
// picked from the keyboard. Both records are cleared at the end.
test('offers a label recorded on an unlisted task and adds it from the keyboard', async ({ page, request }) => {
  const put = (sessionID: string, labels: string[]) =>
    request.put('/api/tasks/records', {
      data: { host: '', agent: 'claude', session_id: sessionID, done: false, labels },
    })
  try {
    expect((await put('e2e-unlisted', ['e2e-used-before'])).ok()).toBe(true)
    await openDashboard(page)
    const card = page.getByRole('region', { name: 'Stopped' }).getByTestId('task-card-local:claude:e2e-stopped')
    await card.click()

    const detail = page.getByRole('complementary', { name: 'Task details' })
    const usedBefore = detail.getByRole('group', { name: 'Used before:' })
    await detail.getByRole('textbox', { name: 'Add a label' }).fill('USED')
    await usedBefore.getByRole('button', { name: 'Add label e2e-used-before' }).focus()
    await page.keyboard.press('Enter')
    await expect(card.getByRole('list', { name: 'Labels' })).toHaveText('e2e-used-before')
    await expect(usedBefore.getByRole('button', { name: 'Add label e2e-used-before' })).toHaveCount(0)

    await page.getByRole('button', { name: 'New task' }).click()
    const dialog = page.getByRole('dialog', { name: 'New task' })
    const tag = dialog.getByRole('group', { name: 'Used before:' }).getByRole('button', { name: 'e2e-used-before' })
    await tag.focus()
    await page.keyboard.press('Space')
    await expect(dialog.getByLabel('Labels')).toHaveValue('e2e-used-before, ')
    await expect(tag).toHaveAttribute('aria-pressed', 'true')
    await dialog.getByRole('button', { name: 'Cancel' }).click()
  } finally {
    expect((await put('e2e-unlisted', [])).ok()).toBe(true)
    expect((await put('e2e-stopped', [])).ok()).toBe(true)
  }
})

// Working directory suggestions (issue #311): a directory a listed task ran
// in is offered on its host, filtered by what is typed and picked from the
// keyboard; Escape closes the list before the dialog.
test('suggests the working directory of a listed task and picks it from the keyboard', async ({ page }) => {
  await openDashboard(page)
  await page.getByRole('button', { name: 'New task' }).click()
  const dialog = page.getByRole('dialog', { name: 'New task' })
  const cwd = dialog.getByRole('combobox', { name: 'Working directory' })
  await expect(cwd).toBeFocused()
  await expect(cwd).toHaveAttribute('aria-expanded', 'false')

  await cwd.fill('E2E-STOP')
  const list = dialog.getByRole('listbox', { name: 'Recent on Local' })
  await expect(list.getByRole('option')).toHaveCount(1)
  await expect(list.getByRole('option')).toContainText('/tmp/e2e-stopped')
  await expect(list.locator('mark')).toHaveText('e2e-stop')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')
  await expect(cwd).toHaveValue('/tmp/e2e-stopped')
  await expect(list).toHaveCount(0)

  await page.keyboard.press('ArrowDown')
  await expect(list).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(list).toHaveCount(0)
  await expect(dialog).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
})

// Starting and resuming run the real launch script under real tmux; claude
// is run-panemux-task-dashboard-e2e.sh's stand-in. Both need tmux.
async function hasTmux(request: APIRequestContext): Promise<boolean> {
  const tasks = await (await request.get('/api/tasks')).json()
  return tasks.tasks.some((task: { id: string }) => task.id === 'local:claude:e2e-in-tmux')
}

test('starts a new task in a tmux session and selects it with its label', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await openDashboard(page)
  await page.getByRole('button', { name: 'New task' }).click()
  const dialog = page.getByRole('dialog', { name: 'New task' })
  await dialog.getByLabel('Working directory').fill('/tmp')
  await dialog.getByLabel('Labels').fill('e2e-launch')
  await dialog.getByLabel('First instruction').fill('--help $(touch /tmp/panemux-e2e-pwned)')
  await dialog.getByRole('button', { name: 'Start' }).click()

  await expect(dialog).toHaveCount(0)
  const detail = page.getByRole('complementary', { name: 'Task details' })
  // The task is listed once the stand-in has written its state file, which
  // can land just after the collection the launch triggers; the next poll
  // (every 10 seconds) then finds it.
  await expect(detail.getByRole('heading', { level: 2 })).toHaveText('tmp', { timeout: 15_000 })
  await expect(detail).toContainText(/tmux task-[0-9a-f]{8}/)
  const selected = page.locator('[data-testid^="task-card-local:claude:"][data-selected="true"]')
  await expect(selected).toContainText('no pane')
  await expect(selected.getByRole('list', { name: 'Labels' })).toHaveText('e2e-launch')
  expect(existsSync('/tmp/panemux-e2e-pwned'), 'the prompt must never reach a shell').toBe(false)
  const sessionID = (await selected.getAttribute('data-testid'))!.replace('task-card-local:claude:', '')
  expect(
    (await request.put('/api/tasks/records', {
      data: { host: '', agent: 'claude', session_id: sessionID, done: false, labels: [] },
    })).ok(),
  ).toBe(true)
})

// Issue #284: Type in pane opens the board's temporary tmux client (#283) on
// the task's session in a popup. The fixture's agent is `sleep` in a tmux
// pane, so what is typed comes back only as the tty's echo — which is enough
// to show the keys reached that session.
test('types into a waiting tmux task from a popup on the board, and closes back to it', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await openDashboard(page)
  const card = page.getByRole('region', { name: 'Waiting for input' }).getByTestId('task-card-local:claude:e2e-in-tmux')
  const typeIn = card.getByRole('button', { name: /^Type in pane/ })
  await typeIn.click()

  const dialog = page.getByRole('dialog', { name: /^Type in pane: / })
  await expect(dialog.getByTestId('task-input-status')).toHaveText('Connected')
  await expect(dialog).toContainText('Local · claude · tmux: e2e-task-dashboard')
  expect(await page.locator('.td-top').evaluate((el) => el.closest('[inert]') !== null)).toBe(true)
  expect(await page.getByRole('region', { name: 'Task board', exact: true }).evaluate((el) => el.closest('[inert]') !== null)).toBe(true)

  const terminalInput = dialog.locator('.xterm-helper-textarea')
  await expect(terminalInput).toBeFocused()
  await page.keyboard.type('pm284-typed')
  await expect.poll(async () => (await dialog.locator('.xterm-rows').textContent()) ?? '').toContain('pm284-typed')

  // Escape belongs to the terminal: the popup stays, and so does focus.
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(terminalInput).toBeFocused()

  const axe = await new AxeBuilder({ page }).include('[role="dialog"]').exclude('.xterm').analyze()
  expect(axe.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([])

  await page.keyboard.press('Control+Shift+Enter')
  await expect(dialog).toHaveAttribute('data-maximized', 'true')
  await expect(terminalInput).toBeFocused()
  const top = (await page.locator('.td-top').boundingBox())!
  const maximized = (await dialog.boundingBox())!
  expect(Math.abs(maximized.y - (top.y + top.height))).toBeLessThan(2)
  expect(maximized.width).toBeGreaterThanOrEqual(page.viewportSize()!.width - 1)
  await page.keyboard.press('Control+Shift+Enter')
  await expect(dialog).toHaveAttribute('data-maximized', 'false')

  await page.keyboard.press('Control+Shift+Escape')
  await expect(dialog).toHaveCount(0)
  await expect(typeIn).toBeFocused()

  // Closing ended only the board's client: the agent's session is still there.
  const tasks = await (await request.get('/api/tasks')).json()
  expect(tasks.tasks.some((task: { id: string }) => task.id === 'local:claude:e2e-in-tmux')).toBe(true)
})

test('opens Type in pane as a full sheet with no maximize on a narrow screen', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await page.setViewportSize({ width: 480, height: 800 })
  await openDashboard(page)
  await page
    .getByRole('region', { name: 'Waiting for input' })
    .getByTestId('task-card-local:claude:e2e-in-tmux')
    .getByRole('button', { name: /^Type in pane/ })
    .click()

  const dialog = page.getByRole('dialog', { name: /^Type in pane: / })
  await expect(dialog.getByTestId('task-input-status')).toHaveText('Connected')
  await expect(dialog).toHaveAttribute('data-sheet', 'true')
  await expect(dialog.getByRole('button', { name: 'Maximize' })).toHaveCount(0)
  const box = (await dialog.boundingBox())!
  expect(box.x).toBeLessThan(1)
  expect(box.width).toBeGreaterThanOrEqual(479)
  await expect(dialog.getByRole('button', { name: 'Close' })).toBeInViewport()

  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(dialog).toHaveCount(0)
})

test('keeps every top-bar action on screen on a narrow screen, the unreadable warning first', async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 800 })
  await openDashboard(page)
  const bar = page.locator('.td-top')
  const warning = bar.getByRole('button', { name: '1 unreadable session state file — show details' })
  await expect(warning).toBeVisible()
  for (const button of [warning, ...['Refresh', 'Hosts…', 'New task', 'Workspaces'].map((name) => bar.getByRole('button', { name, exact: false }))]) {
    await expect(button).toBeInViewport({ ratio: 1 })
  }
  const warningBox = (await warning.boundingBox())!
  const refreshBox = (await bar.getByRole('button', { name: 'Refresh' }).boundingBox())!
  expect(warningBox.y <= refreshBox.y || warningBox.x < refreshBox.x).toBe(true)
})

test('keeps the unreadable state details within a short screen', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 300 })
  await openDashboard(page)
  await page.getByRole('button', { name: /unreadable session state file — show details/ }).click()
  const dialog = page.getByRole('dialog', { name: 'Unreadable session state · 1 file on 1 host' })
  await expect(dialog.getByRole('row')).toHaveCount(2)
  await expect(dialog.getByRole('cell', { name: 'legacy.json' })).toBeVisible()
  const panel = (await dialog.locator('.td-unreadable-panel').boundingBox())!
  expect(panel.y + panel.height).toBeLessThanOrEqual(300)
  await dialog.getByRole('button', { name: 'Copy details' }).scrollIntoViewIfNeeded()
  await expect(dialog.getByRole('button', { name: 'Copy details' })).toBeInViewport({ ratio: 1 })
})

test('resumes a stopped task in a tmux session named after it', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await openDashboard(page)
  const id = 'task-card-local:claude:5d7e3a90-1b2c-4d3e-8f40-51627384a5b6'
  await page.getByRole('region', { name: 'Stopped' }).getByTestId(id).getByRole('button', { name: /^Resume/ }).click()

  const card = page.getByRole('region', { name: 'Idle' }).getByTestId(id)
  // As for a new task, the resumed session may be found by the next poll.
  await expect(card).toContainText('tmux task-5d7e3a90 · no pane', { timeout: 15_000 })
  await expect(card).toHaveAttribute('data-selected', 'true')
})

// Issue #272: a host added from the Hosts… dialog is written to config.yaml's
// ssh_connections, survives a reload, and becomes a dashboard host. The host
// name is in .invalid, so collecting from it fails fast and it shows as a
// host that could not be reached — still a dashboard host.
test('adds a dashboard host, keeps it across a reload, and deletes it', async ({ page }) => {
  await openDashboard(page)
  await page.getByRole('button', { name: 'Hosts…' }).click()
  let dialog = page.getByRole('dialog', { name: /Dashboard hosts/ })
  await expect(dialog).toBeVisible()

  await dialog.getByRole('button', { name: 'Add host' }).click()
  await dialog.getByLabel('Name').fill('e2e-remote')
  await dialog.getByLabel('Host', { exact: true }).fill('e2e-remote.invalid')
  await dialog.getByLabel('Password (optional)').fill('e2e-secret')
  await dialog.getByRole('button', { name: 'Save host' }).click()

  await expect(dialog.getByRole('status')).toContainText('Added e2e-remote')
  const row = dialog.getByRole('row', { name: /^e2e-remote/ })
  await expect(row).toContainText('e2e-remote.invalid')
  await expect(row).toContainText('password set')
  await expect(dialog).not.toContainText('e2e-secret')
  await expect(page.getByRole('list', { name: 'Hosts' })).toContainText('e2e-remote')

  await page.reload()
  await openDashboard(page)
  await page.getByRole('button', { name: 'Hosts…' }).click()
  dialog = page.getByRole('dialog', { name: /Dashboard hosts/ })
  await expect(dialog.getByRole('row', { name: /^e2e-remote/ })).toContainText('e2e-remote.invalid')

  await dialog.getByRole('button', { name: 'Delete e2e-remote' }).click()
  await dialog.getByRole('group', { name: 'Confirm delete' }).getByRole('button', { name: 'Delete' }).click()
  await expect(dialog.getByRole('status')).toContainText('Deleted e2e-remote')
  await expect(dialog.getByRole('row', { name: /^e2e-remote/ })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Close' }).click()
  await expect(page.getByRole('list', { name: 'Hosts' })).not.toContainText('e2e-remote')
})

// Issue #385: the popup previews a page beside its terminal. A key typed in
// the page never reaches the dashboard's window — the page is sandboxed in
// another origin — so the popup's shortcuts wait until focus is back, and
// the preview says so meanwhile.
test('previews a page from the popup, and says where its keys go while the page has them', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await openDashboard(page)
  await page
    .getByRole('region', { name: 'Waiting for input' })
    .getByTestId('task-card-local:claude:e2e-in-tmux')
    .getByRole('button', { name: /^Type in pane/ })
    .click()
  const dialog = page.getByRole('dialog', { name: /^Type in pane: / })
  await expect(dialog.getByTestId('task-input-status')).toHaveText('Connected')

  // The task's agent is `sleep`, so the tty's echo is what prints the path;
  // the run's fixture put the page in the task's directory. A fresh line
  // first, as an earlier test may have left its own echo on this one.
  await page.keyboard.press('Enter')
  await page.keyboard.type('pm385-popup.html ')
  const rows = dialog.locator('.xterm-rows')
  await expect.poll(async () => (await rows.textContent()) ?? '').toContain('pm385-popup.html')
  const link = await rows.evaluate((el) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const at = node.textContent?.indexOf('pm385-popup.html') ?? -1
      if (at < 0) continue
      const range = document.createRange()
      range.setStart(node, at)
      range.setEnd(node, at + 1)
      const box = range.getBoundingClientRect()
      return { x: box.x + box.width / 2, y: box.y + box.height / 2 }
    }
    return null
  })
  expect(link).not.toBeNull()
  await page.mouse.move(link!.x, link!.y)
  await page.mouse.click(link!.x, link!.y)
  const preview = dialog.locator('.pm-preview')
  const pageText = dialog.frameLocator('.pm-preview-frame').locator('p')
  await expect(pageText).toHaveText('pm385-popup')

  const hint = preview.getByRole('status')
  await expect(hint).toHaveCount(0)
  await pageText.click()
  await expect(hint).toHaveText(/Keys go to the page/)
  await page.keyboard.press('Control+Shift+Escape')
  await expect(dialog).toBeVisible()

  await rows.click()
  await expect(hint).toHaveCount(0)
  await page.keyboard.press('Control+Shift+Escape')
  await expect(dialog).toHaveCount(0)
})

// Issue #377: a third agent added through registration alone. The fixture
// runs panemux built with the panemux_fixture_agent tag, and its CLI is
// e2e/fixtures/fixture-agent/panemux-fixture-agent, first on PATH: a fake
// whose session is a process ps names panemux-fixture-agent, and whose
// --print mode answers every summary with the same fixed text. The dashboard,
// the API and the common collection are the shipped ones.
const FIXTURE_AGENT = 'panemux-fixture-agent'
const FIXTURE_SESSION = 'local:panemux-fixture-agent:7c1e2d3f-4a5b-4c6d-8e7f-8091a2b3c4d5'

test('lists a running fixture agent session with its state and its summary, and offers no Resume', async ({ page }) => {
  await openDashboard(page)

  const card = page.getByRole('region', { name: 'Waiting for input' }).getByTestId(`task-card-${FIXTURE_SESSION}`)
  await expect(card).toContainText(FIXTURE_AGENT)
  await expect(card).toContainText('approve the plan')
  await expect(card).not.toContainText('unknown agent')
  await expect(card.getByRole('button', { name: /^Resume/ })).toHaveCount(0)
  // A waiting task is summarized by the poll once its log is collected; the
  // fake CLI's answer is fixed.
  await expect(card.getByTestId('task-next')).toHaveText('Next: Check the summary · 2 left', { timeout: 30_000 })

  await card.click()
  const work = page.getByRole('complementary', { name: 'Task details' }).getByRole('region', { name: 'Work' })
  await expect(work).toContainText('The fixture agent answered its first instruction.')
  await expect(work).toContainText('Run make test-e2e')
})

test('starts a fixture agent task in a tmux session and selects it', async ({ page, request }) => {
  test.skip(!(await hasTmux(request)), 'tmux is not installed here')

  await openDashboard(page)
  await page.getByRole('button', { name: 'New task' }).click()
  const dialog = page.getByRole('dialog', { name: 'New task' })
  await dialog.getByLabel('Agent').selectOption(FIXTURE_AGENT)
  // The host's probe found the fake CLI on PATH.
  await expect(dialog.getByText(/cannot be started on/)).toHaveCount(0)
  await dialog.getByLabel('Working directory').fill('/tmp')
  await dialog.getByLabel('First instruction').fill('--help $(touch /tmp/panemux-e2e-fixture-pwned)')
  await dialog.getByRole('button', { name: 'Start' }).click()

  await expect(dialog).toHaveCount(0)
  const detail = page.getByRole('complementary', { name: 'Task details' })
  // As for Claude Code, the task may be found by the poll after the launch's.
  await expect(detail.getByRole('heading', { level: 2 })).toHaveText('tmp', { timeout: 15_000 })
  const selected = page.locator(`[data-testid^="task-card-local:${FIXTURE_AGENT}:"][data-selected="true"]`)
  await expect(selected).toContainText(/tmux task-[0-9a-f]{8} · no pane/)
  await expect(page.getByRole('region', { name: 'Idle' }).locator(`[data-testid^="task-card-local:${FIXTURE_AGENT}:"]`)).not.toHaveCount(0)
  await expect(detail.getByRole('button', { name: /^Resume/ })).toHaveCount(0)
  expect(existsSync('/tmp/panemux-e2e-fixture-pwned'), 'the prompt must never reach a shell').toBe(false)
})

// Review on #430: the collection is the real one, but it sees only the
// fixture's own processes. The fixture starts a claude process detached from
// it, with a state file, as a developer's own session would be; ps and lsof
// stand-ins answer only for processes that descend from the fixture, so it is
// never listed, and neither is anything else running outside the fixture.
test('lists nothing running outside the fixture', async ({ request }) => {
  const tasks: { id: string; cwd?: string }[] = (await (await request.get('/api/tasks')).json()).tasks
  expect(tasks.map((t) => t.id)).not.toContain('local:claude:e2e-decoy-outside')
  const outside = tasks.filter(
    (t) => t.cwd && !/^\/(private\/)?tmp(\/e2e-stopped)?$|\/panemux-e2e-task-dashboard-home(\/|$)/.test(t.cwd),
  )
  expect(outside.map((t) => `${t.id} ${t.cwd}`)).toEqual([])
})


test('table supports keyboard sorting, shared details, scrolling and narrow screens', async ({ page, request }) => {
  const data = await (await request.get('/api/tasks')).json()
  const template = data.tasks.find((task: { state: string }) => task.state !== 'stop')
  expect(template).toBeTruthy()
  await page.route('**/api/tasks', route => route.fulfill({ json: { ...data,
    tasks: Array.from({ length: 35 }, (_, index) => ({ ...template,
      id: `table-${index}`, cwd: `/tmp/sample-project/session-${index}`,
      summary: { state: 'ready', text: 'A long session summary that stays inside its own cell while every independent column remains readable.' },
    })),
  } }))
  await openDashboard(page)
  await page.getByRole('button', { name: 'Table', exact: true }).focus()
  await page.keyboard.press('Enter')
  const table = page.getByRole('table', { name: 'Tasks' })
  const list = page.getByRole('region', { name: 'Task table', exact: true })
  await expect(table).toBeVisible()
  const headingCenters = await table.getByRole('columnheader').evaluateAll(headings => headings.map(heading => {
    const range = document.createRange()
    range.selectNodeContents(heading.querySelector('button') || heading)
    const rect = range.getBoundingClientRect()
    return rect.y + rect.height / 2
  }))
  expect(Math.max(...headingCenters) - Math.min(...headingCenters)).toBeLessThan(2)
  const sort = table.getByRole('button', { name: /Sort by Session/ })
  await sort.focus()
  await page.keyboard.press('Enter')
  await expect(table.getByRole('columnheader', { name: /Session/ })).toHaveAttribute('aria-sort', 'ascending')
  const session = table.getByRole('button', { name: 'session-2', exact: true })
  await session.focus()
  await page.keyboard.press('Enter')
  const details = page.getByRole('complementary', { name: 'Task details' })
  await expect(details).toBeVisible()
  await expect(session).toHaveAttribute('aria-pressed', 'true')
  await list.evaluate(element => { element.scrollTop = 200; element.scrollLeft = 150 })
  const before = await table.getByRole('columnheader', { name: /Session/ }).boundingBox()
  const viewport = await list.boundingBox()
  expect(Math.abs(before!.y - viewport!.y)).toBeLessThan(3)
  await page.getByRole('button', { name: 'Cards', exact: true }).click()
  await page.getByRole('button', { name: 'Table', exact: true }).click()
  expect(await list.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
  await expect(session).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: 'Workspaces', exact: true }).click()
  await page.getByRole('button', { name: /^Tasks/ }).click()
  await expect(table).toBeVisible()
  await expect(table.getByRole('columnheader', { name: /Session/ })).toHaveAttribute('aria-sort', 'ascending')
  await page.reload()
  await page.getByRole('button', { name: /^Tasks/ }).click()
  await expect(table).toBeVisible()
  await expect(table.getByRole('columnheader', { name: /Session/ })).toHaveAttribute('aria-sort', 'ascending')
  const axe = await new AxeBuilder({ page }).include('.td-table-scroll').analyze()
  expect(axe.violations.map(v => `${v.id}: ${v.help}`)).toEqual([])
  await page.setViewportSize({ width: 600, height: 700 })
  await expect(table).toBeVisible()
  expect(await list.evaluate(element => element.scrollWidth > element.clientWidth)).toBe(true)
  await list.focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => list.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)
})


test('group rows stretch across states with shared scrolling, sticky bands and live moves', async ({ page, request }) => {
  const data = await (await request.get('/api/tasks')).json()
  const template = data.tasks[0]
  expect(template).toBeTruthy()
  const longName = 'beta — integration checks for the sample project and its release pipeline'
  let tasks = Array.from({ length: 9 }, (_, index) => ({ ...template,
    id: `group-${index}`, session_id: `sample-session-${index}`, host: '',
    cwd: `/tmp/sample-project/card-${index}`, labels: [index < 7 ? 'alpha' : longName],
    state: index < 6 ? 'busy' : 'wait', done: false,
    waiting_for: index === 6 ? 'Review the migration plan and its rollback steps before continuing. '.repeat(8) : 'Input needed',
  }))
  tasks.push({ ...tasks[0], id: 'group-done', state: 'stop', done: true, labels: ['finished'] })
  await page.route('**/api/tasks', route => route.fulfill({ json: { ...data, tasks } }))
  await openDashboard(page)
  await page.getByLabel('Group tasks by').selectOption('label')
  const board = page.getByRole('region', { name: 'Task board', exact: true })
  const groups = board.locator('.td-group')
  await expect(groups).toHaveCount(2)
  const aligned = async (columns: number) => {
    const rows = await groups.evaluateAll(elements => elements.map(group => ({
      top: group.getBoundingClientRect().top,
      bottom: group.getBoundingClientRect().bottom,
      cells: [...group.querySelectorAll('.td-col')].map(cell => {
        const rect = cell.getBoundingClientRect()
        return { top: rect.top, bottom: rect.bottom, left: rect.left, width: rect.width }
      }),
    })))
    for (const row of rows) {
      expect(row.cells).toHaveLength(columns)
      for (const edge of ['top', 'bottom'] as const) {
        expect(Math.max(...row.cells.map(c => c[edge])) - Math.min(...row.cells.map(c => c[edge]))).toBeLessThanOrEqual(1)
      }
    }
    for (let i = 1; i < rows.length; i++) {
      expect(rows[i].top).toBeGreaterThanOrEqual(rows[i - 1].bottom - 1)
      rows[i].cells.forEach((cell, j) => {
        expect(Math.abs(cell.left - rows[0].cells[j].left)).toBeLessThanOrEqual(1)
        expect(Math.abs(cell.width - rows[0].cells[j].width)).toBeLessThanOrEqual(1)
      })
    }
  }
  await aligned(5)
  const headingTops = await groups.first().locator('.td-colh-dot').evaluateAll(els => els.map(el => el.getBoundingClientRect().top))
  expect(Math.max(...headingTops) - Math.min(...headingTops)).toBeLessThanOrEqual(1)
  await expect(board.getByRole('region', { name: 'alpha — Stopped' })).toContainText('None')
  await page.getByLabel('Show done').check()
  await expect(groups).toHaveCount(3)
  await aligned(6)
  await page.getByLabel('Show done').uncheck()
  await board.getByRole('button', { name: 'card-0', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Task details' })).toBeVisible()
  await aligned(5)
  await board.focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => board.evaluate(el => el.scrollLeft)).toBeGreaterThan(0)
  await aligned(5)
  // Move the selected card into the next group and change the most populated column.
  tasks = tasks.map(t => t.id === 'group-0' ? { ...t, labels: [longName], state: 'idle' } : { ...t, state: t.done ? t.state : 'wait' })
  await page.getByRole('button', { name: 'Refresh', exact: true }).click()
  const moved = board.getByRole('region', { name: `${longName} — Idle` }).getByTestId('task-card-group-0')
  await expect(moved).toHaveAttribute('data-selected', 'true')
  await expect(page.getByRole('complementary', { name: 'Task details' })).toContainText('card-0')
  await aligned(5)
  // A short scrollport leaves enough room to pin the second group's band.
  await page.setViewportSize({ width: 1100, height: 350 })
  await board.evaluate(el => { el.scrollTop = 0; el.scrollLeft = 0 })
  const scrollInsideGroup = async (index: number) => {
    await board.evaluate((el, groupIndex) => {
      const group = el.querySelectorAll('.td-group')[groupIndex]
      el.scrollTop += group.getBoundingClientRect().top - el.getBoundingClientRect().top + 20
    }, index)
  }
  const assertPinned = async (index: number) => {
    const viewport = await board.boundingBox()
    const band = await groups.nth(index).locator('.td-group-band').boundingBox()
    expect(Math.abs(band!.y - viewport!.y)).toBeLessThanOrEqual(1)
  }
  await scrollInsideGroup(0)
  await assertPinned(0)
  await scrollInsideGroup(1)
  await assertPinned(1)
  const bands = await groups.locator('.td-group-band').evaluateAll(els => els.map(el => {
    const r = el.getBoundingClientRect(); return { top: r.top, bottom: r.bottom }
  }))
  expect(bands[0].bottom).toBeLessThanOrEqual(bands[1].top + 1)
  await page.setViewportSize({ width: 500, height: 700 })
  await aligned(5)
  expect(await board.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(true)
  await page.getByRole('complementary', { name: 'Task details' }).getByRole('button', { name: /Close/ }).click()
  await board.focus()
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => board.evaluate(el => el.scrollLeft)).toBeGreaterThan(0)
  await page.getByLabel('Group tasks by').selectOption('none')
  await expect(groups).toHaveCount(0)
  const columns = board.locator('.td-col')
  await expect(columns).toHaveCount(5)
  const rects = await columns.evaluateAll(els => els.map(el => el.getBoundingClientRect().top))
  expect(rects[1]).toBeGreaterThan(rects[0])
})


test('notification jumps uncover a card underneath its sticky group band', async ({ page, request }) => {
  const data = await (await request.get('/api/tasks')).json()
  const tasks = Array.from({ length: 20 }, (_, i) => ({ ...data.tasks[0],
    id: `notice-${i}`, session_id: `sample-notice-${i}`, host: '',
    cwd: `/tmp/sample-project/notice-${i}`, state: 'wait', done: false,
    labels: [i < 10 ? 'alpha' : 'beta'], waiting_for: 'Review the sample change',
    location: { kind: 'outside', attachable: false },
  }))
  await page.route('**/api/tasks', route => route.fulfill({ json: { ...data, tasks } }))
  await page.addInitScript(() => {
    const recorded: Array<{ click: () => void }> = []
    ;(window as Window & { __groupNotifications?: typeof recorded }).__groupNotifications = recorded
    class MockNotification {
      static permission = 'granted'
      static requestPermission = () => Promise.resolve('granted')
      onclick: (() => void) | null = null
      constructor() { recorded.push({ click: () => this.onclick?.() }) }
      close() {}
    }
    Object.defineProperty(window, 'Notification', { configurable: true, value: MockNotification })
    Object.defineProperty(document, 'hasFocus', { configurable: true, value: () => false })
  })
  let stream: WebSocketRoute | undefined
  const targetTask = { ...tasks[10], state: 'busy' }
  await page.routeWebSocket(/\/ws\/tasks\/events$/, ws => {
    stream = ws
    ws.send(JSON.stringify({ type: 'snapshot', epoch: 'group-notice', seq: 10,
      hosts: [{ name: '', status: 'ok' }], tasks: [targetTask] }))
  })
  await page.setViewportSize({ width: 1100, height: 450 })
  await openDashboard(page)
  await page.getByLabel('Group tasks by').selectOption('label')
  const board = page.getByRole('region', { name: 'Task board', exact: true })
  await board.getByRole('button', { name: 'notice-0', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Task details' })).toBeVisible()
  await expect.poll(() => stream !== undefined).toBe(true)
  stream!.send(JSON.stringify({ type: 'task', epoch: 'group-notice', seq: 11, op: 'changed', prev_state: 'busy',
    task: { ...targetTask, state: 'wait', wait_id: 'w1-group-notice', waiting_for: 'Review the sample change' } }))
  await expect.poll(() => page.evaluate(() =>
    (window as Window & { __groupNotifications?: unknown[] }).__groupNotifications?.length)).toBe(1)
  const card = board.getByRole('region', { name: 'beta — Waiting for input' }).getByTestId('task-card-notice-10')
  await expect(card).toBeVisible()
  await card.evaluate(el => {
    const board = el.closest('.td-board')!
    board.scrollTop += el.getBoundingClientRect().top - board.getBoundingClientRect().top - 8
  })
  const band = board.getByRole('region', { name: 'beta · 10 tasks', exact: true }).locator('.td-group-band')
  const before = await card.boundingBox()
  const beforeBand = await band.boundingBox()
  expect(before!.y).toBeLessThan(beforeBand!.y + beforeBand!.height - 1)
  expect(before!.y).toBeGreaterThanOrEqual((await board.boundingBox())!.y)
  await page.evaluate(() => {
    (window as Window & { __groupNotifications?: Array<{ click: () => void }> }).__groupNotifications![0].click()
  })
  await expect(card.getByRole('button', { name: 'notice-10', exact: true })).toBeFocused()
  const after = await card.boundingBox()
  const afterBand = await band.boundingBox()
  expect(after!.y).toBeGreaterThanOrEqual(afterBand!.y + afterBand!.height - 1)
})
