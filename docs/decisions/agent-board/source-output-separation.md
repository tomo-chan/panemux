---
type: Decision
title: "Keep source data separate from diagnostics (2026-10-31, #435 review round 2)"
description: "Rationale for Keep source data separate from diagnostics."
---

# Keep source data separate from diagnostics (2026-10-31, #435 review round 2)

The combined input lane retained failure diagnostics but also mixed successful git warnings into
rev-parse and tar output. Git source acquisition now uses a local split-output entry point sharing
hostexec's command setup and cancellation. Only stdout is source data; stderr is attached only to
failure diagnostics. The existing combined RunInput contract is unchanged for installers and
prerequisite probes. Regression cases cover warnings during verification, archive creation and both.

Current contracts: [agent board guide](../../agent-board.md).
