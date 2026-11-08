---
type: Decision
title: "One host identity and shared execution (2026-10-25, #438 / #439)"
description: "Rationale for One host identity and shared execution."
---

# One host identity and shared execution (2026-10-25, #438 / #439)

Both boards address the local host and configured SSH names; one identity/execution pool avoids
independent task and pane connections. Name equality was chosen over resolved host/user/port:
distinct configured names stay distinct even for one machine.
Fixed stdin scripts/separate channels preserve quoting and cancellation isolation. Consumers close
only pools they own. Polling never waits on unreachable first dials, while sends can wait within
context. Every lookup checks membership so removed names cannot fall back to OpenSSH.
Reconnect discards in-flight results too. See [architecture](../../architecture.md) for current ownership.
