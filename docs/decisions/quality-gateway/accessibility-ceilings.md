---
type: Decision
title: "D11 \u2014 The accessibility ceiling is per rule, counts nodes, and is lowered by hand"
description: "Use per-rule node ceilings on owned page fixtures and lower them manually instead of failing improvements."
---

# D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand

Recorded 2026-09-03 in the original quality-gateway milestone log.

Total violations let one fix hide a new kind. Per-rule node ceilings catch new kinds and more
affected content. Counts are stable only for fixed pages: other E2E mutators changed them, so the
accessibility spec owns its server/fixture. Separate comparator unit tests exercise rejection arms
that healthy runs never reach; runner suffixes prevent Playwright/Vitest matcher collisions.
Lower counts pass with a concrete manual edit. Failing on improvement makes a brittle ratchet
two-sided and discourages fixes. See [accessibility](../../quality-gateway/accessibility.md).
