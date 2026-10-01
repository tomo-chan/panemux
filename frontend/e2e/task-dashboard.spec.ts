import { existsSync } from 'node:fs'
import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

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
  await page.locator('[data-pane-id="task-dashboard-main"] .xterm-helper-textarea').focus()
  await page.keyboard.type('"$HOME/bin/start-agent" e2e-in-pane')
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
    await page.getByRole('checkbox', { name: 'Show Done column' }).check()
    const done = page.getByRole('region', { name: 'Done' }).getByTestId('task-card-local:claude:e2e-stopped')
    await expect(done.getByRole('list', { name: 'Labels' })).toHaveText('e2e-label')

    await page.getByRole('combobox', { name: 'Split rows by' }).selectOption('label')
    await expect(page.getByRole('region', { name: 'Done' }).getByRole('heading', { level: 3 })).toHaveText('e2e-label')
  } finally {
    expect((await clear()).ok()).toBe(true)
  }
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
