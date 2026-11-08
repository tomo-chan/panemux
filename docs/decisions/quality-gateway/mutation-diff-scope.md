---
type: Decision
title: "D2 \u2014 Mutation testing is scoped to the diff and to pull requests"
description: "Rationale for D2 \u2014 Mutation testing is scoped to the diff and to pull requests."
---

# D2 — Mutation testing is scoped to the diff and to pull requests

Whole-repository mutation gates start red on existing survivors and false positives. Diff/PR
scope asks authors about changes they can act on. The initial cost argument was amended after a
whole-module run took 14m25s, not hours: faster tooling does not remove the existing-survivor problem.
See [mutation](../../development/mutation.md).
