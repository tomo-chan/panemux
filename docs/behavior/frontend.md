---
type: Specification
title: "Behavior: frontend runtime behavior"
description: "Browser initialization, workspace interaction, and frontend runtime behavior."
---

# Behavior: frontend runtime behavior

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Frontend Runtime Behavior

### Initial page load

```text
Browser loads SPA
  -> GET /api/layout
  -> GET /api/display
  -> validate JSON with Zod
  -> render recursive split tree
  -> each TerminalPane opens /ws/{sessionID}
```

### Terminal I/O round trip

```text
User types in xterm.js
  -> browser sends binary WebSocket frame
  -> session.Write(...)
  -> shell/SSH/tmux produces output
  -> session.Read(...)
  -> backend sends binary WebSocket frame
  -> xterm.js writes bytes to the terminal
```

During replayed reconnect output, xterm stdin is suppressed until the replay end marker is fully
applied, so terminal query responses embedded in old output are not regenerated as fresh shell
input.

When an SSH-backed pane loses its transport unexpectedly, panemux classifies that session as
`disconnected` instead of `exited`. The frontend performs one automatic recovery attempt by
recreating the session and reconnecting the pane. Deliberate remote shell termination such as
`exit` remains `exited` and shows the restart action instead of auto-reconnecting.

The same one-shot automatic recovery also triggers if the pane's WebSocket connection itself
repeatedly fails to (re)establish and exhausts its own reconnect attempt budget, even if the
backend never reported a `disconnected` status frame (e.g. during a prolonged network outage that
prevents the WebSocket handshake from completing at all). Either way, if the automatic recovery
attempt itself fails, the pane shows the manual "Reconnect Session" action instead of the
"reconnecting..." indicator forever.

### Selection and copy behavior

- Terminal text can be selected with the mouse using xterm.js standard selection behavior.
- If text is selected, `Cmd+C` or `Ctrl+C` copies the current selection instead of sending terminal input.
- If no text is selected, `Cmd+C` or `Ctrl+C` is left to normal terminal behavior, so shell interrupts still work.
- This interaction is currently validated in Chrome.

For terminal text selection, panemux keeps tmux mouse mode unchanged for `tmux` and `ssh_tmux`
panes. Plain drag therefore continues to follow tmux mouse behavior. To force browser-side xterm
selection while tmux mouse mode is active, hold `Option` during drag on macOS or `Shift` during
drag on Linux and Windows.

### Terminal link detection

- `http://` and `https://` URLs printed into a pane are auto-detected and become clickable through panemux's own xterm.js link provider (`createUrlLinkProvider` in `frontend/src/utils/terminalLinks.ts`).
- Panemux's URL pattern (`TERMINAL_URL_REGEX` in `frontend/src/hooks/useTerminal.ts`) excludes CJK and fullwidth punctuation. Trailing `。`, `、`, `・`, `…` and enclosing `（）`, `「」`, `【】`, `“”` are not part of the detected link.
- Non-ASCII *letters* are never excluded, so raw IRIs such as `https://ja.wikipedia.org/wiki/日本語` stay linkable in full. Fullwidth digits and fullwidth letters stay linkable for the same reason, as do the letters and numerals interleaved into the CJK symbols block itself (`々`, `〆`, `〇` and the ideographic numerals), so `https://ja.wikipedia.org/wiki/日々` is linked whole.
- Known limitation: kana or kanji that directly follows a URL with no delimiter (for example `https://example.com/docsを参照`) is still absorbed into the link. That case cannot be distinguished from a legitimate kana IRI path by pattern matching alone. Separate the URL with whitespace or punctuation, or emit an OSC 8 hyperlink — xterm.js resolves those itself, independently of this pattern, and its built-in handler asks for confirmation before navigating.
- A URL that does not fit the pane is linked whole, across every row it occupies, whether the terminal wrapped it or the program printed its own newline at the pane edge. The second case is the common one in a bordered TUI, in formatted CLI output, and wherever tmux redraws by line — none of which set `isWrapped`.
- Two rows are read as one line when the terminal wrapped them, or when the upper row runs all the way to the pane edge and the lower row starts with something other than a blank. A row whose last column holds the trailing half of a wide character counts as reaching the edge, since the character itself occupies it. That heuristic can join two unrelated rows that happen to meet both conditions; the alternative is a fragment that stays clickable, and a URL cut inside its hostname opens a host the operator never saw.
- When adjacent rows share Unicode box-drawing characters at the same left and right cell columns, those decorations and one optional inner padding cell per side are removed before applying the same continuation rule. The left and right decorations may differ from each other, and inner padding is not required. The characters and cell columns must match between rows; ASCII `|` is never treated as a frame. This keeps ordinary piped output out of the heuristic while allowing URLs wrapped inside TUI frames such as `│ https://… │` to remain one link.
- A match that runs to the pane edge of the last row with nothing continuing it is not offered as a link at all: whether the URL ended there or the rest is off screen is unknowable, and half a URL is worse than none.
- Known limitation: a URL broken well short of the pane edge — a program that prints `https://exam\r\nple.com/path` on an 80-column pane — is still linked as the fragment. A line that stops 60 columns early is indistinguishable from a line that simply ended there, so neither joining nor suppressing is safe.
- `#<number>` references are linked separately to the pane's GitHub pull request (see [Pane Git and PR metadata](#pane-git-and-pr-metadata)). The URL provider is registered first, so a `#123` inside a URL stays part of the URL — which requires both providers to report ranges in the same coordinate system (1-based on both axes, and mapped through cells rather than string indices), since xterm resolves the overlap by comparing the ranges themselves.

### Resize and layout updates

- `ResizeObserver` triggers terminal fit logic when pane size changes.
- The browser sends a `resize` control message with current cols/rows.
- Dragging split dividers updates layout percentages in memory.
- Divider resize remains available during normal terminal use.
- Layout persistence for resize is debounced by 500 ms before `PUT /api/workspaces/{active}/layout`.

### Persistence behavior

- layout and workspace changes are persisted immediately when a save path is available
- pane resize, split, close, quick-add create, move, workspace add/delete/rename, active-workspace changes, `tab_position` changes, and vertical workspace-bar width changes all follow the same immediate-save path
- when no `--config` is given, the default save path is `~/.config/panemux/config.yaml`; the directory is created automatically on first save
- pane maximize state is frontend-local, tracked per workspace, and restored when the user switches away from a workspace and then returns

### Layout editing and pane movement

Layout controls are always available. [Layout UI](../ui-design/layout.md) owns drag gestures,
drop targets, and persistence feedback; [workspace UI](../ui-design/workspaces.md) owns workspace
navigation and pane cards. Layout and workspace mutations persist immediately to affected configs.
Task and host Open select a destination before creating a pane ([Opening a task](task-navigation.md#opening-a-task)).

Pane movement is a re-layout operation, not a session recreation:

- moving a pane does not create a new backend session
- moving a pane does not restart the session
- only the pane's position in the layout tree changes

When a pane moves to a different parent node, the component may be remounted by React. xterm.js terminal instances survive remounting via a module-level `TerminalEntry` map keyed by session ID; the existing canvas is reattached to the new container with `appendChild` rather than `replaceChildren`, preserving React-managed sibling nodes such as overlays and restart controls.

### Split and close semantics

- Splitting a pane creates a new local pane, creates a backend session through `POST /api/sessions`, then rewrites the layout tree so the original and new panes each receive `50%` under a new split node.
- The original pane keeps its current visible terminal contents when split; it must not go blank or reset to a fresh prompt while the new sibling pane is created.
- Closing a pane calls `DELETE /api/sessions/{id}`, removes the pane from the tree, collapses parents with a single child, and renormalizes sizes to total `100`.
- Moving a pane calls the workspace layout save path only; it does not call session create or delete APIs.
- Dropping a pane on another workspace tab moves it into that workspace, inserts it at the destination workspace's right edge, persists both affected workspace layouts, and switches the active workspace to the destination.
- Dragging a pane card from the workspace summary view onto another workspace uses the same move path as dragging from the pane-header handle.

### New terminal creation

- The pane header exposes one-click `Add new pane to the right` and `Add new pane below` actions that immediately create a default `local` pane beside the current pane.
- The dialog supports two bases:
  - blank `local`
  - clone an existing pane's settings
- Before creation, the user can choose placement:
  - workspace `top`, `bottom`, `left`, or `right`
  - beside an existing pane on `top`, `bottom`, `left`, or `right`
- Creation flow:
  1. frontend builds the new `PaneConfig`
  2. frontend calls `POST /api/sessions`
  3. frontend inserts the pane into the active workspace layout
  4. frontend persists with `PUT /api/workspaces/{active}/layout`
- For cloned `tmux` and `ssh_tmux` panes, `tmux_session` is regenerated to avoid collisions.

### Pane Git and PR metadata

See [Pane Git and PR metadata](pane-metadata.md) for worktree resolution, polling and caching.
