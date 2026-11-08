---
type: Specification
title: "Attention Indicators"
description: "Visual attention cues and permission-request actions in panes and task popups."
---

# Attention Indicators

Agent-attention highlighting remains visually distinct from layout-editing affordances.

- pane attention uses an animated gold frame
- workspace attention uses an animated gold tab background
- move targets use blue overlays instead of gold

Using separate colors avoids mixing "this needs your attention" with "you can drop here".

Attention marks an agent waiting for the operator. The [task event contract](../behavior/task-events.md#pane-and-workspace-attention)
owns wait transitions and clearing on focus, workspace selection or wait end. Notification jumps
focus and briefly outline the pane, or select and highlight the task when no pane shows it.

A codex permission request is answered in the codex pane itself, so a pane whose task waits on one
also carries a thin gold strip between its header and its terminal — `Waiting for input: a permission
request from codex`, the Tasks detail's own words, and the detail's `Input given` — so the wait can
be marked answered without opening the dashboard. The Type in pane popup shows the same strip between
its header and its terminal. A pane running several such tasks says how many and offers
`Review in Tasks` instead. The strip is buttons only and takes no terminal keys, including Tab and Escape. It persists after
attention is cleared or the popup closes. `Input given` restores terminal focus unless the operator
moved to another pane while answering; only that action or wait end removes the strip
([permission behavior](../behavior/task-events.md#codex-permission-requests-in-a-pane)).
