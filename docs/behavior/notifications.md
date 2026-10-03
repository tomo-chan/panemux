# Behavior: agent attention notifications

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Agent Attention Notifications

panemux tells the operator when a coding agent starts waiting for them. The input is the
[task event stream](task-events.md): the server observes the agents' own state on the panemux host
and every `ssh_connections` host and publishes each change, and the browser decides what to show.
Terminal output is not searched for prompts, so an agent waiting in a pane no browser has open is
noticed as well, and a wait an agent does not record — codex asking to approve a command
([issue #294](https://github.com/tomo-chan/panemux/issues/294)) — is not noticed at all.

When a task starts a wait ([Pane and workspace attention](task-events.md#pane-and-workspace-attention)):

- the frame of the pane the task matches flashes until the pane receives focus or a click, or the
  task's wait ends (it goes to `busy`, `idle` or `run`, or stops running)
- the containing workspace tab flashes when that workspace is not active, and clears when selected
  or when the task's wait ends
- the browser Notification API is used when permission has already been granted and the wait is not
  currently visible to the user: neither its pane nor, on the task dashboard, its task is on screen
- clicking a browser notification brings the app forward and goes to the task: its pane, focused and
  briefly outlined, in its workspace with any maximized pane hiding it restored; or, when it matches no
  pane, the task dashboard with the task selected and highlighted
- if notification permission is undecided, the browser is asked on the first pointer or key
  interaction instead of waiting for the first wait

Browser notification eligibility is determined by the current UI state:

| Browser state | On screen | Browser notification |
|---|---|---|
| active | the task's pane, in the active workspace and not hidden by maximize | no |
| active | the task dashboard, listing the task | no |
| active | anything else: the pane in another workspace or hidden by maximize, the dashboard with the task filtered out, or the workspaces for a task that matches no pane | yes |
| inactive | anything | yes |

A wait is notified once per `wait_id` in a browser
([Browser notifications](task-events.md#browser-notifications)): each tab keeps the IDs it has
notified in its session storage, so a reload or a reconnect of that tab does not notify the same wait
again. Tabs do not coordinate: two tabs showing panemux can each notify the same wait, and the
notification's `tag` (the `wait_id`) replaces one still shown rather than stacking it. The pane and workspace indicators
reappear after a reload while the task is still waiting, unless this tab already cleared that wait,
which it records in its session storage the same way. A wait is recorded as notified only once a
notification is shown, so one that arrived while permission was not granted is notified on a later
reload or reconnect if it is still going on. A later wait of the same task has a new
`wait_id` and notifies again. The notification shows the agent, the host and the task's directory
name, never what it waits for or conversation text.

Attention detection is the server's: the browser keeps one task event connection per tab rather than a
connection per pane.

For pane-header Git status, local and local tmux panes inspect the local filesystem, while `ssh`
and `ssh_tmux` panes run the equivalent Git inspection on the remote host. This allows headers to
show branch/repository info even when the repository exists only on the SSH target.

Workspace tab summaries and their integrated pane groups use the same Git metadata source and also
poll `/api/sessions` to summarize pane connection state across every workspace, including inactive
ones.

Pane-header Git and PR metadata is fetched immediately when a pane becomes visible, then refreshed
every 10 seconds only while both the browser tab and that pane remain visible.

The backend caches each session's git-info response for 30 seconds so that this steady-state polling
(and any other concurrent viewer of the same session, such as another browser tab) does not repeat
process/transcript scanning, remote git inspection over SSH, or `gh pr view` lookups on every request.
A pane's displayed Git/PR metadata may therefore lag the true state by up to 30 seconds; the cache is
cleared whenever a session is deleted or recreated so a new session never inherits another session's
cached response. Explicit refresh triggers — clicking or focusing a pane, restoring the browser tab,
or opening VS Code — only skip the frontend's own request if it already has a response no older than
10 seconds, matching the steady-state poll interval, so an explicit refresh is bounded by that 10
seconds plus the server-side cache above rather than compounding a larger, independent client-side
window on top of it.

Workspace-summary session-state and Git metadata polling are frontend-only and best-effort. While
the browser tab is visible, the frontend polls every 10 seconds so it can summarize all known
panes across all workspaces without mounting hidden terminal instances. The integrated summary view
marks the currently focused pane so the overview stays aligned with the live terminal focus state.
For `top` and `bottom` workspace bars, pane cards are shown in a hover/focus overlay anchored to
the workspace tab. For `left` and `right` workspace bars, pane cards stay expanded inline beneath
their workspace tab. In vertical mode, the workspace tabs and inline cards scroll inside the bar,
while the `+` action and tab-position controls stay pinned at the bottom.

When a pane is hidden behind maximize, its header Git/PR polling stops until it becomes visible
again. Restoring the browser tab or making a pane visible again triggers an immediate refresh so
newly created PR links become clickable without waiting for the next steady-state poll.

When panemux detects that an interactive `codex` or `claude` session is operating in a sibling Git
worktree for the same repository, pane-header Git/PR metadata and the VS Code open action prefer
that worktree. After the agent exits, panemux keeps the last valid sibling worktree pinned for that
pane session until a newer valid sibling worktree is detected or the pane itself changes to a
different repository.

To avoid repeatedly transferring and parsing unchanged interactive-agent logs, panemux reuses the
last parsed Codex or Claude worktree result while the underlying session-log or transcript file
fingerprint remains unchanged. For SSH-backed panes this check uses remote file metadata first and
only reads the full `jsonl` contents again when the fingerprint changes.

Remote Git inspection currently depends on `git rev-parse --path-format=absolute --git-common-dir`
on the SSH target, which requires Git 2.31 or newer. Older remote Git versions degrade to "no git
info" in the pane header for SSH-backed panes.

For terminal text selection, panemux keeps tmux mouse mode unchanged for `tmux` and `ssh_tmux`
panes. Plain drag therefore continues to follow tmux mouse behavior. To force browser-side xterm
selection while tmux mouse mode is active, hold `Option` during drag on macOS or `Shift` during
drag on Linux and Windows.
