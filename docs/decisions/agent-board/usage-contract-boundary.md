---
type: Decision
title: "Usage remains outside the Agent Board status contract (2026-08)"
description: "Rationale for Usage remains outside the Agent Board status contract."
---

# Usage remains outside the Agent Board status contract (2026-08)

Account-wide usage belongs to the agent provider. Per-pane usage may still be derived by summing the
documented `usage` field already stored in each pane's transcript: that is direct field reading, not
inference from private state. If added, it belongs to panemux's existing pane-inspection surface,
not the cooperative agmsg status schema.

Current contracts: [agent board guide](../../agent-board.md).
