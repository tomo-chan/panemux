---
type: Decision
title: "Live pane eligibility after configuration edits (2026-10-29, #435 / PR #447 review)"
description: "Refresh routing authority after configuration edits instead of pinning pane membership to startup state."
status: deprecated
---

# Live pane eligibility after configuration edits (2026-10-29, #435 / PR #447 review)

Routing now reads immutable maps derived under the API configuration lock for each relay operation;
bootstrap refreshes pane membership each tick. Enabled boards continue running even when the
initial layout is empty. New panes and configured hosts become available without a restart, removed
IDs lose sender/destination authority, and moving a pane revokes its old host. Participation is
synchronized on ticks, clearing instruction state on host changes while retaining historical reports.
Detection continues every five seconds for all eligible panes; the steady-state process/tmux/SSH
probe cost is documented rather than changing detection semantics in this review fix.

Current contracts: [agent board guide](../../agent-board.md).

The instruction-state part was replaced by [PTY onboarding retirement](retire-pty-onboarding.md).
