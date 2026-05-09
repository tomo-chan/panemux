import { expect, test, type Page } from '@playwright/test'

test('renders terminal output when switching to a previously hidden workspace', async ({ page }) => {
  await page.goto('/')

  await page.getByRole('tab', { name: 'Default' }).click()
  await expect(page.getByRole('tab', { name: 'Default' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(async () => await visibleTerminalText(page), {
    message: 'default workspace terminal should render an initial prompt',
  }).toMatch(/\S/)

  await page.getByRole('tab', { name: 'Workspace 2' }).click()

  await expect(page.getByRole('tab', { name: 'Workspace 2' })).toHaveAttribute('aria-selected', 'true')
  await expect.poll(async () => await visibleTerminalText(page), {
    message: 'workspace switch should not leave the terminal blank',
  }).toMatch(/\S/)
})

test('flags an inactive workspace and shows a browser notification for hidden-pane attention', async ({ page }) => {
  await page.addInitScript(() => {
    class MockNotification {
      static permission = 'granted'
      static requestPermission = () => Promise.resolve('granted' as NotificationPermission)

      constructor(title: string, options?: NotificationOptions) {
        ;(window as Window & { __panemuxNotifications?: Array<{ title: string; body: string }> }).__panemuxNotifications ??= []
        window.__panemuxNotifications.push({ title, body: options?.body ?? '' })
      }
    }

    Object.defineProperty(window, 'Notification', {
      configurable: true,
      writable: true,
      value: MockNotification,
    })
  })

  await page.goto('/')
  await page.getByRole('tab', { name: 'Default' }).click()
  await expect(page.getByRole('tab', { name: 'Default' })).toHaveAttribute('aria-selected', 'true')

  await writeCommandToSession(
    page,
    'workspace-2-main',
    "printf 'Codex needs confirmation before continuing. Approve?\\r\\n'",
  )

  await expect(page.getByRole('tab', { name: 'Workspace 2' })).toHaveAttribute('data-attention', 'true')
  await expect
    .poll(async () => {
      return page.evaluate(() => (window as Window & { __panemuxNotifications?: Array<{ title: string; body: string }> }).__panemuxNotifications ?? [])
    })
    .toContainEqual({
      title: 'Agent confirmation requested',
      body: 'Workspace 2 Shell in Workspace 2',
    })
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
