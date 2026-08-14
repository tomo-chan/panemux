import { expect, test, type Request } from '@playwright/test'
import {
  activeElementDescription,
  boardDialog,
  boardOpenButton,
  focusTerminal,
  gotoAndReadSessionToken,
} from './agent-board-helpers'

// Runs against the agent-board.yml fixture (playwright.config.ts's
// chromium-agent-board project): one pane with agent_board.enabled: true and
// an agmsg_path that deliberately points at a directory containing no agmsg
// installation. That is the realistic first-run state — panemux never
// installs agmsg itself (see docs/security.md) — so the board cache stays
// empty and the dashboard renders its empty states, against a real server.
//
// The populated counterpart is agent-board-agmsg.spec.ts.

test('exposes the Agent Board entry point when a pane enables agent_board', async ({ page }) => {
  const sessionToken = await gotoAndReadSessionToken(page)

  expect(sessionToken.agent_board_enabled).toBe(true)
  expect(sessionToken.token).not.toBe('')
  await expect(boardOpenButton(page)).toBeVisible()
})

test('opens the dashboard with the keyboard shortcut while a terminal pane holds focus', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await focusTerminal(page)

  // The whole reason App.tsx registers this shortcut on the capture phase:
  // xterm stops keydown propagation, so a bubble-phase handler would never
  // see it while the terminal has focus.
  await page.keyboard.press('ControlOrMeta+Shift+B')

  await expect(boardDialog(page)).toBeVisible()
  // Opening the panel deliberately does not steal focus from the terminal.
  expect(await activeElementDescription(page)).toBe('textarea.xterm-helper-textarea')

  // The same shortcut toggles the panel back closed.
  await page.keyboard.press('ControlOrMeta+Shift+B')
  await expect(boardDialog(page)).toBeHidden()
})

test('renders the empty states when no agmsg installation exists', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()

  const panel = boardDialog(page)
  await expect(panel.getByText('No pane has reported status yet.')).toBeVisible()
  await expect(panel.getByText('No messages yet.')).toBeVisible()
  // A missing agmsg installation is not an API failure: /api/board/status
  // and /api/board/messages both answer 200 from an empty cache, so no
  // error banner belongs in this state.
  await expect(panel).not.toContainText('Failed to load board status')
  await expect(panel).not.toContainText('Not authorized to view the agent board.')
})

test('polls the board API with a bearer token only while the dashboard is open', async ({ page }) => {
  const boardRequests: Request[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/board/')) boardRequests.push(request)
  })

  const sessionToken = await gotoAndReadSessionToken(page)
  await expect(boardOpenButton(page)).toBeVisible()
  expect(boardRequests.map(requestTarget)).toEqual([])

  await boardOpenButton(page).click()
  await expect(boardDialog(page)).toBeVisible()

  await expect
    .poll(() => boardRequests.map(requestTarget), {
      message: 'opening the dashboard should poll both board endpoints',
    })
    .toEqual(expect.arrayContaining(['/api/board/status', '/api/board/messages?since=0']))

  for (const request of boardRequests) {
    const headers = await request.allHeaders()
    expect(headers.authorization, `${requestTarget(request)} must be authenticated`).toBe(
      `Bearer ${sessionToken.token}`,
    )
  }
})

test('closes on Escape and leaves focus on the terminal it was opened from', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await focusTerminal(page)
  await page.keyboard.press('ControlOrMeta+Shift+B')
  await expect(boardDialog(page)).toBeVisible()

  await page.keyboard.press('Escape')

  await expect(boardDialog(page)).toBeHidden()
  expect(await activeElementDescription(page)).toBe('textarea.xterm-helper-textarea')
})

test('closes when the backdrop is clicked and restores focus to the opener', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await boardOpenButton(page).click()
  await expect(boardDialog(page)).toBeVisible()

  // The overlay fills the viewport and the panel is docked against its right
  // edge, so the far left of the viewport is backdrop.
  await page.mouse.click(20, 200)

  await expect(boardDialog(page)).toBeHidden()
  expect(await activeElementDescription(page)).toBe('button[aria-label="Open agent board"]')
})

test('restores focus to the terminal after the panel itself takes focus', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await focusTerminal(page)
  await page.keyboard.press('ControlOrMeta+Shift+B')
  await expect(boardDialog(page)).toBeVisible()

  // The close button is inside the panel, so this is the one dismissal path
  // where focus provably moves into the overlay first and the element
  // holding it is then removed from the DOM — exactly what
  // useRestoreFocusOnClose exists for.
  const closeButton = page.getByRole('button', { name: 'Close agent board panel' })
  await closeButton.focus()
  expect(await activeElementDescription(page)).toBe('button[aria-label="Close agent board panel"]')
  await closeButton.click()

  await expect(boardDialog(page)).toBeHidden()
  expect(await activeElementDescription(page)).toBe('textarea.xterm-helper-textarea')
})

function requestTarget(request: Request): string {
  const url = new URL(request.url())
  return `${url.pathname}${url.search}`
}
