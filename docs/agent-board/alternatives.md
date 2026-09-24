# Agent Board: alternatives considered

> Part of the current [Agent Board design](../agent-board.md). Historical decisions belong in the
> [Decision log](../DECISIONLOG.md#agent-board).

## Alternatives considered

### Claude Code native cross-session messaging

Claude Code's `ListAgents` and `SendMessage` are not the Agent Board transport because they:

- support Claude sessions only, while Agent Board must interoperate with other agent types;
- do not match panemux's arbitrary SSH-host topology without Remote Control constraints;
- expose no documented external interface that panemux can safely call.

Panemux must not reverse-engineer Claude's session socket or maintain a second transport selected by
agent type. A Claude pane may still use native messaging independently; it is outside the Agent
Board contract.
