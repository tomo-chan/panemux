# Behavior: agent attention notifications

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Agent Attention Notifications

The frontend watches terminal output for conservative agent confirmation prompts such as approval,
permission, proceed requests, and Codex MCP allow menus. Detection runs in the frontend and keeps a
lightweight background WebSocket subscription for every pane across every workspace, so hidden
workspaces are still watched even while their xterm instances are unmounted. When a prompt is
detected:

- the pane frame flashes until the pane receives focus or a click
- the containing workspace tab flashes when that workspace is not active, and clears when selected
- the browser Notification API is used when permission has already been granted and the prompt is not currently visible to the user
- clicking a browser notification brings the app forward, switches to the matching workspace,
  restores a maximized pane that hides the prompt's pane, focuses that pane and outlines it briefly
- if notification permission is undecided, the browser is asked on the first pointer or key
  interaction instead of waiting for the first prompt event

Browser notification eligibility is determined by the current UI state:

| Browser state | Pane state | Browser notification |
|---|---|---|
| active | visible in the active workspace | no |
| active | hidden in another workspace | yes |
| active | hidden by maximize in the active workspace | yes |
| inactive | any pane | yes |

To suppress redraw noise, panemux stores the last browser-notified prompt signature per pane in
browser storage. If the same pane replays the same prompt after a refresh, reconnect, or layout
change, the pane and workspace attention indicators can still reappear, but the browser
notification is not shown again. When the same pane later emits a different prompt, the stored
signature is replaced and the new prompt can notify again.

### Task dashboard waits

The [task dashboard](tasks.md)'s own `wait` state is the second source of the same attention, so an
agent that records it is waiting — Claude Code's `waiting` status, codex's `request_user_input` — is
noticed whether or not its prompt matched a pattern, and even when no pane shows it. Terminal output
detection stays for what that state cannot tell, such as a codex waiting for command approval.

- **Collection.** While the dashboard collects (shown on a visible page) its own collections are
  used. Otherwise the frontend polls
  [`GET /api/tasks/attention`](tasks.md#get-apitasksattention) every 15 seconds for as long as the
  page is open — hidden, in the background, or with the dashboard shown on a hidden page — never two
  at once. A collection that was running when the dashboard took over is discarded, and so is a
  snapshot older than the last one used. A failed collection reports nothing.
- **Pane and workspace.** A waiting task is matched to its pane the way the dashboard's Open is
  (the pane attached to its tmux session, or the pane an agent outside tmux was started from). A
  wait new to the page flashes that pane's frame and, when its workspace is not active, the
  workspace tab, with the same clearing as a prompt's: focusing or clicking the pane, selecting the
  workspace. A task no pane shows raises no frame; when a later collection finds its pane, that
  pane's frame flashes then, without a second notification.
- **Browser notification.** A wait is notified once, under the same rule as a prompt: the table
  above, where the task dashboard, while shown in an active browser, counts as showing every wait.
  A task no pane shows is notified unless that is the case. The notification is titled
  `Agent waiting for input` and its body names only the agent, the host and the task's title
  (`claude on Local: panemux`), never what the agent asks, its log or its prompt. Permission is
  handled as for prompts: never requested by a wait.
- **Clicking it** brings the app forward and, when the task has a pane, shows it as a prompt's click
  does. Without a pane it opens the task dashboard, clears any filter that would hide the task, and
  selects and briefly highlights the task's card; clicking another such notification afterwards
  selects that task in turn.
- **Once per wait.** A wait is known by its `wait_signature`. The signatures already handled are
  kept in browser storage (`panemux:task-attention`, the newest 200, falling back to memory), so the
  same wait is not notified again by a later collection, a reload or a reconnect, while a wait that
  begins after the previous one ended has a new signature and is notified. A wait seen while it was
  visible counts as handled. A wait without a signature raises the frame but never a notification. A
  host that fails to answer keeps what was known about its tasks, so its recovery does not report
  their waits again, and one host failing does not hold up another's.
- **Prompt and wait together.** A prompt and a task wait on the same pane are one wait when the
  prompt appeared no more than 5 seconds before the wait began or any time after: the wait's
  `status_since` is put on the browser's clock through its host's `collected_at`. Whichever is seen
  first notifies; the other raises attention only. The time of each pane's last fresh prompt is kept
  in the same storage, so the order holds across a reload. A prompt on a pane whose task is in a
  signed wait already handled is that wait, as of the last collection, so for up to one collection
  interval after a wait ends a prompt on its pane can be taken for it and not notified. While the
  task's host fails to answer, the wait is held for one collection interval past the host's last
  answer and no longer; when no collection arrives at all (the request or the dashboard's
  collection keeps failing), it lapses two intervals after the last one that did. A prompt after
  that notifies even if the wait may still be going on.

Prompt detection remains frontend-only. The backend still buffers recent terminal output per
session and replays that snapshot when a pane reconnects after a workspace switch or browser reload,
but prompt notifications no longer depend on the pane being visibly mounted at the time the output
arrives.

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
