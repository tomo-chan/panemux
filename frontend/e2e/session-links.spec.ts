import { expect, test } from '@playwright/test'

test('a session link opens once, survives reload and pane movement, and leaves on explicit selection', async ({ page, request }) => {
  // APIRequestContext sends no browser Origin/Referer/Fetch Metadata: the same
  // existing GET an external session-link skill uses, with no board token.
  const response = await request.get('/api/tasks')
  expect(response.ok()).toBe(true)
  const data = await response.json()
  test.skip(!data.tasks.some((task: { session_id: string }) => task.session_id === 'e2e-in-tmux'), 'tmux unavailable')
  const query = '?session_host=&session_agent=claude&session_id=e2e-in-tmux'
  await page.goto('/' + query)
  await expect(page.getByText('Opening session…')).toHaveCount(0)
  const pane = page.locator('[data-pane-id]').filter({ has: page.getByText('e2e-task-dashboard', { exact: true }) })
  await expect(pane).toHaveCount(1)
  const paneId = await pane.getAttribute('data-pane-id')
  await expect(pane.locator('.xterm-helper-textarea')).toBeFocused()
  await page.reload()
  await expect(page.locator(`[data-pane-id="${paneId}"] .xterm-helper-textarea`)).toBeFocused()
  const before = await (await request.get('/api/workspaces')).json()
  const source = before.items.find((workspace: { layout: unknown }) => JSON.stringify(workspace.layout).includes(paneId))
  const originalLayout = structuredClone(source.layout)
  const child = source.layout.children.find((item: { pane?: { id: string } }) => item.pane?.id === paneId)
  source.layout.children = source.layout.children.filter((item: { pane?: { id: string } }) => item.pane?.id !== paneId)
  expect((await request.put(`/api/workspaces/${source.id}/layout`, { data: source.layout })).ok()).toBe(true)
  // Add the same pane to a second workspace through the normal workspace API.
  const created = await request.post('/api/workspaces')
  expect(created.ok()).toBe(true)
  const workspace = await created.json()
  const destination = workspace.active
  const destinationLayout = workspace.items.find((item: { id: string }) => item.id === destination).layout
  expect(destination).toBeTruthy()
  expect((await request.put(`/api/workspaces/${destination}/layout`, { data: { direction: 'horizontal', children: [{ ...child, size: 100 }] } })).ok()).toBe(true)
  await page.goto('/' + query)
  await expect(page.locator(`[data-pane-id="${paneId}"] .xterm-helper-textarea`)).toBeFocused()
  await expect(page).toHaveURL(new RegExp('session_id=e2e-in-tmux'))
  await page.getByRole('button', { name: /^Tasks/ }).click()
  const card = page.getByTestId('task-card-local:claude:e2e-in-tmux')
  await card.getByRole('button', { name: 'Copy session link' }).click()
  // Clipboard permission differs by browser; either feedback supplies the URL.
  await expect(card.getByText('Copied').or(card.getByRole('textbox', { name: 'Session link' }))).toBeVisible()
  await page.getByTestId('task-card-local:claude:e2e-outside').locator('.td-card-title').click()
  await expect(page).not.toHaveURL(/session_id=/)
  expect((await request.put(`/api/workspaces/${destination}/layout`, { data: destinationLayout })).ok()).toBe(true)
  expect((await request.put(`/api/workspaces/${source.id}/layout`, { data: originalLayout })).ok()).toBe(true)
  expect((await request.delete(`/api/workspaces/${destination}`)).ok()).toBe(true)
})

test('stopped, missing and malformed sessions leave the app usable', async ({ page }) => {
  await page.goto('/?session_host=&session_agent=claude&session_id=e2e-stopped')
  await expect(page.getByRole('status')).toContainText('Resume manually')
  await expect(page.getByRole('region', { name: 'Task dashboard' })).toBeVisible()
  await page.goto('/?session_host=&session_agent=claude&session_id=missing-session')
  await expect(page.getByRole('status')).toContainText('Session not found')
  await page.goto('/?session_id=%FF')
  await expect(page.getByRole('status')).toContainText('Invalid session link')
  await page.getByRole('button', { name: 'Clear link' }).click()
  await expect(page).not.toHaveURL(/session_id=/)
})
