# Behavior: Agent Board REST API

> Part of the [behavior specification](../behavior.md).

## Agent Board REST API

Every `/api/board/*` route requires `Authorization: Bearer <server.auth_token>`; invalid or missing
credentials return `401` before handler processing. `GET /api/session-token` is the deliberate
exception described below.

### `GET /api/session-token`

Returns browser bootstrap data:

```json
{ "token": "a1b2c3...", "command_center_enabled": true, "agent_board_enabled": true }
```

The endpoint is unauthenticated because the browser uses it to learn a generated token. It returns
`403` when the socket peer is not loopback, when `Host` is not `localhost`, a loopback address, or
`0.0.0.0`, or when any `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` value is present. Forwarding
headers are rejected regardless of the addresses they contain. Valid direct requests return `200`.
The rationale and accepted reverse-proxy limitation are specified in
[Auth token and transport encryption](../security/auth.md#auth-token-and-transport-encryption).

The capability flags are independent. `agent_board_enabled` reflects the current configured panes;
`command_center_enabled` reflects command-center configuration. The frontend uses them to expose
the two surfaces separately.

For every board-enabled pane, background bootstrap detects a supported agent on two consecutive
polls, confirms agmsg on that host, and writes one onboarding instruction. There is no REST endpoint
for this process; see [Bootstrap flow](../agent-board/bootstrap.md#bootstrap-flow).

### `GET /api/board/status`

Returns the current in-memory status snapshot without contacting agmsg:

```json
{
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
`{"statuses":{}}`, never `null`.

### `GET /api/board/messages?since=<seq>`

Returns cached rows after the panemux-local sequence `since`, which defaults to `0`. This sequence
is unrelated to agmsg's opaque, host-local message ID.

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

Sends one message directly to each resolved pane's host without PTY injection:

```json
{ "to": ["pane-a", "pane-b"], "body": "please review" }
```

Every recipient is resolved before delivery starts. After that validation, delivery is fail-fast but
not transactional: a later host failure may follow successful earlier sends. History reflects the
message after relay polling reads it back.

- `400`: malformed JSON.
- `422`: empty recipients or body, or any unknown pane ID.
- `502`: downstream send failure, with `{"error":"...","delivered":[...]}`.
- `200`: `{"delivered":[...]}` containing every reached pane ID.

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
