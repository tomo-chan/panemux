# Agent Board: command center

> Part of the current [Agent Board design](../agent-board.md).

## Command center

### What it is

The command center is one persistent conversation per panemux instance, executed as a short-lived
headless Claude subprocess for each query. A panemux-owned session ID provides continuity; no
long-running agent daemon or PTY is introduced.

The model receives exactly three MCP tools:

- `board_status`;
- `board_messages`;
- `board_broadcast`.

These tools call panemux's authenticated loopback REST API. The command center never calls agmsg,
composes shell commands, or routes hosts itself, and therefore does not require a local agmsg
installation. Broadcasts use the reserved `_system` sender and the same relay path as browser
broadcasts.

### Process lifecycle

On the first successful query, panemux mints and persists a v4 UUID and supplies it explicitly to
Claude. Later queries resume that ID. A session ID reported by the subprocess is ignored so the
command center cannot adopt an ambient Claude conversation.

Each query runs in an empty temporary directory with:

- no user, project, or local setting sources;
- strict MCP configuration containing only the board server;
- only the three board tools allowed;
- sandboxing enabled;
- panemux's instructions supplied as a system-prompt addition.

An optional `~/.config/panemux/command-center/CLAUDE.md` may refine those instructions. No inherited
settings file may broaden tools or enable hooks. `--dangerously-skip-permissions`, Bash, filesystem
tools, and unrelated MCP servers are forbidden.

Only one query may run at a time. A concurrent prompt receives `busy` immediately and is not
queued. Every started query ends with exactly one `done` or `error` frame.

Queries have a five-minute default timeout. Non-zero exit, malformed stream output, and timeout are
explicit errors. Failure while resuming clears the stale session ID so the next query starts a new
conversation; a failed first query does not persist its newly minted ID.

The stdio MCP server follows JSON-RPC 2.0 and MCP response rules: exactly one of `result` or `error`,
object-valued successful results, and an explicit `null` ID when a request ID cannot be determined.

### Authorization

The command center uses the same bearer-token boundary as other board APIs. There is no pane-level
role model. Anyone who can authenticate already has access to panemux's full terminal surface, so a
second command-center-specific tier would not reduce the effective privilege.

A command-center message is still an instruction, not pre-authorized action. The receiving agent's
normal confirmation policy remains in force.

### API and streaming

`WS /ws/board-command` accepts a prompt and streams Claude's structured output. Non-fatal problems,
such as failure to persist history after an otherwise successful query, appear as warnings on the
terminal `done` or `error` frame. Exact frame shapes are specified in
[WebSocket protocols](../behavior/websocket.md#command-center-websocket-protocol).

Panemux stores the streamed lines it already observed rather than parsing Claude's private
transcripts. Because Claude's stream does not echo the prompt, panemux adds one `panemux_prompt`
entry at the start of each query that actually begins. Failed queries retain that prompt; rejected
busy requests do not.

`GET /api/board/command/history` returns this chronological captured history. Tool use and model
output remain interleaved as they occurred.

History is appended as private JSONL rather than atomically replaced. A crash may truncate one
line. Loading skips any malformed line and keeps valid entries around it; a later append starts on
a new line so it does not merge with an unterminated tail. History is best-effort conversation
context, not authoritative state.

### UI

- `Cmd/Ctrl+Shift+K` opens the command palette. The shortcut uses capture phase so it works while a
  terminal owns focus.
- The palette loads recent history, streams the current response, and opens its WebSocket only while
  visible.
- The command-history panel reads the same history through REST and needs no WebSocket.
- Both surfaces are absent when `command_center_enabled` is false.
- Closing either surface restores the element that previously held focus.

Plain `Cmd/Ctrl+K` is intentionally unused because shells and readline commonly bind it.

### Scope kept intentionally narrow for now

There is exactly one command-center session per panemux instance, not per workspace, and no query
queue or concurrent orchestrator support.
