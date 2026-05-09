import { expect, test } from '@playwright/test'

test('styles the xterm viewport scrollbar to match the terminal chrome', async ({ page }) => {
  await page.goto('/')

  const viewport = page.locator('.xterm-viewport').first()
  await expect(viewport).toBeVisible()

  // This comes from xterm itself, so keep it as a sanity check that the terminal
  // viewport is initialized before asserting the theme rules added by this change.
  const overflowY = await viewport.evaluate((el) => {
    const element = el as HTMLElement
    const style = window.getComputedStyle(element)
    return style.overflowY
  })

  const matchingRules = await page.evaluate(() => {
    const matches: Array<{
      selectorText: string
      width: string
      height: string
      background: string
      border: string
      borderRadius: string
      scrollbarWidth: string
      scrollbarColor: string
    }> = []

    for (const sheet of Array.from(document.styleSheets)) {
      let rules: CSSRuleList
      try {
        rules = sheet.cssRules
      } catch {
        continue
      }

      for (const rule of Array.from(rules)) {
        if (!(rule instanceof CSSStyleRule)) continue
        if (rule.selectorText?.includes('xterm-viewport')) {
          matches.push({
            selectorText: rule.selectorText,
            width: rule.style.width,
            height: rule.style.height,
            background: rule.style.background,
            border: rule.style.border,
            borderRadius: rule.style.borderRadius,
            scrollbarWidth: rule.style.getPropertyValue('scrollbar-width'),
            scrollbarColor: rule.style.getPropertyValue('scrollbar-color'),
          })
        }
      }
    }

    return matches
  })

  expect(overflowY).toBe('scroll')
  expect(matchingRules).toEqual(
    expect.arrayContaining([
      expect.objectContaining({
        selectorText: '.xterm .xterm-viewport',
        scrollbarWidth: 'thin',
        scrollbarColor: 'rgb(63, 63, 70) rgb(26, 27, 30)',
      }),
      expect.objectContaining({
        selectorText: '.xterm .xterm-viewport::-webkit-scrollbar',
        width: '8px',
        height: '8px',
      }),
      expect.objectContaining({
        selectorText: '.xterm .xterm-viewport::-webkit-scrollbar-track',
        background: 'rgb(26, 27, 30)',
      }),
      expect.objectContaining({
        selectorText: '.xterm .xterm-viewport::-webkit-scrollbar-thumb',
        background: 'rgb(63, 63, 70)',
        border: '2px solid rgb(26, 27, 30)',
        borderRadius: '999px',
      }),
    ]),
  )

  // Pseudo-element hover state is not asserted here because Playwright cannot
  // reliably force hover on the scrollbar thumb across engines.
})
