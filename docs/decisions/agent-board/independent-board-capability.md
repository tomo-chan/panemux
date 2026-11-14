---
type: Decision
title: "Dashboard capability is independent from command-center capability (2026-08-22, PR #171)"
description: "Represent dashboard enablement separately so it can run independently of the command center."
---

# Dashboard capability is independent from command-center capability (2026-08-22, PR #171)

The dashboard can be enabled without the command center and vice versa. A single
`command_center_enabled` flag could not represent both cases, so `/api/session-token` also returns
`agent_board_enabled`. The dashboard remains read-only; broadcast composition stays in the command
center/API surface.

Current contracts: [agent board guide](../../agent-board.md).
