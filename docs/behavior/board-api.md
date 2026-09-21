# Behavior: Agent Board REST API

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Agent Board REST API

Full design and rationale live in [agent-board.md](../agent-board.md); this section documents only the
request/response shapes and status codes of what is actually implemented today. Every `/api/board/*`
endpoint in this section requires `Authorization: Bearer <server.auth_token>` — see
[security/auth.md](../security/auth.md#auth-token-and-transport-encryption). A missing or incorrect token returns
`401` before the handler runs. The one exception is `GET /api/session-token`, documented in its own
subsection below, which is deliberately unauthenticated.

### `GET /api/session-token`

Returns the bearer token the browser dashboard needs to authenticate every other `/api/board/*`
request and the `/ws/board-command` connection — there is no other way for the frontend's own
JavaScript to learn a token that may have been randomly generated on first run (see
`config.Config.EnsureAuthToken`, [security/auth.md](../security/auth.md#auth-token-and-transport-encryption)) and
is never sent to the browser any other way. **This endpoint is deliberately not behind
`bearerAuthMiddleware`** — nothing could bootstrap the token without already knowing it otherwise.

It is gated by its own, narrower check instead: the caller's `RemoteAddr` must be a loopback IP *and*
its `Host` header must also name a loopback authority (`localhost`/`127.0.0.1`/`::1`, any port). Both
are required — see [security/auth.md's Auth token and transport
encryption](../security/auth.md#auth-token-and-transport-encryption) for why RemoteAddr alone doesn't defend
against DNS rebinding, and why relying on CORS here (an earlier revision of this document's claim) was
wrong. A non-loopback `server.host` deployment cannot use this endpoint at all, by design.

Response:

```json
{ "token": "a1b2c3...", "command_center_enabled": true, "agent_board_enabled": true }
```

`agent_board_enabled` (added alongside the Phase 3 dashboard UI) is `true` when at least one
configured pane has `agent_board.enabled: true`, computed by scanning `cfg.AllPanes()` on every
request rather than cached at startup. It is deliberately independent of `command_center_enabled`:
a config can enable `agent_board` without `command_center`, or vice versa, and the frontend needs
both flags to decide whether to show the "Agent Board" dashboard button and the "Command History"
button separately.

- `403`: the caller failed the loopback RemoteAddr+Host check above
- `200`: otherwise, always — there is no other failure mode for this handler

Note this route lives at `/api/session-token`, not under `/api/board/`, despite belonging
conceptually to Agent Board: chi routes any path starting with `/api/board/` into the
`bearerAuthMiddleware`-wrapped sub-router regardless of where else a handler for that literal path is
registered, so a route literally named `/api/board/session-token` would always require the token it
exists to hand out. This is not a stylistic choice — an earlier revision of this endpoint lived at
that path and was silently caught by the auth middleware it was supposed to bypass.

**Bootstrap flow** (not a REST/WS endpoint — a background behavior). For every pane with
`agent_board.enabled: true`, panemux polls every 5s for a live, agmsg-detectable coding-agent
process (one of six agent types; see [agent-board/bootstrap.md's Bootstrap
flow](../agent-board/bootstrap.md#bootstrap-flow)) and, once detected on two consecutive polls and agmsg is
confirmed present on that pane's host, writes a one-time onboarding instruction directly into the
pane's terminal — visible in the browser the same way any other terminal output is. This happens
with no operator action beyond setting the config flag; there is no API call to trigger or observe
it directly (its effect is only visible in the pane's own terminal output and, once the agent
follows the instruction, in `GET /api/board/status`/`/messages` after the relay's next poll).

### `GET /api/board/status`

Returns a snapshot of panemux's in-memory status cache. No `AgmsgClient` call happens on this
request — the relay goroutine is what keeps the cache current by polling agmsg on a schedule.

Response:

```json
{
  "statuses": {
    "pane-a": {
      "updated_at": "2026-08-10T12:00:00Z",
      "state": "working",
      "cwd": "/home/user/project",
      "branch": "feature/x",
      "repo": "owner/repo",
      "pr_url": "https://github.com/owner/repo/pull/123",
      "last_tool": "Edit internal/api/handler.go",
      "summary": "fixing failing tests"
    }
  }
}
```

An empty cache returns `200` with `{"statuses":{}}`, never `null`. All fields besides `updated_at`
are omitted (not emitted as empty strings) when the reporting pane didn't include them.

- `200`: snapshot returned (including when empty)

### `GET /api/board/messages?since=<seq>`

Returns board message history newer than `since`, `BoardCache`'s own panemux-local sequence number —
not an agmsg-native `id`, which isn't comparable across hosts. `since` defaults to `0` when omitted.

Response:

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

`is_status` marks a row as a pane's own status self-report rather than an ordinary message. Status
rows are appended to history alongside real messages, so a client has to tell them apart to avoid
rendering raw `board_status` JSON as if someone had sent it. It is computed server-side by
`internal/board`'s `IsStatusRow` rather than left to the client: Go's `json.Unmarshal` matches field
names case-insensitively and errors on a type mismatch, so a JavaScript re-implementation of the same
rule diverges on real inputs (`{"KIND":"board_status"}` is a status row to Go but not to
`JSON.parse`, and `{"kind":"board_status","state":123}` is an ordinary message to Go but a status row
to a naive client check).

`epoch` identifies the `BoardCache` instance that assigned these `seq` values. The cache is in-memory
only, so a panemux restart renumbers from 1 while a browser may still hold a cursor from before it —
`?since=300` against a cache whose newest row is `seq` 3 returns an empty list forever, and the
client's feed stops updating without any error to show for it. A client that sees `epoch` change must
reset its cursor to `0` and re-read. The value is opaque: compare it for equality only, never parse
or order it.

- `400`: `since` is present but not a valid integer
- `200`: messages returned (`"messages":[]` when there are none, never `null`)

### `POST /api/board/broadcast`

Sends `body` to every pane ID in `to`, via the shared relay's `Broadcast`, directly to each target's
own host — never via PTY injection, so it is safe to send to a pane mid-turn. Delivery is immediate,
but the message appears in `GET /api/board/messages`'s history only after the relay's next poll
cycle reads it back.

Request body:

```json
{ "to": ["pane-a", "pane-b"], "body": "please review" }
```

- `400`: invalid JSON
- `422`: `to` is empty, `body` is empty, or `to` names one or more pane IDs the relay doesn't know
  about (`board.UnknownPaneError`, which names every unresolvable pane ID at once)
- `502`: a downstream `AgmsgClient`/SSH error while relaying to a resolved pane's host. Broadcasting
  is fail-fast, not all-or-nothing, once every `to` ID has resolved: it stops at the first `Send`
  failure, so an earlier pane in `to` may already have received the message. The response body is
  `{ "error": "...", "delivered": ["pane-a"] }` — the pane IDs successfully delivered to before the
  failure — so the caller can tell which panes to avoid re-sending to on retry.
- `200`: `{ "delivered": ["pane-a", "pane-b"] }`, the pane IDs the broadcast actually reached

### `GET /api/board/command/history`

Returns the command center's own captured turn-by-turn conversation history — read directly from a
local file the WS handler (below) appends to while streaming a query's output, never re-derived from
Claude Code's transcript after the fact. Requires the bearer token like every other route in this
section.

Response:

```json
{
  "entries": [
    { "at": "2026-08-10T12:00:00Z", "raw": { "type": "system", "subtype": "init", "session_id": "abc" } },
    { "at": "2026-08-10T12:00:03Z", "raw": { "type": "result", "result": "..." } }
  ]
}
```

`raw` is exactly one line of the command center subprocess's own `--output-format=stream-json`
output, unmodified. There is no pagination; the whole captured history is returned every time.

- `200`: entries returned (`"entries":[]` when the command center has never run or `command_center`
  is disabled — an empty or missing history file is not an error)
- `500`: the history file exists but could not be read (a genuinely corrupt file, not the ordinary
  missing-file case above)
