// The comparator behind a11y.spec.ts's accessibility gate, and the ceiling it
// compares against.
//
// It lives in its own module rather than inside the spec so it can be unit
// tested. On a healthy repository every observed count equals its ceiling
// exactly, so a Playwright run only ever exercises the `actual === allowed`
// path and both returned arrays come back empty — none of the four branches
// that matter (new rule, risen count, lowerable count, vanished rule) runs at
// all. A comparator whose interesting branches only execute when the
// repository is already broken is a comparator whose inversions ship green, so
// they are pinned in a11y-ceiling.test.ts instead of relying on someone having
// perturbed the UI by hand once.

// The page states a11y.spec.ts scans. Naming them as a union rather than a
// bare string keeps a typo in a ceiling key from silently becoming an
// unenforced entry — `CEILINGS` would no longer typecheck.
export type ScanLabel = 'dashboard' | 'pane-settings'

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
//
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
// a node count is a property of whatever happens to be rendered: running the
// spec after pane-move.spec.ts's splits, on the server it used to share, the
// same two counts read 10 and 11. That is why the spec has its own panemux
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
export const CEILINGS: Record<ScanLabel, Record<string, number>> = {
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

// The shape this needs from axe's `Result`, and no more: taking the structural
// minimum is what lets the unit tests build cases as plain objects instead of
// faking an axe run.
export type ObservedViolation = {
  id: string
  impact?: string | null
  nodes: unknown[]
}

export type CeilingCheck = {
  /** One message per rule over its ceiling. Empty means the gate passes. */
  failures: string[]
  /** One message per ceiling now above reality. Never fails the run. */
  lowerable: string[]
}

export function checkAgainstCeiling(
  label: ScanLabel,
  violations: ObservedViolation[],
  ceilings: Record<ScanLabel, Record<string, number>> = CEILINGS,
): CeilingCheck {
  const ceiling = ceilings[label]

  // Sum per rule id rather than trusting axe to emit one entry per rule. Both
  // loops below then read this map and only this map: an earlier version
  // counted here but reported by iterating `violations`, so a rule arriving as
  // two entries produced one correct count and two identical failure lines,
  // which reads as two problems.
  const observed = new Map<string, number>()
  for (const v of violations) {
    observed.set(v.id, (observed.get(v.id) ?? 0) + v.nodes.length)
  }

  const failures: string[] = []
  for (const [id, actual] of observed) {
    const allowed = ceiling[id] ?? 0
    if (actual <= allowed) continue
    const impact = violations.find((v) => v.id === id)?.impact ?? 'unknown'
    failures.push(
      allowed === 0
        ? `${label}: new violation '${id}' (${impact}, ${actual} node(s)). ` +
          `Fix it, or — if it is genuinely acceptable — add it to CEILINGS['${label}'] with a reason.`
        : `${label}: '${id}' rose to ${actual} node(s), ceiling is ${allowed}. ` +
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
