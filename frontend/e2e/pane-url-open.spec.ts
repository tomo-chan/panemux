import { expect, test, type Page } from '@playwright/test'

// The browser shim's OSC sequence is the one part of URL opening that no unit
// test can prove on its own: it has to survive a real PTY, the WebSocket, and
// xterm's own parser before the pane can ask about it.
test('asks before opening a URL a program in the pane handed to panemux', async ({ page }) => {
  await page.goto('/')
  await page.getByRole('tab', { name: 'Default' }).click()
  await expect.poll(async () => await visibleTerminalText(page), {
    message: 'pane should render a prompt before the shim is invoked',
  }).toMatch(/\S/)

  // Runs the shim panemux installed and exported as $BROWSER for this pane,
  // exactly as `xdg-open`-style browser opens inside the pane would.
  await writeCommandToSession(page, 'default-main', '"$BROWSER" https://example.com/device-code')

  const strip = page.locator('[data-pane-url-request="true"]')
  await expect(strip).toBeVisible()
  await expect(strip.getByTitle('https://example.com/device-code')).toBeVisible()
  await expect(strip.getByRole('button', { name: 'Open' })).toBeVisible()

  // The sequence itself must never be drawn into the terminal.
  expect(await visibleTerminalText(page)).not.toContain('panemux-open;')

  await strip.getByRole('button', { name: 'Ignore' }).click()
  await expect(strip).toHaveCount(0)
})

async function visibleTerminalText(page: Page) {
  const text = await page.locator('.xterm-rows').first().textContent()
  return text ?? ''
}

async function writeCommandToSession(page: Page, sessionID: string, command: string) {
  await page.evaluate(
    async ({ sessionID, command }) => {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
      const url = `${protocol}//${location.host}/ws/${sessionID}`

      await new Promise<void>((resolve, reject) => {
        const ws = new WebSocket(url)
        let connected = false

        ws.binaryType = 'arraybuffer'
        ws.onerror = () => reject(new Error(`failed to open websocket for ${sessionID}`))
        ws.onopen = () => {
          connected = true
          ws.send(new TextEncoder().encode(`${command}\n`))
          setTimeout(() => {
            ws.close()
            resolve()
          }, 150)
        }
        ws.onclose = () => {
          if (!connected) {
            reject(new Error(`websocket closed before opening for ${sessionID}`))
          }
        }
      })
    },
    { sessionID, command },
  )
}
