# Agent Board: alternatives considered

> Part of the current [Agent Board design](../agent-board.md).

## Alternatives considered

### Claude Code's native cross-session messaging (2026-08-08)

Claude Code [added native cross-session
messaging](https://code.claude.com/docs/en/cross-session-messaging) (v2.1.224+): a `ListAgents`
tool for discovering other Claude Code sessions and a `SendMessage` tool for delivering plain text
to one of them by name, callable by Claude itself, not by an external process. Same-machine delivery
goes over a per-session Unix domain socket the receiving session binds and never touches Anthropic
servers; delivery to a session on another machine or [Claude Code on the
web](https://code.claude.com/docs/en/claude-code-on-the-web) goes through Anthropic servers via a Remote Control connection, and is
reply-only in that direction — a session here cannot originate a message to a session it hasn't
first heard from.

This looked promising enough to evaluate seriously against Agent Board's own goals, and was
rejected as a replacement (or a parallel mechanism) for the same reasons the earlier `native`
SQLite draft was rejected in favor of agmsg, plus one more specific to how this feature is exposed:

- **Claude-only.** It has no notion of Codex, Gemini CLI, or any other agent type — exactly the
  interoperability agmsg provides and the `native` draft could not (see [Design
  principles](../agent-board.md#design-principles)'s "one messaging mechanism, not two"). Adopting it for board
  traffic would mean two different mechanisms depending on which agent is in a pane, which is the
  same shape of problem this document already reasoned its way out of once.
- **Doesn't fit panemux's arbitrary-SSH-host model.** Same-machine delivery requires both sessions
  to see the same on-disk registration files — it says nothing about an SSH-reached remote host.
  Cross-machine delivery exists, but only through a Remote Control connection and only as a reply,
  never as an originating send — a fundamentally different, heavier operational model than "the
  operator already has agmsg installed on that host," and one that still couldn't originate the
  status self-report or command-center broadcast this design depends on across hosts.
- **No documented interface for panemux itself to use.** The only sanctioned way to send or receive
  is Claude calling `SendMessage`/`ListAgents` itself; there is no REST/CLI entry point a Go process
  can call directly. The one adjacent primitive that looks like an external hook —
  `CLAUDE_CODE_MESSAGING_SOCKET`, the inbox socket path exported to a session's own child processes
  — has no documented wire format here, and panemux is the *parent* of a pane's shell (and thus an
  ancestor, not a child, of the Claude process running inside it), so it would not even qualify for
  the "own-child" delivery path this feature defines. Writing to that socket directly would be
  exactly the kind of undocumented-internal-format reverse-engineering [Design
  principles](../agent-board.md#design-principles)'s "ask the agent; don't reverse-engineer its internal state" rule
  already rejects for agmsg's `messages.db` — the principle applies here just as much as it did
  there, including to a first-party Anthropic feature.

None of this rules out a Claude session itself choosing to use `SendMessage` for its own,
non-board-related purposes — that's between the user and their agent, and orthogonal to what Agent
Board specifies. It also doesn't rule out a narrower future integration (for example, prompting a
Claude pane to use `SendMessage` instead of `send.sh` when both sender and receiver are confirmed
local Claude sessions) if a real need for it shows up later. But nothing in this design should be
built toward that today: it would reintroduce exactly the two-mechanism problem "one messaging
mechanism, not two" exists to prevent, for a narrower agent-type and host-topology coverage than
agmsg already provides.
