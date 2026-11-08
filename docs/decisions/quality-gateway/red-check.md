---
type: Decision
title: "D4 \u2014 Verify \"the test came first\" after the fact (red-check)"
description: "Require each changed test to pass at HEAD and fail under revert so honest siblings cannot mask tautologies."
---

# D4 — Verify "the test came first" after the fact (red-check)

Recorded 2026-08-28 in the original quality-gateway milestone log.

TDD chronology cannot be reconstructed reliably, but changed tests can be checked against removing
the implementation. Each case first passes at HEAD, then fails under its stack's revert.
Aggregate failure lets one honest test vouch for siblings; a HEAD build/collection failure otherwise
masquerades as evidence against the revert. Benchmarks and zero selected cases prove nothing.
Changed functions/cases, not files, define scope. Frontend JSON reports provide per-case verdicts;
regex matching accidentally selects siblings. Source ranges/report locations share boundaries for
parameterized cases; there is no whole-file fallback. A HEAD-passing case absent after revert is
red because only implementation files moved. Setup-only changes with no mapped case cannot be checked.
No implementation means nothing to revert; implementation without tests warns to allow refactors.
Each language stack needs its own revert. Unrelated Go tests use visible per-test exemptions;
package-only scope would blind cross-package integration tests, and whole-PR exemption is too broad.
Missing prerequisites, skipped/unrunnable selected tests and unavailable scratch worktrees fail.
Root-package scratch builds need an embedded frontend placeholder: unbuildable HEAD proves nothing.
Visible PR-label exemptions cover legitimate cases rather than hiding them in configuration.
See [red-check](../../development/red-check.md) for authoritative scope, failure and exemption rules.
