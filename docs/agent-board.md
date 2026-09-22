# Agent Board

Agent Board adds self-reported agent status, cross-pane messaging, a read-only dashboard, and an
optional conversational command center to panemux. This guide states the current design; the
[Decision log](DECISIONLOG.md#agent-board) records the phases, rejected alternatives, and fixes that
produced it.

## What it does

- Board-enabled panes report working state, repository, branch, worktree, and PR information through
  an operator-installed agmsg instance.
- Panemux aggregates those reports into one status view and bounded message history.
- Messages can be sent to panes without injecting keystrokes into a live PTY.
- A relay moves messages between agmsg installations on hosts already connected to panemux.
- A read-only browser dashboard shows pane state and message history.
- An optional command center runs a headless Claude query that can read board state and broadcast
  through three narrowly scoped MCP tools.

Agent Board is additive. Missing or incompatible agmsg installations disable board behavior for the
affected host without preventing its terminal panes from running.

## Responsibility boundary

```text
browser dashboard / command center
                |
          authenticated panemux API
                |
        BoardCache and relay
          /             \
 local agmsg scripts   SSH exec -> remote agmsg scripts
          |                         |
      local agents              remote agents
```

Panemux owns configuration, polling, relay cursors, its in-memory aggregate, authentication, and
the dashboard/command-center surfaces. agmsg owns agent membership, message storage, delivery to
agent sessions, and its message schema. Panemux invokes only agmsg's documented scripts; it does not
read agmsg's SQLite database or team files directly.

## Design principles

- **Ask the agent for external state.** Agents obtain their own Git/PR context with normal tools and
  report it. Panemux does not treat private transcript formats as the Agent Board contract.
- **Use one messaging protocol.** Every board message uses agmsg. Panemux does not maintain a second
  Claude-only protocol.
- **Use existing connections.** Local operations use local processes; remote operations use the
  pane's existing SSH transport. Panemux starts no additional listening daemon.
- **Never install panemux on a remote host.** A remote host needs only an agmsg installation managed
  by the operator.
- **Detect dependencies; do not install them.** Panemux reports/skips an unavailable agmsg backend
  and never runs an installer on the operator's behalf.
- **Keep one script-execution boundary.** Only `internal/board` calls agmsg. The command center and
  browser use panemux's authenticated API.
- **Treat panemux as a trusted relay, not end-to-end encryption.** Relayed message plaintext exists
  in the panemux process between the two SSH-protected hops.

## Current surfaces

| Surface | Role |
|---|---|
| `internal/board` | agmsg clients, polling relay, cache, cursor, bootstrap records, and own-send ledger |
| `internal/commandcenter` | one-query-at-a-time Claude subprocess, session continuity, and history |
| `internal/boardmcp` | stdio MCP server exposing `board_status`, `board_messages`, and `board_broadcast` |
| `/api/board/*` | authenticated status, messages, broadcast, and command history APIs |
| `/ws/board-command` | authenticated command-center event stream, registered only when enabled |
| `/api/session-token` | browser bootstrap data for board authentication and feature visibility |
| `BoardDashboardPanel` | read-only status/history overlay |
| `CommandPalette`, `CommandHistoryPanel` | query entry, live output, and persisted conversation view |

Exact request/response behavior lives in [Agent Board REST API](behavior/board-api.md) and
[Command Center WebSocket Protocol](behavior/websocket.md#command-center-websocket-protocol).

## Document map

| Question | Deep dive |
|---|---|
| What alternatives were rejected? | [Alternatives considered](agent-board/alternatives.md) |
| How are packages and host boundaries arranged? | [Architecture](agent-board/architecture.md) |
| What agmsg contract does panemux rely on? | [Integration with agmsg](agent-board/agmsg-integration.md) |
| How do status and messages flow? | [Message flow](agent-board/message-flow.md) |
| How are cross-host messages relayed? | [Relay](agent-board/relay.md) |
| How is an interactive agent onboarded? | [Bootstrap](agent-board/bootstrap.md) |
| How does the headless coordinator work? | [Command center](agent-board/command-center.md) |
| What are the exact API and config surfaces? | [API and config](agent-board/api-and-config.md) |
| Which threats and trust assumptions apply? | [Security model](agent-board/security-model.md) |
| What remains intentionally unsupported? | [Known limitations](agent-board/limitations.md) |
| How is compatibility with agmsg verified? | [agmsg contract](agent-board/agmsg-contract.md) |
| Which state-machine property is modeled? | [Model checking](agent-board/model-checking.md) |
| How is the subsystem tested? | [Testing plan](agent-board/testing-plan.md) |

## Related documents

- [Architecture](architecture.md) — whole-system component ownership.
- [Behavior specification](behavior.md) — runtime and protocol contracts.
- [Security design](security.md) — implementation requirements.
- [UI design](ui-design.md#agent-board-ui) — dashboard, palette, and history interactions.
- [Decision log](DECISIONLOG.md#agent-board) — chronological reasoning and superseded designs.
