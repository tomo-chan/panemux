import { expect, test } from '@playwright/test'
import { boardDialog, boardOpenButton, focusTerminal, gotoAndReadSessionToken } from './agent-board-helpers'

// Runs against the default workspace-switch.yml fixture, which has no pane
// with agent_board.enabled — the negative half of the gate
// GET /api/session-token's agent_board_enabled flag drives. The entry point
// must be absent entirely, not merely inert.

test('hides the Agent Board entry point when no pane enables agent_board', async ({ page }) => {
  const sessionToken = await gotoAndReadSessionToken(page)

  expect(sessionToken.agent_board_enabled).toBe(false)
  await expect(boardOpenButton(page)).toHaveCount(0)
})

test('ignores the Agent Board shortcut when no pane enables agent_board', async ({ page }) => {
  const boardRequestTargets: string[] = []
  page.on('request', (request) => {
    const { pathname } = new URL(request.url())
    if (pathname.startsWith('/api/board/')) boardRequestTargets.push(pathname)
  })

  const sessionToken = await gotoAndReadSessionToken(page)
  expect(sessionToken.agent_board_enabled).toBe(false)

  await focusTerminal(page)
  await page.keyboard.press('ControlOrMeta+Shift+B')

  await expect(boardDialog(page)).toHaveCount(0)
  expect(boardRequestTargets).toEqual([])
})
