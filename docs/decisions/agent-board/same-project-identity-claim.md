---
type: Decision
title: "Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)"
description: "Rationale for Same-project panes claim an agmsg actas lock."
status: deprecated
---

# Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)

agmsg's default watcher resolves every identity registered for one project and agent type. Two
Claude panes in the same directory therefore subscribed to each other's messages even though their
board identities were distinct. Bootstrap now follows agmsg's own template: join, claim the pane ID
against the Claude session ID, then run `watch.sh` with the pane ID as its narrowing argument. A
held lock is reported instead of disturbing the owning watcher.

Panemux-generated participation was replaced by [PTY retirement](retire-pty-onboarding.md); agmsg now owns participation and identity selection.

Current contracts: [agent board guide](../../agent-board.md).
