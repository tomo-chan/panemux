---
type: Specification
title: "Task Dashboard REST and Event Stream API"
description: "Task and host terminal REST routes, Codex hook endpoints, and the /ws/tasks/events wire protocol."
---

# Task Dashboard REST and Event Stream API

Wire contracts for the [task dashboard](../backend/tasks/index.md): task and host-terminal REST routes,
Codex hook endpoints, and the `/ws/tasks/events` event stream. Collection, state derivation,
availability, and navigation rules live in [task collection](../backend/tasks/collection.md),
[task lifecycle and navigation](../backend/tasks/lifecycle.md), and [task events](../backend/tasks/events.md).

## `GET /api/tasks`

Collects every host. Returns JSON with `hosts`, `agents`, `tasks`, and `summaries_enabled`;
individual host errors do not fail the request (`200` even if all fail).

- `hosts`: panemux host first (`""`), then `ssh_connections` keys in name order. `status` is `ok`,
  `error` (with `error`), or `connecting`; `collected_at` is present when `status` is `ok`.
- `unreadable_state_files`: host state files that could not be read, by filename, omitted when none
  ([Unreadable state files](../backend/tasks/collection.md#unreadable-state-files)). `reason` is
  `not_json`, `invalid_pid`, or `invalid_session_id`; `detail` is omitted when empty; `pid` and
  `location` are the live Claude process named by the file (omitted when none). Reported only on
  this route, never on `/ws/tasks/events`.
- `availability`: host [agent availability](../backend/tasks/collection.md#agent-availability) for
  each agent's declared `collect`, `launch`, and `resume` capabilities in registry order, present
  even on failed hosts (`reason`, `detail`, and `observed_version` omitted when empty).
- `agents`: every registered agent ([agent registry](../backend/agent-registry.md)) in registry order with
  `id`, `display_name`, `capabilities`, and fixed adapter `notes` (`notes.launch` omitted when
  empty), including non-collected agents (`detect`/`board` only). A `panemux_fixture_agent`-tagged
  build (never shipped) appends `panemux-fixture-agent` (`detect`, `collect`, `launch`, `summary`;
  see [Adding an agent](../backend/agent-registry.md#adding-an-agent)).
- `tasks`: per host, running tasks in `id` order followed by stopped tasks newest first (`[]` when
  empty).
- `id`: `local:<agent>:<key>` on the panemux host or `ssh:<host>:<agent>:<key>` on an SSH host,
  where `<key>` is the session ID or `pid-<pid>` for a Codex process without a session yet or a
  Claude process without a readable state file.
- `session_id`, `cwd`, `waiting_for`, `status_since`, `started_at`, `pid`, `git`, and
  `location.pane_id` are omitted when unknown. `waiting_for` appears only in `wait`.
- `wait_signature`: present only in `wait` when the agent recorded when the wait began
  ([Wait signature](../backend/tasks/collection.md#wait-signature)).
- `git`: `repo`, `repo_url`, `branch`, `pr_url`, `pr_number`, `issues`, and `autolinks` (each
  omitted when empty). Issues carry positive `number`, HTTP(S) `url`, and optional `repo`
  (`owner/name`, which may differ from the PR repo). Autolinks carry matched `text` and `url`
  ([Metadata rules](../backend/tasks/lifecycle.md#repository-branch-pull-request-issues-and-references)).
- `done` and `labels`: the task's record, omitted when false/empty.
- `actions`: declared task [actions](../backend/tasks/collection.md#agent-availability) (`resume`,
  `summary` with `status`, `reason`, `detail`; `{}` when none). `state_note`: fixed text from the
  collect adapter (omitted when empty).
- `summaries_error`: present only while summaries are enabled and persistence fails
  ([Saved summaries](../backend/tasks/lifecycle.md#saved-summaries-and-codex-specifics)); summaries are still generated
  and displayed.
- `summaries_enabled`: `task_dashboard.summary.enabled`. `summary` appears only while true and only
  for summarized or in-flight tasks ([Summaries](../backend/tasks/lifecycle.md#summaries)): `state`
  is `pending`, `ready`, `error` (with `error`), or `unreadable`; `text`, `remaining` (omitted when
  empty), and `summarized_at` retain the last answer regardless of `state`; `outdated` is true when
  the log changed since that answer/attempt; `done_candidate` is true when ready, current, and
  nothing remains; `unexpected_model` names a non-Haiku answer model (omitted otherwise).
- `records_error`: present only when the record file could not be read (tasks listed without
  records).
- `known_labels`: deduplicated labels in the record file in case-insensitive alphabetical order
  ([Done and labels](../backend/tasks/lifecycle.md#done-and-labels)), omitted when empty or when
  `records_error` is present.
- No bearer token required ([current boundaries](../overview.md#current-boundaries)). The
  [shared browser request guard](../security/auth.md#shared-browser-request-guard) rejects
  cross-authority requests with `403` before dialing hosts while preserving header-free CLI access;
  the same guard applies to every mutation below.

## `POST /api/tasks/hosts/{name}/reconnect`

Drops the named `ssh_connections` host's dashboard connection and remembered failure so the next
`GET /api/tasks` dials immediately. Returns `204`, `404` for an unknown host, or `403` cross-site.

## `PUT /api/tasks/records`

Replaces one task's record:

```json
{ "host": "", "agent": "claude", "session_id": "7c21e0a4", "done": true, "labels": ["dashboard"] }
```

- `host` is `""` or an `ssh_connections` key; `agent` is `claude` or `codex`; `session_id` matches
  `^[a-zA-Z0-9_-]+$`. All fields are replaced (omitting `labels` clears them); unknown fields are
  refused.
- `200`: stored record with normalized `labels` and both `done` and `labels` present.
- `400`: invalid body/record (reason in body); `404`: unknown `host`, except clearing (`done: false`
  and no labels) is allowed for any host so removed hosts can be cleaned up; `403`: cross-site;
  `500`: read/write failure (nothing changed).
- Does not collect; the dashboard applies the response immediately and ignores any in-flight
  `GET /api/tasks`.

## `POST /api/tasks`

Starts a new task:

```json
{ "host": "", "agent": "claude", "cwd": "/workspace/user/project", "prompt": "Fix the flaky test", "labels": ["payment"] }
```

- Every field except `labels` is required (`agent` must declare `launch`, currently `claude` or
  `codex`, otherwise `agent must be one of: claude, codex`; `cwd`/`prompt` follow
  [Starting a task](../backend/tasks/lifecycle.md#starting-a-task)). Unknown fields are refused.
- `201` (Claude):

  ```json
  { "id": "local:claude:0f0e0d0c-0b0a-4908-8706-050403020100", "session_id": "0f0e0d0c-0b0a-4908-8706-050403020100", "tmux_session": "task-0f0e0d0c", "labels": ["payment"] }
  ```

  `labels` is omitted when none were given; `records_error` appears instead if saving failed.
  Codex answers with `tmux_session` (and `pending_labels` when given):

  ```json
  { "tmux_session": "task-0a1b2c3d", "pending_labels": ["payment"] }
  ```

- `400` invalid body/agent/label/cwd/prompt; `404` unknown `host`; `409` host refusal (missing
  tmux or CLI—including cached probe `cli_missing` before execution, see
  [Agent availability](../backend/tasks/collection.md#agent-availability)—Codex lacking
  `--no-daemon`, missing directory, existing tmux session, tmux failure, temp file failure, each
  with a fixed message); `502` host unreachable/no answer; `403` cross-site.

## `POST /api/tasks/resume`

Resumes a stopped task for an agent declaring `resume` (`claude` default when omitted, or `codex`):

```json
{ "host": "", "agent": "codex", "session_id": "01a0e2b9-d054-7cc2-9278-a5e25ebcc524" }
```

- `200`: `id`, `session_id`, and `tmux_session`.
- `400` invalid body, non-resuming agent, invalid `session_id` (UUID for both), or unknown/refused
  working directory; `404` unknown `host`; `409` not a stopped task of that agent on the host or
  host refusal as in `POST /api/tasks`; `502` collection/reachability failure; `403` cross-site.

## `POST /api/tasks/attach`

Opens the board's temporary attach
([Opening a task](../backend/tasks/lifecycle.md#opening-a-task-view-state-and-temporary-attaches)) to a task's tmux session:

```json
{ "id": "local:claude:7c21e0a4" }
```

- Server recollects the task's host and takes tmux session and host from that collection, never the
  request.
- `201` (new attach) or `200` (existing attach for the task):

  ```json
  { "session_id": "board-0123456789abcdef", "tmux_session": "task-7c21" }
  ```

  Terminal streams on `/ws/{session_id}`; not listed by `GET /api/sessions`.
- `400` invalid body or empty `id`; `404` task no longer reported or unknown host; `409` task
  outside tmux or unattachable session; `502` collection/attach failure; `403` cross-site.

`DELETE /api/tasks/attach/{session_id}` ends the attach immediately (`204`; `404` for non-attach
IDs including pane sessions; `403` cross-site).

## `POST /api/tasks/summary`

Requests a summary for one task:

```json
{ "host": "", "agent": "codex", "session_id": "5d7e3a90-1b2c-4d3e-8f40-51627384a5b6" }
```

- `agent` accepts `claude` (default when omitted) or `codex` (UUID-only). Starts a summary unless
  ready or running for the current log; retries failed attempts. Does not collect (session must be
  in the host's last collection with a log).
- `202` immediately with the task's `summary` object (usually `pending`); the result arrives via
  `GET /api/tasks`.
- `400` invalid body or `session_id` not matching `^[a-zA-Z0-9_-]+$`; `404` session/host not in last
  collection with a log; `409` summaries disabled; `503` server shutting down; `403` cross-site.

## `POST /api/tasks/input-given`

Marks a Codex permission-request wait as answered
([Codex permission requests](../backend/tasks/collection.md#codex-permission-requests)):

```json
{ "wait_signature": "w1-<hex>" }
```

- Next collection shows the task as its turn reads without the request (usually `busy`). Unknown
  signatures change nothing.
- `204`; `400` invalid body or empty `wait_signature`; `403` cross-site.

## `GET` and `POST /api/tasks/codex-hook`, `POST /api/tasks/codex-hook/auto-update`

Checks, installs/updates, or toggles auto-update for panemux's Codex permission hook
([Codex permission requests](../backend/tasks/collection.md#codex-permission-requests)):

```text
GET  /api/tasks/codex-hook?host=gpu-box
POST /api/tasks/codex-hook   { "host": "gpu-box" }
```

- `host` is an `ssh_connections` key or empty/omitted for the panemux host; paths and hook content
  are always panemux's own.
- `200` with `{ "status": "<status>", "auto_update": <bool> }`, optional `"error"` (for `invalid`
  and `unknown`), optional `"auto_updated": true` or `"auto_error"`, and optional
  `"auto_settings_error"` (forcing `"auto_update": false`). `status` is `absent`, `missing`,
  `installed`, `outdated`, `invalid`, or `unknown`. `POST` reads back status after writing and
  writes nothing when already `installed`; `GET` runs auto-update when `auto_update` is enabled and
  status is `outdated`.
- `404` unknown host; `POST` returns `502` with reason when absent, `invalid`, or check/write fails
  (and `400` on strict-decode failure). During an update, `GET` returns `200` with `unknown` and
  `"error": "tool is updating"`, while overlapping `POST` writes nothing and returns `502`. Both
  return `403` cross-site.
- `POST /api/tasks/codex-hook/auto-update` with `{ "host": "gpu-box", "enabled": true }` toggles
  the setting without touching the host: `200` `{ "auto_update": true }`, `404` unknown host, `400`
  strict-decode error, `500` settings file read/write error, `403` cross-site.

## `POST /api/hosts/session-name`

Generates an `ssh_tmux` session name for a host
([Opening a host](../backend/tasks/lifecycle.md#opening-a-host)):

```json
{ "connection": "gpu-box" }
```

- `200` `{ "tmux_session": "gpu-box-1a2b3c4d" }` (fresh name each call; none stored).
- `400` invalid body or empty `connection`; `404` unknown `ssh_connections` key; `403` cross-site.

## `POST /api/hosts/terminal`

Opens a temporary host terminal ([Opening a host](../backend/tasks/lifecycle.md#opening-a-host)):

```json
{ "connection": "gpu-box", "type": "ssh_tmux", "tmux_session": "gpu-box-1a2b3c4d" }
```

- `type` is `ssh` or `ssh_tmux`. `tmux_session` is valid for `ssh_tmux` only and must pass
  [tmux session rules](../security/process-and-ssh.md#tmux-sessions) (generated when omitted).
- `201` `{ "session_id": "board-…", "tmux_session": "gpu-box-1a2b3c4d" }` (`tmux_session` is `""`
  for `ssh`); streams on `/ws/{session_id}`.
- `400` invalid body, empty `connection`, invalid `type`, `tmux_session` on `ssh`, or invalid name;
  `404` unknown connection; `502` start failure; `403` cross-site; `429` when `4` active (non-exited)
  host terminals are already open on that connection.

`DELETE /api/hosts/terminal/{session_id}` ends the terminal immediately (`204`; `404` for non-host
terminal IDs including task attaches and pane sessions; `403` cross-site).

## `GET /ws/tasks/events`

The live stream of running agent task state changes. The server sends JSON text frames; anything
the client sends is ignored. Observation lifecycle and browser receiver rules live in
[Task events](../backend/tasks/events.md).

- **Unauthenticated**, like [`/ws/{sessionID}`](workspace.md#terminal-websocket-protocol) and
  [`GET /api/tasks`](#get-apitasks) (no bearer token).
- **Cross-authority requests return `403` before upgrading or collecting**, under the
  [WebSocket browser connection policy](workspace.md#websocket-browser-connection-policy)
  (protecting host dialing while preserving header-free CLI compatibility).
- Client frames are capped at `512 bytes`; a larger frame closes the connection.

### Frames

Every frame carries `type`, `epoch`, and `seq`. The first frame on every connection is a `snapshot`
of all currently known hosts and running tasks:

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

Subsequent frames report one change each in order:

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
| `task` | `added` | `task`: new task view |
| `task` | `changed` | `task`: new view; `prev_state`: prior state |
| `task` | `removed` | `task`: last observed view; `prev_state`: its state |
| `host` | `added`, `changed`, `removed` | `host` |

A state transition is a `changed` frame where `prev_state != task.state`; a `changed` frame with the
same state indicates another view field changed (`wait_id`, `waiting_for`, `cwd`, or `location`). A
task first seen already waiting arrives as `added` in `wait`.

### Task view

| Field | Meaning |
|---|---|
| `id`, `host`, `agent`, `session_id`, `cwd` | Same as [`GET /api/tasks`](#get-apitasks) |
| `state` | `busy`, `wait`, `idle`, `run`, or `unknown` |
| `waiting_for` | Agent-reported wait reason; present only in `wait` |
| `wait_id` | Wait identity (`w1-…` signature or `e1-<epoch>-<seq>`, see [The wait ID](../backend/tasks/events.md#the-wait-id)); present only in `wait` |
| `status_since` | Server-clock entry time as of this frame; not compared for diffing ([Tasks and changes](../backend/tasks/events.md#tasks-and-changes)) |
| `location` | `kind`, `tmux_session`, `pane_id`, `attachable` ([Where a task runs](../backend/tasks/lifecycle.md#where-a-task-runs)) |

No frame carries conversation text, a prompt, a command line, or a process ID.

### Host view

`name` (`""` for the panemux host), `status` (`pending`, `ok`, `connecting`, or `error`; see
[Hosts](../backend/tasks/events.md#hosts)), and `error` when `status` is `error`.

### Ordering: `epoch` and `seq`

`epoch` is chosen at server startup and changes only on restart. `seq` monotonically numbers every
published frame across all hosts and tasks in that epoch. A snapshot's `seq` is the last change it
includes, so the next frame on the same connection is `seq + 1`.

A receiver observing a `seq` gap, backward `seq`, changed `epoch`, or unknown `type` discards its
state and reconnects: **recovery is always a fresh snapshot** (the server retains no replay history).
