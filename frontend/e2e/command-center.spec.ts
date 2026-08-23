import { expect, test, type Page } from '@playwright/test'
import { activeElementDescription, focusTerminal, gotoAndReadSessionToken } from './agent-board-helpers'

// Runs against the command-center.yml fixture (playwright.config.ts's
// chromium-command-center project), whose server is started by
// run-panemux-command-center-e2e.sh with a stub `claude` first on PATH.
//
// Only the `claude` binary is stubbed. Everything between the browser and
// it is the real implementation: the palette's own WebSocket, the
// subprotocol bearer-token handshake BoardCommandHandler performs, the
// Runner's subprocess spawn and stream-json parsing, the history file it
// writes, and the authenticated GET /api/board/command/history the panel
// reads back. No response is intercepted or faked in the browser.
//
// The exact argv panemux hands the CLI is not asserted here — that is
// pinned in internal/commandcenter/runner_test.go against the real
// binary's documented parsing, which a stub cannot reproduce.

function palette(page: Page) {
  return page.getByRole('dialog', { name: 'Command center' })
}

function historyPanel(page: Page) {
  return page.getByRole('dialog', { name: 'Command center history' })
}

// A per-run marker, so assertions cannot pass against a turn left behind by
// an earlier run in the same history file.
function uniquePrompt(label: string): string {
  return `${label} ${Date.now()}`
}

async function openPalette(page: Page) {
  await page.keyboard.press('ControlOrMeta+Shift+K')
  await expect(palette(page)).toBeVisible()
  // The palette disables its own submit button until the WebSocket is open,
  // so waiting on it is waiting on a completed authenticated handshake.
  await expect(page.getByRole('button', { name: 'Send' })).toBeEnabled()
}

async function ask(page: Page, prompt: string) {
  await page.getByLabel('Command center prompt').fill(prompt)
  await page.getByRole('button', { name: 'Send' }).click()
}

test('opens the palette with the keyboard shortcut while a terminal pane holds focus', async ({ page }) => {
  const token = await gotoAndReadSessionToken(page)
  expect(token.command_center_enabled).toBe(true)

  await focusTerminal(page)
  // The reason App.tsx registers this shortcut on the capture phase: xterm
  // stops keydown propagation, so a bubble-phase handler never sees it.
  await page.keyboard.press('ControlOrMeta+Shift+K')

  await expect(palette(page)).toBeVisible()
  // Unlike the board dashboard, the palette does take focus — it exists to
  // be typed into immediately.
  expect(await activeElementDescription(page)).toBe('input[aria-label="Command center prompt"]')
})

test('streams a real subprocess answer back into the turn', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await openPalette(page)

  const prompt = uniquePrompt('which panes are blocked?')
  await ask(page, prompt)

  const transcript = page.getByTestId('command-palette-transcript')
  // The prompt echo, then the two things summarizeStreamLines keeps from
  // the stream: assistant text and a named tool call. The stub's init,
  // stream_event and user frames must not appear as lines of their own.
  await expect(transcript.getByText(`> ${prompt}`)).toBeVisible()
  await expect(transcript.getByText(`Checking the board for: ${prompt}`)).toBeVisible()
  await expect(transcript.getByText('→ board_status').last()).toBeVisible()
  // .last(): the palette also replays recent history above the live turn,
  // so this constant answer text legitimately appears once per earlier
  // query in the same server's history file. The prompt echo above is what
  // identifies this turn; this asserts the newest answer rendered.
  await expect(transcript.getByText('Two panes are on the board.').last()).toBeVisible()
  await expect(transcript).not.toContainText('stream_event')
  await expect(transcript).not.toContainText('subtype')
})

test('shows a turn as still in flight until its subprocess exits', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await openPalette(page)

  // The stub delays before its final frames when the prompt says so, which
  // is the only way to observe the mid-stream state deterministically.
  const prompt = uniquePrompt('answer slowly please')
  await ask(page, prompt)

  const transcript = page.getByTestId('command-palette-transcript')
  await expect(transcript.getByText(`Checking the board for: ${prompt}`)).toBeVisible()
  // The pending marker the palette renders while a turn is not done.
  await expect(transcript.getByText('…', { exact: true })).toBeVisible()

  await expect(transcript.getByText('Two panes are on the board.').last()).toBeVisible()
  await expect(transcript.getByText('…', { exact: true })).toBeHidden()
})

test('keeps the answered turn in the history panel after the palette closes', async ({ page }) => {
  await gotoAndReadSessionToken(page)
  await openPalette(page)

  const prompt = uniquePrompt('what is on the board?')
  await ask(page, prompt)
  await expect(
    page.getByTestId('command-palette-transcript').getByText('Two panes are on the board.').last(),
  ).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(palette(page)).toBeHidden()

  // A separate authenticated fetch of the history the Runner persisted to
  // disk — not the palette's own in-memory turns.
  await page.getByRole('button', { name: 'Open command center history' }).click()
  const panel = historyPanel(page)
  await expect(panel).toBeVisible()
  await expect(panel.getByText(`> ${prompt}`)).toBeVisible()
  await expect(panel.getByText('Two panes are on the board.').first()).toBeVisible()
  await expect(panel).not.toContainText('No history yet.')
})

test('refuses a command WebSocket that presents the wrong token', async ({ page }) => {
  await gotoAndReadSessionToken(page)

  // The palette cannot express this: it always dials with the token the
  // page fetched. Dialing directly is what proves the subprotocol check is
  // load-bearing rather than decorative.
  const outcome = await page.evaluate(
    () =>
      new Promise<string>((resolve) => {
        const ws = new WebSocket(`ws://${location.host}/ws/board-command`, ['not-the-real-token'])
        ws.onopen = () => {
          ws.close()
          resolve('open')
        }
        ws.onerror = () => resolve('error')
        ws.onclose = () => resolve('closed')
        setTimeout(() => resolve('timeout'), 5000)
      }),
  )

  expect(outcome).not.toBe('open')
  expect(outcome).not.toBe('timeout')
})

test('accepts the command WebSocket that presents the configured token', async ({ page }) => {
  const { token } = await gotoAndReadSessionToken(page)

  // The positive half of the check above: same dial, real token. Without
  // it, "not open" would also pass against a route that rejected everyone.
  const outcome = await page.evaluate(
    (realToken) =>
      new Promise<string>((resolve) => {
        const ws = new WebSocket(`ws://${location.host}/ws/board-command`, [realToken])
        ws.onopen = () => {
          ws.close()
          resolve('open')
        }
        ws.onerror = () => resolve('error')
        ws.onclose = () => resolve('closed')
        setTimeout(() => resolve('timeout'), 5000)
      }),
    token,
  )

  expect(outcome).toBe('open')
})
