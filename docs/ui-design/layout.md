---
type: Specification
title: "Layout Editing"
description: "Pane drag targets and feedback for optimistic layout persistence."
---

# Layout Editing

## Drag and Drop

Drag-and-drop is always available from the pane header handle. There is no separate edit mode.

### States
- Normal: full opacity, no move highlight.
- Drag source: fades and slightly scales down without detaching the live terminal canvas;
  global `grabbing` cursor and a 0.15 s transition confirm the drag.
- Pane edge: pointer proximity chooses the nearest edge and a translucent blue half-pane preview.
- Divider: retains its resize role normally; a blue overlay during drag advertises insertion.
- Workspace edge: becomes a drop zone during drag for a new outer split.

### Interaction model
Workspace edges create outer splits; pane edges insert beside the target; dividers insert relative
to the adjacent subtree boundary. [Frontend behavior](../behavior/frontend.md#split-and-close-semantics)
owns session and persistence semantics.

## Transient Error Banner

Pane creation and moves are optimistic in the UI and then persisted. If persistence fails, the user needs immediate feedback because the visible layout can temporarily diverge from saved config.

The create/move error banner pattern:

- appears in the top-right of the workspace content area
- uses a destructive but subdued palette (`#2f1313`, `#7f1d1d`, `#fca5a5`)
- includes an explicit dismiss button
- remains separate from modal dialog errors because it belongs to an in-place interaction, not a form
- stacks vertically when both a create failure and a move failure are present at the same time

The banner is hidden until persistence rejects, and clears on dismissal or the next create/move attempt.
