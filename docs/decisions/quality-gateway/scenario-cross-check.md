---
type: Decision
title: "D7 \u2014 The ledger is cross-checked, not just required"
description: "Resolve scenario paths and test names to detect stale coverage claims rather than merely requiring a ledger row."
---

# D7 — The ledger is cross-checked, not just required

Recorded 2026-08-30 in the original quality-gateway milestone log.

A required scenario row can still lie when its named test moves or disappears. Resolve claimed
paths and Go test names to catch stale coverage, not merely missing prose. Shortcuts, WebSocket
routes and generated binaries resemble paths, so the checker's false positives need regression
tests too. See [scenario ledger](../../scenarios.md).

Current contracts: [quality gateway guide](../../quality-gateway.md).
