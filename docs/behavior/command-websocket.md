---
type: Specification
title: "Command Center WebSocket protocol"
description: "Command Center authentication, query frames, subprocess lifetime, and resume continuity."
---

# Command Center WebSocket protocol

The [browser connection policy](websocket.md#browser-connection-policy) applies before upgrading.

## Query protocol

Endpoint: `GET /ws/board/command` — the Spotlight palette's chat connection.
The old `/ws/board-command` is an ordinary terminal URL for a pane named `board-command`;
there is no command alias or redirect. Existing command clients must update their URL and
old browser tabs must reload. The pane ID remains allowed. Full design lives in
[agent-board/command-center.md](../agent-board/command-center.md#command-center).

Browsers send the token as a WebSocket subprotocol: `new WebSocket(url, [token])`. The server
compares `Sec-WebSocket-Protocol` against `server.auth_token` in constant time. Missing or wrong
credentials return `401` without upgrading; success echoes the token in that response header.
The token is not a query parameter, avoiding disclosure through access logs, browser history or
same-origin Referer. See [authentication](../security/auth.md).

Once connected, the client may send any number of prompts sequentially over the same connection:

- client → server (text frame): `{"prompt": "..."}`
- server → client (text frames), one of:
  - `{"type":"line","raw":{...}}` — one raw `--output-format=stream-json` line from the command
    center subprocess, forwarded as it arrives
  - `{"type":"error","message":"..."}` — the query failed (non-zero exit, malformed stream-json
    output, or a context cancellation/timeout); always the last frame for that query
  - `{"type":"done"}` — the query finished successfully; always the last frame for that query
  - either terminal frame may also carry `"warnings":["..."]` — e.g.
    `{"type":"done","warnings":["persisting command center history: ..."]}`. The key is omitted
    entirely unless something around the query failed; see the paragraph below
  - `{"type":"busy"}` — a query was already in flight against the command center's single session
    (see [agent-board/command-center.md's Concurrency](../agent-board/command-center.md#process-lifecycle)); the new prompt was
    rejected outright, not queued

A client that disconnects mid-query does not stop the underlying subprocess or corrupt the next
query's `--resume` continuity — the server keeps draining (but no longer forwarding) the query's
remaining output so the subprocess is never left blocked, and the command center's own busy state is
released normally once it finishes.

The subprocess has a 5-minute default timeout (`commandcenter.RunnerConfig.QueryTimeout`), after
which it is force-killed. Each server-to-client write has a 10-second deadline; a client that stops
reading without closing enters the same drain-without-forwarding path after a failed write.

A query that starts ends with exactly one of `error` and `done`. A `busy` prompt starts no query and
receives neither terminal frame; `busy` is its final response. Non-fatal failures travel as `warnings` on either terminal frame
and are also logged. A failed history write does not turn a successful answer into a failed query:
the palette shows the warning under the answer. Failed queries carry history warnings too, so lost
history remains visible when the palette next seeds itself from `GET /api/board/command/history`.

**A query killed by the timeout above is not treated as a `--resume` rejection.** The client sees a
distinct `{"type":"error","message":"claude query timed out after 5m0s"}` (or whatever
`QueryTimeout` is configured to), and the command center's persisted session id is left untouched —
running long says nothing about whether `claude` would still recognize that id on a fresh attempt.
Only a genuine resume failure (the CLI's own non-zero exit when it no longer recognizes the id — e.g.
the user cleared `~/.claude`, or the session was garbage collected) clears the persisted id, so it
isn't retried forever; a timeout on an otherwise-healthy, still-resumable conversation never does.

A malformed `--output-format=stream-json` line cancels the subprocess immediately, **then drains**
its remaining output and reaps it. Draining before cancellation could block on a silent process
until the full timeout; not draining could leave a writer blocked and prevent reaping. The query's
single `error` frame is emitted after reaping, when the history-write outcome is known, so it can
carry `warnings` and release busy state without waiting for the query timeout.

**The persisted `--resume` session id is validated before every use, not only when this Runner itself
wrote it.** `--resume`'s value is optional in the claude CLI's own argument parser, so a value
beginning with `-` would be parsed as a new CLI flag rather than a `--resume` value if passed through
as-is. A persisted id that doesn't match `^[A-Za-z0-9][A-Za-z0-9._-]*$` (the shape of every id claude
itself has ever been observed to emit) is treated exactly like no persisted id at all: panemux clears
it, mints a new v4 UUID, and runs the query with `--session-id <uuid>` instead of `--resume`. The
minted UUID is persisted for later queries; any session id reported by the subprocess is ignored so
the command center cannot adopt an ambient Claude conversation.
