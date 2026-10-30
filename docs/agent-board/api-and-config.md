# Agent Board: API and configuration

> Part of the current [Agent Board design](../agent-board.md).

## API additions

Exact payloads and status codes live in [Agent Board REST API](../behavior/board-api.md#agent-board-rest-api).

| Endpoint | Contract |
|---|---|
| `GET /api/board/status` | Read the in-memory status snapshot without contacting agmsg |
| `GET /api/board/messages?since=<seq>` | Read cache history after a panemux-local sequence |
| `POST /api/board/broadcast` | Send to resolved pane IDs on their owning hosts |
| `WS /ws/board/command` | Run and stream one command-center query |
| `GET /api/board/command/history` | Read captured command-center history |
| `GET /api/session-token` | Loopback-only browser bootstrap for the token and capability flags |

All `/api/board/*` routes require the bearer token. `/ws/board/command` carries the same token by
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
  enabled: false # opt in for the whole instance
  mode: monitor  # monitor | turn | both | off
  team: "panemux"
  agmsg_auto_update: false # accepted and stored; automatic updates are not currently executed
```

Enablement, delivery mode, and team are instance-wide. There are no pane or host overrides.
Configure these settings in YAML; Pane Settings does not expose them. Each host uses
`~/.agents/skills/agmsg` relative to its own home. Local discovery uses the injectable home-directory
seam. The removed pane `agent_board` block and `agent_board.agmsg_path` produce migration errors.

Use the task-board host menu for manual Install / Update of the pinned agmsg source.
`agmsg_auto_update` is persisted but remains inactive; no checkbox is shown yet.
See [installer behavior](agmsg-integration.md#manual-install--update).

Both boards use the panemux host and `ssh_connections` keys. An SSH pane participates only when
its `connection` exactly matches a key; resolved hostname, user and port are not compared. A
`~/.ssh/config`-only pane is warned about at startup and excluded without a validation error.
Add a name-only `ssh_connections` entry to use its OpenSSH configuration for both boards.

`command_center` is a sibling of `agent_board` because it talks only to panemux's API and can run
without enabling Agent Board or local agmsg installation. The browser receives independent
`command_center_enabled` and `agent_board_enabled` flags.

`_system` is reserved. Configuration rejects it as a pane ID or team, but external processes can
still ask agmsg to use that name; relay sender validation must therefore rely on the own-send ledger,
not the identifier alone.
