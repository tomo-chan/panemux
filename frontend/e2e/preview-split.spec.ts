import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { expect, test, type Locator, type Page } from '@playwright/test'

// The split preview (issue #385) in a real browser: what jsdom cannot lay
// out or a mocked terminal cannot end. Each pane is half the window
// (preview.yml).

const SIDE_MIN_PX = 160

test.describe.configure({ mode: 'serial' })

test('keeps both sides at least 160 px wide in a half-width pane', async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 700 })
  const pane = await openPreview(page, 'preview-left', 'pm385-layout')
  const divider = pane.getByRole('separator', { name: 'Resize terminal and preview' })

  // 80:20 would leave the preview less than 160 px of a 450 px pane, so the
  // divider stops where both sides fit, and says so.
  await divider.focus()
  for (let i = 0; i < 8; i++) await page.keyboard.press('ArrowRight')
  await expectBothSidesFit(pane)
  await expectAnnouncedShare(pane, divider)
  const atEnd = await terminalWidth(pane)

  // The first press back moves the terminal's edge.
  await page.keyboard.press('ArrowLeft')
  await expect.poll(() => terminalWidth(pane)).toBeLessThan(atEnd - 1)
  await expectAnnouncedShare(pane, divider)

  for (let i = 0; i < 12; i++) await page.keyboard.press('ArrowLeft')
  await expectBothSidesFit(pane)
  await expectAnnouncedShare(pane, divider)
  const atStart = await terminalWidth(pane)
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => terminalWidth(pane)).toBeGreaterThan(atStart + 1)

  await page.keyboard.press('ArrowRight')
  await divider.dblclick()
  await expect(divider).toHaveAttribute('aria-valuenow', '50')
  await pane.getByRole('button', { name: 'Close preview' }).click()
  await expect(pane.locator('.pm-preview')).toHaveCount(0)
})

test('hides the preview when the shell in the pane exits', async ({ page }) => {
  const pane = await openPreview(page, 'preview-right', 'pm385-exit')

  await writeToSession(page, 'preview-right', 'exit\n')

  await expect(pane.locator('.pm-preview')).toHaveCount(0)
  await expect(pane.locator('.pm-preview-terminal')).toBeVisible()
})

// Issue #386: a localhost URL on the panemux host — here a server this test
// starts, as a development server would be — opens beside the terminal from
// the browser shim, an OSC 8 link and a printed link alike. Its page runs its
// scripts in its own origin, which is what a development server's modules and
// hot reload need.
test.describe('a localhost page', () => {
  let server: Server
  let origin: string

  test.beforeAll(async () => {
    server = createServer((_req, res) => {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end('<p id="o">static</p><script>document.getElementById("o").textContent = "served " + location.origin</script>')
    })
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
    origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
  })

  test.afterAll(async () => {
    await new Promise<void>((resolve) => server.close(() => resolve()))
  })

  test('previews a page the browser shim asks to open, after Preview', async ({ page }) => {
    const pane = await openPane(page, 'preview-left')

    await writeToSession(page, 'preview-left', `"$BROWSER" ${origin}/\n`)
    const strip = pane.locator('[data-pane-url-request="true"]')
    await expect(strip.getByRole('button', { name: 'Open' })).toBeVisible()
    await expect(strip.getByRole('button', { name: 'Ignore' })).toBeVisible()
    await strip.getByRole('button', { name: 'Preview' }).click()

    await expect(strip).toHaveCount(0)
    await expectPage(pane, origin)
    await pane.getByRole('button', { name: 'Close preview' }).click()
  })

  test('asks before previewing an OSC 8 link, which opens once', async ({ page }) => {
    const pane = await openPane(page, 'preview-left')

    await writeToSession(page, 'preview-left', `clear && printf '\\033]8;;${origin}/\\033\\\\pm386-osc8\\033]8;;\\033\\\\\\n'\n`)
    await clickRow(page, pane, /^pm386-osc8\s*$/)
    const strip = pane.locator('[data-pane-url-request="true"]')
    await expect(strip).toContainText('This link goes to')
    await strip.getByRole('button', { name: 'Preview' }).click()

    await expectPage(pane, origin)
    await expect(strip).toHaveCount(0)
    await pane.getByRole('button', { name: 'Close preview' }).click()
  })

  test('previews a printed localhost link when it is clicked', async ({ page }) => {
    const pane = await openPane(page, 'preview-left')

    await writeToSession(page, 'preview-left', `clear && echo ${origin}/\n`)
    await clickRow(page, pane, new RegExp(`^${origin.replace(/[.]/g, '\\.')}/\\s*$`))

    await expectPage(pane, origin)
    await expect(pane.locator('[data-pane-url-request="true"]')).toHaveCount(0)
    await pane.getByRole('button', { name: 'Close preview' }).click()
  })
})

async function openPane(page: Page, paneId: string): Promise<Locator> {
  await page.goto('/')
  const pane = page.locator(`[data-pane-id="${paneId}"]`)
  await expect.poll(async () => (await pane.locator('.xterm-rows').textContent()) ?? '').toMatch(/\S/)
  return pane
}

async function clickRow(page: Page, pane: Locator, text: RegExp) {
  const row = pane.locator('.xterm-rows > div').filter({ hasText: text })
  await expect(row).toHaveCount(1)
  const box = (await row.boundingBox())!
  await page.mouse.move(box.x + 4, box.y + box.height / 2)
  await page.mouse.click(box.x + 4, box.y + box.height / 2)
}

async function expectPage(pane: Locator, origin: string) {
  await expect(pane.locator('.pm-preview')).toBeVisible()
  await expect(pane.locator('.pm-preview-target')).toContainText(`local · ${origin}/`)
  await expect(pane.getByText(/refuses to be shown in a frame/)).toBeVisible()
  await expect(pane.frameLocator('.pm-preview-frame').locator('#o')).toHaveText(`served ${origin}`)
}

async function terminalWidth(pane: Locator): Promise<number> {
  return (await pane.locator('.pm-preview-terminal').boundingBox())!.width
}

// The ratio the divider announces is the share the terminal is drawn at.
async function expectAnnouncedShare(pane: Locator, divider: Locator) {
  const split = (await pane.locator('.pm-preview-split').boundingBox())!
  const share = ((await terminalWidth(pane)) / split.width) * 100
  const announced = Number(await divider.getAttribute('aria-valuenow'))
  expect(Math.abs(announced - share)).toBeLessThanOrEqual(1)
}

async function expectBothSidesFit(pane: Locator) {
  const split = (await pane.locator('.pm-preview-split').boundingBox())!
  const terminal = (await pane.locator('.pm-preview-terminal').boundingBox())!
  const preview = (await pane.locator('.pm-preview').boundingBox())!
  const close = (await pane.getByRole('button', { name: 'Close preview' }).boundingBox())!

  expect(terminal.width).toBeGreaterThanOrEqual(SIDE_MIN_PX - 0.5)
  expect(preview.width).toBeGreaterThanOrEqual(SIDE_MIN_PX - 0.5)
  expect(terminal.x).toBeGreaterThanOrEqual(split.x - 0.5)
  expect(preview.x + preview.width).toBeLessThanOrEqual(split.x + split.width + 0.5)
  expect(close.x + close.width).toBeLessThanOrEqual(split.x + split.width + 0.5)
}

// Writes a page into a fresh directory, cds the pane's shell there, prints
// the page's name alone on a line and clicks it, as an operator would.
async function openPreview(page: Page, paneId: string, marker: string): Promise<Locator> {
  await page.goto('/')
  const pane = page.locator(`[data-pane-id="${paneId}"]`)
  await expect.poll(async () => (await pane.locator('.xterm-rows').textContent()) ?? '').toMatch(/\S/)

  const name = `${marker}.html`
  await writeToSession(
    page,
    paneId,
    `cd "$(mktemp -d "\${TMPDIR:-/tmp}/panemux-e2e-preview.XXXXXX")" && ` +
      `printf '<p>${marker}</p>' > ${name} && clear && echo ${name}\n`,
  )
  const row = pane.locator('.xterm-rows > div').filter({ hasText: new RegExp(`^${marker}\\.html\\s*$`) })
  await expect(row).toHaveCount(1)
  const box = (await row.boundingBox())!
  // The link's first cells; hovering lets xterm find the link before the click.
  await page.mouse.move(box.x + 4, box.y + box.height / 2)
  await page.mouse.click(box.x + 4, box.y + box.height / 2)

  await expect(pane.locator('.pm-preview')).toBeVisible()
  await expect(pane.frameLocator('.pm-preview-frame').locator('p')).toHaveText(marker)
  return pane
}

async function writeToSession(page: Page, sessionID: string, data: string) {
  await page.evaluate(
    async ({ sessionID, data }) => {
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:'
      await new Promise<void>((resolve, reject) => {
        const ws = new WebSocket(`${protocol}//${location.host}/ws/${sessionID}`)
        ws.binaryType = 'arraybuffer'
        ws.onerror = () => reject(new Error(`failed to open websocket for ${sessionID}`))
        ws.onopen = () => {
          ws.send(new TextEncoder().encode(data))
          setTimeout(() => {
            ws.close()
            resolve()
          }, 150)
        }
      })
    },
    { sessionID, data },
  )
}
