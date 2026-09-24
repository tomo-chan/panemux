# Agent Board

Agent Board adds self-reported agent status, cross-pane messaging, a read-only dashboard, and an
optional conversational command center. This guide summarizes the current design; the
[Decision log](DECISIONLOG.md#agent-board) records its history.

## What it does

- Board-enabled panes report status through an operator-installed agmsg instance.
- Panemux aggregates reports and bounded message history across connected hosts.
- Panes exchange messages through agmsg, without PTY injection.
- The browser provides a read-only dashboard.
- An optional headless Claude command center reads the board and broadcasts through three scoped
  MCP tools.

Agent Board is additive: missing or unreachable agmsg affects board features on that host, not its
terminal panes. An untested agmsg version produces a warning but does not block startup.

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

Panemux owns configuration, polling, relay cursors, the in-memory aggregate, authentication, and UI
surfaces. agmsg owns membership, durable messages, delivery, and its schema. Panemux calls only
agmsg's scripts; it never reads agmsg's database or team files directly.

## Design principles

- **Ask the agent for external state.** Agents query and report their own Git/PR context; private
  transcript formats are not part of the contract.
- **Use one messaging protocol.** Every board message uses agmsg. Panemux does not maintain a second
  Claude-only protocol.
- **Use existing connections.** Remote operations reuse the pane's SSH transport; panemux starts no
  additional listener.
- **Never install panemux on a remote host.** A remote host needs only an agmsg installation managed
  by the operator.
- **Detect dependencies; do not install them.** Panemux skips unavailable agmsg backends and never
  runs an installer.
- **Keep one script-execution boundary.** Only `internal/board` calls agmsg. The command center and
  browser use panemux's authenticated API.
- **Treat panemux as a trusted relay.** Message plaintext exists in the panemux process between SSH
  hops; Agent Board is not end-to-end encrypted.

## Current surfaces

| Surface | Role |
|---|---|
| Dashboard | read-only pane status and message history |
| Command palette | one-at-a-time command-center queries with streamed output |
| Command history | persisted command-center conversation view |
| `/api/board/*` | authenticated status, messages, broadcast, and history API |
| `/ws/board-command` | authenticated command-center stream |
| `/api/session-token` | loopback-only browser bootstrap for authentication and feature flags |

Exact request/response behavior lives in [Agent Board REST API](behavior/board-api.md) and
[Command Center WebSocket Protocol](behavior/websocket.md#command-center-websocket-protocol).

## Document map

| Question | Deep dive |
|---|---|
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
| What alternatives were rejected? | [Alternatives considered](agent-board/alternatives.md) |

## Related documents

- [Architecture](architecture.md) — whole-system component ownership.
- [Behavior specification](behavior.md) — runtime and protocol contracts.
- [Security design](security.md) — implementation requirements.
- [UI design](ui-design.md#agent-board-ui) — dashboard, palette, and history interactions.
- [Decision log](DECISIONLOG.md#agent-board) — chronological reasoning and superseded designs.
