---
type: Specification
title: "Task Actions"
description: "Task creation, details, labels, directory suggestions, and terminal popups."
---

# Task Actions

[Dashboard presentation](task-dashboard.md) defines navigation and lists;
[task behavior](../behavior/tasks.md) owns collection, capabilities, and execution contracts.

- **New task.** A modal form over the dashboard: Host (every host on the board, an unreachable one
  marked "(unreachable)"), Working directory, Agent (the registered agents that declare `launch`,
  by ID: `claude`, `codex`), Labels (comma-separated, optional) and First instruction. Under Agent
  goes the chosen agent's launch note from the server — with `codex`, that the labels are recorded
  once codex has started its session, and to open the task's pane if codex stops at a start-up
  screen — and, when the chosen host's probe found the agent's CLI missing, "<agent> cannot be
  started on <host>: <why>." with `Start` disabled. Under Labels, `Used before:` offers the labels used before
  ([Labels used before](task-actions.md#labels-used-before)) as toggle tags: one in the field shows solid with `✓`
  (`aria-pressed` true), one not dashed with `+`. A click adds the label at the end of the field,
  replacing what is typed after the last comma unless that is a label used before in full (shown
  `✓`, so kept), and ending with `, `; on a `✓` tag it takes the label out and keeps every other. The text after
  the last comma filters the `+` tags; the `✓` tags always show, filtered or folded. The working directory takes focus when it
  opens, without opening its list ([Working directory suggestions](task-actions.md#working-directory-suggestions)),
  so `Escape` still closes the form at once; changing Host empties it. An empty directory, a relative one, or an empty instruction is refused in the form, with
  the reason under the fields; anything the server or the host refuses is shown there as
  "Could not start: …", and the form keeps what was typed. While the task starts, `Start` reads
  `Starting…`, and neither it, `Cancel`, `Escape` nor a click outside dismisses the form. Once it has
  started the form closes; a blue notice names the tmux session and host and says selection waits
  for the agent to start. Selection follows [Starting a task](../behavior/tasks.md#starting-a-task), without opening a pane.
  For codex the notice also explains pending labels; failures to record labels appear in red.
- **Detail panel.** Fixed to the window height at the right. Its head — state, host, agent, start,
  title, waiting reason, `Type in pane` (in the interactive blue) or, for a waiting or idle task
  that cannot take it, "Type in pane is not available: <reason>.", the open action or the reason
  there is none (a stopped task whose agent resumes offers `Resume` instead, and says why when it is
  disabled; a task of an unknown agent says it offers only the pane: its pane actions stay, while
  `Resume`, `Summarize`, `Mark done` and labels are not shown), `Input given` for a codex
  task waiting on a permission request its hook recorded, `Mark done` or `Mark not done`, and for a done candidate the line "The summary
  finds no work left: a candidate for Mark done." — stays in place, and only the body below scrolls:
  state notes from `state_note`; **Work** with summary, numbered remaining work (or "No remaining
  work found."), summary status and controls; available repository, branch, PR, issue and reference
  links; labels; task → agent → tmux → pane → workspace; directory and session ID.
  Issue links say "closed by the pull request", References say "from the branch name or pull request
  title". Work shows `Summarizing…`, failure reasons, outdated/unreadable/off states (naming
  `task_dashboard.summary.enabled`), and `Summarize` / `Summarize again`. Unreadable logs disable the
  button with an explanation; changed logs re-enable it and show "The conversation has changed since
  it could not be read." [Summaries](../behavior/tasks.md#summaries) owns eligibility, automatic
  requests and retry rules; [Codex sessions](../behavior/tasks.md#codex-sessions) owns state notes.
  `Mark done` asks first, inside the panel, and says where the task will be afterwards: a running
  task stays in its column until it stops; a stopped one moves to Done, and the question adds that
  the column is hidden until `Show done` is checked when it is. `Mark not done` does not ask.
  `Input given` does not ask either: the task shows as its turn reads (usually Working) at once, also
  while the task event stream still has the wait, and a failure is shown under the actions as
  "Could not mark the input given: <reason>" ([behavior](../behavior/tasks.md#codex-permission-requests)). Labels
  are removed with the `×` on each and added with a text box and `Add`. Under the box, `Used before:`
  offers the labels used before that the task does not have
  ([Labels used before](task-actions.md#labels-used-before)); a click adds one at once, as `Add` does, and what is
  typed in the box filters them. When the task has all of them it says "Every label used before is on
  this task." A save that fails shows its
  reason in the head and keeps what was typed. A save belongs to the task it was made for: selecting
  another task while it runs leaves that task's controls enabled, and the result is not shown there.
  A task without a session ID offers neither, and says so. At 1000px and narrower it slides over the board
  with a close button.
- **Summary agent.** A task whose agent declares `summary` (Claude and Codex) offers the same summary
  controls. Each uses its own agent account on the panemux host. Where the server says the summary is
  unavailable (no session ID yet, no conversation log found), `Summarize` is disabled and Work says
  why; an agent that does not summarize shows no control at all.
- **Resume.** `Resume` reads `Resuming…` and is disabled while its request runs; a refusal is shown in
  a red alert naming the task, and a resumed task is selected. Nothing else on the board waits for it.
- **Type in pane.** A popup over the board ([behavior](../behavior/tasks.md#opening-a-task)), centered,
  up to 1100×760 with a 24px margin, over a dark backdrop; a click on the backdrop does not close it,
  since a drag selecting terminal text can end there. Everything behind it — the top bar, filters,
  board, detail panel — is `inert`. The header has the task's state pill and title, a connection
  chip (`Connecting` grey, `Connected` green, `Disconnected` gold, `Failed` red), then `Go to pane`
  (or `Open`), the maximize toggle and `Close`; under them host · agent · `tmux: <session>` in the
  monospace face. Notes sit between the header and the terminal: the task having moved on the board
  or no longer being listed, a workspace pane showing the same session (and which client sizes the
  window), and a red "… Input is not being sent." with `Retry` or `Reconnect`. The terminal is
  dimmed while it takes no input; before the attach answers its place says which tmux session it is
  connecting to and that keys are not sent yet. A footer repeats the keys and that closing leaves
  the agent and its tmux session running.
  - **Keys.** While the terminal has focus every key is the terminal's, `Escape` and `Tab` included
    (`Escape` is claude's interrupt and codex's cancel). `Cmd/Ctrl+Shift+Esc` closes and
    `Cmd/Ctrl+Shift+Enter` maximizes from anywhere in the popup; both are captured before the
    terminal sees them. With focus in the header, `Escape` closes and `Tab` / `Shift+Tab` cycle
    within the popup. Focus starts on the popup and moves into the terminal each time it connects;
    when the terminal stops taking input, focus stays in the popup.
  - **Maximize.** The toggle, a double-click on the header (not on its buttons) or
    `Cmd/Ctrl+Shift+Enter` makes the popup cover everything below the top bar, which stays in view
    and inert. Only the terminal's size changes — its connection, input target and focus do not —
    and restoring brings back the board exactly as it was. The choice is remembered in the browser
    (`localStorage`) and the next popup opens the same way.
  - **Narrow screens** (720px and narrower). The popup is a full-window sheet from the start, with no
    maximize toggle, and its header and `Close` stay in view.
- **Host terminal.** `Type in pane` from the host connection menu opens the same popup on a host
  terminal: the header has the connection type as its pill and the host as its title, the
  connection chip, the maximize toggle and `Close`, and `tmux: <session>` under them for
  `ssh_tmux`. It has no `Go to pane` / `Open`, and its keys and focus are the task popup's
  (`useTerminalPopupKeyboard`).
- **After opening.** The dashboard closes, the pane takes focus, and it is outlined in the
  interactive blue for about two seconds (a steady outline with reduced motion).

### Labels used before

The labels the record file holds (`known_labels`,
[Done and labels](../behavior/tasks.md#done-and-labels)) are offered as tags in a `Used before:` row
under both label inputs — New task's Labels field and the detail panel's `Add a label` box — rather
than as a dropdown, so they can be seen and picked without typing. The label filter does not offer
them.

- They are in case-insensitive alphabetical order, each in its label's color.
- What is typed filters them by a case-insensitive part of the label; an empty field shows them all.
  When no label used before matches, entered or not, the row says so ("No label used before contains “…”.") and what was typed is
  still added as a new label.
- More than eight after filtering are folded: the first eight show with `+<rest> more`, which shows
  them all and becomes `Show less` (`aria-expanded`, `aria-controls` the row). Eight or fewer offer
  no button. Eight keeps the detail panel's row to about two lines at its 360px width.
- Every tag and the fold button are buttons, reached with Tab and pressed with Enter or Space.
- None are offered when there are none, or while the record file cannot be read; the inputs work as
  before.

### Working directory suggestions

New task's Working directory is a combobox: under the field, a list headed `Recent on <host>`
offers the directories the chosen host's tasks on the board ran in
([Starting a task](../behavior/tasks.md#starting-a-task)), one per row — the path in monospace, then
the agent and when it was last used (`claude · 2h ago`, `codex · now` for a running task). Running
tasks' directories come first, then the rest, most recently used first.

- A click in the field, the `▾` button beside it, `↓`/`↑`, or typing opens the list; the focus the
  form gives the field on opening does not. Typing filters the rows by a case-insensitive part of the
  path and marks the matching part in the accent color.
- `↓`/`↑` move the highlighted row (wrapping), `Enter` puts its path in the field and closes the
  list — with no row highlighted `Enter` submits the form as before — and `Escape` closes only the
  list; a second `Escape` closes the form. A click on a row does what `Enter` does. Leaving the
  field closes the list; `▾` moves the focus into the field, so this holds for a list it opened
  from another field too. Keys that belong to an IME composition (`Enter` confirming a conversion)
  are left to the IME. The highlighted row follows its directory when a board update reorders the
  rows.
- The input has `role="combobox"`, `aria-autocomplete="list"`, `aria-expanded`, `aria-controls` and
  `aria-activedescendant`; `aria-expanded` is true whenever the list is shown, and `aria-controls`
  names the `listbox` — named by its heading, each row an `option` with `aria-selected` — or, when it
  has no row, the message below. The `▾` button is skipped by Tab.
- When nothing matches what is typed the list says "No recent directory matches. Starting will use
  the path as typed."; when the host has no task with a directory, "No directory used on this host
  yet. Type an absolute path."
- Nothing is stored, so there is no way to remove a row, and no limit beyond the tasks on the board;
  the list scrolls past about seven rows.
