# Agent Board: API and config additions

> Part of the [Agent Board design](../agent-board.md). Read that document's status note first — it says which parts of this design are shipped.

## API additions

Every row is implemented; see [docs/behavior.md](../behavior/board-api.md#agent-board-rest-api) for exact
request/response shapes and status codes. Every `/api/board/*` endpoint requires the bearer token
described in [Security model](security-model.md#security-model), but — a deliberate, narrower choice than an earlier
revision of this document specified — that gate covers **only** `/api/board/*`, not the pre-existing
`/api/*` routes or `/ws/{sessionID}`: retrofitting auth onto the already-relied-upon unauthenticated
routes is a separate, larger change with its own frontend work — see
[security.md](../security/auth.md#auth-token-and-transport-encryption). `GET /api/session-token` is the one
deliberate exception to "every `/api/board/*` endpoint requires the token" — see its own row below
and [docs/behavior.md](../behavior/board-api.md#get-apisession-token) for why. `WS /ws/board-command` is
authenticated too, but via a WebSocket subprotocol rather than the `Authorization` header — see
[docs/behavior.md](../behavior/websocket.md#command-center-websocket-protocol).

| Endpoint | Purpose |
|---|---|
| `GET /api/board/status` | A snapshot of panemux's in-memory status cache — no `AgmsgClient` call happens on this request (see [Architecture](architecture.md#architecture)) |
| `GET /api/board/messages?since=<seq>` | History feed for the dashboard UI. `<seq>` is `BoardCache`'s own panemux-local sequence number (see [Package layout](architecture.md#package-layout)), not an agmsg-native `id` — those aren't comparable across hosts |
| `POST /api/board/broadcast` | `{ "to": ["pane-a","pane-b"], "body": "..." }`; sends directly to each target's own host via `AgmsgClient` (never via PTY injection, so it is safe to send to a pane mid-turn); delivery to the pane is immediate, but the message appears in `GET /api/board/messages`' history only after the relay's next poll cycle reads it back — see [Known limitations](limitations.md#known-limitations) |
| `WS /ws/board-command` | Command center chat: client sends `{"prompt": "..."}`, server streams the headless Claude response — see [Command center](command-center.md#command-center) |
| `GET /api/board/command/history` | Command center's own captured conversation history — see [Command center](command-center.md#command-center) |
| `GET /api/session-token` | **Deliberately unauthenticated** — hands the browser dashboard the bearer token it needs to call every route above and open the WS connection, since there is no other way for the frontend's own JavaScript to learn a token that may have been randomly generated on first run. Not part of the original design; added because nothing in it specified how the browser itself (as opposed to the command center subprocess) would learn the token. Lives at `/api/session-token`, not `/api/board/session-token` — see [docs/behavior.md](../behavior/board-api.md#get-apisession-token) for why that distinction is load-bearing, not stylistic. Its response also carries `agent_board_enabled` (added in Phase 3, likewise not part of the original design — see the Phase 3 status note in [agent-board.md](../agent-board.md)), computed by scanning every configured pane for `agent_board.enabled: true`, so the frontend can gate the "Agent Board" dashboard button independently of `command_center_enabled`. |

## Config additions

```yaml
server:
  host: "127.0.0.1"
  port: 8080
  auth_token: ""   # empty = auto-generate on first run, saved to ~/.config/panemux/token (0600)

command_center:
  enabled: true   # default false; talks only to panemux's own REST API, no local agmsg needed

agent_board:
  team: "panemux"  # default; all board-enabled panes share this agmsg team unless overridden
  agmsg_path: "~/.agents/skills/agmsg"  # default; where scripts/api.sh is expected, per-host override possible

panes:
  - id: pane-a
    type: local
    agent_board:
      enabled: true
      mode: monitor    # monitor (default) | turn (legacy) | both | off, mirrors agmsg's own /agmsg mode

  - id: pane-b          # e.g. a Codex pane
    type: ssh
    connection: build-host
    agent_board:
      enabled: true
```

A global `agent_board.enabled` default may also be supported so individual panes don't need to
repeat it, but board features on any given host still require agmsg to already be present there —
panemux will not install it, per [Integration with agmsg](agmsg-integration.md#integration-with-agmsg).

**Why `command_center` is a top-level key, not nested under `agent_board`, despite both belonging to
the same Agent Board feature.** Nesting it would imply command_center depends on agent_board/agmsg
being configured too, which is false by design: the command center never calls agmsg directly (see
[Command center](command-center.md#command-center)) and works with every pane's `agent_board.enabled` left `false`.
The two keys are siblings in this config because their actual dependency graph is siblings — not
because they're unrelated features that happen to share a document.

**`_system` is reserved and validated where panemux can actually enforce it.** `internal/config/
validate.go` rejects any pane config whose `id` is literally `_system`, the same way it already
rejects duplicate pane IDs (see [architecture.md](../architecture.md)) — panemux will not let itself be
configured into a collision with its own reserved sentinel. This is a real but partial guarantee:
nothing stops an operator or a live agent from running agmsg's own `join.sh <team> _system ...`
by hand, outside any pane panemux bootstrapped, since agmsg's roster is agmsg's own state and
`join.sh` is not gated by panemux at all. That gap is exactly why the [own-send
ledger](architecture.md#package-layout) check in [Cross-host relay](relay.md#cross-host-relay) does not trust the
`_system` string by itself even after this validation — config-time reservation closes the
"panemux accidentally misconfigures itself" case, not the "an agmsg team member deliberately
registers the reserved name" case, which only the ledger check closes.
