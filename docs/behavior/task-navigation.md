---
type: Specification
title: "Task navigation and terminals"
description: "Task locations, pane navigation, temporary terminals, and dashboard view-state behavior."
---

# Task navigation and terminals

Part of the [task dashboard](tasks.md).

## Where a task runs

| `location.kind` | Meaning |
|---|---|
| `tmux` | The agent's process, or one of its ancestors, is a tmux pane's process; `tmux_session` names the session |
| `outside` | The agent is running, but not under tmux |
| `daemon` | A codex session run by codex's shared daemon; which pane shows it cannot be told ([Codex sessions](task-collection.md#codex-sessions)) |
| `none` | No process is running for the task |

`attachable` is true when a `tmux` / `ssh_tmux` pane can attach to `tmux_session`, whose name must
match the pane's tmux session name rule (`^[a-zA-Z0-9_.-]+$`).

`pane_id` is present only for `outside`: the pane the agent was started from, as its environment
names it ([below](#the-pane-of-an-agent-outside-tmux)).

The pane that belongs to a task is found in the browser from the current workspaces:

- In tmux: a `tmux` pane for a task on the panemux host, or an `ssh_tmux` pane on the task's
  connection, whose `tmux_session` is the task's tmux session.
- Outside tmux: the pane whose ID is `pane_id`, and only when it is a `local` pane for a task on the
  panemux host or an `ssh` pane on the task's connection. Any other pane with that ID is not the
  task's pane.

## The pane of an agent outside tmux

- `local` and `ssh` panes start their shell with `PANEMUX_PANE_ID` set to the pane's ID, whatever
  `url_open.browser_shim` says. Every process started from that shell inherits it, including one
  that is later detached from the shell.
- Only a pane ID matching `^[A-Za-z0-9_.-]{1,128}$` is set; a pane with any other ID gets no
  variable, and its agents cannot be found from the dashboard. IDs panemux generates
  (`pane-<time>-<random>`) always match.
- A `PANEMUX_PANE_ID` panemux itself inherited (panemux run inside one of its own panes) is removed
  from every local pane, so a pane never carries another pane's ID.
- An `ssh` pane that would otherwise use the SSH shell request runs a command instead, to export the
  variable, and execs the login shell as the browser shim does
  ([Browser-open interception](url-open.md#browser-open-interception)).
- `tmux` and `ssh_tmux` panes do not set it. An agent under tmux is located through tmux, and a
  `PANEMUX_PANE_ID` it carries — inherited by whatever started the tmux server — is ignored.
- Collection reads the variable from the agent's own process environment, not a parent's: on Linux
  hosts from `/proc/<pid>/environ`, and on macOS hosts from `ps -E`. On other hosts nothing is
  reported and an agent outside tmux cannot be opened.
- macOS support is based on replayed macOS 26.3.1 `ps -E` output, not a run on a macOS or SSH
  host or an older release. Its space-delimited environment is ambiguous: collection removes the
  separately read command prefix, requires a leading space, and accepts exactly one remaining
  ` PANEMUX_PANE_ID=`, up to the next space. Multiple occurrences yield no ID, and command
  arguments are never treated as environment. A single occurrence inside another value after a
  space can be mistaken for an ID (including after newline-plus-space, but not newline alone);
  Linux does not accept that. A space in the value truncates it on macOS and is rejected on Linux.
  Panemux-generated values contain no spaces. These limits are accepted because IDs are only
  claims checked against a workspace pane's kind and host, under
  [collection security](../security/command-execution.md#task-dashboard-collection).
- Apple's own binaries (`/bin/sleep`, `/bin/zsh`) and other users' processes expose no environment
  on macOS. Claude, Codex's node wrapper/native binary and node do expose it. See the
  [macOS decision](../DECISIONLOG.md#reading-panemux_pane_id-on-macos-through-ps--e-2026-09-27-issue-263).
- The environment is the one the process was started with: changing the variable afterwards inside
  a running agent has no effect.
- The value is untrusted, since any process of the user can set it. Collection keeps only a value
  matching the rule above, and the browser opens only a pane of the matching kind and host that the
  workspaces hold.

## Opening a task

| Task | Result |
|---|---|
| In tmux, with a pane already attached | The pane's workspace becomes active and the pane takes focus |
| In tmux, no pane yet, attachable | A `tmux` (panemux host) or `ssh_tmux` (its connection) pane attaching to the session is added at the right edge of the workspace chosen in Open in workspace, through the ordinary `POST /api/sessions` and layout save; the pane's `tmux new-session -A` attaches to the running session |
| In tmux, not attachable | Not opened; the reason is shown |
| Outside tmux, in a `local` / `ssh` pane the workspaces hold | That pane's workspace becomes active and the pane takes focus |
| Outside tmux, naming a pane no workspace holds, or no pane | Not opened; the reason is shown |
| Stopped | Not opened; a stopped task whose agent resumes offers `Resume` ([Resuming a task](task-launch.md#resuming-a-task)) |
| Unknown | Not opened |

For a new pane, cards, details and the Type in pane popup all open **Open in workspace**.
It always appears, even with one workspace, listing workspace names as radio options with the
active workspace selected. **Open here** confirms the destination. **+ New workspace** reveals a
name field and changes confirmation to **Create and open**. A blank name after trimming, or a name
identical to an existing workspace title, shows an input error without creating anything.
A new workspace is created with `POST /api/workspaces`, then named with
`PUT /api/workspaces/{id}`, before creating the pane. The workspace already contains its default
local **Terminal** shell pane; the requested task or host pane is added to its right. Once created, the workspace becomes
the picker selection, so a retry uses that workspace rather than creating another one. If naming
fails, the workspace remains with its default title, an error appears and no task or host pane is
created by that attempt; **Open here** can then open in the retained workspace. A failure after
session creation resumes without creating the same session again, and a pane already saved in the
destination layout is not inserted twice when activation is retried. If the pane was already saved
in a different workspace, the error asks to select that workspace to finish opening. A selected
workspace that no longer exists, or a failed request, shows an error without falling back to another workspace. Cancel, Escape and × close
the picker without creating a pane; confirmation disables its controls until the request completes.

After a successful Open, the chosen workspace becomes active, the dashboard closes and the pane
takes focus and is briefly outlined. Go to pane bypasses the picker and navigates immediately. Opening the same tmux session again
while its pane is still being created does not create a second pane.

**Type in pane.** A task waiting for input or idle can also be answered without leaving the
dashboard: `Type in pane` opens a popup over the board holding a real terminal on the task's tmux
session — the board's temporary attach below — rather than a log or a message form. It does not
switch workspaces or add a pane, and it neither starts nor resumes the agent.

| Task | Type in pane |
|---|---|
| Waiting for input or idle, in an attachable tmux session (a pane on it or not) | Offered on the card and in the detail panel, beside `Open` / `Go to pane` |
| Waiting for input or idle, in a session name a pane cannot attach to | Not offered; the detail panel says why |
| Waiting for input or idle, outside tmux | Not offered; the detail panel says why, and points to `Go to pane` when a workspace holds its pane. The board does not put a second view on a pane's PTY |
| Waiting for input or idle, run by codex's shared daemon | Not offered; the detail panel says why |
| Any other state | Not offered, and no reason is shown |

- **One attach per press.** The button reads `Connecting…` and is disabled from the press until the
  popup closes; the popup opens at once in `Connecting`. An attach that answers after the popup was
  closed — or after another task's popup was opened — is ended at once and never shown. The
  exception is a popup reopened on the same task: the server answers its request with that same
  attach, so the late answer is left for it.
- **Connection.** The header shows `Connecting`, `Connected`, `Disconnected` or `Failed`. The terminal
  takes keys only while `Connected`: before that, and after a disconnect, a failed request, a
  WebSocket that gave up, or a tmux client that exited (a session that ended — including one that
  ended between the server's check and the attach), it is dimmed and inert, and the popup says
  "Input is not being sent." with `Retry` or `Reconnect`. Both send `POST /api/tasks/attach` again
  and read its answer over a new WebSocket; a pane's `/api/sessions/{id}/restart` is never asked.
- **Bound to the task.** The popup stays on the task ID it was opened for. When a poll moves the task
  to another column it says "Moved to <column> on the board. This terminal stays on <task>.", and
  when the board stops listing it, that it no longer does; the terminal, its connection and focus do
  not change. The task's card carries a `Typing` tag while its popup is open.
- **With a workspace pane on the same session.** The popup is a second tmux client on that session:
  input from either reaches it, and the popup says that the window takes the size of the client that
  was used last.
- **Closing** (`Close`, `Cmd/Ctrl+Shift+Esc`, or `Escape` while focus is in the popup's header) sends
  `DELETE /api/tasks/attach/{session_id}`, which ends only the board's tmux client; the agent and
  its tmux session keep running. The dashboard leaving with the popup open ends it the same way:
  the layer shortcut unmounts it, and a reload or a closed tab sends the same `DELETE` from
  `pagehide` as a `keepalive` request, which outlives the page. Were that request lost, the server
  still destroys the attach once its last WebSocket has been closed for the grace period. A page
  that comes back from the back/forward cache with the popup open asks for the attach again, as
  `Retry` does. The board's selection, filters and scroll position are as they
  were, and focus returns to the button the popup was opened from (the same task's button where its
  card has moved to, or the dashboard when there is none).
- The popup's `Go to pane` / `Open` closes it and opens the task as above.

**The board's temporary attach.** What Type in pane uses: a terminal on a task's tmux session that
is not a pane. [`POST /api/tasks/attach`](task-api.md#post-apitasksattach) opens a tmux client on the running
session, which the browser reads and writes over `/ws/{session_id}` like a pane's, and the layout
never holds it.

- It only attaches (`tmux attach-session -t =<name>`, an exact match) and never creates a session; a
  session that has ended fails the request. tmux reports a missing session only after its client has
  started, so the server first runs `tmux has-session -t =<name>` on the same host. A session that
  ends between that check and the attach still yields `201`; its client exits at once and the
  WebSocket shows tmux's message, and the attach is then destroyed like any other.
- One attach per task: opening the task again while it is open returns the same session, and two
  requests at once create one.
- It is destroyed by `DELETE /api/tasks/attach/{session_id}`, or 10s after the last WebSocket reading
  it closes, or 10s after it was created if none connected; a reload within that time reconnects to
  the same client. A client that has exited is replaced on the next request. Attaches are held in
  memory only and end with panemux.
- Destroying it ends only that tmux client; the tmux session and the agent keep running.
- panemux sets no tmux option for it. With tmux's default `window-size latest`, the window takes the
  size of the client that was used last and returns to the other client's size once the attach ends.

### Dashboard view state

The dashboard and the workspaces are switched with the `← Tasks` and `Workspaces` buttons or with
`Cmd/Ctrl+Shift+<display.task_dashboard_shortcut>` (`S` unless configured), from either layer.
The search text, host and label filters, Group mode, Cards/Table view, sort and Show done setting belong to App's
in-memory `TaskDashboardViewState` and survive switching to any workspace and back in the same
browser tab. The dashboard is still unmounted when leaving, ending its temporary terminal
attachments and timers; selection, details, popups and scroll position are not retained.
Reloading resets search, host and label filters and Show done to empty search, All hosts,
All labels and off. Group (None / Host / Label / Repository), Cards/Table and sort are restored from
versioned localStorage, independently of these transient filters. No API or config file stores display preferences.
[Dashboard UI design](../ui-design/task-dashboard.md#lists-and-filters) defines columns and group
presentation. Session selection, PR links and shared Open / Go to pane, Type in pane and Resume
act independently. Switching lists preserves selection, details and the terminal popup without
reconnecting it; each list retains its scroll position. Cards keep collection order.
Defaults are Cards / None / State ascending. Unknown enums (including unavailable Session group),
corrupt JSON and unsupported versions fall back to defaults. Denied or full storage leaves settings
usable in memory. Loading fallback defaults does not overwrite the stored record; an explicit
Group, view or sort change writes the current display preferences.
Search, selection and terminal connections are never stored there.

In grouped Cards, group keys use raw host identity (`Local` on SSH displays as `Local (SSH)`),
and catch-all keys differ from actual names. Label duplicates count once per group; notification
visibility counts unique task IDs. Selection, highlighting and temporary input follow task IDs
across groups and states. Closing input returns focus to its surviving origin, another visible
input button for the task, its selection action, or the visible list when no longer displayed.

The shared listed-task set applies search, host, label and Show done before grouping, and its unique
task IDs feed attention tracking. A restarted task marked done remains in its live state. The row-sort
utility leaves the source array and group order intact, sorting only within each group, including
label duplicates and catch-all groups. Session title, host and agent display name (unknown agents use
their ID), repository and branch use fixed-English natural, case-insensitive comparison. State uses
board column order, with Running / unknown tied; only stopped tasks marked done sort as Done.
Elapsed uses the same status timestamp and numeric seconds as display, with no fallback to start
time: invalid or missing timestamps are unknown, future timestamps are zero. Every sort uses one
reference time; display ticks alone need not re-sort. Missing values stay last in either direction;
ties use fixed task-ID order, also when descending. New keys start ascending except Elapsed (longest
first); selecting the current column heading reverses direction. Only the active heading has
`aria-sort`, with an arrow and direction in its button name. Labels and Actions are not sortable.
The table uses one horizontal scroll region with sticky column headers, including on narrow screens.
Each notification jump scrolls and focuses the task selection button once in the active list.
In grouped Cards, the card is uncovered below its group's variable-height sticky header band.
Switching views during its highlight preserves focus on the view control and does not extend the highlight. Collection updates
keep keyed rows, selection and keyboard focus attached to the same task ID.
A stored host or label absent from the current list is displayed and applied as All, while the
stored value is kept through loading and collection failures and restored when available again.
A notification opening a task still clears the filters that hide it, and those cleared settings
are kept on the next round trip.

## Opening a host

A host chip in the dashboard's host list opens a terminal on that host:

| Host | Chip |
|---|---|
| A host of `ssh_connections` whose last collection succeeded (`ok`) | A button (`Open a terminal on <host>`) that opens the connection menu |
| The panemux host (`""`) whose last collection succeeded | A button (`Codex input notifications on Local`) that opens a menu with that item alone; the panemux host is not opened from here |
| `connecting`, `error` or not yet collected | Not a button; an `error` host keeps its `Reconnect` button |

Under its actions, the connection menu carries the host's
[`Codex input notifications`](task-codex-permissions.md#codex-permission-requests) item. The panemux host's menu has the item
and `Close` only.

The connection menu is a dialog under the chip ([UI](../ui-design/dashboard-hosts.md)). It offers
`ssh` (the default) or `ssh_tmux`, and three actions:

- **`ssh_tmux` session name.** Choosing `ssh_tmux` asks the server for a new tmux session name
  ([`POST /api/hosts/session-name`](task-api.md#post-apihostssession-name)) once per menu, and shows it; `Open`
  and `Type in pane` stay disabled until it arrives. The name is the connection name with every
  character outside `[a-zA-Z0-9_.-]` replaced by `-`, then `-` and 8 random hex digits
  (`gpu-box-1a2b3c4d`). A failed request is shown in the menu.
- **`Open`** adds a new `ssh` or `ssh_tmux` pane on that connection at the right edge of the workspace chosen in **Open in workspace**, using the same picker,
  new-workspace creation and failure handling as [Opening a task](#opening-a-task). Successful
  confirmation closes the dashboard, activates the destination workspace, focuses and outlines the pane.
  It never moves to a pane the host already has; every press adds one. An `ssh_tmux` pane creates
  the named session (`tmux new-session -A`).
- **`Type in pane`** (the emphasised action) opens a popup over the board holding a host terminal
  ([`POST /api/hosts/terminal`](task-api.md#post-apihoststerminal)), like a task's Type in pane: it does not
  switch workspaces or add a pane, and every press opens a new terminal. Closing it (`Close`,
  `Cmd/Ctrl+Shift+Esc`, the dashboard leaving, or a reload's `pagehide`) sends
  `DELETE /api/hosts/terminal/{session_id}`: an `ssh` terminal logs out; an `ssh_tmux` terminal ends
  only its tmux client, and the tmux session it created stays on the host.
- **`Cancel`**, `Escape`, or a press outside the menu closes it; focus returns to the chip.
- When a poll finds the host no longer `ok` while its menu is open, the menu closes and stays closed
  when the host comes back; focus moves to the host's `Reconnect` button, or to the dashboard while
  it is connecting. Closing a host terminal whose chip a poll replaced returns focus to the host's
  chip as it is now, or to the dashboard.

A host terminal is a board attach in every respect but its creation: served by `/ws/{session_id}`,
never in the layout or `GET /api/sessions`, destroyed by its `DELETE` or 10s after its last
WebSocket closed (or after creation if none connected), and held in memory only.
`DELETE /api/tasks/attach/{id}` and `DELETE /api/hosts/terminal/{id}` each end only their own kind,
and `POST /api/tasks/attach` never returns or removes a host terminal. At most 4 are open on one
connection at a time.
