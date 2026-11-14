---
type: Specification
title: "Behavior: agent attention notifications"
description: "Agent attention notifications and their relationship to task state and browser visibility."
---

# Behavior: agent attention notifications

> Part of the [behavior specification](../behavior.md).

## Agent Attention Notifications

The [task event stream](task-events.md#task-events) observes agent state, rather than searching
terminal output for prompts. Codex command approval waits require the host's
[permission hook](task-codex-permissions.md#codex-permission-requests).

The receiver contract owns attention and notification eligibility:

- [Pane and workspace attention](task-events.md#pane-and-workspace-attention): task matching,
  host health, focus/click clearing and per-tab persistence of cleared waits.
- [Browser notifications](task-events.md#browser-notifications): visibility, notification permission,
  `wait_id` deduplication, snapshot/reconnect eligibility, storage limits, contents and click navigation.
- [Store and connection](task-events.md#store-and-connection): one connection per tab, including
  while the page is hidden.

If notification permission is undecided, the browser asks on the first pointer or key interaction,
instead of waiting for the first wait.

Pane-header Git/PR resolution, transcript reuse and workspace-summary polling are specified in
[Pane Git and PR metadata](pane-metadata.md). Terminal selection modifiers are specified in
[Selection and copy behavior](frontend.md#selection-and-copy-behavior).
