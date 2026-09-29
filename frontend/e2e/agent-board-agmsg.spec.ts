import { expect, test } from '@playwright/test'
import { boardDialog, boardOpenButton, gotoAndReadSessionToken } from './agent-board-helpers'

// Runs against the agent-board-agmsg.yml fixture (playwright.config.ts's
// chromium-agent-board-agmsg project), whose agent_board.agmsg_path points
// at the stub agmsg installation run-panemux-agent-board-e2e.sh seeds before
// starting the server.
//
// Only agmsg itself is stubbed, and only because it is a separate,
// operator-installed tool panemux deliberately never installs (see
// docs/security.md); the two shell scripts under e2e/fixtures/agmsg stand in
// for the ones a real installation provides. Everything the dashboard
// actually depends on is the real implementation: the relay really execs
// those scripts, parses their JSONL, validates each row's from/to against
// the configured panes, writes the real BoardCache, and serves it over the
// real bearer-token-gated /api/board endpoints. No response is intercepted
// or faked in the browser.

test('renders the pane status and message the relay read from the agmsg installation', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()

  const panel = boardDialog(page)
  await expect(panel).toBeVisible()

  // Pane status card, from the board_status self-report row: the pane is on
  // the board, and this is what it is doing.
  await expect(panel.getByText('board-main', { exact: true })).toBeVisible()
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

  // The fixture's second pane is board-enabled but never sends a status
  // row, so it is listed as not joined rather than omitted — the whole
  // point of listing configured panes, not just reporting ones.
  await expect(panel.getByText('board-worker', { exact: true })).toBeVisible()
  await expect(panel.getByText('not joined')).toHaveCount(1)

  // The same fixture row also carries repo, branch, cwd and pr_url, which
  // the card deliberately does not render — panemux computes those itself
  // for the pane header, and a self-reported copy can contradict it. The
  // card having no <a> at all is the same decision seen from the security
  // side (docs/security.md's Agent-reported values in the dashboard UI).
  await expect(panel.getByText('example/project')).toHaveCount(0)
  await expect(panel.getByText('feature/agent-board')).toHaveCount(0)
  await expect(panel.getByText('/workspace/user/project')).toHaveCount(0)
  await expect(panel.getByRole('link')).toHaveCount(0)

  // Ordinary cross-pane message row.
  await expect(panel.getByText('board-main → board-worker')).toBeVisible()
  await expect(panel.getByText('Handing the dashboard panel over for review.')).toBeVisible()

  // The status self-report is stored in agmsg history like any other
  // message, so the message feed has to filter it out itself — raw
  // board_status JSON must never show up as if someone had sent it.
  await expect(panel).not.toContainText('board_status')
  await expect(panel).not.toContainText('board-main → _system')
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
