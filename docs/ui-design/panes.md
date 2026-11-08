---
type: Specification
title: "Pane Header"
description: "Pane chrome, Git links, worktree menus, and split and quick-add affordances."
---

# Pane Header

## Layout
Left to right: `⠿` drag handle, connection dot, pane type, optional title, optional Git metadata
and PR link, reconnecting text, then right-aligned actions. Chrome uses the shared header palette.
Repository and `#<number>` PR shortcuts are inline text links, with stronger blue and an underline
on hover, without pills or outlines. [Frontend behavior](../behavior/frontend.md#pane-git-and-pr-metadata)
owns metadata resolution.

## Multiple worktrees

When the pane's active agent has diverged into exactly one sibling worktree, the header shows that worktree's repo, branch, and PR shortcut inline, exactly as described above.

When two or more distinct worktrees are active at once (for example, several Claude Task subagents each working in a different sibling worktree), the header does not try to fit every branch and PR shortcut inline — the compact header would not have room and would become unreadable. Instead the header shows a single inline text trigger labeled `<N> worktrees` (e.g. `2 worktrees`), styled like the other inline text links rather than as a pill or badge. Clicking the trigger opens a small popover menu anchored below the header, listing every active worktree as its own `repo ⎇ branch #<number>` line using the same inline-link treatment as the single-worktree case. The menu closes on Escape or on clicking outside it.

## Split vs quick-add
Split inherits the current configuration; quick-add creates a default `local` pane to the right
or below. Both sit at the pane boundary and use monochrome SVG pane-outline icons; quick-add has
a plus marker to distinguish fresh context from splitting the current context.

## Drag handle

Pane movement starts only from the `⠿` handle in the header. The terminal body remains available for selection, focus, mouse reporting, and shell input.

The handle uses `#4a7ea5` and a `grab` cursor. While dragging it switches to `grabbing`, matching editor chrome that treats pane movement as a direct manipulation gesture.
