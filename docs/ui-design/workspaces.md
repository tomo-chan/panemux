---
type: Specification
title: "Workspace Bar"
description: "Workspace navigation, status groups, bar positioning, and cross-workspace drag sources."
---

# Workspace Bar

The workspace bar is always visible, including with one workspace. It contains `← Tasks`
([Task Dashboard](task-dashboard.md)), workspace tabs with summaries and inline rename/delete,
`+` to create a workspace, and bar-position controls.

## Tab position controls

Top/bottom/left/right buttons use `aria-pressed` on the active position.

The four direction buttons form one horizontal tab-like cluster, including on left/right bars.

The position cluster sits on the opposite end of the workspace tabs:

- top/bottom bars: tabs stay at the leading side, position controls stay at the far trailing side
- left/right bars: tabs stay near the top, position controls stay at the bottom

For left/right bars, the bar width is user-resizable by dragging its inner edge. The chosen width is shared across workspaces and restored on reload, while the pane layout inside the remaining work area continues to use its own saved percentage splits.

## Integrated workspace summaries
Each tab shows a one-line count of panes, connected, disconnected, exited, and pending sessions,
plus a compact pane-name strip. Pending means session data has not arrived yet.

## Workspace pane groups

Each workspace renders as a grouped strip anchored to its tab surface. The pane cards are not an independent dashboard panel; they are visually and interactively subordinate to the workspace tab they belong to.

Presentation depends on bar position:

- top/bottom bars: pane cards appear as a hover/focus overlay attached to the tab
- left/right bars: pane cards stay expanded inline under the tab

When the bar is on the left or right, the tab list and inline pane groups live in a dedicated scroll region. The fixed footer below them keeps the `+` action and all four tab-position buttons visible even when the workspace list is taller than the viewport.

Each group shows its workspace title and aggregate counts, then one card per pane: type,
connection state, available repository/branch/PR, attention badges, and a stronger selection
treatment for the focused pane.

Clicking a pane card switches to that workspace if needed, focuses the corresponding pane, and clears pane/workspace attention. This makes the summary surface a status-driven navigator rather than a passive label.

Pane cards are also drag sources for cross-workspace moves. The drag gesture starts from the overview card itself, not from the pane body, so terminal interaction and layout interaction remain separate.
