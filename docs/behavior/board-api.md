---
type: Specification
title: "Behavior: Agent Board REST API"
description: "Agent Board REST payloads, authorization, status codes, and operations."
---

# Behavior: Agent Board REST API

> Part of the [behavior specification](../behavior.md).

## Agent Board REST API

Every `/api/board/*` route requires `Authorization: Bearer <server.auth_token>`; invalid or missing
credentials return `401` before handler processing. The
[shared browser request guard](../security/auth.md#shared-browser-request-guard) runs first,
returning `403` for refused browser authority/evidence even with a valid token. `GET /api/session-token` is the deliberate
exception described below.

### `GET /api/session-token`

Returns browser bootstrap data:

```json
{ "token": "a1b2c3...", "command_center_enabled": true, "agent_board_enabled": true }
```

The endpoint is unauthenticated because the browser uses it to learn a generated token. The
shared browser request guard also applies before token disclosure. It returns
`403` when the socket peer is not loopback, when `Host` is not `localhost`, a loopback address, or
`0.0.0.0`, or when any `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` value is present. Forwarding
headers are rejected regardless of the addresses they contain. Valid direct requests return `200`.
The rationale and accepted reverse-proxy limitation are specified in
[Auth token and transport encryption](../security/auth.md#loopback-token-bootstrap).

The capability flags are independent. `agent_board_enabled` reflects top-level `agent_board.enabled`;
`command_center_enabled` reflects command-center configuration. The frontend uses them to expose
the two surfaces separately.

For every eligible configured pane, background polling detects supported agents for display
and checks host availability after two consecutive detections. It never writes to a PTY. There is no REST endpoint
for this process; see [Bootstrap flow](../agent-board/bootstrap.md#bootstrap-flow).

### `GET /api/board/status`

Returns the current in-memory identity, status and participation snapshots without contacting agmsg
or collecting tasks.
`participation` is keyed by pane ID and includes host, detected registry agent type (empty when none),
`instructed` (always false, retained for compatibility), and optional `last_report`.
Detection follows the current pane session. Reports can outlive a session.
The dashboard lists a pane when its agent type is non-empty or a status report exists.

```json
{
  "identities": {
    "pane-a@local": {
      "host": "local", "team": "panemux", "name": "pane-a", "pane_id": "pane-a",
      "source": "self_report", "state": "working", "waiting_for": null,
      "status_since": null, "task_observed_at": null, "last_message_at": null,
      "self_report": {
        "updated_at": "2026-08-10T12:00:00Z", "state": "working",
        "cwd": "/workspace/user/project", "branch": "feature/x", "repo": "owner/repo",
        "pr_url": "https://github.com/owner/repo/pull/123",
        "last_tool": "Edit internal/api/handler.go", "summary": "Fixing the failing relay tests"
      }
    }
  },
  "participation": {
    "pane-a": {
      "pane_id": "pane-a", "host": "local", "agent_type": "codex", "instructed": false,
      "last_report": "2026-08-10T12:00:00Z"
    }
  },
  "statuses": {
    "pane-a": {
      "updated_at": "2026-08-10T12:00:00Z",
      "state": "working",
      "cwd": "/workspace/user/project",
      "branch": "feature/x",
      "repo": "owner/repo",
      "pr_url": "https://github.com/owner/repo/pull/123",
      "last_tool": "Edit internal/api/handler.go",
      "summary": "Fixing the failing relay tests"
    }
  }
}
```

Fields other than `updated_at` are omitted when unreported. An empty cache returns `200` with
`{"identities":{},"participation":{},"statuses":{}}`, never `null`.

#### Identity entries

`identities` is always present and maps `name@BoardID` to the following fields. The team is
the single configured `agent_board.team`; `local` and `ssh:local` are distinct hosts.

| Field | Type and meaning |
|---|---|
| `host`, `team`, `name` | Strings identifying the observed name and its source |
| `pane_id` | Current matching pane ID, or null; never an invented pane link |
| `source` | `task`, `self_report`, or `unreported` |
| `state` | Task state or arbitrary self-reported state string, or null |
| `waiting_for` | Observed task waiting reason, or null |
| `status_since` | Observed task StatusSince as RFC3339, or null |
| `task_observed_at` | Successful task collection time as RFC3339, or null |
| `last_message_at` | Latest source message time among retained history rows, or null |
| `self_report` | Latest self-report with the same fields as a `statuses` entry, or null |

All nullable fields are emitted, including null values. A report with no state has
`source: "self_report"` and `state: null`. Its `updated_at` is the cache reception time,
not the start of that state. Self-reported text is untrusted and does not prove liveness.

The list combines host-qualified status reports, senders in the validated bounded history,
and current panes with a detected agent or a corresponding task observation. Rejected rows,
qualified `from` values and `_system` never introduce identities; a recipient alone does not
introduce an identity. A name observed only in history disappears when its last row leaves
the existing 2000-row ring. Reports retain their existing in-memory lifetime. No roster is read.

A matching pane requires the same host and name equal to its current configured pane ID.
Task priority additionally requires its location to match that pane: the same tmux session and
pane kind, or an outside task claiming that local/SSH pane on the same connection. No new
agmsg-name-to-session mapping is inferred. Task priority requires exactly one live task for
that pane and exactly one matching pane for that task across all workspaces on the same host.
Only this one-to-one match supplies State, WaitingFor and StatusSince; its self-report remains
supplemental. Multiple tasks matching one pane, or multiple panes sharing a task’s tmux session,
fall back to self-report or unreported, as does a failed or pending latest host collection.
Equal tmux session names on different hosts do not make a match ambiguous.

Task observations come only from existing `/api/tasks` and task-event collections. Without
such a collection there is no task priority. Successful collections replace that host’s
observations; status reads do not refresh them. Full and live collections receive a monotonic
in-process generation when collection starts (one generation for a full multi-host collection).
Each host retains its newest applied generation, including failed or pending results with no tasks.
Older results, whether successful or failed, are ignored, so a slow host cannot let an older
full collection restore local task priority after a newer live failure. Wall-clock timestamps
are display metadata and do not determine this ordering. `task_observed_at` shows observation age.
There is no added polling or expiry timer. Panes without observations or detected agents are omitted.

Successfully adding, deleting or changing an `ssh_connections` entry clears that key’s task
observations and advances its generation watermark. In-flight results from before the change
cannot repopulate the cache, including after deletion and re-addition of the same target.
Unchanged entries and failed configuration saves preserve their observations. A removed host’s
results are ignored. Task priority resumes only after a new successful collection for the current
configuration; self-reports retain their existing independent lifetime.

The task-event publisher stages Board cache updates until it accepts a live result. Results it
discards because observation stopped or the host was removed do not update Board task priority.

For example, `worker@local` and `worker@ssh:build` are independent entries, both with
`pane_id: null` unless a configured pane named `worker` exists on that same host. Neither
listing nor absence is authorization, registration, reachability, or delivery assurance.

### `GET /api/board/messages?since=<seq>`

Returns cached rows after the panemux-local sequence `since`, which defaults to `0`. This sequence
is unrelated to agmsg's opaque, host-local message ID.

The original `host`, `team`, `from` and `to` are preserved. For a normal sender, combine
`from` with the source `host` to obtain its qualified reply identity. A qualified `to` already
names its host. An ordinary unqualified recipient is host-local, except for the existing pane-ID
compatibility route to that pane’s owning host. `_system` remains a reserved endpoint.
History does not snapshot past pane ownership: after a pane moves, its old unqualified
recipient host cannot always be reconstructed. No additional identity fields are inferred.

```json
{
  "messages": [
    {
      "seq": 42,
      "host": "local",
      "team": "panemux",
      "from": "pane-a",
      "to": "pane-b",
      "body": "please review",
      "at": "2026-08-10T12:00:00Z",
      "is_status": false
    }
  ],
  "epoch": "3f1c9a2b7d4e5061"
}
```

`is_status` lets clients exclude status JSON from conversation history. `epoch` identifies the
in-memory cache instance; clients must reset `since` to `0` when it changes because sequences restart
after a panemux restart. The epoch is opaque and equality-only.

- `400`: `since` is not an integer.
- `200`: rows returned; no rows is `"messages":[]`, never `null`.

### `POST /api/board/broadcast`

Sends one message to each resolved recipient’s host without PTY injection:

```json
{ "to": ["pane-a", "worker@local", "worker@ssh:build"], "body": "please review" }
```

Qualified recipients need only a configured Board host, without a pane or roster entry.
Unqualified recipients must remain existing pane IDs; arbitrary bare names are not resolved
by broadcast. Parsing and resolution use the shared relay rules and SSH-key validator.
Every recipient is resolved before delivery starts. After that validation, delivery is fail-fast but
not transactional: a later host failure may follow successful earlier sends. History reflects the
message after relay polling reads it back.

- `400`: malformed JSON.
- `422`: empty recipients or body, NUL in the body, or any invalid/unknown recipient
  (including unknown hosts and qualified `_system`). No send starts.
- `502`: downstream send failure, with `{"error":"...","delivered":[...]}`.
- `200`: `{"delivered":[...]}` containing every reached recipient in its input spelling and input order.

### `GET /api/board/command/history`

Returns chronological command-center entries captured from the structured subprocess stream:

```json
{
  "entries": [
    { "at": "2026-08-10T12:00:00Z", "raw": { "type": "panemux_prompt", "text": "Summarize the board" } },
    { "at": "2026-08-10T12:00:03Z", "raw": { "type": "result", "result": "..." } }
  ]
}
```

There is no pagination. Missing or empty history returns `200` with `"entries":[]`; an existing but
unreadable history file returns `500`.
