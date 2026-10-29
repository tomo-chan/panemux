# Agent Board: bootstrap flow

> Part of the current [Agent Board design](../agent-board.md).

## Bootstrap flow

The operator opts in for the whole instance with top-level `agent_board.enabled: true`.
Every configured pane with a board host is eligible, using the global `agent_board.mode` and team.
Only detected agents on hosts with agmsg receive participation instructions.

The bootstrap watcher polls eligible panes for a registered interactive agent ID, using process
rules that were verified against the messaging integration. It asks the board participation port
for availability, address validation, and the instruction; only the adapter knows agmsg.
The supported registry IDs are `claude`, `codex`, `cursor`, `gemini`, `grok-build`, or `opencode`. The
process rules are the Board rules in the [agent registry](../agent-registry.md#detection-rules).
Explicit-only and desktop agent types are not guessed.

A pane is bootstrapped only when:

- its pane ID and team are valid agmsg identifiers;
- the same session is detected on two consecutive polls;
- agmsg is present at the resolved path on that pane's host.

The two-poll debounce reduces, but does not eliminate, the chance of interleaving onboarding text
with user input. Enabling Agent Board globally is consent to one-time PTY instructions for all detected agent
panes on participating hosts. With global `turn` or `both`, this also consents to agmsg writing
project hook files for those agents.

### Onboarding instruction

Panemux writes one instruction asking the agent to:

1. join the shared team with the pane ID as its agmsg identity;
2. for `claude-code`, whose board adapter alone claims an identity
   ([agmsg integration](agmsg-integration.md)), claim that identity for the current session and restrict monitor delivery to
   it;
3. send every board message directly through `send.sh --force`;
4. report status to `_system` using the schema in [Message flow](message-flow.md#status-self-report-and-message-flow);
5. when configured for `turn` or `both`, apply agmsg's delivery mode and follow its returned
   directive.

Using the pane ID is mandatory: routing, status keys, and sender validation all assume agmsg
identities equal configured pane IDs.

`delivery.sh set` may write agmsg hook configuration into the pane's project. That repository-local
state can outlive both pane and panemux, and disabling Agent Board does not remove it. Operators
remove it through agmsg's own workflow.

### Failure and retry rules

- Paths and presence are resolved through the shared host execution port, independently of the
  pane's transport. A failed resolution or presence probe causes no PTY write and is retried on
  later polls, including after an initially unreachable host recovers. Failed SSH dials share the
  task board's 60-second cooldown and manual reconnect.
- The onboarding instruction itself is still written to the detected pane's terminal.
- Repeated warnings are emitted once per pane, session, and failure kind until that condition
  recovers. A later recurrence is logged again.
- A zero-byte PTY write failure is retried up to three times.
- A partial write is never retried because another full instruction could corrupt the existing
  partial input.
- After exhaustion or a partial write, that session is abandoned; recreating the pane creates a new
  eligible session.

Successful bootstrap is tracked by session identity. Persisted state suppresses repeat onboarding
after a panemux restart only for tmux-backed panes, whose underlying process may have survived.
Fresh local and SSH sessions are eligible again because panemux recreated their shells.

This deliberately does not detect an agent process restart inside an otherwise unchanged pane. See
[Known limitations](limitations.md#known-limitations).

Path discovery and presence probes have independent warning streaks. Once path discovery succeeds,
its warning clears even if the next presence probe fails. The participation port distinguishes a
path failure and supplies operator-facing tool/setting labels; the adapter keeps the existing
agmsg warning wording while the watcher remains transport-independent.
