import { expect, test, type Page } from '@playwright/test'

test('renders terminal output when switching to a previously hidden workspace', async ({ page }) => {
  await page.goto('/')

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

async function visibleTerminalText(page: Page) {
  const text = await page.locator('.xterm-rows').first().textContent()
  return text ?? ''
}
