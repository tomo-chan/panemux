---
type: Decision
title: "Type in pane: the popup over the board (2026-10-02, issue #284)"
description: "Reuse terminal behavior in a focused popup while leaving Escape and Tab with the active agent terminal."
---

# Type in pane: the popup over the board (2026-10-02, issue #284)

Issue [#280](https://github.com/tomo-chan/panemux/issues/280) chose a popup (with maximize) over
expanding the task in place: a wide terminal and an unambiguous input target were worth more than
seeing other tasks while answering. #284 builds it on #283's attach. Decided:

- **`useTerminal` directly, not `TerminalPane`.** `TerminalPane` brings a pane's header, status bar,
  layout context and drag handling, none of which a popup has. A small `TaskTerminal` uses
  `useTerminal` — so IME, paste, copy and scrollback are a pane's — and `useTerminal` gained one
  option, `recoverOnDisconnect`, because its recovery calls `/api/sessions/{id}/restart`, which knows
  only panes and answers `404` for a board attach. A shared terminal surface extracted from
  `TerminalPane` was rejected as a refactor of the pane for no behavior the popup needs.
- **Retry and Reconnect ask for the attach again.** An exited client is replaced only by a new
  `POST`, and one that is still alive comes back as the same session; either way the popup reads it
  over a new WebSocket. Nothing is shown as sent while the terminal is not connected: it is inert,
  not merely dimmed.
- **Escape belongs to the terminal.** claude interrupts and codex cancels with it, so the popup
  closes with `Close`, `Cmd/Ctrl+Shift+Esc`, or `Escape` only from the header. For the same reason
  the popup's own focus trap leaves `Tab` to a focused terminal, which the shared
  `useModalKeyboard` would have taken.
- **The maximized state is kept in `localStorage`.** It is a per-browser display preference;
  `config.yaml` would have needed a backend change for a choice that does not belong to the server.
- **English labels.** The design in #280 was written in Japanese (`Paneで入力`, `閉じる`, …); the
  dashboard's UI is English throughout, so the popup's are `Type in pane`, `Close`, `Connecting…`,
  `Typing` and so on.

Current contracts: [tasks guide](../../behavior/tasks.md).
