---
type: Decision
title: "D8 \u2014 Per-block coverage gates the diff, not a baseline"
description: "Gate changed blocks because a repository baseline starts red and concurrent zero-count blocks make ceilings noisy."
---

# D8 — Per-block coverage gates the diff, not a baseline

Recorded 2026-08-30 in the original quality-gateway milestone log.

At d0e88ee, 275–278 of 1801 blocks were unexecuted; six identical runs differed on three concurrent
blocks. A repository-wide zero-block gate starts red and a checked-in ceiling flakes both ways.
Diff scope starts green without another baseline/exclusion list. Excluded packages report not
measured, never covered; reasoned inline exemptions cover residue. Existing blocks remain a backlog,
not an unactionable gate. See [coverage](../../development/coverage.md).
