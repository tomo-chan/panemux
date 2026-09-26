# Agent Board: API and configuration

> Part of the current [Agent Board design](../agent-board.md).

## API additions

Exact payloads and status codes live in [Agent Board REST API](../behavior/board-api.md#agent-board-rest-api).

| Endpoint | Contract |
|---|---|
| `GET /api/board/status` | Read the in-memory status snapshot without contacting agmsg |
| `GET /api/board/messages?since=<seq>` | Read cache history after a panemux-local sequence |
| `POST /api/board/broadcast` | Send to resolved pane IDs on their owning hosts |
| `WS /ws/board-command` | Run and stream one command-center query |
| `GET /api/board/command/history` | Read captured command-center history |
| `GET /api/session-token` | Loopback-only browser bootstrap for the token and capability flags |

All `/api/board/*` routes require the bearer token. `/ws/board-command` carries the same token by
WebSocket subprotocol. `/api/session-token` is deliberately outside the authenticated subtree and
uses the guarded unauthenticated contract in [Agent Board REST API](../behavior/board-api.md#get-apisession-token).
All other existing `/api/*` routes and `/ws/{sessionID}` remain unauthenticated; changing that
boundary requires a separate frontend and server change.

## Config additions

```yaml
server:
  host: "127.0.0.1"
  port: 8080
  auth_token: ""   # empty: generate and persist a private token

command_center:
  enabled: true     # default: false; independent of agmsg availability

agent_board:
  team: "panemux"
  agmsg_path: "~/.agents/skills/agmsg"

panes:
  - id: pane-a
    type: local
    agent_board:
      enabled: true
      mode: monitor # monitor (default) | turn | both | off
```

Board enablement is per pane. All enabled panes use the instance-wide team. agmsg must already be
installed on each participating host; panemux never installs it.

`command_center` is a sibling of `agent_board` because it talks only to panemux's API and can run
without any board-enabled pane or local agmsg installation. The browser receives independent
`command_center_enabled` and `agent_board_enabled` flags.

`_system` is reserved. Configuration rejects it as a pane ID or team, but external processes can
still ask agmsg to use that name; relay sender validation must therefore rely on the own-send ledger,
not the identifier alone.
