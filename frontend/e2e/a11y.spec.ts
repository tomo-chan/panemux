import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page, type TestInfo } from '@playwright/test'

// Interaction capability is the other ISO 25010 characteristic
// docs/quality-gateway.md records as thinly protected: three focus-restoration
// E2E tests and no accessibility checks at all. This is the first
// accessibility measurement.
//
// **It measures; it does not gate.** Roadmap item 7 of issue #180 says so
// explicitly, and the reason is design principle 4. Turning axe on over an
// existing UI produces a backlog, and a gate that starts red is a gate people
// route around on day one — taking the gates that do work with it. So every
// violation is recorded, in the run's own log and as an attachment, and the
// only assertion is that the scan actually ran.
//
// What "later" looks like: once a few runs' worth of counts exist, freeze the
// current count as a ceiling that may fall but not rise. That is a threshold
// chosen from data, which is what the roadmap asks for.

const IMPACTS = ['critical', 'serious', 'moderate', 'minor'] as const

async function scan(page: Page, testInfo: TestInfo, label: string) {
  const results = await new AxeBuilder({ page })
    // The terminal is a canvas xterm.js owns and draws into. Auditing it here
    // would report on xterm's markup rather than panemux's, every run, with
    // nothing this repository can act on. Its accessibility is xterm's own
    // concern and its screen-reader mode is a separate feature.
    .exclude('.xterm')
    .analyze()

  const counts = Object.fromEntries(
    IMPACTS.map((impact) => [impact, results.violations.filter((v) => v.impact === impact).length]),
  )

  const summary = results.violations
    .map((v) => `${v.impact ?? 'unknown'}\t${v.id}\t${v.nodes.length} node(s)\t${v.help}`)
    .sort()
    .join('\n')

  // Both outputs matter: the log line is what a CI run shows without anyone
  // downloading anything, and the attachment is what someone actually fixing
  // these needs.
  console.log(
    `a11y[${label}] total=${results.violations.length} ` +
      IMPACTS.map((i) => `${i}=${counts[i]}`).join(' '),
  )
  await testInfo.attach(`axe-${label}.txt`, {
    body: summary || 'no violations',
    contentType: 'text/plain',
  })

  return results
}

test('records the accessibility violations on the dashboard', async ({ page }, testInfo) => {
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()

  const results = await scan(page, testInfo, 'dashboard')

  // The scan ran and looked at something. Asserting on the violation count is
  // deliberately NOT done here — see the note at the top of this file.
  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
})

test('records the accessibility violations with the pane settings dialog open', async ({ page }, testInfo) => {
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()

  await page.getByTitle('Pane settings').first().click()
  await expect(page.getByRole('dialog')).toBeVisible()

  const results = await scan(page, testInfo, 'pane-settings')

  // A modal dialog is where accessibility problems are both most likely and
  // most consequential — focus trapping, labelling, escape handling — so it
  // gets its own scan rather than being folded into the dashboard's.
  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
})
