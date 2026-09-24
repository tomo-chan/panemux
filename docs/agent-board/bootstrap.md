# Agent Board: bootstrap flow

> Part of the current [Agent Board design](../agent-board.md).

## Bootstrap flow

Each pane opts in with `agent_board.enabled: true`. The team and agmsg path are shared instance
settings; pane-level team overrides do not exist.

Panemux polls opted-in panes for an interactive agent type that agmsg itself marks as reliably
process-detectable: `claude-code`, `codex`, `cursor`, `gemini`, `grok-build`, or `opencode`.
Explicit-only and desktop agent types are not guessed.

A pane is bootstrapped only when:

- its pane ID and team are valid agmsg identifiers;
- the same session is detected on two consecutive polls;
- agmsg is present at the resolved path on that pane's host.

The two-poll debounce reduces, but does not eliminate, the chance of interleaving onboarding text
with user input. Enabling Agent Board on the pane is the operator's consent to this one-time PTY
write.

### Onboarding instruction

Panemux writes one instruction asking the agent to:

1. join the shared team with the pane ID as its agmsg identity;
2. for `claude-code`, claim that identity for the current session and restrict monitor delivery to
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

- Missing or unreachable agmsg causes no PTY write and is retried on later polls.
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
