# Agent Board: known limitations

> Part of the current [Agent Board design](../agent-board.md).

## Known limitations

### Availability and recovery

- Host setup is resolved once when panemux starts. A remote host with no reachable board session at
  that time remains unavailable to relay and bootstrap until panemux restarts, even if SSH later
  recovers.
- The status/history cache is memory-only. Restart recovery uses bounded backfill and can omit older
  rows outside that window.
- Broadcast delivery is immediate, but dashboard history is updated only when the relay polls the
  row back. The visible lag is at most one poll interval under normal operation.

### Delivery guarantees

- Cross-host forwarding is best-effort. A missing destination client or failed send is not retried:
  the source row remains visible in history and its cursor advances.
- A crash between a successful forward and cursor persistence can produce a duplicate.
- Delivery is not guaranteed complete. Because agmsg has no forward cursor, a burst larger than the
  poll window can drop its oldest overflow rows.
- There are no message claim or lease semantics.
- agmsg identities are not cryptographically authenticated. The relay rejects unknown and
  cross-host senders, but a local same-user process can impersonate another valid pane on that host.

### Bootstrap

- Onboarding writes synthesized text into the PTY. The two-poll debounce reduces but cannot remove
  the risk of interleaving with user keystrokes.
- Bootstrap tracks the pane session, not the agent process. Restarting an agent inside the same pane
  does not trigger onboarding again; recreating the pane does.
- Same-project identity isolation is verified only for `claude-code`. Other supported agent types
  may cross-receive when multiple panes share a project directory.
- The multiline input encoding has not been verified against live instances of all detectable agent
  types; an incompatible client could receive fragmented prompts.
- Process detection can reject an interactive command whose quoted prompt contains a headless-mode
  exclusion token as a separate whitespace token.
- Applying agmsg `turn` or `both` mode can write persistent repository-local hook configuration.
  Panemux does not remove it when Agent Board is disabled.

### Status and usage

- Status is cooperative. An agent that stops reporting remains stale; panemux has no independent
  Agent Board liveness signal and does not infer replacement state.
- Agent Board does not report account-wide or per-pane token usage. Account usage belongs to the
  agent provider; any future per-pane measurement should extend the existing pane-inspection
  surface rather than the agmsg status schema.

### External dependency

agmsg guarantees compatibility for its read API, not for every write and onboarding script Agent
Board uses. The pinned version and two-tier compatibility contract detect drift but cannot prevent a
future operator-installed release from breaking integration. A detected version outside the covered
range warns and continues.

### Command center

- There is one command-center conversation per panemux instance, with no workspace-specific or
  concurrent sessions.
- Each query starts a new `claude -p` process, so latency includes process startup.
