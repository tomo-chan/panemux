---
type: Decision
title: "D10 \u2014 The contract fixtures are rewritten on every run, not diffed against"
description: "Rewrite Go captures before frontend validation so stale schemas see changed keys instead of stopping at golden diffs."
---

# D10 — The contract fixtures are rewritten on every run, not diffed against

Recorded 2026-09-03 in the original quality-gateway milestone log.

Golden comparison in Go stops before stale frontend schemas see a changed response. Go therefore
rewrites captured fixtures before frontend validation. Parsing alone misses renamed optional keys
because Zod strips them: parsed output must also equal capture.
Normalize nondeterministic values/order, never keys/types. Re-capture tests detect machine paths
rather than trusting committed files, and repeated captures detect unstable ordering.
Optional-field gaps derive from schemas and the full corpus, with reasoned exceptions removed when
coverage appears. Manual coverage claims once falsely reported completeness.
See [contract fixtures](../../development/contract-fixtures.md) for authoritative sequencing/rules.
