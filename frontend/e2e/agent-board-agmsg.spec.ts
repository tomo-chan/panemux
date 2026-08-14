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

  // Pane status card, from the board_status self-report row.
  await expect(panel.getByText('board-main', { exact: true })).toBeVisible()
  await expect(panel.getByText('working', { exact: true })).toBeVisible()
  await expect(panel.getByText('example/project')).toBeVisible()
  await expect(panel.getByText('feature/agent-board')).toBeVisible()
  await expect(panel.getByText('Wiring the dashboard panel')).toBeVisible()
  await expect(panel.getByText('tool: Edit')).toBeVisible()
  await expect(panel.getByText('/workspace/user/project')).toBeVisible()

  const prLink = panel.getByRole('link', { name: 'PR #42' })
  await expect(prLink).toHaveAttribute('href', 'https://github.com/example/project/pull/42')

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
