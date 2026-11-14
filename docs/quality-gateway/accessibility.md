---
type: Specification
title: "Accessibility ceilings"
description: "Per-rule accessibility ceilings and the procedure for lowering them after fixes."
---

# Accessibility ceilings

### Accessibility

`frontend/e2e/a11y.spec.ts` enforces the counts below through the comparator and map in
`frontend/e2e/a11y-ceiling.ts`. The counts reproduced exactly on a second machine. Measurements
use the spec's own fixture and panemux process (`frontend/e2e/a11y.yml`, port 4178), excluding
`.xterm` because xterm.js owns that canvas and markup. See
[D11](../decisions/quality-gateway/accessibility-ceilings.md) for fixture isolation and the
per-rule node-count ceiling.

| Page state | Rule | Impact | Ceiling (nodes) |
|---|---|---|---|
| Dashboard | `aria-required-children` | critical | 1 |
| Dashboard | `color-contrast` | serious | 2 |
| Dashboard | `region` | moderate | 7 |
| Pane settings dialog open | `aria-required-children` | critical | 1 |
| Pane settings dialog open | `select-name` | critical | 1 |
| Pane settings dialog open | `color-contrast` | serious | 10 |
| Pane settings dialog open | `region` | moderate | 7 |

The task dashboard and its host connection menu open (issue #314) are scanned too, with the host
list stubbed to one reachable remote host since the fixture has no SSH server. They have no rows:
every rule is held at zero there. Their first CI measurement found one `scrollable-region-focusable`
(serious) node in each — the board, which at 1280px scrolls sideways and, with the fixture's empty
task list, held nothing focusable. The board was made a tab stop of its own rather than given a
ceiling.

Every listed row is an existing violation, not approval of it. The ceiling avoids starting
with a red gate (principle 4): counts may fall and must not rise; absent rules have ceiling zero,
so new kinds fail immediately. D11 explains per-rule node counts rather than looser alternatives.

Perturbation with an `<img>` lacking `alt` and a low-contrast `<span>` in `TerminalPane` failed
both scans on both branches: new `image-alt` had ceiling zero and `region` rose from 7 to 9.
The shared-server version passed alone but failed inside `make test-e2e`;
[D11](../decisions/quality-gateway/accessibility-ceilings.md) records the isolation correction.

**Lowering a ceiling after fixing something.** The run does the arithmetic; the edit is manual and
takes two files:

1. Run `make test-e2e`. A count that came in under its ceiling prints as
   `a11y[<page state>] ceiling can be lowered` with the exact replacement — `set
   CEILINGS['pane-settings']['color-contrast'] to 9 (was 10)`, or `remove 'select-name' from
   CEILINGS['pane-settings']` when a rule is gone entirely. The same text is in the run's
   `axe-<page state>.txt` attachment.
2. Apply it to `CEILINGS` in `frontend/e2e/a11y-ceiling.ts` and to the table above, in the same commit
   as the fix.

The run passes either way. Nothing forces step 2 — deliberately, per D11 — so a fix that skips it
silently leaves room for the next regression.
