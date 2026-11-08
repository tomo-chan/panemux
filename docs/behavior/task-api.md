---
type: Specification
title: "Task and host terminal REST API"
description: "Task and host terminal payloads, response fields, status codes, and failure contracts."
---

# Task and host terminal REST API

Part of the [task dashboard](tasks.md).

## `GET /api/tasks`

Collects from every host and returns:

```json
{
  "hosts": [{ "name": "", "status": "ok", "collected_at": "2026-09-25T12:00:00Z",
    "availability": [{ "agent": "claude", "capability": "collect", "status": "available" }] }],
  "agents": [{ "id": "claude", "display_name": "Claude Code",
    "capabilities": ["detect", "collect", "launch", "resume", "workdir", "summary", "board"], "notes": {} }],
  "tasks": [{ "id": "local:claude:7c21e0a4", "host": "", "agent": "claude",
    "session_id": "7c21e0a4", "cwd": "/workspace/user/project", "state": "wait",
    "waiting_for": "input needed", "status_since": "2026-09-25T11:57:00Z",
    "location": { "kind": "tmux", "tmux_session": "task-7c21", "attachable": true },
    "actions": { "summary": { "status": "available" } } }],
  "summaries_enabled": false
}
```

- `hosts` lists the panemux host first, then `ssh_connections` keys in name order. `status` is `ok`,
  `error` (with `error`) or `connecting`; `collected_at` is present when `status` is `ok`.
- `unreadable_state_files` lists the host's state files that could not be read, by file name, and is
  omitted when there are none ([Unreadable state files](task-collection.md#unreadable-state-files)). `reason` is
  `not_json`, `invalid_pid` or `invalid_session_id`; `detail` is omitted when empty; `pid` and
  `location` are the live claude process the file name names, omitted when it names none. Only
  this route reports them: the task event stream does not.
- `availability` is the host's [agent availability](task-collection.md#agent-availability): each agent's `collect`,
  `launch` and `resume`, as it declares them, in registry order. It is present on every host, one that
  failed included. `reason`, `detail` and `observed_version` are omitted when empty.
- `agents` is every registered agent ([agent registry](../agent-registry.md)), in registry order:
  its `id`, `display_name`, the `capabilities` it declares, and the fixed `notes` its adapters give
  on its own constraints — `notes.launch` is what to know before starting one, omitted when there is
  nothing. It lists agents the task board does not collect as well (`detect` and `board` only).
  A build with the `panemux_fixture_agent` tag, which no shipped binary is, lists a third agent
  after the shipped ones, `panemux-fixture-agent` (detect, collect, launch and summary). Its tasks,
  availability and actions are reported by the same rules as Claude Code's and codex's
  ([Adding an agent](../agent-registry/adding-an-agent.md)).
- `tasks` lists each host's running tasks in `id` order, then its stopped tasks newest first. It is
  `[]` when there are none.
- `id` is `local:<agent>:<key>` for the panemux host and `ssh:<host>:<agent>:<key>` for an SSH host,
  where the key is the session ID, `pid-<pid>` for a codex process with no session yet and for a
  claude process no readable state file names.
- `session_id`, `cwd`, `waiting_for`, `status_since`, `started_at`, `pid`, `git` and
  `location.pane_id` are omitted when unknown. `waiting_for` is present only in the `wait` state.
- `wait_signature` is present only in the `wait` state, and only when the agent recorded when the
  wait began ([Wait signature](task-collection.md#wait-signature)).

- `git` carries `repo`, `repo_url`, `branch`, `pr_url`, `pr_number`, `issues` and `autolinks`,
  each omitted when empty. Issues carry positive `number`, HTTP(S) `url` and optional `repo`
  (`owner/name`, which can differ from the PR repository). Autolinks carry matched `text` and `url`.
  [Metadata rules](task-records.md#repository-branch-pull-request-issues-and-references) own lookup,
  filtering and reference matching.
- `done` and `labels` are the task's record, omitted when it is not done or has no labels.
- `actions` are the task's [actions](task-collection.md#agent-availability) — `resume` and `summary`, each with a
  `status`, a `reason` and a `detail` — and is `{}` when it offers none. `state_note` is a fixed
  text the task's agent's collect adapter gives on what its state means (codex's `run`, a session in
  codex's shared daemon), omitted when it says nothing.
- `summaries_error` is why summaries are not being saved ([Saved summaries](task-summaries.md#saved-summaries)); they
  are still made and shown. It is present only while summaries are enabled and saving fails.
- `summaries_enabled` is `task_dashboard.summary.enabled`. `summary` is present only while it is
  true, and only for a task that has been summarized or is being summarized
  ([Summaries](task-summaries.md#summaries)). Its `state` is `pending` (a summary is running or waiting to run),
  `ready`, `error` (with `error`) or `unreadable`. `text`, `remaining` and `summarized_at` are the
  last answer whenever there is one, whatever `state` says; `outdated` is true when the log has
  changed since that answer — or, for `unreadable` and `error`, since that attempt — and `done_candidate` when the answer is ready and current and lists
  nothing remaining. `remaining` is omitted when empty. `unexpected_model` is the model that made the
  last answer when it was not a Haiku, and is omitted otherwise.
- `records_error` is present only when the record file could not be read; the tasks are then listed
  without records.
- `known_labels` is every label in the record file, listed task or not, once each, in
  case-insensitive alphabetical order ([Done and labels](task-records.md#done-and-labels)). It is omitted when the
  file holds no labels and when `records_error` is present.
- The request answers `200` even when every host failed; failures are in `hosts`.
- No bearer token is required ([current boundaries](../overview.md#current-boundaries)).
  The [shared browser request guard](../security/auth.md#shared-browser-request-guard) rejects
  cross-authority requests with `403` before dialing hosts, with header-free CLI compatibility.
  The same guard applies to every mutation below.

## `POST /api/tasks/hosts/{name}/reconnect`

Drops the named host's dashboard connection and any remembered connection failure, so the next
`GET /api/tasks` dials it at once. `name` is an `ssh_connections` key. Returns `204`, `404` for a
name that is not one, or `403` for a cross-site request as above.

## `PUT /api/tasks/records`

Replaces the record of one task:

```json
{ "host": "", "agent": "claude", "session_id": "7c21e0a4", "done": true, "labels": ["dashboard"] }
```

- `host` is `""` for the panemux host or an `ssh_connections` key; `agent` is `claude` or `codex`;
  `session_id` must match `^[a-zA-Z0-9_-]+$`. Every field is replaced: a request without `labels`
  clears them. An unknown field is refused.
- Answers `200` with the record as stored — labels normalized, both `done` and `labels` always
  present. `400` for a body or record that is not valid (the reason is in the body), `404` for a
  `host` that is not an `ssh_connections` key — except on a request that clears the record
  (`done` false and no labels), which is accepted for any host so a removed host's records can be
  cleared — `403` for a cross-site request as above, and `500` when the file cannot be read or
  written; nothing is changed then.
- It does not collect: the dashboard applies the answer to the task at once, and ignores a
  `GET /api/tasks` that was already running when the record was saved.

## `POST /api/tasks`

Starts a task:

```json
{ "host": "", "agent": "claude", "cwd": "/workspace/user/project", "prompt": "Fix the flaky test", "labels": ["payment"] }
```

- Every field but `labels` is required in effect: `agent` must be an agent that declares `launch`
  (`claude` or `codex`; otherwise `agent must be one of: claude, codex`), and `cwd` and
  `prompt` follow [Starting a task](task-launch.md#starting-a-task). An unknown field is refused.
- Answers `201`:

  ```json
  { "id": "local:claude:0f0e0d0c-0b0a-4908-8706-050403020100", "session_id": "0f0e0d0c-0b0a-4908-8706-050403020100", "tmux_session": "task-0f0e0d0c", "labels": ["payment"] }
  ```

  `id` is the one `GET /api/tasks` lists the task under. `labels` is what was recorded, omitted when
  none were given; `records_error` is present instead when they could not be recorded. A codex task
  answers with `tmux_session` only, plus `pending_labels` — the labels held until its session is
  known — when it was given any:

  ```json
  { "tmux_session": "task-0a1b2c3d", "pending_labels": ["payment"] }
  ```
- `400` for a body, agent, label, directory or instruction that is not valid, `404` for a `host`
  that is not an `ssh_connections` key, `409` when the host refused — tmux or the agent missing (the
  agent's also when the host's probe found its CLI missing, before anything runs; see
  [Agent availability](task-collection.md#agent-availability)), a codex
  without `--no-daemon`, the directory missing, a tmux session of that name existing, tmux failing, the temporary file not
  written, each with a fixed message — `502` when the host could not be reached or did not answer,
  and `403` for a cross-site request as for `GET /api/tasks`.

## `POST /api/tasks/resume`

Resumes a stopped task of an agent that declares `resume` (claude or codex):

```json
{ "host": "", "agent": "codex", "session_id": "01a0e2b9-d054-7cc2-9278-a5e25ebcc524" }
```

- `agent` is `claude` (the default when it is omitted) or `codex`, the agents that declare `resume`.
- Answers `200` with `id`, `session_id` and `tmux_session` as above.
- `400` for a body that is not valid, an agent that does not resume, or a `session_id` the agent's resume does not accept (a UUID for both),
  or a stopped session whose working directory is unknown or refused; `404` for an unknown `host`;
  `409` when the session is not a stopped task of that agent on the host, or the host refused as above; `502` when the host could not be
  collected or reached; `403` for a cross-site request.

## `POST /api/tasks/attach`

Opens the board's temporary attach ([Opening a task](task-navigation.md#opening-a-task)) to a task's tmux session:

```json
{ "id": "local:claude:7c21e0a4" }
```

- `id` is a task ID from `GET /api/tasks`. The server collects that task's host again and takes the
  tmux session and host from it, never from the request.
- Answers `201` with the new attach, or `200` with the one already open for the task:

  ```json
  { "session_id": "board-0123456789abcdef", "tmux_session": "task-7c21" }
  ```

  The terminal is `/ws/{session_id}`. The session is not listed by `GET /api/sessions`.
- `400` for a body that is not valid or an empty `id`; `404` for a task the host no longer reports
  or an ID naming no host; `409` for a task outside tmux or in a session a pane cannot attach to;
  `502` when the host could not be collected or the attach failed (the session has ended, tmux or
  the SSH connection failed); `403` for a cross-site request.

`DELETE /api/tasks/attach/{session_id}` ends the attach at once and answers `204`; `404` for an ID
that is not a board attach (a pane's session included), and `403` for a cross-site request.

## `POST /api/tasks/summary`

Asks for one task's summary:

```json
{ "host": "", "agent": "codex", "session_id": "5d7e3a90-1b2c-4d3e-8f40-51627384a5b6" }
```

- `agent` accepts `claude` or `codex`; omitted means `claude` for older clients. Codex IDs must be UUIDs.
- Starts a summary unless the one for the log as it is now is ready or running; a failed one is
  retried. It does not collect: the session must be listed with a log by the host's last collection.
- Answers `202` at once with the task's `summary` as `GET /api/tasks` reports it (usually
  `pending`); the summary itself arrives with a later `GET /api/tasks`.
- `400` for a body that is not valid or a `session_id` that does not match `^[a-zA-Z0-9_-]+$`, `404`
  for a session the host's last collection did not list with a log (an unknown host included), `409`
  when summaries are disabled, `503` while panemux is shutting down, and `403` for a cross-site
  request as for `GET /api/tasks`.

## `POST /api/tasks/input-given`

Marks the codex permission request a task waits on as answered ([Codex permission
requests](task-codex-permissions.md#codex-permission-requests)):

```json
{ "wait_signature": "w1-<hex>" }
```

- `wait_signature` is the task's from `GET /api/tasks`. From the next collection that task is shown as
  its turn reads without the request (usually `busy`). A signature that names no permission request
  changes nothing.
- Answers `204`; `400` for a body that is not valid or an empty `wait_signature`; `403` for a
  cross-site request.

## `GET` and `POST /api/tasks/codex-hook`, `POST /api/tasks/codex-hook/auto-update`

Whether a host's codex has panemux's permission hook, and installing or updating it ([Codex
permission requests](task-codex-permissions.md#codex-permission-requests)):

```
GET /api/tasks/codex-hook?host=gpu-box
POST /api/tasks/codex-hook   { "host": "gpu-box" }
```

- `host` is an `ssh_connections` key, or empty (or absent from the query) for the panemux host. The
  request names nothing else: the paths and the content written are panemux's own.
- Both answer `200` with `{ "status": "<status>" }`, and `"error"` with the reason for `invalid` and
  `unknown`. The status is `absent` (no codex), `missing`, `installed`, `outdated`, `invalid` (a
  `hooks.json` panemux will not rewrite) or `unknown` (the check could not run). `POST` answers the
  status read back after writing, and writes nothing for a host already `installed`.
- Both also carry `"auto_update"`, whether the host's hook is updated by itself, and, once this
  panemux process did so, `"auto_updated": true` or `"auto_error"` with why it failed. A setting that
  could not be read gives `"auto_settings_error"` with why, and `"auto_update": false`. `GET` runs
  that update itself when it finds `outdated`
  ([`Update automatically`](task-codex-permissions.md#codex-permission-requests)), and answers the status read back after it.
- `404` for a host that is not in `ssh_connections`. `POST` answers `502` with the reason when the
  host has no codex, its `hooks.json` is `invalid`, or the check or the write failed, and `400` for a
  body it cannot decode strictly. During an update `GET` answers `200` with `unknown` and
  `"error": "tool is updating"`; an overlapping `POST` writes nothing and answers `502` with that
  reason, as for other write failures. Both answer `403` to a cross-site request.
- `POST /api/tasks/codex-hook/auto-update` with `{ "host": "gpu-box", "enabled": true }` turns
  `Update automatically` on or off for that host and answers `200` with `{ "auto_update": true }`.
  It touches nothing on the host. `404` for a host not in `ssh_connections`, `400` for a body it
  cannot decode strictly, `500` with the reason when the settings file cannot be read or written,
  `403` for a cross-site request.

## `POST /api/hosts/session-name`

Generates a tmux session name for an `ssh_tmux` terminal on a host ([Opening a host](task-navigation.md#opening-a-host)):

```json
{ "connection": "gpu-box" }
```

- Answers `200` with `{ "tmux_session": "gpu-box-1a2b3c4d" }`. Each request returns a new name; the
  server keeps none of them.
- `400` for a body that is not valid or an empty `connection` (the panemux host); `404` for a
  connection that is not in `ssh_connections`; `403` for a cross-site request.

## `POST /api/hosts/terminal`

Opens a new host terminal ([Opening a host](task-navigation.md#opening-a-host)):

```json
{ "connection": "gpu-box", "type": "ssh_tmux", "tmux_session": "gpu-box-1a2b3c4d" }
```

- `type` is `ssh` or `ssh_tmux`. `tmux_session` is for `ssh_tmux` only and must pass the tmux
  session name rule ([security](../security/command-execution.md#tmux-session-name-tmux-ssh_tmux-sessions)); without one
  the server generates it as `POST /api/hosts/session-name` does.
- Answers `201` with `{ "session_id": "board-…", "tmux_session": "gpu-box-1a2b3c4d" }`
  (`tmux_session` is `""` for `ssh`). The terminal is `/ws/{session_id}`.
- `400` for a body that is not valid, an empty `connection`, another `type`, a `tmux_session` on
  `ssh` or one the rule refuses; `404` for a connection that is not in `ssh_connections`; `502` when
  the terminal could not be started; `403` for a cross-site request; `429` when 4 host terminals
  are already open on that connection (one whose shell has exited does not count).

`DELETE /api/hosts/terminal/{session_id}` ends it at once and answers `204`; `404` for an ID that is
not a host terminal (a task's board attach and a pane's session included), and `403` for a
cross-site request.
