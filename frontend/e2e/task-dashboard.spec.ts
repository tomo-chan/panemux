import { expect, test, type Page } from '@playwright/test'

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
