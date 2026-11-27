---
type: Specification
title: "Task Events Observation and Receiver"
description: "Task event model, wait IDs, per-host observation lifecycle, subscriber fan-out, and browser receiver state rules."
---

# Task Events Observation and Receiver

Panemux observes running agent sessions on the panemux host and every `ssh_connections` host and
publishes every state change over `/ws/tasks/events`. The server only observes and publishes; each
receiver decides notifications, attention frames, and dashboard projections. Wire frames, schemas,
and `epoch`/`seq` ordering live in [`GET /ws/tasks/events`](../../api/tasks.md#get-wstasksevents);
visual presentation lives in [Task Dashboard and Attention UI](../../ui/tasks.md#attention-indicators-and-browser-notifications).

## Observation model

- **Observed state only**: uses [task collection](collection.md#collection) without stopped-session
  searches. Terminal output is never scanned for prompts; Codex command-approval waits require the
  [Codex permission hook](collection.md#codex-permission-requests).
- **Agents only**: non-agent programs are never covered; Devin is not yet supported
  ([issue #276](https://github.com/tomo-chan/panemux/issues/276)).
- **Pane matching is receiver-side**: events carry `location`; the server does not know which pane,
  if any, displays a task.

### Tasks and changes

A task is one running agent session identified by [`GET /api/tasks`](../../api/tasks.md#get-apitasks)
`id` (`local:<agent>:<key>` or `ssh:<host>:<agent>:<key>`). Two cases change `id` and publish as
`removed` + `added` rather than a guessed rename: a `pid-<pid>` Codex process acquiring its rollout,
and `/resume` inside a running Claude Code switching sessions. Allowed states are `busy`, `wait`,
`idle`, `run`, and `unknown` (never `stop`; tasks that stop running are `removed`).

Each observed task reduces to its [task view](../../api/tasks.md#task-view); a change is any field
difference **other than `status_since`** (which drifts up to `1 s` across host-clock conversions and
is carried but never diffed). PID, start time, and conversation log are excluded from the view.

### The wait ID

| Task state | `wait_id` |
|---|---|
| `wait` with a [wait signature](collection.md#wait-signature) | The `wait_signature` (`w1-…`), stable across collections, reloads, and server restarts |
| `wait` without a signature | `e1-<epoch>-<seq>` ([stream position](../../api/tasks.md#ordering-epoch-and-seq) when first observed); stable while in that wait with the same `waiting_for` until server restart |
| Any other state | Omitted |

`e1-…` records observation time, never wait start, and gets a new ID after a server restart; leaving
and re-entering `wait` always produces a new `wait_id`.

### Hosts

| Status | Meaning |
|---|---|
| `pending` | Observation started; host has not answered yet |
| `ok` | Last collection on this host succeeded |
| `connecting` | Connection setup still in progress ([Hosts and connections](collection.md#hosts-and-connections)) |
| `error` | Last collection failed (`error` explains why) |

**A non-`ok` host publishes no task events**: its tasks stay as last observed until it answers
again, when differences publish (waits starting and ending while failing are never published). One
slow or failing host never delays others. Added `ssh_connections` hosts publish as `added` in
`pending`; removed hosts publish as `removed` along with their tasks. Unreadable/missing/unknown
agent files publish as `unknown` or `run` without a wait ID.

## Lifecycle

```text
 subscribers: 0 ──open──▶ 1..n ──last close──▶ 0
 observation:  stopped ──▶ running ──none left──▶ stopped (model kept)
```

- **Subscriber-driven**: observation runs only while at least one subscriber is connected (shared
  across all subscribers). The first subscriber starts immediate observation across all hosts
  (unanswered hosts appear `pending` in the initial snapshot).
- **Per-host cycle**: each host runs `5 s` after its previous observation finishes (no overlap; slow
  hosts lengthen only their own cycle up to the `15 s` collection timeout). Configured hosts are
  reread each cycle so `ssh_connections` additions/removals take effect.
- **Stop preserves model**: when the last subscriber disconnects, observation stops while retaining
  last-observed tasks, `epoch`, `seq`, and unsigned `wait_id`s (so reloads do not re-mint unsigned
  wait IDs). Restarting marks hosts `pending` until they answer and publishes diffs against the
  retained model.
- **In-flight runs finish cleanly**: stopping does not interrupt an in-flight host script (closing a
  remote channel is not known to stop it); results arriving after stop are discarded, and a new
  subscriber arriving before completion resumes without overlapping.
- **Non-blocking fan-out**: publishing never waits on a slow subscriber. Frames are never dropped
  mid-stream; a stalled connection is closed so its tab reconnects from a fresh snapshot. Socket
  writes carry no deadline, so only the stalled connection's handler waits for its socket to close.

## Receiver

Each browser tab owns one `/ws/tasks/events` connection and one store feeding attention, browser
notifications, and dashboard live states.

### Store and connection

- Holds `epoch`, `seq`, tasks by `id`, hosts by name, and stream status (`connecting`, `live`,
  `offline`).
- Every frame is Zod-validated; validation failures trigger reconnect like a `seq` gap.
- Reconnects after close with `2–30 s` backoff and **no attempt limit** so notifications recover
  across server restarts. Stays open while the tab is hidden.

### Pane and workspace attention

- Matches tasks to panes per [Where a task runs](lifecycle.md#where-a-task-runs).
- A matched pane has attention while its task is in `wait` on an `ok` host unless that `wait_id` was
  cleared in this tab (derived from the store on each frame). Inactive workspace tabs flash while
  containing a pane with attention and clear on selection or wait end.
- **Non-`ok` hosts give no attention**: prevents stale waits in `pending` reload snapshots from
  flashing until the host answers `ok` (the host `ok` frame precedes task diffs).
- Clears on pane focus/click, workspace selection, task leaving `wait`/`removed`, or host leaving
  `ok` (another still-waiting task on the same pane keeps attention; transient `unknown`/host errors
  restore attention when `wait` returns unless cleared meanwhile).
- Cleared `wait_id`s persist in tab `sessionStorage` (same retention/500-entry cap as notified IDs)
  so reloads do not re-show cleared attention.

### Codex permission requests in a pane

A matched pane whose task waits on a Codex permission request (`agent == "codex"`,
`waiting_for == "a permission request from codex"`, `w1-` `wait_id`) on an `ok` host displays the
inline permission strip (as does the Type in pane popup). Clearing attention or closing the popup
does **not** dismiss the strip; it clears only when the wait ends, the host leaves `ok`, the stream
disconnects, or `Input given` succeeds (which suppresses that `wait_id` across panes and dashboard
in this tab and restores terminal focus if focus stayed on the clicked strip). Multiple matching
tasks on one pane show `Review in Tasks` opening the dashboard on the first. `request_user_input`
questions never show this strip.

### Browser notifications

- Fires once per `wait_id` when the wait is **not visible** (browser inactive, or neither its
  matched pane nor a dashboard listing it is on screen; unmatched tasks are visible only on the
  dashboard).
- Only notifies waits starting on an `ok` host (live frame or `ok` snapshot). Waits in a non-`ok`
  snapshot are not judged again on host recovery, whereas waits starting while observation was
  stopped publish as a change on recovery and notify normally.
- Waits arriving before the tab has loaded workspaces are neither notified nor recorded (attention
  still applies).
- **Per-tab independence**: tabs do not coordinate visibility or deduplication.
- Notified `wait_id`s persist in tab `sessionStorage` (dropping IDs absent from snapshots, capped at
  newest `500`; falls back to page memory if storage is unavailable). Recorded only after actually
  shown (if permission is granted later, an `ok` reconnect snapshot notifies it; a post-reload
  `pending` snapshot does not).
- Uses `tag: wait_id` so duplicate notifications replace rather than stack; displays agent, host,
  and directory basename only (never `waiting_for`, prompts, or conversation text).
- Clicking focuses/outlines the matched pane (switching workspace and un-maximizing if needed,
  clearing attention), or opens the task dashboard clearing hiding filters and selecting the task.

### Task dashboard

The dashboard loads full records from [`GET /api/tasks`](../../api/tasks.md#get-apitasks) and
overlays `state`, `waiting_for`, wait, and `status_since` from the live stream whenever connected
and present (an omitted `status_since` clears rather than reusing an earlier collected timestamp).
