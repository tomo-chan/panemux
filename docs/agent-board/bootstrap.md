# Agent Board: pane detection

> Part of the current [Agent Board design](../agent-board.md).

## Bootstrap flow

Top-level `agent_board.enabled: true` enables detection and display for configured panes whose
hosts resolve. The watcher never writes to a pane's PTY, including on recovery, restart, or
repeated failures. It does not join agents, claim identities, configure delivery, or send
participation instructions. Participation, identity selection, and reception use
[agmsg's official workflow](agmsg-integration.md#integration-with-agmsg). Panemux does not provide
its own automatic participation hook. Board relay and status attribution require joining the configured
team with an agmsg name equal to the pane ID on its owning host.

Detection uses the registry's Board rules for `claude`, `codex`, `cursor`, `gemini`, `grok-build`,
and `opencode`. Explicit-only and desktop agents are not guessed. Each poll refreshes the detected
agent type for display; `instructed` remains false for API compatibility. A self-report can
outlive a pane session and does not establish routing authority.

Address validation precedes detection. After the same session is detected on two consecutive
polls, host availability is checked through the shared execution port. A successful check is
remembered for that session, so later polls only refresh detection. Session replacement, a host
change, or pane removal discards the confirmation. Failed path discovery,
presence probes, and missing installations are retried on later polls without terminal input.
Failed SSH dials share the task board's cooldown and manual reconnect behavior. A managed tool
update is treated as not checked: no probe warning is logged, and checking resumes after the pause.

Warnings are emitted once per pane, session, and failure kind until recovery. Path discovery and
presence probes have independent warning streaks. Recreating a session starts a new streak.

Eligibility follows live configuration. New panes are detected on subsequent polls; removal
revokes relay addresses immediately. Participation entries refresh every tick. Detection continues
for every eligible pane; see [detection cost](limitations.md#detection-cost).

`board-bootstrap-state.json` is neither read nor written at startup or during polling. An existing
file is left untouched. `agent_board.mode` is validated, warned about once per configuration load,
and ignored for one release. Saves omit the deprecated key while retaining the in-memory API
default; see [configuration](api-and-config.md#config-additions).
