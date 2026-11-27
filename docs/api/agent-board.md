---
type: Specification
title: "Agent Board REST and Command Center WebSocket API"
description: "Session-token bootstrap, authenticated /api/board/* REST routes, and the /ws/board/command WebSocket protocol."
---

# Agent Board REST and Command Center WebSocket API

HTTP and WebSocket wire contracts for [Agent Board](../agent-board.md) and the command center.
Security requirements live in [Authentication](../security/auth.md) and
[Agent Board security](../security/agent-board.md); YAML configuration lives in
[Agent Board configuration](../agent-board/api-and-config.md#config-additions).

## Authorization and endpoint summary

Every `/api/board/*` route requires `Authorization: Bearer <server.auth_token>`; missing or invalid
credentials return `401` before handler processing. The
[shared browser request guard](../security/auth.md#shared-browser-request-guard) runs first and
returns `403` for refused browser authority/evidence even with a valid token. `GET /api/session-token`
sits deliberately outside the authenticated subtree, and all non-board `/api/*` and `/ws/{sessionID}`
routes remain unauthenticated.

| Endpoint | Contract |
|---|---|
| `GET /api/session-token` | Loopback-only browser bootstrap for bearer token and capability flags |
| `GET /api/board/status` | Qualified identities and compatible pane snapshots without host I/O |
| `GET /api/board/messages?since=<seq>` | Cached message history after a panemux-local sequence |
| `POST /api/board/broadcast` | Send to qualified names or compatible pane IDs on their owning hosts |
| `GET /api/board/command/history` | Captured command-center conversation entries |
| `GET /ws/board/command` | Subprotocol-authenticated single-session command-center query stream |

## `GET /api/session-token`

Returns browser bootstrap data:

```json
{ "token": "a1b2c3...", "command_center_enabled": true, "agent_board_enabled": true }
```

- Unauthenticated so the bundled frontend can learn a generated token; the shared browser request
  guard runs before disclosure.
- Returns `403` when the socket peer is not loopback, when `Host` is not `localhost`, a loopback
  address, or `0.0.0.0`, or when any `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` header is
  present (regardless of the addresses inside). Valid direct requests return `200`
  ([loopback token bootstrap](../security/auth.md#loopback-token-bootstrap)).
- `agent_board_enabled` (`agent_board.enabled`) and `command_center_enabled` (`command_center.enabled`)
  are independent flags so the frontend exposes the two surfaces separately.
- Background agent detection and host availability checks run automatically without PTY writes and
  have no REST endpoint ([Bootstrap flow](../agent-board/bootstrap.md#bootstrap-flow)).

## `GET /api/board/status`

Returns in-memory identity, status, and participation snapshots without contacting agmsg or
collecting tasks. `participation` is keyed by pane ID with `host`, detected registry `agent_type`
(empty when none), `instructed` (always `false`, retained for compatibility), and optional
`last_report`. Detection follows the current pane session; reports can outlive a session. The
dashboard lists a pane when `agent_type` is non-empty or a status report exists.

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

### Identity entries

`identities` is always present and maps `name@BoardID` to the fields below. `team` is the single
configured `agent_board.team`; `local` and `ssh:local` are distinct hosts.

| Field | Type and meaning |
|---|---|
| `host`, `team`, `name` | Strings identifying the observed name and its source |
| `pane_id` | Current matching pane ID, or `null` (never invented) |
| `source` | `task`, `self_report`, or `unreported` |
| `state` | Task state or arbitrary self-reported state string, or `null` |
| `waiting_for` | Observed task waiting reason, or `null` |
| `status_since` | Observed task `StatusSince` as RFC3339, or `null` |
| `task_observed_at` | Successful task collection time as RFC3339, or `null` |
| `last_message_at` | Latest source message time among retained history rows, or `null` |
| `self_report` | Latest self-report with the same fields as a `statuses` entry, or `null` |

- All nullable fields are emitted including `null`. A report with no state yields
  `source: "self_report"` and `state: null`; `updated_at` is cache reception time, not state start.
  Self-reported text is untrusted and does not prove liveness.
- The map combines host-qualified status reports, senders in validated bounded history, and current
  panes with a detected agent or matching task observation. Rejected rows, qualified `from` values,
  and `_system` never introduce identities; a recipient alone never introduces one. History-only
  names disappear when their last row leaves the `2000`-row ring; reports keep their in-memory
  lifetime. No roster is read.
- A matching pane requires the same host and `name` equal to its current configured pane ID. Task
  priority additionally requires location match (same tmux session and pane kind, or an outside task
  claiming that local/SSH pane on the same connection) with **exactly one** live task for that pane
  and **exactly one** matching pane for that task across all workspaces on that host. Only this
  one-to-one match supplies `state`, `waiting_for`, and `status_since` (keeping `self_report`
  supplemental). Ambiguous matches or failed/pending latest host collection fall back to
  `self_report` or `unreported`. Identical tmux session names on different hosts are not ambiguous.
- Task observations come solely from existing `/api/tasks` and task-event collections (no extra
  polling or expiry timer; status reads never refresh them). Each collection receives a monotonic
  in-process generation at start; each host retains its newest applied generation (including failed
  or pending empty results) and ignores older results regardless of wall-clock timestamps.
- Adding, deleting, or editing an `ssh_connections` entry under the config lock after save discards
  that key's pooled execution connection and pending dial, clears its task observations, and
  advances its generation watermark so stale in-flight results cannot repopulate the cache (even
  after delete + re-add). Unchanged entries and failed saves keep their observations; removed host
  results are ignored. The task-event publisher stages Board cache updates and discards updates when
  observation stopped or the host was removed.
- Example: `worker@local` and `worker@ssh:build` are independent entries, both with `pane_id: null`
  unless a configured pane named `worker` exists on that host. Neither listing nor absence implies
  authorization, registration, reachability, or delivery assurance (listing is never a send
  prerequisite).

## `GET /api/board/messages?since=<seq>`

Returns cached rows after the panemux-local sequence `since` (default `0`, unrelated to agmsg's
opaque host-local message ID).

Original `host`, `team`, `from`, and `to` are preserved. Combine a normal `from` with source `host`
for its qualified reply identity; a qualified `to` already names its host; an unqualified `to` is
host-local except for the existing pane-ID compatibility route to that pane's owning host; `_system`
is reserved. History does not snapshot past pane ownership after pane moves, and no extra identity
fields are inferred.

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

`is_status` lets clients filter out status JSON. `epoch` is an opaque equality-only cache-instance
identifier; clients must reset `since` to `0` when `epoch` changes after a server restart.

- `400`: `since` is not an integer
- `200`: rows returned (`"messages":[]` when none, never `null`)

## `POST /api/board/broadcast`

Sends one message to each resolved recipient's host without PTY injection:

```json
{ "to": ["pane-a", "worker@local", "worker@ssh:build"], "body": "please review" }
```

Qualified recipients require only a configured Board host (no pane or roster entry); unqualified
recipients must be existing pane IDs (arbitrary bare names are rejected). Parsing and resolution use
the shared relay rules and SSH-key validator. All recipients are resolved before any send starts;
after validation, delivery is fail-fast but not transactional. History reflects delivered messages
once relay polling reads them back.

- `400`: malformed JSON
- `422`: empty recipients or body, NUL in body, or any invalid/unknown recipient (including unknown
  hosts and qualified `_system`); no send starts
- `502`: downstream send failure with `{"error":"...","delivered":[...]}`
- `200`: `{"delivered":[...]}` listing every reached recipient in input spelling and order

## `GET /api/board/command/history`

Returns chronological command-center entries captured from the structured subprocess stream (no
pagination):

```json
{
  "entries": [
    { "at": "2026-08-10T12:00:00Z", "raw": { "type": "panemux_prompt", "text": "Summarize the board" } },
    { "at": "2026-08-10T12:00:03Z", "raw": { "type": "result", "result": "..." } }
  ]
}
```

- `200`: entries (`"entries":[]` when missing or empty)
- `500`: existing history file is unreadable

## `GET /ws/board/command`

Command center chat WebSocket for the Spotlight palette. The
[WebSocket browser connection policy](workspace.md#websocket-browser-connection-policy) applies
before upgrading. `/ws/board-command` is an ordinary terminal URL for a pane named `board-command`
with no alias or redirect. Full subprocess design lives in
[Command center](../agent-board/command-center.md#command-center).

### Authentication

Browsers send the bearer token as a WebSocket subprotocol (`new WebSocket(url, [token])`). The
server compares `Sec-WebSocket-Protocol` against `server.auth_token` in constant time, returning
`401` without upgrading when missing or invalid and echoing the token on success. Avoiding query
parameters keeps the token out of access logs, browser history, and same-origin `Referer` headers
([Authentication](../security/auth.md)).

### Query frames and lifecycle

Once connected, the client may send prompts sequentially over the same socket:

- **Client → server (text frame)**: `{"prompt": "..."}`
- **Server → client (text frames)**:
  - `{"type":"line","raw":{...}}` — raw `--output-format=stream-json` line forwarded as emitted
  - `{"type":"error","message":"..."}` — terminal frame for a failed query (non-zero exit, malformed
    `stream-json`, or cancellation/timeout)
  - `{"type":"done"}` — terminal frame for a successful query
  - Optional `"warnings":["..."]` on either terminal frame (omitted unless a non-fatal surrounding
    step such as history persistence failed; also logged)
  - `{"type":"busy"}` — final response rejecting a prompt outright (never queued) when a query is
    already in flight ([Process lifecycle](../agent-board/command-center.md#process-lifecycle))

Every started query ends with exactly one of `error` or `done`; a `busy` prompt starts no query and
receives neither. A failed history write never turns a successful answer into `error`; failed
queries carry history warnings too.

- **Client disconnect & deadlines**: Mid-query client disconnect does not stop the subprocess or break
  `--resume` continuity; the server drains remaining output without forwarding and releases busy
  state on exit. Subprocess default timeout is `5m` (`commandcenter.RunnerConfig.QueryTimeout`),
  after which it is force-killed. Each server-to-client write has a `10s` deadline; a stalled reader
  enters the same drain-without-forwarding path.
- **Timeout vs `--resume` reset**: A query killed by `QueryTimeout` emits
  `{"type":"error","message":"claude query timed out after 5m0s"}` (or the configured timeout) and
  leaves the persisted session ID intact. Only a genuine resume failure (CLI non-zero exit when the
  ID is no longer recognized) clears the persisted ID.
- **Malformed `stream-json`**: Cancels the subprocess immediately, **then drains** remaining output
  and reaps it before emitting the single `error` frame (with any history `warnings`) and releasing
  busy state.
- **Persisted `--resume` ID validation**: Validated before every use against
  `^[A-Za-z0-9][A-Za-z0-9._-]*$` (preventing leading-dash values from being parsed as CLI flags).
  An invalid ID is cleared and replaced with a freshly minted v4 UUID via `--session-id <uuid>`;
  any session ID reported by the subprocess is ignored so the command center never adopts an ambient
  Claude conversation.
