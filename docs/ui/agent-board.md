---
type: Specification
title: "Agent Board UI"
description: "Agent Board dashboard overlay, command center palette and history, and agmsg host menu."
---

# Agent Board UI

Agent Board reuses the shared modal, overlay-panel, color, status, and focus-restoration patterns
([UI Overview](overview.md)).

## Dashboard

![The Agent Board dashboard as a right-side overlay on a workspace, listing two panes' status cards and their latest messages.](../images/agent-board.png)

- Opens from the Agent Board button or `Cmd/Ctrl+Shift+B`; both entry points are absent when the
  capability is disabled.
- Appears as a right-side overlay and closes via button, backdrop, or `Escape`.
- Lists panes with a detected agent or a status report; idle shells stay hidden until reporting. A
  detected pane without a report shows `agent detected`, which does not claim a successful join.
  Pane Settings exposes no board controls (global enablement lives in YAML and legacy `mode` is
  ignored).
- Displays pane ID, title, state, summary, last tool, and report age. Self-reported repository,
  branch, and PR values are intentionally omitted because the pane header already displays
  panemux-derived values.
- Treats state as free text and maps known states to the status palette: `working` green, `idle`
  blue, `waiting` attention gold, and everything else neutral.
- Keeps identity and metadata on one line; summaries may wrap to four lines.
- Marks reports older than five minutes as `stale` and dims rather than hides them.

## Command center

- `Cmd/Ctrl+Shift+K` opens a focused command palette. Plain `Cmd/Ctrl+K` remains free for standard
  shell and readline bindings.
- Each prompt creates one turn containing the prompt, streamed output, progress state, and any
  inline error. A failed turn does not close or disable the palette.
- Command history opens as a right-side panel for longer reading alongside terminal work.
- Command-center entry points are absent when the capability is disabled.
- Global shortcuts register in capture phase so terminal focus does not swallow them. Closing any
  Agent Board surface restores the previously focused element. Full data-flow rules live in
  [Command center](../agent-board/command-center.md#command-center).

## agmsg in the host menu

When Agent Board is globally enabled, `HostTools` lists agmsg via `HostToolItem` beneath the host
menu actions. It shows `Not installed` / `Install`, older release / `Update`, `Installed` with
version, newer release warning (kept unchanged), `Cannot install` with setup reasons, or
`Not checked`.

- `Install` / `Update` explains that the tested installer can write outside the skill directory and
  that existing agent scripts keep running. Errors preserve installer exit/output details; success
  asks agents to restart and explains that panemux participates from its next poll.
- **Update automatically (all hosts)** edits the single global preference in every host menu,
  including hosts with newer or unconfirmed copies. Consent stays visible beside it even when no
  manual action is offered, explaining unattended execution and writes outside the skill directory.
- During automatic work the menu asks to reopen for the result; automatic failures remain visible
  with manual `Update` available for retry, and success reports `Updated automatically` with restart
  guidance.

![agmsg manual Update in the host menu](../images/agmsg-host-tool.png)
