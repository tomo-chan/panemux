---
type: Specification
title: "Task Dashboard"
description: "Dashboard layers, filtering, list views, cards, and state colors."
---

# Task Dashboard

![The task dashboard layer: the top bar with host chips and its buttons, the filter row, one column per state with session cards, and the detail panel of the selected waiting session on the right.](../images/task-dashboard.png)

The task dashboard ([behavior](../behavior/tasks.md)) is a second layer over the workspaces rather than
an overlay panel: it covers the whole window, and the workspaces stay mounted beneath it so every
terminal keeps its connection and scrollback. While it is shown the workspace layer is `inert`, so a
terminal that had focus cannot receive what is typed into the dashboard.

- **Switching layers.** The workspace bar starts with a `← Tasks` button, which carries the number
  of tasks waiting for input from the last collection in a gold badge; its accessible name says the
  count is "when last checked", since nothing collects while the workspaces are shown. The dashboard's top bar has a
  `Workspaces` button back, which shows the shortcut beside its label. `Cmd/Ctrl+Shift+S` switches
  either way; the letter is `display.task_dashboard_shortcut`. Like the palette's and the board's
  shortcuts it is registered on the keydown capture phase, so it fires while a terminal pane has
  focus. panemux opens on the workspaces. While the dashboard is shown, `Cmd/Ctrl+Shift+K` and
  `Cmd/Ctrl+Shift+B` do nothing, and the palette, history panel and Agent Board close when it
  appears: they belong to the workspace layer, which is inert then.
- **Top bar.** The title, one chip per host (`Local` for the panemux host) with its running count, a
  red chip with the error and a `Reconnect` button for a host that failed, and `connecting…` for a
  host whose connection is still coming up; then, at the right, the actions as one group: a warning
  `⚠ <n> unreadable` while any host has a state file it could not read, when the board was last
  updated, `Refresh`, `Hosts…`, `New task` (in the interactive blue), and `Workspaces`. A long host
  error is truncated in the chip and shown in full as its tooltip. With more hosts than fit on one
  line, the actions wrap to the next line as a whole, flush right, rather than one by one; only a
  screen too narrow for the group on a line of its own wraps it inside, so no action is cut off.
- **Filter bar.** Text filter over the card title, directory, branch, PR number, reference (such as `JIRA-123`), session ID,
  tmux session name and summary text, matching any part of one ignoring case. The summary text matches whether the card
  shows it or, for a task waiting for input, only the details do; it stays searchable while a newer summary is pending
  or it is outdated, and a summary that arrives re-filters the board at once. Every session the dashboard starts is
  named `task-<8 hex>`, so `task` matches all of them, and a short number can match the hex of one. Group splits by none,
  host, label, or repository; a host filter; a label filter listing every label on the board; and a `Show done`
  checkbox, off by default. Search text, host, label, Group and Show done settings stay in effect
  when switching to any workspace and back, for the lifetime of the page in that browser tab.
  Group, Cards/Table and table sorting are restored after reload from versioned, Zod-validated
  browser storage; without valid storage they start at Cards / None / State ascending. Reloading starts with empty search, All hosts and labels and Show done off.
  A selected host or label missing from the current list displays and filters as All without
  discarding the choice; it takes effect again when that host or label returns, including after
  loading or collection failures. Opening a task from a notification clears hiding filters, and
  the cleared settings carry over to the next round trip. Task selection, the detail panel,
  popups and scroll position are reset when leaving the dashboard.
- **Cards / Table.** Native pressed-state buttons change only the list. Selection, details and the
  Type in pane popup keep their lifecycle; each list retains an independent scroll position.
  Table uses a single semantic table with Session (title and short summary or wait reason), State,
  Elapsed, Host, Agent display name, Repository and PR link, Branch, Labels and Actions. A Session
  button selects details; links and action buttons do not select the row. Done and label editing
  remain in details. Shared action capability checks and running indicators apply to both views.
  Group headings span all nine columns in separate row groups, retaining the existing group order.
  Each sortable column has a native button; only the selected heading has `aria-sort` and an arrow,
  with the direction in its accessible name. New keys start ascending, except Elapsed (longest first);
  repeated presses reverse direction. Sorting stays within groups and does not reorder cards.
  One focusable horizontal scroll region holds the entire table, with sticky column headers.
  Narrow screens retain the existing responsive details panel and scroll the table horizontally.
  Collection updates retain keyed task focus; each notification jump focuses and scrolls the active
  list once. A view change during the highlight keeps focus on the view switch and does not restart it.

  ![Task table with independent sortable columns and the shared details panel.](../images/task-dashboard-table.png)
- **Kanban.** Columns in the order a person should look at them: Waiting for input (highlighted in
  gold, "Needs you"), Working, Idle, Running / unknown, Stopped, and Done when the checkbox asks for
  it. A task marked done sits in Done only while it is stopped, so with the column hidden a finished
  task leaves the board rather than crowding Stopped; one that runs again stays in its state's
  column. Column headers stay visible while the board scrolls; the board is a tab stop of its own
  ("Task board") so the keyboard can scroll it sideways even with no card in it. Host, Label and
  Repository create full-width group rows: one group heading and count, followed by all state
  headings with group-local counts and equal-height column bodies. Empty columns retain `None`;
  the tallest card stack sets each row's height, without clipping or individual scroll areas.
  State titles align along the top. The group name and state headings form one sticky band,
  replaced by the next group's band. Notification jumps reveal their card below the active band.
  All rows share column widths and one horizontal scroll position, including below 560 px and
  beside details; None retains the single board and its narrow-screen vertical columns.
  Groups are derived after filtering and Done exclusion; empty groups disappear, and an empty
  grouped board says `No tasks`. The catch-all row ("Not in a Git repository", "No label") is last. Split by label, a
  task with two labels appears in both rows. A catch-all row is keyed apart from every name, so a
  label or repository that happens to be called "No label" gets a row of its own. Local and an SSH
  connection named Local remain separate, with the latter headed `Local (SSH)`.
- **Cards.** A left border in the state's color; host, agent and how long the task has been in its
  state; the working directory's last segment as the title with the full directory under it; for a
  waiting task its reason and "open the pane to respond", and for any other task its summary, two
  lines at most, dimmed while outdated; `Next: <first remaining item> · <n> left` when the summary
  lists work remaining; repository, branch, and the PR, issue and reference links (`PR #87`,
  `Issue #252`, `JIRA-123`; each opens a new tab; an issue in another repository than the PR's is
  `owner/name#9`); its labels as colored tags, each label always the same color; a green `Done` tag
  in the meta line for a task marked done that is running again, or a dashed green `Done?` tag for a
  done candidate — a task not marked done whose summary finds no work left; and at the bottom where
  the task runs with its `Open` / `Go to pane` button, or `Resume` for a stopped task whose agent
  resumes (disabled when the server says it cannot be resumed). The meta line names the agent, as
  "<agent> (unknown agent)" when the registry does not know it. A waiting or idle task in an attachable tmux session also has `Type in pane` before it, and
  a blue `Typing` tag in the meta line while its popup is open. A task that cannot be opened shows why instead of a button: for an agent outside tmux, "not
  in a panemux pane" or, when the pane it names is in no workspace, "its pane is in no workspace".
  The card is a pointer target for selection, and its title is a button, so the card never nests its
  links and buttons inside another interactive element.

[Task Actions](task-actions.md) defines creation, details and terminal popups;
[Dashboard Hosts](dashboard-hosts.md) defines host menus, tools and host editing.

State colors:

| State | Color |
|---|---|
| Waiting for input | `#e2b86b` |
| Working | `#4ec9b0` |
| Idle, Running | `#8fa6c4` |
| Unknown | `#c586c0` |
| Stopped | `#80858d` |
| Done | `#7fae6a` |

Label colors come from the mock's palette (`#569cd6`, `#4ec9b0`, `#9cdcfe`, `#d7a26b`, `#b48ead`,
`#c678dd`, `#8a9199`, `#e06c6c`), picked by a hash of the label so a label keeps its color.
