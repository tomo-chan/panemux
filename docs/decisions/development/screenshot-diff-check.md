---
type: Decision
title: "Retaking the screenshots is checked by diff, not by pixels (2026-10-03, issue #285)"
description: "Require screenshot diffs instead of brittle pixel comparisons across timestamps and font rendering environments."
---

# Retaking the screenshots is checked by diff, not by pixels (2026-10-03, issue #285)

A pull request that changes the presentation layer (`frontend/src/components/`, `App.tsx`,
`styles/`, `index.html`) or the capture itself must also change `docs/images/`, unless it carries
the `screenshots-exempt` label (`.github/workflows/screenshots.yml`). Regenerating the images in CI
and comparing them pixel by pixel was not chosen: the images carry relative times ("2s ago") and
font rendering differs between a developer's machine and the runner, so the comparison would fail
on changes nobody made. `hooks/`, `schemas/` and `utils/` are outside the trigger for the reason
the scenario ledger's check keeps its trigger narrow: they change what is on screen far less often
than they change, and a check that fires without cause gets bypassed.

Current contracts: [development guide](../../index.md).
