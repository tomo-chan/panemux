import { expect, type Page } from '@playwright/test'

// Shared helpers for the two Agent Board dashboard specs. Deliberately a
// plain module rather than exports on one of the specs: importing a spec
// file from another spec file would re-register its tests under the
// importing file.

export interface SessionTokenResponse {
  token: string
  command_center_enabled: boolean
  agent_board_enabled: boolean
}

// gotoAndReadSessionToken loads the dashboard and returns the
// /api/session-token payload the frontend itself used to decide whether the
// Agent Board UI is available at all. Reading the real response (rather than
// only asserting on rendered output) is what keeps an "is not rendered"
// assertion from being vacuous: it proves the gating fetch already resolved
// before anything was asserted about its absence.
export async function gotoAndReadSessionToken(page: Page): Promise<SessionTokenResponse> {
  const response = page.waitForResponse(
    (candidate) => new URL(candidate.url()).pathname === '/api/session-token',
  )
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()
  return (await (await response).json()) as SessionTokenResponse
}

export function boardDialog(page: Page) {
  return page.getByRole('dialog', { name: 'Agent board' })
}

export function boardOpenButton(page: Page) {
  return page.getByRole('button', { name: 'Open agent board' })
}

// focusTerminal clicks into the xterm surface and confirms the terminal's
// own hidden textarea really took focus, so a later focus assertion cannot
// pass by accident against a pane that never got focus at all.
export async function focusTerminal(page: Page) {
  await page.locator('.xterm-screen').first().click()
  expect(await activeElementDescription(page)).toBe('textarea.xterm-helper-textarea')
}

export async function activeElementDescription(page: Page): Promise<string> {
  return page.evaluate(() => {
    const element = document.activeElement
    if (!element) return 'none'
    const tag = element.tagName.toLowerCase()
    if (element.classList.length > 0) return `${tag}.${element.classList[0]}`
    const label = element.getAttribute('aria-label')
    return label ? `${tag}[aria-label="${label}"]` : tag
  })
}
