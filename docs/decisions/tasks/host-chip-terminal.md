---
type: Decision
title: "Opening a host from its chip on the dashboard (2026-10-09, issue #314)"
description: "Rationale for Opening a host from its chip on the dashboard."
---

# Opening a host from its chip on the dashboard (2026-10-09, issue #314)

A host has no unique task session, so its chip creates a terminal instead of guessing an existing
pane/session. A popover chooses ssh/ssh_tmux and destination without crowding the chips.
Server-owned name generation avoids duplicating validation in the browser. Popup lifetime reuses
task attaches with separate host/task deletion routes. Session selection for an existing remote
tmux session and a special ssh-only gesture were rejected.
See [task behavior](../../behavior/tasks.md) for the current interaction.
