import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page, type TestInfo } from '@playwright/test'

// Interaction capability is the other ISO 25010 characteristic
// docs/quality-gateway.md records as thinly protected: three focus-restoration
// E2E tests and, before this file, no accessibility checks at all.
//
// **This is a ceiling, not a target.** Roadmap item 7 of issue #180 took the
// measurement without gating on it, deliberately: turning axe on over an
// existing UI produces a backlog, and a gate that starts red is a gate people
// route around on day one — taking the gates that do work with it (design
// principle 4). Issue #194 is the follow-up that step was waiting for. Now
// that the numbers exist, they are frozen as the maximum rather than `0` being
// demanded. Every violation below is a violation this repository still has;
// none of them is approved by being listed here.
//
// So: a count may fall, and must not rise. A rule that is not listed has a
// ceiling of zero, so a *new* kind of violation fails the run the day it
// appears, which is the regression this gate exists to catch.

const IMPACTS = ['critical', 'serious', 'moderate', 'minor'] as const

// The two page states scanned below. Naming them as a union rather than a bare
// string keeps a typo in a ceiling key from silently becoming an unenforced
// entry — `CEILINGS` would no longer typecheck.
type ScanLabel = 'dashboard' | 'pane-settings'

// **The ceiling is per rule ID and counts nodes.** Both halves of that were
// open questions in #194, and the reasons are worth recording, because the
// looser alternatives are cheaper and both fail to catch a real regression:
//
//   - *A total count only* would let one violation be fixed and a different
//     one introduced in the same change without the number moving. That is
//     exactly the "fixed one, added one" case, and it is the most likely way
//     for this UI to regress while looking maintained.
//   - *Rule IDs without node counts* would treat `color-contrast` at 2 nodes
//     and at 10 nodes as the same fact. They are not: the dashboard and the
//     open dialog differ by 8 nodes on that one rule, so an entire dialog's
//     worth of unreadable text could appear on the dashboard and count as no
//     change.
//
// The cost of the tighter form is brittleness, and it is real rather than
// hypothetical, so it is worth being precise about what was and was not
// checked. Against a *fixed* rendered page these counts are stable: they
// reproduced identically to the ones #187 recorded, on different hardware, in
// a different container, on a later run — including `region` at 7 nodes and
// `color-contrast` at 10, the counts a layout- or font-sensitive rule would be
// expected to move. What they are not stable against is a different page, and
// a node count is a property of whatever happens to be rendered: running this
// spec after pane-move.spec.ts's splits, on the server it used to share, the
// same two counts read 10 and 11. That is why this spec has its own panemux
// process and fixture (e2e/a11y.yml, port 4178) — the alternative is a gate
// that fails on the order of the suite, which is principle 4's false positive
// exactly. See quality-gateway.md's "Accessibility" section.
//
// **Lowering a ceiling is a manual edit to this map, and the run tells you
// exactly what to write.** When a count comes in under its ceiling the scan
// prints the replacement entry and attaches it, but does not fail — the issue
// asks for a ceiling that may fall, and failing a run for *improving*
// accessibility would train people to stop improving it. The nudge is loud so
// that the ceiling does not quietly stay above reality and re-admit the
// regression it was set to catch.
const CEILINGS: Record<ScanLabel, Record<string, number>> = {
  dashboard: {
    // critical — the pane grid's role/child structure.
    'aria-required-children': 1,
    // serious — 2 nodes on the dashboard chrome.
    'color-contrast': 2,
    // moderate — 7 nodes outside any landmark.
    region: 7,
  },
  'pane-settings': {
    'aria-required-children': 1,
    // critical — the dialog's unlabelled <select>. Only in this state.
    'select-name': 1,
    // serious — the dialog adds 8 nodes to the dashboard's 2.
    'color-contrast': 10,
    region: 7,
  },
}

type CeilingCheck = {
  failures: string[]
  lowerable: string[]
}

function checkAgainstCeiling(
  label: ScanLabel,
  violations: { id: string; impact?: string | null; nodes: unknown[] }[],
): CeilingCheck {
  const ceiling = CEILINGS[label]
  const observed = new Map<string, number>()
  for (const v of violations) {
    // axe reports one entry per rule, but summing is still correct and is what
    // keeps this from depending on that.
    observed.set(v.id, (observed.get(v.id) ?? 0) + v.nodes.length)
  }

  const failures: string[] = []
  for (const v of violations) {
    const allowed = ceiling[v.id] ?? 0
    const actual = observed.get(v.id) ?? 0
    if (actual <= allowed) continue
    failures.push(
      allowed === 0
        ? `${label}: new violation '${v.id}' (${v.impact ?? 'unknown'}, ${actual} node(s)). ` +
          `Fix it, or — if it is genuinely acceptable — add it to CEILINGS['${label}'] with a reason.`
        : `${label}: '${v.id}' rose to ${actual} node(s), ceiling is ${allowed}. ` +
          `The ceiling may fall, not rise: fix the new node(s) rather than raising it.`,
    )
  }

  const lowerable: string[] = []
  for (const [id, allowed] of Object.entries(ceiling)) {
    const actual = observed.get(id) ?? 0
    if (actual >= allowed) continue
    lowerable.push(
      actual === 0
        ? `remove '${id}' from CEILINGS['${label}'] (was ${allowed})`
        : `set CEILINGS['${label}']['${id}'] to ${actual} (was ${allowed})`,
    )
  }

  return { failures, lowerable }
}

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
      `a11y[${label}] ceiling can be lowered — edit CEILINGS in e2e/a11y.spec.ts and the ` +
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
