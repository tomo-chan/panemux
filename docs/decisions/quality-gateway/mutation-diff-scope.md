---
type: Decision
title: "D2 \u2014 Mutation testing is scoped to the diff and to pull requests"
description: "Scope mutation to changed lines and PRs so existing survivors cannot make every branch start red."
---

# D2 — Mutation testing is scoped to the diff and to pull requests

Recorded 2026-08-28 in the original quality-gateway milestone log.

Whole-repository mutation gates start red on existing survivors and false positives. Diff/PR
scope asks authors about changes they can act on. The initial cost argument was amended after a
whole-module run took 14m25s, not hours: faster tooling does not remove the existing-survivor problem.
See [mutation](../../development/mutation.md).
