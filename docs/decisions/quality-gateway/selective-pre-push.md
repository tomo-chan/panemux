---
type: Decision
title: "D14 \u2014 Selective pre-push checks and full CI"
description: "Check pushed changes locally and full suites in CI, falling back to full checks when scope is unknown."
---

# D14 — Selective pre-push checks and full CI

Recorded 2026-10-09, issue #335. Full pre-push suites made every small push pay the entire cost.
The hook checks the pushed range against its remote branch, or merge base with origin/main on first
push. Changed packages run locally; race detection/importers remain CI responsibilities.
Unknown ranges or changes that cannot be narrowed fall back to make check instead of verifying
nothing. CI added omitted script suites/triggers when local full-suite coverage ended.
Completion still needs a manual make check or successful PR CI.
See [push protection](../../development/push-protection.md).
