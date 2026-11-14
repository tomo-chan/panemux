---
type: Decision
title: "Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)"
description: "Attach history warnings to the single terminal frame instead of violating stream completion semantics."
---

# Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)

A failed history write was first represented as `error` followed by `done`, violating the protocol's
one-terminal-frame rule; limiting the warning to successful turns then hid it whenever the query
also failed. `warnings` now rides whichever single terminal frame ends the turn. Malformed stream
output cancels the subprocess before its remaining output is drained, preventing the drain from
holding the busy flag and client error until the full timeout.

Current contracts: [runtime guide](../../behavior.md).
