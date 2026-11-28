import { expect, test } from '@playwright/test'
import { boardDialog, boardOpenButton, gotoAndReadSessionToken } from './agent-board-helpers'

// Runs against the agent-board-agmsg.yml fixture (playwright.config.ts's
// chromium-agent-board-agmsg project), whose fake HOME contains the fixed
// agmsg installation run-panemux-agent-board-e2e.sh seeds before
// starting the server.
//
// Only agmsg itself is stubbed, and only because it is a separate,
// operator-installed tool panemux deliberately never installs (see
// docs/security/agent-board.md); the two shell scripts under e2e/fixtures/agmsg stand in
// for the ones a real installation provides. Everything the dashboard
// actually depends on is the real implementation: the relay really execs
// those scripts, parses their JSONL, validates host-local identities,
// writes the real BoardCache, and serves it over the
// real bearer-token-gated /api/board endpoints. The layout-only test explicitly
// supplies status/history fixtures; delivery tests use the real relay path.

test('renders the pane status and message the relay read from the agmsg installation', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()

  const panel = boardDialog(page)
  await expect(panel).toBeVisible()

  // Pane status card, from the board_status self-report row: the pane is on
  // the board, and this is what it is doing.
  await expect(panel.getByText('board-main@local', { exact: true })).toBeVisible()
  await expect(panel.getByText('working', { exact: true })).toBeVisible()
  await expect(panel.getByText('tool: Edit')).toBeVisible()

  // The summary is longer than one line of the 420px panel. A clipped
  // string is still "visible" to Playwright, so assert the rendered box is
  // taller than a single line instead — that is what wrapping actually
  // means, and it is the assertion jsdom cannot make.
  const summary = panel.getByText(/^Wiring the dashboard panel so a pane/)
  await expect(summary).toBeVisible()
  await expect(summary).toContainText('silently missing')
  const box = await summary.boundingBox()
  expect(box).not.toBeNull()
  expect(box!.height).toBeGreaterThan(20)

  // The pane title sits beside the ID so a column of panes is readable.
  await expect(panel.getByText('Board Main', { exact: true })).toBeVisible()

  // An idle shell with no report or detected agent is omitted.
  await expect(panel.getByText('board-worker', { exact: true })).toHaveCount(0)

  // The same fixture row also carries repo, branch, cwd and pr_url, which
  // the card deliberately does not render — panemux computes those itself
  // for the pane header, and a self-reported copy can contradict it. The
  // card having no <a> at all is the same decision seen from the security
  // side (docs/security/agent-board.md's Agent Board remote writes and dashboard UI).
  await expect(panel.getByText('example/project')).toHaveCount(0)
  await expect(panel.getByText('feature/agent-board')).toHaveCount(0)
  await expect(panel.getByText('/workspace/user/project')).toHaveCount(0)
  await expect(panel.getByRole('link')).toHaveCount(0)

  // Ordinary cross-pane message row.
  await expect(panel.getByText('board-main@local → board-worker')).toBeVisible()
  await expect(panel.getByText('Handing the dashboard panel over for review.')).toBeVisible()

  // The status self-report is stored in agmsg history like any other
  // message, so the message feed has to filter it out itself — raw
  // board_status JSON must never show up as if someone had sent it.
  await expect(panel).not.toContainText('board_status')
  await expect(panel).not.toContainText('board-main → _system')
})

test('observes pane-free names and sends to an unobserved qualified name from Message mode', async ({ page }) => {
  test.setTimeout(90_000)
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()
  const panel = boardDialog(page)
  const helper = panel.getByRole('article').filter({ hasText: 'helper@local' })
  await expect(helper.getByText('Self-report', { exact: true })).toBeVisible()
  await expect(helper.getByText(/No pane/)).toBeVisible()
  await expect(helper.getByRole('button')).toHaveCount(1) // short report: copy only; no pane actions
  const observer = panel.getByRole('article').filter({ hasText: 'observer@local' })
  await expect(observer.getByText('Unreported')).toBeVisible()
  await expect(observer.getByRole('button', { name: 'Copy address' })).toBeVisible()
  await panel.getByRole('button', { name: 'Close agent board panel' }).click()

  await page.keyboard.press(process.platform === 'darwin' ? 'Meta+Shift+k' : 'Control+Shift+k')
  const command = page.getByRole('dialog', { name: 'Command center', exact: true })
  await command.getByRole('button', { name: 'Message', exact: true }).click()
  await expect(command.getByRole('option', { name: 'helper@local · observed' })).toHaveCount(1)
  await command.getByLabel('Add recipient').selectOption('helper@local')
  await expect(command.getByLabel('Recipients')).toHaveValue('helper@local')
  await command.getByLabel('Recipients').fill('helper@local, unseen@local')
  const body = `Qualified UI message ${Date.now()}.`
  await command.getByLabel('Message body').fill(body)
  await command.getByRole('button', { name: 'Send message' }).click()
  await expect(command.getByRole('status')).toHaveText('Sent to: helper@local, unseen@local')
  await page.keyboard.press('Escape')
  await boardOpenButton(page).click()
  await expect(panel.getByText(body)).toHaveCount(2, { timeout: 60_000 })
  // agmsg stores the resolved host-local name. Keep that original recipient
  // and the source host; do not invent a qualified history snapshot.
  await expect(panel.getByText('_system → helper', { exact: true })).toBeVisible()
  await expect(panel.getByText('_system → unseen', { exact: true })).toBeVisible()
})

test('shows a message that arrives while the dashboard is already open', async ({ page }) => {
  // Two independent 5s polls stand between the broadcast and the rendered
  // row: the relay reading agmsg, then the panel reading the board API.
  test.setTimeout(90_000)

  const sessionToken = await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()

  const panel = boardDialog(page)
  await expect(panel).toBeVisible()

  const body = `Broadcast from the e2e suite at ${Date.now()}.`
  const delivered = await page.evaluate(
    async ({ token, body }) => {
      const response = await fetch('/api/board/broadcast', {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ to: ['board-worker'], body }),
      })
      if (!response.ok) throw new Error(`broadcast failed with ${response.status}`)
      return (await response.json()) as { delivered: string[] }
    },
    { token: sessionToken.token, body },
  )
  expect(delivered.delivered).toEqual(['board-worker'])

  // The panel is never reopened: this asserts the poll picks the new row up
  // on its own, after the relay has read it back out of agmsg.
  await expect(panel.getByText(body)).toBeVisible({ timeout: 60_000 })
  await expect(panel.getByText('_system → board-worker')).toBeVisible()
})

test('wraps long task waiting reasons and valid qualified history addresses at narrow widths', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 860 })
  const host = `ssh:${'build'.repeat(60)}`
  const reason = `approval-${'argument'.repeat(60)}`
  const now = new Date().toISOString()
  await page.route('**/api/board/status', route => route.fulfill({ json: {
    identities: { 'worker@local': { host: 'local', team: 'demo', name: 'worker', pane_id: null, source: 'task',
      state: 'wait', waiting_for: reason, status_since: now, task_observed_at: now, last_message_at: null, self_report: null } },
    statuses: {}, participation: {},
  } }))
  await page.route('**/api/board/messages?*', route => route.fulfill({ json: { epoch: 'layout', messages: [
    { at: now, host, team: 'demo', from: 'worker', to: 'orch@local', body: 'remote long host', seq: 1, is_status: false },
    { at: now, host: 'local', team: 'demo', from: 'worker', to: `reviewer@${host}`, body: 'local same name', seq: 2, is_status: false },
  ] } }))
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()
  const panel = boardDialog(page)
  const waiting = panel.getByText(`Waiting for: ${reason}`, { exact: true })
  await expect(waiting).toBeVisible()
  const sender = panel.getByText(`worker@${host} → orch@local`, { exact: true })
  await expect(sender).toBeVisible()
  const timestamp = sender.locator('..').locator('div').first()
  await expect(timestamp).toContainText(host)
  for (const line of [waiting, sender, timestamp]) {
    expect(await line.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
    expect((await line.boundingBox())!.height).toBeGreaterThan(20)
  }
  await expect(panel.getByText(`worker@local → reviewer@${host}`, { exact: true })).toBeVisible()
  expect(await panel.locator('article').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  expect(await panel.evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
})

test('closing and reopening during a real send preserves pending and yields one history row', async ({ page }) => {
  test.setTimeout(90_000)
  await gotoAndReadSessionToken(page)
  let release!: () => void
  const held = new Promise<void>(resolve => { release = resolve })
  let posts = 0
  await page.route('**/api/board/broadcast', async route => {
    posts++
    const response = await route.fetch()
    await held
    await route.fulfill({ response })
  })
  const shortcut = 'ControlOrMeta+Shift+K'
  await page.keyboard.press(shortcut)
  const command = page.getByRole('dialog', { name: 'Command center', exact: true })
  await command.getByRole('button', { name: 'Message', exact: true }).click()
  await command.getByLabel('Recipients').fill('helper@local')
  const body = `One send across close ${Date.now()}.`
  await command.getByLabel('Message body').fill(body)
  await command.getByRole('button', { name: 'Send message' }).click()
  await expect(command.getByRole('button', { name: 'Sending…' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await page.keyboard.press(shortcut)
  await command.getByRole('button', { name: 'Message', exact: true }).click()
  await expect(command.getByLabel('Message body')).toHaveValue(body)
  await expect(command.getByRole('button', { name: 'Sending…' })).toBeDisabled()
  release()
  await expect(command.getByRole('status')).toHaveText('Sent to: helper@local')
  expect(posts).toBe(1)
  await page.keyboard.press('Escape')
  await boardOpenButton(page).click()
  await expect(boardDialog(page).getByText(body, { exact: true })).toHaveCount(1, { timeout: 60_000 })
})

test('offers summary expansion only when four rendered lines clip the text', async ({ page }) => {
  const now = new Date().toISOString()
  const base = { host: 'local', team: 'demo', pane_id: null, source: 'self_report', state: 'idle',
    waiting_for: null, status_since: null, task_observed_at: null, last_message_at: null }
  const long = Array.from({ length: 12 }, (_, i) => `Report line ${i}`).join('\n')
  const adaptive = 'Adaptive text '.repeat(12)
  await page.route('**/api/board/status', route => route.fulfill({ json: { identities: {
    'short@local': { ...base, name: 'short', self_report: { updated_at: now, summary: 'Ready' } },
    'long@local': { ...base, name: 'long', self_report: { updated_at: now, summary: long } },
    'adaptive@local': { ...base, name: 'adaptive', self_report: { updated_at: now, summary: adaptive } },
  }, statuses: {}, participation: {} } }))
  await page.setViewportSize({ width: 600, height: 860 })
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()
  const panel = boardDialog(page)
  const card = (name: string) => panel.getByRole('article').filter({ hasText: `${name}@local` })
  await expect(card('short').getByRole('button', { name: 'Show more' })).toHaveCount(0)
  await expect(card('adaptive').getByRole('button', { name: 'Show more' })).toHaveCount(0)
  const report = card('long').getByText(/^Report line 0/)
  const collapsed = (await report.boundingBox())!.height
  await card('long').getByRole('button', { name: 'Show more' }).click()
  expect((await report.boundingBox())!.height).toBeGreaterThan(collapsed)
  await card('long').getByRole('button', { name: 'Show less' }).click()
  await expect(card('long').getByRole('button', { name: 'Show more' })).toBeVisible()
  await page.setViewportSize({ width: 300, height: 860 })
  await expect(card('adaptive').getByRole('button', { name: 'Show more' })).toHaveCount(1)
  await page.setViewportSize({ width: 600, height: 860 })
  await expect(card('adaptive').getByRole('button', { name: 'Show more' })).toHaveCount(0)
})
