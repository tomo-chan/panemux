# Agent Board: testing plan

> Part of the current [Agent Board design](../agent-board.md). Follow the repository-wide TDD,
> coverage, and efficacy rules in [DEVELOPMENT.md](../../DEVELOPMENT.md).

## Testing plan

Tests must protect these contracts rather than mirror implementation structure.

### Message and relay semantics

- Status requires the exact `board_status` discriminator; malformed or merely similar JSON remains
  an ordinary message.
- Latest status wins per host-qualified name in the configured team; existing pane API keys
  retain matching host reports. History preserves a stable panemux-local order and source host/team.
- agmsg IDs remain opaque and host-local.
- Cursor loss and a crash before persistence may replay rows; failed cross-host sends are not
  retried after the cursor advances; overflow preserves the documented bounded-loss behavior.
- Pane-free unqualified senders are accepted; qualified senders, wrong-host pane senders and
  unmatched `_system` senders are rejected. Qualified and bare same-name recipients cannot collide.
- Qualified forwarding strips the recipient host and adds the source host to the sender. Bare
  pane-to-pane compatibility preserves the sender; pane-free senders are qualified even when
  addressing a bare pane ID. Copies enter neither history, state nor forwarding again.
- The own-send ledger preserves duplicate occurrences, expires entries, and removes failed sends.
- Broadcast validates all recipients before delivery and reports any partial delivery after a
  downstream failure.

### Execution and security

- Every remote argument is shell-safe on both reads and writes.
- Arbitrary message bodies round-trip without execution or corruption.
- Board writes always use `--force`; sender validation compensates for the relaxed roster check.
- Non-loopback configuration without an explicit token is rejected.
- Board REST and command WebSocket routes require the token; legacy terminal routes retain their
  current boundary.
- Session-token bootstrap rejects forwarding headers and requires a loopback peer plus a
  loopback-equivalent `Host`, including the documented `0.0.0.0` exception.

### Bootstrap and dependency handling

- Once a host path is resolved, a missing installation or failed presence probe never writes to the
  PTY and remains retryable; path-resolution failures retry through shared host execution on later polls.
- Home-path expansion produces absolute local and remote paths before command construction.
- Detection, presence debounce, and warning suppression follow [Bootstrap flow](bootstrap.md#bootstrap-flow).
- No Board path calls `Session.Write`, including recovery and restarted sessions.
- Legacy mode is validated, warned about once per configuration load, and ignored.
- Presence checks require two consecutive detections, stop after success for the pane session,
  retry failures, reset on session/host changes and removal, and treat managed pauses silently.
- Saving config omits legacy mode without changing its in-memory API value or regenerating warnings.
- Retired bootstrap state is left untouched; the reader/writer implementation is removed.

### Command center

- First use mints a panemux-owned session ID; later use resumes it; failures follow the documented
  persistence and reset rules.
- Only one query runs at a time, every started query has one terminal frame, and timeouts stop the
  subprocess.
- The subprocess receives only the three board tools and isolated settings, never permission bypass,
  shell, or filesystem access.
- Prompts, streamed output, tool use, warnings, and failures remain correctly ordered in captured
  history.
- MCP responses conform to JSON-RPC and MCP success/error shapes.

### Frontend contracts

- All API and WebSocket payloads are runtime-validated before use.
- Capability flags independently gate the dashboard and command-center surfaces.
- The dashboard distinguishes configured-but-unreported, active, stale, and removed-but-reporting
  panes and excludes status JSON from conversation history.
- Overlays stream updates, show failures in context, close by documented interactions, and restore
  focus.

The separate [agmsg compatibility contract](agmsg-contract.md#agmsg-compatibility-contract) checks
the real external scripts; [model checking](model-checking.md#state-machine-model-checking) covers
ledger state sequences beyond hand-selected examples.
