---
type: Decision
title: "Global board participation and installer split (2026-10-29, #435 / #438)"
description: "Replace pane switches with instance-wide opt-in while preserving the existing same-user trust boundary."
---

# Global board participation and installer split (2026-10-29, #435 / #438)

Instance-wide opt-in replaced pane switches, with fixed installation paths and migration errors
rather than silently discarded config. Same-host configured panes become addresses, broadening
impersonatable names within the existing same-user boundary.
Pinned source acquisition, installer consent and update coordination were agreed before splitting
implementation for review; stage-completion notes are omitted. See [Agent Board](../../agent-board.md)
and [installer security](../../security/agent-board.md) for authoritative current contracts.
The onboarding/mode part was replaced by [PTY retirement](retire-pty-onboarding.md): opt-in no longer
authorizes generated participation instructions.
