# Behavior: task events

A Japanese translation is in [task-events.ja.md](task-events.ja.md). This English document is the
authoritative one; a change updates both in the same pull request.

## Task Events

panemux observes the running agent sessions on the panemux host and on every `ssh_connections` host,
and publishes every change in their state as an event over one WebSocket. The server only observes
and publishes: it does not decide whether a change deserves a notification, a flashing frame or
nothing. Each receiver decides that for itself. The browser is the one receiver today: its task
dashboard, its pane and workspace attention and its browser notifications are all projections of
the same stream ([Receiver](#receiver)).

- **What is observed is the tasks' state, and nothing else.** The input is the collection the task
  dashboard already runs, without its searches for stopped sessions
  ([Collection](tasks.md#collection)). Terminal output is not read for prompts; a state an agent does
  not record — codex waiting for command approval
  ([issue #294](https://github.com/tomo-chan/panemux/issues/294)) — is not observed and shows as the
  state the agent's files do give (`busy`).
- **Only agents are covered.** An agent is supported by adding its observation to the collection,
  not by a pattern on its output. Devin is not covered yet
  ([issue #276](https://github.com/tomo-chan/panemux/issues/276)); a program that is not an agent is
  never covered.
- **Matching a task to a pane is the receiver's job.** Each event carries where the task runs; the
  server does not know which pane, if any, shows it.

### The model

#### Tasks

A task is one running agent session, identified by the task `id` of
[`GET /api/tasks`](tasks.md#get-apitasks): `local:<agent>:<key>` on the panemux host and
`ssh:<host>:<agent>:<key>` on a connection. The key is normally the session ID, so the id is stable
for as long as the session runs. Two cases change it, and both are published as one task removed and
another added rather than joined by a guess:

- a codex process that has no session yet (`pid-<pid>`) gets one, and
- `/resume` inside a running Claude Code switches the process to another session
  ([States](tasks.md#states)).

The states are those of [States](tasks.md#states) that a running task can have: `busy`, `wait`,
`idle`, `run` and `unknown`. `stop` is not used. A task that is no longer running is **removed**,
because the collection cannot tell an exit from a `/resume` or a host losing its process, and
calling any of them `stop` would be a guess.

#### What counts as a change

Each observed task is reduced to its *view* — the fields of the [task frame](#task-view) — and a
change is a view that differs from the previous one in anything but `status_since`. `status_since`
is converted from the host's clock on every collection and can move by up to a second while nothing
changed ([States](tasks.md#states)), so it is carried, as of the change, but never compared. A task's
process ID, its start time and its conversation log are not part of the view.

#### The wait ID

A task in `wait` carries a `wait_id`, the identity of the wait it is in. A receiver uses it to tell a
wait it has already handled from a new one.

| The task | `wait_id` |
|---|---|
| is in `wait` and has a [wait signature](tasks.md#wait-signature) | the `wait_signature` itself (`w1-…`), the same across collections, reloads and server restarts |
| is in `wait` without one (the agent did not record when the wait began) | `e1-<epoch>-<seq>`: the [stream position](#ordering-epoch-and-seq) at which the wait was first observed. It stays the same while the task stays in that wait with the same `waiting_for`, and is valid until the server restarts |
| is in any other state | none |

The second form records when panemux *observed* the wait. It is not a wait start and never becomes a
`wait_signature`. A server restart gives a still-unsigned wait a new one, so a receiver may handle that
wait once more. A wait the task leaves and enters again always gets a new `wait_id`.

#### Hosts

Each host is published with a status:

| Status | Meaning |
|---|---|
| `pending` | Observation has started and this host has not answered yet |
| `ok` | The last collection on this host succeeded |
| `connecting` | Its connection is still being set up ([Hosts and connections](tasks.md#hosts-and-connections)) |
| `error` | The last collection failed; `error` says why |

**A host that is not `ok` publishes no task event.** Its tasks stay as last observed and the host's
status is what tells a receiver they are old: they are neither removed nor kept waiting by a guess.
When the host answers again, its tasks are compared with what was last observed and the difference is
published. A wait that began and ended while the host was failing is never published, since it was
never observed. One host failing, or being slow, does not delay or hide the others.

A host added to `ssh_connections` is published as added, in `pending`. A host removed from it is
published as removed, and so is each of its tasks.

Unreadable, missing or unknown agent files never produce a wait: the collection already reports them
as `unknown` or `run`, without a signature ([Wait signature](tasks.md#wait-signature)), and that is
what is published.

### `GET /ws/tasks/events`

The task event stream. The server sends JSON text frames; anything the client sends is ignored.

- **Not authenticated**, like [`/ws/{sessionID}`](websocket.md#websocket-protocol) and
  [`GET /api/tasks`](tasks.md#get-apitasks): no bearer token.
- **Cross-site requests are refused with `403` before upgrading and before any collection starts**:
  the same rule as `GET /api/tasks` (a `Sec-Fetch-Site` of `cross-site` or `same-site`, or an `Origin`
  whose hostname or effective port differs from the request Host), and the upgrade's own Origin check as on
  `/ws/{sessionID}`. Opening the stream makes every host be dialed, so the refusal protects that side
  effect ([Task dashboard collection](../security/command-execution.md#task-dashboard-collection)).
- A frame the client sends is limited to 512 bytes; a larger one closes the connection.

#### Frames

Every frame has `type`, `epoch` and `seq`.

The first frame on every connection is a snapshot of everything currently known:

```json
{
  "type": "snapshot",
  "epoch": "9f2c41d07ab35e88",
  "seq": 120,
  "hosts": [
    { "name": "", "status": "ok" },
    { "name": "gpu-box", "status": "error", "error": "connect to gpu-box: dial tcp: i/o timeout" }
  ],
  "tasks": [
    {
      "id": "local:claude:7c21e0a4",
      "host": "",
      "agent": "claude",
      "session_id": "7c21e0a4",
      "cwd": "/workspace/user/panemux",
      "state": "wait",
      "waiting_for": "input needed",
      "wait_id": "w1-6728c5554228dcb7cc58711bbf3636eb24a57f8348e865de9855773e17b9bb47",
      "status_since": "2026-10-02T09:59:58Z",
      "location": { "kind": "tmux", "tmux_session": "task-7c21", "attachable": true }
    }
  ]
}
```

After it, one frame per change, in order:

```json
{ "type": "task", "epoch": "9f2c41d07ab35e88", "seq": 121,
  "op": "changed", "prev_state": "busy",
  "task": { "id": "ssh:gpu-box:codex:0199a6…", "host": "gpu-box", "agent": "codex", "state": "wait", "…": "…" } }

{ "type": "host", "epoch": "9f2c41d07ab35e88", "seq": 122,
  "op": "changed",
  "host": { "name": "gpu-box", "status": "error", "error": "…" } }
```

| `type` | `op` | Carries |
|---|---|---|
| `snapshot` | — | `hosts`, `tasks` (running tasks, `[]` when none) |
| `task` | `added` | `task`: the new task's view |
| `task` | `changed` | `task`: the new view; `prev_state`: the state before |
| `task` | `removed` | `task`: the view last observed; `prev_state`: its state |
| `host` | `added`, `changed`, `removed` | `host` |

A state transition is a `changed` frame whose `prev_state` differs from `task.state`; a `changed`
frame with the same state is a change of something else (its `wait_id`, `waiting_for`, `cwd` or
location). A task that appears already waiting is an `added` frame in `wait`.

#### Task view

| Field | Meaning |
|---|---|
| `id`, `host`, `agent`, `session_id`, `cwd` | As in [`GET /api/tasks`](tasks.md#get-apitasks) |
| `state` | `busy`, `wait`, `idle`, `run` or `unknown` |
| `waiting_for` | What a `wait` task waits for, as the agent wrote it; only on `wait` |
| `wait_id` | [The wait ID](#the-wait-id); only on `wait` |
| `status_since` | When the task entered its state, converted to the server's clock as of this frame; not compared ([What counts as a change](#what-counts-as-a-change)) |
| `location` | `kind`, `tmux_session`, `pane_id`, `attachable`, exactly as in [Where a task runs](tasks.md#where-a-task-runs) |

No frame carries conversation text, a prompt, a command line or a process ID.

#### Host view

`name` (`""` for the panemux host), `status` ([Hosts](#hosts)), and `error` when `error`.

#### Ordering: `epoch` and `seq`

`epoch` is chosen when the server starts and changes only with a restart. `seq` counts every frame
published in that epoch, for all hosts and tasks together. A snapshot's `seq` is the last change it
includes, so the next frame on the same connection is `seq + 1`.

A receiver that sees a gap in `seq`, a `seq` going back, a different `epoch` or a `type` it does not
know discards what it holds and reconnects: **recovery is always a fresh snapshot.** The server keeps
no history to replay.

### Lifecycle

```text
 subscribers: 0 ──open──▶ 1..n ──last close──▶ 0
 observation:  stopped ──▶ running ──none left──▶ stopped (model kept)
```

- **Observation runs only while the stream has a subscriber**, and once however many there are. The
  first subscriber starts it; every host is observed at once and the subscriber's snapshot shows
  hosts not answered yet as `pending`.
- **Each host has its own cycle.** A host is observed again 5 seconds after its previous observation
  finished, so observations of one host never overlap, and a slow host — up to the collection's
  per-host timeout ([Hosts and connections](tasks.md#hosts-and-connections)) — lengthens only its own
  cycle. The hosts are read again on every cycle, so a host added to or removed from
  `ssh_connections` is picked up.
- **Observation stops when the last subscriber leaves.** A page reload therefore stops it and starts
  it again, observing every host once more. What was last observed, the epoch, `seq` and the unsigned
  wait IDs are kept, so a reload does not give a still-unsigned wait a new ID; when observation starts
  again every host is `pending` until it answers, and the differences from what was kept are
  published.
- **Stopping does not interrupt an observation in flight.** It runs to the collection's per-host
  timeout and its result is discarded: closing a host's command channel is not known to stop the
  script it runs there, so cutting it short would only leave that script running unwatched. A
  subscriber that arrives before it finishes resumes observation with it rather than starting a
  second, overlapping observation of the host.
- Publishing never waits for one subscriber, so a connection that stops reading cannot hold up the
  others. A frame is never dropped from the middle of a connection's stream: a connection that cannot
  take its frames is closed, and its tab reconnects from a fresh snapshot. Writes have no deadline,
  so closing that subscription does not interrupt a write already blocked on the connection: the
  connection's own handler waits until its socket closes, and nothing else waits with it.

### Receiver

The browser holds one connection and one store per tab, and every surface that needs task state reads
it from that store.

#### Store and connection

- The store holds `epoch`, `seq`, the tasks by `id`, the hosts by name, and whether the stream is
  `connecting`, `live` or `offline`.
- Every frame is validated against its schema; a frame that fails validation is treated like a gap.
- The tab reconnects after a close with a backoff from 2 to 30 seconds and **no limit on attempts**,
  so notifications come back after a server restart. The connection stays open while the page is
  hidden: notifications are for exactly that time.

#### Pane and workspace attention

- A task is matched to a pane from its `location` and `host` by the same rule as the task dashboard's
  Open ([Opening a task](tasks.md#opening-a-task)): a tmux task to the `tmux` / `ssh_tmux` pane on its
  session, a task outside tmux to the `local` / `ssh` pane its `pane_id` names. Every task in one tmux
  session matches the same pane. A codex daemon task, or one outside tmux in no pane, matches none.
- A matched pane has attention while its task is in `wait` on a host that is `ok`, unless that
  `wait_id` was cleared in this tab. Attention is read from the store as it stands after each frame,
  not kept from one frame to the next, so a reload shows a wait that is still current again.
- **A wait on a host that is not `ok` gives no attention.** Such a host's tasks are as last observed
  ([Hosts](#hosts)), and while observation was stopped they are what was kept from before
  ([Lifecycle](#lifecycle)): a snapshot after a reload has every host `pending`, and a wait in it
  may already have been answered. It shows once the host answers with the task still in `wait`. The
  host's `ok` frame comes before the difference it found, so a wait that had ended shows for no longer
  than the frames between the two.
- Attention clears as described in [Agent attention notifications](notifications.md#agent-attention-notifications):
  the pane on focus or click, the workspace tab on selecting the workspace — and, in addition, whenever
  the task is no longer in `wait` on an `ok` host, wherever it was answered: a `changed` frame to
  `busy`, `idle`, `run` or `unknown`, `removed`, or its host turning `pending`, `connecting` or
  `error`. A pane that shows another task still waiting keeps its attention. Attention that went
  with `unknown` or a failing host comes back when the task is seen in `wait` with that `wait_id`
  again, unless it was cleared in this tab meanwhile.
- The `wait_id`s whose attention a person cleared are kept in the tab's session storage, under the
  same rules as the notified ones below, which is what keeps a reload from showing them again.

#### Browser notifications

- A wait the user cannot currently see is notified once per `wait_id`, under the conditions in
  [Agent attention notifications](notifications.md#agent-attention-notifications). A wait is visible
  when the browser is active and either its pane is on screen, or the task dashboard is on screen and
  lists the task. A task that matches no pane is therefore visible only on the dashboard.
- A wait is notified only when it starts while its host is `ok`: a frame that starts it, or a
  snapshot that has it with its host `ok`. A wait that a snapshot has under a host that is not `ok` is
  not notified, and is not judged again when that host answers — its host's `ok` frame comes before
  the difference it found, so the tab cannot tell at that point whether the wait is still current. A
  wait that began while observation was stopped is still notified: when the host answers, it is
  published as a change from what was kept.
- A wait that starts before the tab has loaded its workspaces is not notified, and not recorded: the
  tab cannot yet tell whether its pane is on screen. This is the moment after a page load or reload; a
  wait started then, or still unnotified in its first snapshot, is not notified by this tab, but its
  pane and workspace keep their attention.
- **Each tab decides and notifies on its own; tabs do not coordinate.** Every tab judges visibility
  from its own screen, so two tabs showing panemux can both notify the same wait.
- A tab records the `wait_id`s it has notified in its session storage, which survives a reload of
  that tab and is not shared with other tabs, so a reload or a reconnect does not notify the same wait
  again. The record drops the IDs no longer in a snapshot and keeps at most the 500 newest. Without
  session storage the record lives in the page's memory and a reload may notify again. A new
  `wait_id` — a later wait of the same task — is notified again.
- A wait is recorded only once its notification is shown. While permission is not granted nothing is
  shown and nothing is recorded, so a wait still going on after permission is granted is notified by
  a reconnect's snapshot that has it with its host still `ok` (observation kept running). A snapshot
  after observation stopped — a reload of the only tab — has its host `pending` and does not notify
  it, nor does the host's answer: a wait that began before permission was granted is then not
  notified by that tab, though its pane flashes. A wait that begins after permission is granted is
  notified as usual.
- The notification's `tag` is the `wait_id`, so a notification of the same wait that is still shown is
  replaced rather than stacked.
- The notification names the agent, the host and the task's directory name only: not what the task
  waits for (`waiting_for`), and no conversation text or prompt.
- Clicking it brings the app forward. The task is matched where the stream last placed it, at the
  click rather than when it was notified. With a matched pane, the pane's workspace is selected, a
  maximized pane hiding it is restored, and it is focused, briefly outlined and its attention cleared.
  Without one, the task dashboard opens with any filter hiding the task cleared, and the task is
  selected and highlighted.

#### Task dashboard

The dashboard lists tasks from [`GET /api/tasks`](tasks.md#get-apitasks), which adds what the stream
does not carry: stopped sessions, git and pull request details, records and summaries. A task's
`state`, `waiting_for`, wait and `status_since` come from the stream whenever the stream is live and
has that task, so a change, and the time spent in the new state, shows as soon as it is published
rather than at the dashboard's next collection. A frame that omits `status_since` means the time is
not known, so the dashboard shows none rather than the collected time of an earlier state.

## Related Documents

- Task dashboard collection and states: [tasks.md](tasks.md)
- Notification conditions and attention indicators: [notifications.md](notifications.md)
- Collection command safety: [Task dashboard collection](../security/command-execution.md#task-dashboard-collection)
- Architecture: [architecture.md](../architecture.md)
