import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page, type TestInfo } from '@playwright/test'

import { checkAgainstCeiling, type ScanLabel } from './a11y-ceiling'

// Interaction capability is the other ISO 25010 characteristic
// docs/quality-gateway.md records as thinly protected: three focus-restoration
// E2E tests and, before this file, no accessibility checks at all.
//
// This file drives the four page states and reports what it found. The ceiling
// it holds them to, and the comparator that applies it, live in
// ./a11y-ceiling.ts — separated so the comparator can be unit tested, because
// a green run exercises none of its interesting branches. Read that file for
// why the ceiling is per rule ID, why it counts nodes, and why lowering it is
// a manual edit.

const IMPACTS = ['critical', 'serious', 'moderate', 'minor'] as const

async function scan(page: Page, testInfo: TestInfo, label: ScanLabel) {
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

  const { failures, lowerable } = checkAgainstCeiling(label, results.violations)

  // Both outputs matter: the log line is what a CI run shows without anyone
  // downloading anything, and the attachment is what someone actually fixing
  // these needs.
  console.log(
    `a11y[${label}] total=${results.violations.length} ` +
      IMPACTS.map((i) => `${i}=${counts[i]}`).join(' '),
  )
  if (lowerable.length > 0) {
    console.log(
      `a11y[${label}] ceiling can be lowered — edit CEILINGS in e2e/a11y-ceiling.ts and the ` +
        `Accessibility table in docs/quality-gateway.md:\n  ${lowerable.join('\n  ')}`,
    )
  }
  await testInfo.attach(`axe-${label}.txt`, {
    body:
      (summary || 'no violations') +
      (lowerable.length > 0 ? `\n\nceiling can be lowered:\n${lowerable.join('\n')}` : ''),
    contentType: 'text/plain',
  })

  return { results, failures }
}

test('holds the dashboard at or below its accessibility ceiling', async ({ page }, testInfo) => {
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()

  const { results, failures } = await scan(page, testInfo, 'dashboard')

  // The scan ran and looked at something. Without this a page that failed to
  // render at all would report zero violations and pass the ceiling check
  // trivially, which is the one way this gate could go quietly green while
  // measuring nothing.
  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
  expect(failures, failures.join('\n')).toEqual([])
})

test('holds the pane settings dialog at or below its accessibility ceiling', async ({ page }, testInfo) => {
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()

  await page.getByTitle('Pane settings').first().click()
  await expect(page.getByRole('dialog')).toBeVisible()

  const { results, failures } = await scan(page, testInfo, 'pane-settings')

  // A modal dialog is where accessibility problems are both most likely and
  // most consequential — focus trapping, labelling, escape handling — so it
  // gets its own ceiling rather than being folded into the dashboard's.
  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
  expect(failures, failures.join('\n')).toEqual([])
})

// The task dashboard, with the host list a remote host makes. The fixture has
// no SSH server, so the task collection is stubbed: one remote host the
// collection reached, which is what makes its chip a button.
async function showTaskDashboard(page: Page) {
  await page.route(/\/api\/tasks$/, (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    return route.fulfill({
      json: {
        hosts: [
          { name: '', status: 'ok', collected_at: '2026-01-01T00:00:00Z' },
          { name: 'gpu-box', status: 'ok', collected_at: '2026-01-01T00:00:00Z' },
        ],
        tasks: [],
        known_labels: [],
        summaries_enabled: false,
      },
    })
  })
  await page.goto('/')
  await expect(page.locator('[data-pane-id]').first()).toBeVisible()
  await page.getByRole('button', { name: /^Tasks/ }).click()
  await expect(page.getByRole('button', { name: 'Open a terminal on gpu-box' })).toBeVisible()
}

test('holds the task dashboard at or below its accessibility ceiling', async ({ page }, testInfo) => {
  await showTaskDashboard(page)

  const { results, failures } = await scan(page, testInfo, 'task-dashboard')

  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
  expect(failures, failures.join('\n')).toEqual([])
})

test('holds the host connection menu at or below its accessibility ceiling', async ({ page }, testInfo) => {
  await showTaskDashboard(page)

  await page.getByRole('button', { name: 'Open a terminal on gpu-box' }).click()
  await expect(page.getByRole('dialog', { name: 'Open a terminal on gpu-box' })).toBeVisible()

  const { results, failures } = await scan(page, testInfo, 'host-connect-menu')

  expect(results.passes.length + results.violations.length).toBeGreaterThan(0)
  expect(failures, failures.join('\n')).toEqual([])
})
