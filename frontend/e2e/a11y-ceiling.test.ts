import { describe, expect, it } from 'vitest'

import { CEILINGS, checkAgainstCeiling, type ObservedViolation, type ScanLabel } from './a11y-ceiling'

// Why this file exists at all: on a healthy repository the observed counts
// equal the ceilings exactly, so a real Playwright run only ever takes the
// `actual === allowed` path and returns two empty arrays. Every branch that
// makes the gate a gate — new rule, risen count, and both lowerable forms —
// runs only when something is already wrong, which is precisely when nobody
// wants to be discovering that the comparator itself is broken. The
// perturbation in #194's test plan proved the gate bites once, by hand; this
// keeps it proven.

const FIXTURE: Record<ScanLabel, Record<string, number>> = {
  dashboard: { 'color-contrast': 2, region: 7 },
  'pane-settings': { 'select-name': 1 },
}

function violation(id: string, nodes: number, impact?: string | null): ObservedViolation {
  return { id, impact, nodes: Array.from({ length: nodes }, (_, i) => i) }
}

describe('checkAgainstCeiling', () => {
  it('passes silently when every count sits exactly on its ceiling', () => {
    // The shape of every green CI run, and the reason the other cases below
    // cannot be left to the E2E suite to discover.
    const result = checkAgainstCeiling(
      'dashboard',
      [violation('color-contrast', 2, 'serious'), violation('region', 7, 'moderate')],
      FIXTURE,
    )

    expect(result).toEqual({ failures: [], lowerable: [] })
  })

  it('fails a rule that is not in the ceiling at all, naming its impact and count', () => {
    const { failures, lowerable } = checkAgainstCeiling(
      'dashboard',
      [violation('image-alt', 3, 'critical')],
      FIXTURE,
    )

    expect(failures).toEqual([
      "dashboard: new violation 'image-alt' (critical, 3 node(s)). " +
        "Fix it, or — if it is genuinely acceptable — add it to CEILINGS['dashboard'] with a reason.",
    ])
    // A rule at 0 nodes because it is absent is not "lowerable to 0" — the
    // ceiling entries that did not appear are what that list is for.
    expect(lowerable).toEqual([
      "remove 'color-contrast' from CEILINGS['dashboard'] (was 2)",
      "remove 'region' from CEILINGS['dashboard'] (was 7)",
    ])
  })

  it('reports an unlabelled impact as unknown rather than undefined', () => {
    const { failures } = checkAgainstCeiling('dashboard', [violation('image-alt', 1)], FIXTURE)

    expect(failures[0]).toContain("new violation 'image-alt' (unknown, 1 node(s))")
  })

  it('fails a listed rule whose node count rose above its ceiling', () => {
    const { failures } = checkAgainstCeiling(
      'dashboard',
      [violation('color-contrast', 3, 'serious'), violation('region', 7, 'moderate')],
      FIXTURE,
    )

    expect(failures).toEqual([
      "dashboard: 'color-contrast' rose to 3 node(s), ceiling is 2. " +
        'The ceiling may fall, not rise: fix the new node(s) rather than raising it.',
    ])
  })

  it('does not fail a count that fell, and prints the replacement entry', () => {
    // The issue's title is explicit that a count may fall. Failing here would
    // make improving accessibility break the build.
    const { failures, lowerable } = checkAgainstCeiling(
      'dashboard',
      [violation('color-contrast', 1, 'serious'), violation('region', 7, 'moderate')],
      FIXTURE,
    )

    expect(failures).toEqual([])
    expect(lowerable).toEqual(["set CEILINGS['dashboard']['color-contrast'] to 1 (was 2)"])
  })

  it('says remove, not set to 0, when a listed rule is gone entirely', () => {
    const { failures, lowerable } = checkAgainstCeiling(
      'dashboard',
      [violation('region', 7, 'moderate')],
      FIXTURE,
    )

    expect(failures).toEqual([])
    expect(lowerable).toEqual(["remove 'color-contrast' from CEILINGS['dashboard'] (was 2)"])
  })

  it('reports a failure and a lowerable entry from the same scan', () => {
    // Fixing one rule while another regresses is the whole reason the ceiling
    // is per rule; both halves have to survive being in the same result.
    const { failures, lowerable } = checkAgainstCeiling(
      'dashboard',
      [violation('color-contrast', 1, 'serious'), violation('region', 9, 'moderate')],
      FIXTURE,
    )

    expect(failures).toEqual([
      "dashboard: 'region' rose to 9 node(s), ceiling is 7. " +
        'The ceiling may fall, not rise: fix the new node(s) rather than raising it.',
    ])
    expect(lowerable).toEqual(["set CEILINGS['dashboard']['color-contrast'] to 1 (was 2)"])
  })

  it('sums a rule split across several axe entries into one count and one message', () => {
    // axe emits one entry per rule today, and the comparator deliberately does
    // not depend on that. The first version summed correctly but reported by
    // iterating the raw entries, so this case produced one right count and two
    // identical failure lines — which reads as two separate problems.
    const { failures } = checkAgainstCeiling(
      'dashboard',
      [violation('color-contrast', 2, 'serious'), violation('color-contrast', 9, 'serious')],
      FIXTURE,
    )

    expect(failures).toEqual([
      "dashboard: 'color-contrast' rose to 11 node(s), ceiling is 2. " +
        'The ceiling may fall, not rise: fix the new node(s) rather than raising it.',
    ])
  })

  it('reads the ceiling for the label it was given, not the other one', () => {
    // `select-name` is in the pane-settings ceiling and not the dashboard's.
    // Reading the wrong label would make it pass on the dashboard, where it is
    // exactly the new-violation case the gate exists for.
    const onDashboard = checkAgainstCeiling(
      'dashboard',
      [violation('select-name', 1, 'critical')],
      FIXTURE,
    )
    const inDialog = checkAgainstCeiling(
      'pane-settings',
      [violation('select-name', 1, 'critical')],
      FIXTURE,
    )

    expect(onDashboard.failures).toHaveLength(1)
    expect(onDashboard.failures[0]).toContain("new violation 'select-name'")
    expect(inDialog.failures).toEqual([])
  })

  it('passes a clean scan against a ceiling of nothing', () => {
    const result = checkAgainstCeiling('dashboard', [], {
      dashboard: {},
      'pane-settings': {},
    })

    expect(result).toEqual({ failures: [], lowerable: [] })
  })
})

describe('CEILINGS', () => {
  it('covers both scanned page states', () => {
    // a11y.spec.ts scans two states; a label with no entry would make every
    // violation on that page read as new, which is a different failure from
    // the one intended and would be blamed on the page.
    expect(Object.keys(CEILINGS).sort()).toEqual(['dashboard', 'pane-settings'])
  })

  it('carries no zero or negative entry', () => {
    // A ceiling of 0 is what an *absent* rule already means, so writing one
    // explicitly says nothing while looking like it permits something. It is
    // the shape a bad lowering edit would leave behind — `set ... to 0` is
    // never printed; `remove` is.
    for (const [label, rules] of Object.entries(CEILINGS)) {
      for (const [id, allowed] of Object.entries(rules)) {
        expect(allowed, `${label}/${id}`).toBeGreaterThan(0)
      }
    }
  })

  it('is used as the default when no ceiling map is passed', () => {
    // The spec calls the two-argument form, so the default has to be the real
    // map rather than the fixture the cases above inject.
    const { failures } = checkAgainstCeiling('pane-settings', [
      violation('select-name', 1, 'critical'),
    ])

    expect(failures).toEqual([])
  })
})
