---
type: Guide
title: "UI Design"
description: "Shared interface principles and routes to screen and interaction contracts."
---

# UI Design

Split, close, maximize, settings, new panes, workspace add/rename/delete and pane moves are
available during terminal work, without an edit mode. Screen and interaction
contracts live in [ui-design/](ui-design/index.md); runtime and persistence contracts live in
[frontend behavior](behavior/frontend.md).
## Design Principles

Three core principles guide the interactive UI:

**State Visibility** — the user must always know what action is available and what action is in progress. Pane movement, pane insertion, transient errors, and modal editing should all be visible without requiring hidden modes.

**Affordances** — visual cues should match physical intuition. Grab cursors indicate draggable elements. Edge highlights and divider overlays show where a pane can land. Buttons that create or mutate layout should stay grouped with the structure they affect.

**Feedback** — transitions between states are animated (0.15–0.2 s) to prevent abrupt jumps and to make cause-and-effect legible. A ghosted drag source, grabbing cursors, and visible alerts on failed persistence keep the UI honest when optimistic updates are used.

---
## Color Palette

| Role | Value | Usage |
|---|---|---|
| Interactive blue | `#569cd6` | drop-target highlight, selected workspace-position control |
| Drag handle blue | `#4a7ea5` | header move handle `⠿` |
| Normal header background | `#252526` | pane header background |
| Normal header border | `#333` | pane header bottom border |
| Workspace bar background | `#202124` | workspace bar surface |
| Active workspace tab | `#2f3540` | active workspace tab background |
| Position-control selected | `#3a4350` | selected top/bottom/left/right tab-position button |
| Dialog background | `#252526` | modal dialog surface |
| Dialog border | `#444` | modal dialog outline |
| Error banner background | `#2f1313` | move-persistence failure alert |
| Error banner border | `#7f1d1d` | move-persistence failure alert border |
| Error banner text | `#fca5a5` | move-persistence failure alert text |
## Document map

Task list presentation is in [Task Dashboard](ui-design/task-dashboard.md),
creation and terminal popups in [Task Actions](ui-design/task-actions.md), and host controls in
[Dashboard Hosts](ui-design/dashboard-hosts.md). Old section headings below retain their routes.
## Workspace Bar
See [Workspace Bar](ui-design/workspaces.md).
### Contents
See [Workspace Bar](ui-design/workspaces.md).
### Why keep the bar visible with one workspace
See [Workspace Bar](ui-design/workspaces.md).
### Tab position controls
See [Workspace Bar](ui-design/workspaces.md#tab-position-controls).
### Integrated workspace summaries
See [Workspace Bar](ui-design/workspaces.md#integrated-workspace-summaries).
### Workspace pane groups
See [Workspace Bar](ui-design/workspaces.md#workspace-pane-groups).
## Pane Header
See [Pane Header](ui-design/panes.md).
### Layout
See [Pane Header](ui-design/panes.md#layout).
### Multiple worktrees
See [Pane Header](ui-design/panes.md#multiple-worktrees).
### Split vs quick-add
See [Pane Header](ui-design/panes.md#split-vs-quick-add).
### Drag handle
See [Pane Header](ui-design/panes.md#drag-handle).
### Why header-only drag
See [Pane Header](ui-design/panes.md).
## Drag and Drop
See [Layout Editing](ui-design/layout.md).
### States
See [Layout Editing](ui-design/layout.md#states).
### Interaction model
See [Layout Editing](ui-design/layout.md#interaction-model).
## Modal Dialogs
See [Modal Dialogs](ui-design/dialogs.md).
### Keyboard behaviour, shared by every modal
See [Modal Dialogs](ui-design/dialogs.md#keyboard-behaviour-shared-by-every-modal).
### Open in workspace
See [Modal Dialogs](ui-design/dialogs.md#open-in-workspace).
### Confirmation dialog
See [Modal Dialogs](ui-design/dialogs.md#confirmation-dialog).
## Transient Error Banner
See [Layout Editing](ui-design/layout.md).
### Error state lifecycle
See [Layout Editing](ui-design/layout.md).
## Pane URL Strip
See [Pane URLs and Split Preview](ui-design/preview.md).
## Split Preview
See [Pane URLs and Split Preview](ui-design/preview.md).
## Attention Indicators
See [Attention Indicators](ui-design/attention.md).
## Task Dashboard
See [Task Dashboard](ui-design/task-dashboard.md).
### Labels used before
See [Task Actions](ui-design/task-actions.md#labels-used-before).
### Working directory suggestions
See [Task Actions](ui-design/task-actions.md#working-directory-suggestions).
## Agent Board UI
See [Agent Board UI](ui-design/agent-board.md).
### Dashboard
See [Agent Board UI](ui-design/agent-board.md#dashboard).
### Command center
See [Agent Board UI](ui-design/agent-board.md#command-center).
### agmsg in the host menu
See [Agent Board UI](ui-design/agent-board.md#agmsg-in-the-host-menu).
