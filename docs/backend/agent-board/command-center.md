---
type: Specification
title: "Command Center Execution"
description: "Headless Claude subprocess lifecycle, three scoped MCP tools, history capture, and authorization boundaries."
---

# Command Center Execution

The Command Center is one persistent conversation per panemux instance, executed as a short-lived
headless `claude` subprocess per query using a panemux-owned session ID for continuity (no
long-running agent daemon or PTY). WebSocket wire frames live in
[Agent Board API](../../api/agent-board.md#get-wsboardcommand); UI lives in
[Agent Board UI](../../ui/agent-board.md#command-center); subprocess isolation and CLI flag rules
live in [Agent Board security](../../security/agent-board.md#command-center-subprocess-execution).

## Scoped MCP tools

The subprocess receives exactly three stdio MCP tools (`internal/boardmcp`, JSON-RPC 2.0 with
exactly one of `result`/`error`, object-valued results, and explicit `null` ID when request ID is
undetermined), all calling panemux's authenticated loopback REST API:

- `board_status`: returns REST `identities` (including pane-free names and state sources) plus
  compatible pane `statuses`/`participation` maps.
- `board_messages`: returns original source `host`/`team`/`from`/`to` with `seq`/`epoch` (combine
  sender name and source `BoardID` for qualified replies).
- `board_broadcast`: accepts `name@local`, `name@ssh:<connection-key>`, and legacy pane IDs without
  requiring a pane or roster entry, sending as `_system` through the standard relay path.

The Command Center never calls agmsg directly, composes shell commands, or requires a local agmsg
installation. Tools forward REST JSON without stripping host fields; tool descriptions and default
system prompt distinguish observed task state from supplemental self-reports and note that listed
names prove neither registration, liveness, nor delivery.

- Argument type errors return JSON-RPC invalid-params errors.
- REST validation errors (`422`, including NUL bodies; zero sends executed) and downstream send
  failures (`502`, preserving earlier deliveries) return MCP `isError: true` with HTTP status code
  and REST body (including partial `delivered` recipients; see
  [`POST /api/board/broadcast`](../../api/agent-board.md#post-apiboardbroadcast)).

## Process lifecycle

- On the first successful query, panemux mints and persists a v4 UUID and passes it explicitly;
  later queries resume that ID. Any session ID emitted by the subprocess is ignored so the Command
  Center never adopts an ambient Claude conversation.
- Each query executes in an empty temporary working directory with:
  - no user, project, or local setting sources (`--setting-sources ""`);
  - strict MCP config containing only the board MCP server;
  - only the three board tools allowed and acting tools denied by name;
  - sandboxing enabled (`{"sandbox":{"enabled":true}}`);
  - panemux instructions passed as a system-prompt addition (optionally refined by
    `~/.config/panemux/command-center/CLAUDE.md`; no inherited file may broaden tools or enable
    hooks; `--dangerously-skip-permissions`, Bash, filesystem tools, and other MCP servers are
    forbidden).
- **Concurrency & deadlines**: at most one query runs at a time; concurrent prompts receive `busy`
  immediately without queuing. Every started query terminates with exactly one `done` or `error`
  frame. Default timeout is `5m`; non-zero exit, malformed stream-JSON, and timeout return explicit
  errors. A genuine resume failure clears the persisted session ID so the next query starts fresh (a
  failed first query never persists its minted ID; a timeout leaves the persisted ID intact).

## Authorization, history capture, and scope

- **Authorization**: shares the bearer-token boundary of `/api/board/*` with no separate per-pane
  role tier (authenticated callers already have full terminal access). Messages sent by the Command
  Center remain ordinary agent instructions subject to the receiving agent's own confirmation
  policy.
- **History capture**: panemux records observed `stream-json` lines directly rather than parsing
  private Claude transcripts, prepending one `panemux_prompt` entry whenever a query starts (failed
  queries retain it; rejected `busy` requests do not). History is appended as private (`0600`)
  JSONL served chronologically by `GET /api/board/command/history` (interleaving tool calls and
  output). Because history is append-only best-effort context, readers skip any crash-truncated line
  and subsequent appends start on a fresh line. Non-fatal history write failures surface as
  `warnings` on `done`/`error` frames.
- **Narrow scope**: one Command Center session per panemux instance (not per workspace), with no
  query queue or concurrent orchestrator support.
