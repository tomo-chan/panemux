# Agent Board: command center

> Part of the [Agent Board design](../agent-board.md). Read that document's status note first — it says which parts of this design are shipped.

## Command center

### What it is

- A single, persistent **headless** Claude session, not a pane and not a PTY. panemux invokes it as
  a short-lived subprocess per query — `claude -p --resume <command-center-session-id>
  --output-format=stream-json "<prompt>"` — rather than a long-running process, so this does not
  introduce the "new daemon" [Design principles](../agent-board.md#design-principles) rules out. `--resume` against
  one fixed session id is what gives the command center conversational continuity across separate
  queries.
- **It reads and writes the board through panemux's own authenticated REST API — `GET
  /api/board/status`, `GET /api/board/messages`, `POST /api/board/broadcast` — the same endpoints
  the browser dashboard uses, over loopback, with a token panemux injects into the subprocess's
  environment.** The LLM itself never composes the HTTP call: panemux points the subprocess at a
  narrow MCP server it provides (see [Process lifecycle](#process-lifecycle)) that exposes exactly
  those three operations as tools and makes the actual authenticated request on the model's behalf.
  This is a correction from an earlier revision of this document, which had the
  command center shell out to `send.sh`/`api.sh` directly. That was wrong on two counts: it made
  the LLM itself responsible for composing safely-escaped shell invocations, a second, unaudited
  path to the same exec sink alongside `AgmsgClient`'s own (see [Security
  model](security-model.md#security-model)); and it meant the command center could only ever see the *local* agmsg
  installation's status, never a remote pane's, because [Cross-host relay](relay.md#cross-host-relay)
  intercepts `_system`-addressed status reports before they ever leave the host they were written
  on. Reading `BoardCache` through `GET /api/board/status` (already aggregated across every host by
  the relay) fixes both: `AgmsgClient` stays the *only* code that ever calls agmsg's scripts, and
  the command center sees every pane's status, not just local ones.
- **The command center therefore needs no local agmsg installation of its own** — it never calls
  agmsg directly, so `command_center.enabled: true` is not gated on the same agmsg-presence check a
  board-enabled pane is (see [Config additions](api-and-config.md#config-additions)).
- Sending a message is `POST /api/board/broadcast` with the command center's own reserved `from`
  identity, `_system` — the exact same code path a human-triggered broadcast takes, which already
  resolves the destination pane's host and calls that host's `AgmsgClient.Send` (always `--force`)
  without the command center needing any host-routing logic of its own.

### Process lifecycle

- **First run.** No persisted command-center session id exists yet the first time a query arrives.
  panemux **mints its own v4 UUID** and pins the conversation to it with `--session-id <uuid>`, then
  persists that id to a small local file (`~/.config/panemux/command-center-session.json`, the same
  kind of local bookkeeping file as the relay cursor in [Cross-host relay](relay.md#cross-host-relay)). Every
  later query reuses it: `claude -p --resume <id> ...`. Note `--verbose` is required alongside
  `-p --output-format=stream-json`; the CLI refuses to stream structured output in print mode without
  it.

  **The id the subprocess reports is deliberately ignored.** An earlier revision omitted
  `--session-id` on a first run and adopted whatever `session_id` the stream reported, on the
  assumption that a `-p` invocation without `--resume` starts a fresh conversation. Verified against
  the real CLI, it does not: it reports the *ambient* session id of the Claude Code session the
  environment already belongs to, so the command center silently attached itself to a conversation it
  does not own — one holding full tool permissions, while the command center is launched with three.
  See [security/command-center.md's command center section](../security/command-center.md#command-center-subprocess-execution).

  The subprocess is also isolated from the operator's own configuration: `--setting-sources` is passed
  with an empty value (no user, project or local settings, so operator hooks never fire inside it),
  `--strict-mcp-config` limits it to the board MCP server this query configured, and `cmd.Dir` is an
  empty per-query temp directory rather than wherever panemux was launched. Since an empty
  `--setting-sources` also suppresses `CLAUDE.md` discovery, panemux's own instructions are passed via
  `--append-system-prompt`. An operator may place a `CLAUDE.md` in
  `~/.config/panemux/command-center/` to refine those instructions; it is optional. No settings file is
  accepted from any source — a settings value can nullify `--allowedTools`, so panemux sends only its
  own fixed, narrowing document (currently `{"sandbox":{"enabled":true}}`). See
  [security/command-center.md](../security/command-center.md#command-center-subprocess-execution).
- **Permissions.** The subprocess never receives `--dangerously-skip-permissions`. It has no PTY to
  surface an interactive approval prompt through, and this design does not substitute a blanket
  bypass for that missing prompt. Instead panemux runs a narrow, purpose-built MCP server exposing
  exactly three tools — `board_status`, `board_messages`, `board_broadcast`, thin wrappers around the
  three REST endpoints in [API additions](api-and-config.md#api-additions) — and launches the command center with
  `--allowedTools` scoped to only those three, no `Bash`, no filesystem tools, and no other MCP
  servers an interactive Claude Code session might otherwise have configured. This is also why the
  command center goes through an MCP server rather than a `Bash`+`curl` tool call: an MCP tool can be
  individually allow-listed ahead of time, while a generic `Bash` grant cannot be scoped down to "only
  run curl against this one loopback endpoint" — granting `Bash` at all would hand the command center
  everything `Bash` can do, which is exactly the blanket-bypass outcome this design avoids.
- **Wire shape.** That server answers with the response shape JSON-RPC 2.0 §5 requires — and with
  MCP's narrower reading of it — not merely one its current client happens to accept: **exactly one**
  of `result`/`error` on every response, a `result` that is always an *object* when there is no
  error, and an `id` that is JSON `null` — never absent — when the request's own id could not be
  determined, which is the parse-error case. Result and id used to be expressed with `omitempty`,
  which drops the key instead: `notifications/initialized` sent as a *request* (a known method with
  nothing to report, so neither method-not-found nor a result) was answered with neither member, and
  a parse error with no id at all. The empty object rather than `null` is the MCP half: its schema
  defines a successful response's result as `{ _meta?: ..., [key: string]: unknown }`, so `null`
  fails it exactly as an absent key does — fixing §5 alone would have moved that response from one
  invalid shape to another. Nothing observed today rejects any of these, but the client here is an
  LLM subprocess's own MCP layer, whose strictness panemux does not control and cannot pin.
  See #210.
- **Concurrency.** At most one query may be in flight against the command center's session id at a
  time. A `WS /ws/board-command` request that arrives while one is already running is rejected
  immediately with an explicit "command center busy" error rather than queued — two concurrent
  `claude -p --resume <same-id>` invocations against one session id have no ordering guarantee from
  the CLI itself, and building a queue would add state-machine complexity this design deliberately
  avoids for a feature kept to [one session per instance](#scope-kept-intentionally-narrow-for-now).
- **Failure modes.** A subprocess that exits non-zero, emits malformed `stream-json`, or times out
  surfaces as an explicit error frame on the WS connection — never a silently empty response, so the
  frontend can distinguish "no output yet" from "the query failed." The timeout is a real, enforced
  `context.WithTimeout` wrapping the subprocess's own context
  (`commandcenter.RunnerConfig.QueryTimeout`, default 5 minutes) — not merely aspirational: the WS
  handler's own request context comes from an already-hijacked HTTP connection, which the standard
  library never cancels on client disconnect, so this timeout is what actually bounds a hung or
  abandoned query's lifetime. A failed query never corrupts `--resume` continuity for the next one:
  the persisted session id is replaced only by a fresh first-run capture, never derived from a failed
  query's absent or partial output — and a `--resume`d query that itself fails clears the stale
  session id it was resuming, so a `claude`-side session that no longer exists (e.g. the operator
  cleared `~/.claude`) doesn't leave every future query retrying the same dead id forever.

### Authorization

The command center's privilege (it can message *any* board-enabled pane) is not granted by any
per-pane role — there is no pane role in this design. It is granted the same way every other
capability in panemux is: WS/REST access to the command center's own endpoints requires the global
bearer-token auth described in [Security model](security-model.md#security-model).

**Trust implication, stated explicitly:** a message the command center sends is an ordinary
instruction to the receiving pane, not something pre-authorized — the same caveat already called
out for the `SendMessage` tool in Claude Code itself. The receiving pane's own normal confirmation
flow still applies.

### API and streaming

- `WS /ws/board-command`: the frontend sends `{"prompt": "..."}`, panemux runs `claude -p --resume
  <id> --output-format=stream-json "<prompt>"` and streams the subprocess's output back as it
  arrives, so the palette can show live output instead of waiting for the full response. Exactly one
  frame ends a query that started — `error` or `done`, never both, with `busy` marking a prompt that
  never became a query — and a non-fatal failure around a query rides whichever of the two it ends
  with, as `"warnings":["..."]`, rather than claiming the query itself failed. Today the only such
  failure is a history write that could not land; see
  [behavior/websocket.md](../behavior/websocket.md#command-center-websocket-protocol) for the full frame contract.
- `GET /api/board/command/history`: returns the command center's own turn-by-turn history. This is
  **not** re-derived from Claude Code's transcript file after the fact — per [Design
  principles](../agent-board.md#design-principles)'s "ask, don't reverse-engineer" rule, panemux persists what it
  already captured directly from the `--output-format=stream-json` stream while relaying it to the
  WS client (a documented, stable CLI output contract), appending it to a local file panemux fully
  owns the format of. Because the board tool calls the command center makes appear as ordinary
  tool-use entries in that same captured stream, the returned history interleaves "what the command
  center did on the board" and "what it told the user" in one chronological feed.

  **"What the user asked" is the one part the stream does not carry**, contrary to what this
  paragraph claimed until the history panel was actually read against a real capture: a real run
  emits `stream_event`, `system`, `assistant` and `result` frames, and the prompt that produced them
  appears in none of them. panemux therefore records it itself, as the first entry of each turn,
  under type `panemux_prompt` — a type the CLI never emits, so a reader can always distinguish a
  panemux-written entry from a relayed subprocess line. A turn whose subprocess failed still has its
  prompt recorded; a turn whose subprocess never started does not, since there is no exchange to
  record.

### UI

- `Cmd/Ctrl+Shift+K` opens a Spotlight-style modal palette (`CommandPalette.tsx`), registered on the
  keydown capture phase so it reaches the handler even when a terminal pane currently has focus.
  Plain `Cmd/Ctrl+K` was deliberately not used: it's already bound in many shells/readline setups a
  terminal pane could be running, and would be swallowed as literal pane input rather than reaching
  the browser as a shortcut.
- The palette shows recent history inline on open (via the history endpoint above) and streams the
  live response turn-by-turn as it's generated, only opening its own `/ws/board-command` connection
  while the palette itself is open.
- A separate, persistently accessible history panel (`CommandHistoryPanel.tsx`, reachable via a small
  "Command History" button) exposes the same history outside the quick-palette flow, for scrolling
  back further than what the palette shows inline. It reads only the REST history endpoint — it needs
  no WS connection of its own.
- Both surfaces are gated on `command_center_enabled` from `GET /api/session-token`: neither the
  keyboard shortcut nor the history button is wired up at all when the command center is disabled,
  rather than being present but non-functional.
- A third surface, `BoardDashboardPanel.tsx`, is gated the same way but on `agent_board_enabled`
  instead: an "Agent Board" button next to "Command History", plus `Cmd/Ctrl+Shift+B` on the same
  capture-phase registration. Its own `useBoardStatus` hook polls `GET /api/board/status` (full
  snapshot every 5s, paused while the tab is hidden — the same `document.hidden` pattern
  `useSessionsOverview.ts` already uses) and `GET /api/board/messages?since=<seq>` (incremental,
  capped at the most recent 500 messages client-side) and filters out `to === "_system"` /
  `kind === "board_status"` rows from the message feed client-side, since the relay also appends
  those to history (see [Status self-report and message flow](message-flow.md#status-self-report-and-message-flow))
  and the dashboard's message feed is meant to show conversation, not raw status JSON.

See [ui-design.md's Agent Board UI section](../ui-design.md#agent-board-ui) for how these
surfaces reuse this repository's existing dialog/overlay patterns and status vocabulary instead of
introducing a parallel visual language.

### Scope, kept intentionally narrow for now

Exactly one command center session per panemux instance — not per-workspace, not multiple
concurrent command centers. Nothing in this design forecloses that later, but nothing here should
be built to anticipate it either, per this repository's own guidance against designing for
hypothetical future requirements.
