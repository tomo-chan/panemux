---
type: Decision
title: "Automatic-update event delivery (2026-10-31, #435 B3 review)"
description: "Coalesce confirmation events during active checks and schedule new hosts without adding a periodic watcher."
---

# Automatic-update event delivery (2026-10-31, #435 B3 review)

An off-to-on event received during a host's active check is retained as one pending confirmation.
The same worker consumes it after the current attempt, coalescing overlapping events and keeping
shutdown joining and per-host update admission intact. A host addition uses the existing successful
SSH-connection persistence event to notify tool management, after releasing the config lock.
Only that host is checked for enabled global tools; unrelated hosts' retained failures are not
retried. Failed saves and disabled preferences schedule nothing. No periodic watcher is added.
Current-state installer/security descriptions describe these shipped capabilities directly.

Current contracts: [agent board guide](../../agent-board.md).
