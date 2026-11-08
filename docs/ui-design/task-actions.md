---
type: Specification
title: "Task Actions"
description: "Task creation, details, labels, directory suggestions, and terminal popups."
---

# Task Actions

[Dashboard presentation](task-dashboard.md) defines navigation and lists;
[task behavior](../behavior/tasks.md) owns collection, capabilities, and execution contracts.

## Starting and selecting

[Starting a task](../behavior/tasks.md#starting-a-task) owns launch fields, eligibility, validation,
label persistence and execution. The modal shows Host (unreachable hosts marked "(unreachable)"), Working directory,
Agent, optional comma-separated Labels and First instruction. Show the server's launch note and
unavailable-CLI reason below Agent; disable Start when unavailable. Focus Working directory without
opening suggestions; changing Host clears it. Empty/relative directories and empty instructions show
field errors below fields; server/host failures show "Could not start: …"
while retaining input. While starting, show `Starting…` and disable all dismissal.

On success, close the form and show a blue notice naming host and tmux session, explaining selection
once the agent starts and Codex's pending labels. A collection selects the launched task unless the
operator selected another task meanwhile; manual selection cancels auto-selection and the notice.
Codex is selected as its process first, with the notice remaining, then as its session when listed,
which clears the notice. No pane opens. Failure to record a started task's labels is a red alert.

## Details and actions

The right panel fills the window height; at 1000px and narrower it slides over the board with Close.
Its fixed head holds state, host, agent, start, title, wait reason and actions; only the body scrolls.
Use the server's capabilities/reasons for Type in pane, Open / Go to pane, Resume, Input given,
Mark done / Mark not done, summaries and labels. Unknown agents retain pane actions only.
[Task behavior](../behavior/tasks.md#opening-a-task), [resuming](../behavior/tasks.md#resuming-a-task),
[permission input](../behavior/tasks.md#codex-permission-requests), and [Done and labels](../behavior/tasks.md#done-and-labels)
own availability, effects and record identity.

- Type in pane is interactive blue; an unavailable waiting/idle task says "Type in pane is not
  available: <reason>." A done candidate says "The summary finds no work left: a candidate for Mark done."
- Body: server state notes, **Work**, links, labels, task → agent → tmux → pane → workspace,
  directory and session ID. Show only available link rows; issue links say "closed by the pull
  request", References "from the branch name or pull request title".
- Work shows summary and numbered remaining work, or "No remaining work found." It shows
  `Summarizing…`, failures, outdated/unreadable states, off (naming `task_dashboard.summary.enabled`),
  or "Summarized when it stops working." while busy without a summary. Offer Summarize / Summarize
  again for no current summary or a failed one; show refused requests below it. Unreadable logs
  keep a disabled button with a tooltip saying the log cannot be read; after a change, enable it
  and say "The conversation has changed since it could not be read." Missing session/log disables
  summaries with a reason; agents without summary capability show no control.
  [Summaries](../behavior/tasks.md#summaries) owns scheduling, accounts and retries;
  [Codex sessions](../behavior/tasks.md#codex-sessions) owns state-note meanings.
- Mark done asks inline, explaining the destination column and whether Show done hides it.
  Mark not done and Input given do not ask. Failed input confirmation appears under the actions
  as "Could not mark the input given: <reason>".
- Labels use × to remove, a text box and Add, plus [Used before](#labels-used-before) tags that add
  immediately and filter with input. When exhausted: "Every label used before is on this task."
  No session ID means no label/done controls, with an explanation. Failed saves retain input and
  show their reason in the head, only on the task saved; another selection remains enabled.
- Resume reads `Resuming…` and disables only that task's action. Failures are red and name the task;
  success selects the resumed task.

## Terminal popups

[Opening a task](../behavior/tasks.md#opening-a-task) owns Type in pane eligibility, attach/input
states, status notes, closing and focus restoration. [Opening a host](../behavior/tasks.md#opening-a-host)
owns the host-terminal variant. Both use this presentation:

- Centered dark backdrop; at most 1100×760 with 24px margins. Backdrop clicks do not close the
  popup because terminal selection can end there. Everything behind it is `inert`.
- Header: task state pill and title, connection chip (`Connecting` grey, `Connected` green,
  `Disconnected` gold, `Failed` red), `Go to pane` / `Open`, maximize and `Close`; below it,
  host · agent · `tmux: <session>` in monospace. Host terminals use connection type and host
  as pill/title, show tmux only for `ssh_tmux`, and omit `Go to pane` / `Open`.
- Runtime notes sit between header and terminal; errors are red. Before attachment, the terminal
  area names the session and says keys are not sent yet. The footer repeats keys and explains
  that closing leaves the agent and its tmux session running.
- Terminal focus keeps every key, including Tab and Escape. With header focus, Tab / Shift+Tab
  cycle inside the popup and Escape closes. Focus starts on the popup, enters the terminal on
  connection and stays in the popup if input stops.
- The maximize button, header double-click outside buttons, and captured `Cmd/Ctrl+Shift+Enter`
  toggle coverage of everything below the inert top bar. Only terminal size changes; connection,
  input target and focus persist. Restore returns the board unchanged; localStorage remembers
  the choice. Captured `Cmd/Ctrl+Shift+Esc` closes from anywhere.
- At 720px and narrower, it is a full-window sheet with no maximize toggle; header and Close
  remain visible.
- After Open or Go to pane, the destination pane has a blue outline for about two seconds,
  steady with reduced motion.

## Labels used before

The labels the record file holds (`known_labels`,
[Done and labels](../behavior/tasks.md#done-and-labels)) are offered as tags in a `Used before:` row
under both label inputs — New task's Labels field and the detail panel's `Add a label` box — rather
than as a dropdown, so they can be seen and picked without typing. The label filter does not offer
them.

In New task, selected tags are solid `✓` with `aria-pressed=true`, others dashed `+`. Clicking +
replaces the text after the last comma unless it is already a complete used label, adds the label
and ends with `, `; clicking ✓ removes only that label. The last fragment filters + tags; ✓ tags
remain visible even when filtered or folded.

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

## Working directory suggestions

The Working directory combobox lists `Recent on <host>`: monospace path, agent and last-used time
(`claude · 2h ago`, `codex · now`). [Starting a task](../behavior/tasks.md#starting-a-task) owns the
source, ordering and lifetime of suggestions.

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
- The list scrolls past about seven rows; it has no remove-row control.
