# Architecture

## System Structure

The system is split into a Go backend and a React frontend, bundled together at build time. The Go server owns process/session management and serves the built SPA. The frontend owns layout rendering, browser terminal integration, and user interactions.

## Backend

### `main.go`

Entrypoint responsibilities:

- parse CLI flags
- load YAML config or default config
- create the session manager
- start all configured sessions
- start the HTTP server
- shut down gracefully on signal

Why this design: startup orchestration is centralized, so session boot, config loading, and HTTP serving have one clear lifecycle.

### `internal/config`

This package loads and validates YAML configuration, expands `~/` paths, exposes flattened pane traversal, and persists workspace/layout updates.

Why it exists as a separate package:

- keeps config rules out of handlers
- gives one source of truth for layout validation
- makes config behavior easy to test without network/session dependencies

Workspace model:

- `workspaces` is the standard config shape. Each item has an `id`, `title`, and recursive `layout`.
- `workspaces.active` selects the layout shown by the UI. If it is empty, the first workspace becomes active.
- `workspaces.tab_position` controls the tab rail position: `top`, `bottom`, `left`, or `right`.
- Legacy top-level `layout` configs are accepted at load time and normalized into a single `default` workspace. The next save writes only `workspaces`, so old configs migrate automatically.
- Read helpers return a normalized workspace view without mutating the in-memory config; migration is written only through save/update paths.
- Pane IDs are validated as globally unique across all workspaces because sessions and WebSockets are keyed by pane ID.

Notable design choices:

- `LayoutChild.Size` is `float64` so drag-resize can preserve fractional percentages.
- structs carry both `yaml` and `json` tags so the same shape can be read from config and served through the API.

### `internal/session`

This package defines the shared `Session` interface and concrete implementations for:

- local shell via PTY
- SSH shell
- local tmux attach
- tmux attach over SSH

Why an interface-first session layer:

- all pane types expose the same read/write/resize/close contract
- WebSocket and API layers stay backend-agnostic
- new session types can be added without reshaping frontend protocols

Optional capability interfaces extend the base `Session` contract without breaking existing types:

- `CWDGetter` — implemented by `LocalSession` and `SSHSession`; returns the live working directory of the running shell. `LocalSession` reads it via `lsof` (macOS) or `/proc/<pid>/cwd` (Linux). `SSHSession` runs `pwd` over a new exec channel on the existing SSH connection.
- `SSHConnNamer` — implemented by `SSHSession`; returns the panemux connection alias used when building the `code --remote ssh-remote+<host>` command.

### `internal/board` (foundation implemented; relay, bootstrap, and API surface still planned)

A package that replaces transcript-based Claude activity inference with a self-reported channel:
panes report status (including branch/PR/cwd, gathered by the agent's own `git`/`gh` calls rather
than inferred by panemux) and exchange messages through an operator-installed
[agmsg](https://github.com/fujibee/agmsg) instance, plus a relay for messages addressed across
hosts. panemux owns no message schema or storage of its own — it is only ever a client of agmsg's
own documented scripts (`scripts/api.sh`, `scripts/send.sh`), never a reader of its internal
SQLite file.

The package's foundational pieces are implemented and tested: `Row`/`Status` and the
`board_status`-discriminated status-report parsing, `BoardCache` (the in-memory status/history view
[agent-board.md's Architecture section](agent-board.md#architecture) describes), `ownSendLedger`
(the forgery-detection primitive [Security
model](agent-board.md#security-model) describes), and the two `AgmsgClient` implementations —
`LocalAgmsgClient` (plain `exec.Command`, no shell involved) and `RemoteAgmsgClient` (the SSH exec
channel, with the base64-encode-then-allowlist body escaping and identifier allowlisting
[security.md](security.md#agent-board-remote-writes) describes). Two new optional session capability
interfaces, `BoardHostID` and `BoardExecutor`, extend the same pattern as
`CWDGetter`/`ActiveWorkdirGetter` above and are implemented on all four session types
(`BoardHostID`) and on `SSHSession`/`TmuxSSHSession` (`BoardExecutor`'s `RunBoardCommand`).

Not yet implemented: the relay goroutine that polls every host's agmsg on a schedule and populates
`BoardCache` as a side effect, the bootstrap flow that writes a one-time onboarding instruction into
a pane's PTY, the `/api/board/*` and `/ws/board-command` REST/WS surface, and the command center.
The `server.auth_token` config field, its non-loopback validation rule, and a constant-time bearer
auth middleware are implemented (see [security.md](security.md#auth-token-and-transport-encryption)),
but that middleware is not yet connected to any route.

The same design also specifies a **command center**: a single headless `claude -p --resume`
subprocess, invoked per query, that reads and writes the board exclusively through panemux's own
authenticated REST API (never agmsg directly) via a narrow, purpose-built MCP server panemux
provides — so `internal/board` stays the only code that ever calls agmsg's scripts even though the
command center is a second, independent consumer of the board's data. This is a substantial part of
the design's own scope (its own process lifecycle, permission model, and streaming API), not a minor
addendum to the messaging/relay piece described above.

Full design and rationale for both pieces live in [agent-board.md](agent-board.md); do not treat
that document's API/config surface as implemented until its status note says so.

### `internal/api`

REST endpoints expose workspaces, layout compatibility, display settings, session lifecycle operations, and editor integrations.

Workspace-related endpoints:

- `GET /api/workspaces` returns the workspace list, active workspace ID, tab position, and each workspace layout.
- `POST /api/workspaces` adds a single-local-pane workspace and makes it active.
- `PUT /api/workspaces/tab-position` changes `workspaces.tab_position` and persists it.
- `PUT /api/workspaces/{id}` renames a workspace.
- `DELETE /api/workspaces/{id}` removes a workspace, closes that workspace's sessions after persistence succeeds, and refuses to delete the last workspace.
- `PUT /api/workspaces/active` switches the active workspace and persists the selection.
- `PUT /api/workspaces/{id}/layout` updates a specific workspace layout.
- `GET/PUT /api/layout` remain as compatibility endpoints for the active workspace layout.

`POST /api/sessions/{id}/open-vscode` launches VSCode pointed at the session's live working directory. Like `GET /api/sessions/{id}/git-info`, it may prefer the worktree of an active interactive `codex` or `claude` process when that worktree belongs to the same repository, and it keeps using the last valid sibling worktree after the agent exits until the pane changes repository context. For local sessions it runs `code <cwd>`; for SSH sessions it runs `code --remote ssh-remote+<connection> <cwd>`. The binary is located via `exec.LookPath("code")` with a macOS app-bundle fallback.

Why REST here:

- layout and display data are request/response resources, not streams
- easier to test and inspect than pushing everything through WebSocket
- clear separation between configuration mutations and terminal byte transport

### `internal/ws`

The WebSocket handler bridges browser clients to sessions. The interactive terminal uses one primary
socket per visible pane, and the frontend attention monitor may open additional read-only
subscribers for the same session ID while a workspace is hidden.

Protocol split:

- binary frames: raw terminal input/output
- text frames: JSON control messages such as `resize`, `status`, and replay lifecycle markers

Why this split:

- avoids encoding terminal traffic into JSON
- keeps control messages explicit and versionable
- matches the low-latency needs of terminal streaming
- works with the backend session manager's fan-out model, where one session can have multiple
  concurrent subscribers receiving the same output stream
- lets the frontend suppress xterm stdin only while replayed snapshot bytes are being re-applied,
  which prevents replayed terminal queries from generating fresh replies back into the PTY

Replay lifecycle contract:

- the backend emits replay lifecycle only around buffered snapshot delivery, never around live output
- `replay:start` means "all following binary frames are replay bytes until `replay:end`"
- `replay:end` means "no more replay bytes will be sent on this connection"; the frontend may still
  be draining already-scheduled xterm writes
- if a replay control frame write fails, the handler stops forwarding on that connection instead of
  risking live output after an incomplete replay transition
- if the connection dies after `replay:start` but before the frontend observes a matching `replay:end`,
  the frontend may temporarily retain replay suppression state until the next socket open resets it

### `internal/server`

This package wires chi routes, middleware, REST handlers, WebSocket handlers, and static file serving.

Why `chi`:

- minimal abstraction over `net/http`
- small API surface
- route composition is clear and cheap for a service this size

## Frontend

### React + Vite

React renders a recursive pane tree and keeps client-side layout state manageable. Vite provides fast development startup and a simple production build pipeline.

Why React:

- recursive split layouts map naturally to components
- local state transitions for resize/split/close are straightforward
- the app needs interactive UI logic more than a large framework runtime

Why Vite:

- low-config setup
- fast local dev loop
- build output is easy to embed into the Go binary

### `useLayout`

Fetches `/api/workspaces` and `/api/display`, applies runtime validation, tracks the active workspace layout, and persists layout changes back to the active workspace. The workspace bar remains visible even with a single workspace so that workspace add, inline rename, delete, and tab-position controls remain available. Fast default-pane creation is exposed from the pane header so the common right/down add path does not require a dialog. Delete uses a confirmation dialog before calling the workspace delete API.

Why this hook:

- keeps server synchronization in one place
- isolates debounce/persistence logic from view components
- makes split/close behavior easier to reason about and test

### Pane Git/PR resolution

`GET /api/sessions/{id}/git-info` resolves repository metadata in two stages:

- first, it asks the session for its live working directory
- second, it may ask for an active interactive agent workdir override
- local and local tmux sessions inspect Git metadata on the local filesystem; SSH and SSH+tmux sessions inspect Git metadata on the remote host over the existing SSH connection so remote-only repositories still render header Git info

The override path is intentionally narrow:

- only interactive `codex` and `claude` processes are considered
- local and local tmux sessions only consider descendants of the pane's own shell process or active tmux pane process
- SSH sessions scan the remote process list on the current SSH connection, and SSH+tmux sessions restrict that scan to the active remote tmux pane's process tree
- only worktrees that belong to the same Git common dir as the pane's base repository are accepted

When a valid override worktree is accepted, the API layer caches that sibling worktree per pane
session. Later requests reuse it when no active agent workdir is currently detectable, but only
while the pane still resolves to the same Git common dir and a different worktree root. This keeps
pane headers and editor-opening behavior stable after an agent exits without allowing stale
cross-repository reuse.

For interactive Codex sessions, all four session implementations (`local`, `ssh`, `tmux`, and `ssh_tmux`) may inspect the open Codex session log under `~/.codex/sessions/...jsonl` when the Codex process has that file open. Panemux currently prefers the latest `exec_command.arguments.workdir` recorded in `response_item` / `function_call` log entries, then falls back to `turn_context.cwd`, then `session_meta.cwd`.

To avoid repeatedly reparsing unchanged agent logs, panemux caches the last resolved workdir per
session-log path fingerprint. Local sessions use file size plus modtime; SSH-backed sessions ask
the remote host for lightweight file metadata first and only transfer the full `jsonl` contents
again when that fingerprint changes.

This ordering is intentional and reflects observed Codex behavior as of `codex-tui` `0.130.0`:

- the interactive Codex process may keep its OS-level process cwd at the original pane directory
- `session_meta.cwd` and later `turn_context.cwd` may also remain pinned to that original pane directory
- individual tool calls still record their actual execution directory in `exec_command.arguments.workdir`

Panemux treats that `workdir` field as the strongest available signal for the active worktree because it is the only one that changes when Codex executes tools inside a sibling Git worktree while the parent interactive process remains attached to the original pane directory. If a future Codex release changes this logging contract, compare new logs against these three fields before changing the resolver so behavior remains reviewable and intentional.

For interactive Claude sessions, all four session implementations may inspect `~/.claude/sessions/<pid>.json` to locate the active transcript under `~/.claude/projects/...`. Panemux derives the active worktree from that transcript in priority order: the latest `Bash` tool `cd ... &&` target, then the latest top-level `cwd` field recorded on transcript entries, then the latest non-auxiliary file-touch path (`Read`/`Edit`/`Write`/etc, or file-history snapshot). The `Bash` `cd` target is checked first for the same reason Codex's `workdir` field takes priority over `session_meta.cwd`/`turn_context.cwd` above: the top-level `cwd` field reflects the interactive Claude process's own OS-level working directory, set once at launch and never updated for that process's lifetime, so it cannot by itself signal that a Bash tool call `cd`'d into a sibling Git worktree. Because a real Claude Code transcript has a non-empty top-level `cwd` on nearly every record, naively preferring it over the `Bash` `cd` target makes that detection unreachable in practice. A file-touch path remains a weaker signal than the top-level `cwd`, since touching a single unrelated file elsewhere does not by itself indicate the agent moved its active work there.

A `Bash` `cd` target, once seen, remains authoritative for the rest of that transcript until a *later* `Bash` `cd` target replaces it; neither a subsequent top-level `cwd` record nor a subsequent file-touch path can displace it. This is intentional and mirrors the Codex `workdir` precedence above: an explicit `cd` is treated as a durable "the agent has moved its base of operations here" signal, and the two weaker signals below it are not reliable enough evidence that the agent moved back to justify overriding it.

### `useWorkspaceAttentionMonitor`

Opens lightweight background WebSocket subscriptions for every pane ID across all workspaces and
feeds terminal output through the agent-attention detector, even when a workspace is not active and
its xterm panes are unmounted.

Why this hook:

- decouples prompt notifications from visible xterm mounts
- keeps inactive workspace attention behavior consistent with active panes
- centralizes per-pane stream decoding, visibility gating, and last-notified dedupe

The hook derives browser-notification eligibility from:

- active workspace id
- maximized pane id
- browser activity via `document.visibilityState` and `document.hasFocus()`

Each detected prompt is normalized into a stable signature. The last signature that produced a
browser notification is persisted per pane in browser storage, so terminal replay after a refresh or
WebSocket reconnect does not re-notify the same prompt.

### `useBrowserNotificationPermission`

Requests Notification API permission on the first user interaction when the browser permission state
is still `default`, so the first detected prompt does not need to race a permission prompt.

### `useWebSocket`

Owns a single socket connection, reconnect behavior, and validated text-frame handling.

Why this hook:

- prevents reconnection logic from leaking into terminal rendering code
- stores callbacks in refs to avoid reconnects on rerender
- keeps transport behavior reusable and testable

### `useTerminal`

Owns xterm.js setup, fit behavior, byte forwarding, and resize reporting.

When the backend labels buffered reconnect output with replay control frames, this hook temporarily
sets `xterm.options.disableStdin = true` while those replay bytes are written. That keeps xterm's
auto-generated terminal replies from being forwarded as accidental shell input during browser
refreshes or workspace remounts.

Replay state ownership in this hook:

- `replayActive`: true between `replay:start` and `replay:end`
- `replayWriteDepth`: count of replay `term.write(...)` calls whose callbacks have not fired yet
- `awaitingReplayEnd`: true after `replay:start` and false once `replay:end` has been received for the current connection
- `disableStdin`: derived safety switch; forced on whenever replay is active or draining, forced off
  on reconnect reset and after the final replay write callback

This gives the hook a three-phase replay lifecycle:

1. `live`: no replay pending, stdin enabled
2. `replay pending end`: replay frames still arriving, stdin disabled
3. `replay draining`: end marker received, but queued xterm writes still draining, stdin disabled

The hook resets all replay fields on each WebSocket open so an interrupted replay from a previous
connection cannot suppress stdin for the new connection.

This lifecycle is also captured as an Alloy model in
[replay_state.als](models/replay_state.als), which treats
reconnect, replay frame delivery, replay-control write failure, socket close, and replay write
completion as explicit transition events and checks the stale-suppression invariants over bounded
traces.

State-transition verification rule:

- Code that introduces or changes externally observable state transitions should have a matching
  Alloy model under `docs/models/`.
- Changes to transition logic should update that model in the same PR so the checked state machine
  remains aligned with the implementation.
- GitHub Actions runs Alloy checks only when the model files change, so model maintenance is the
  mechanism that keeps transition verification on the critical path.

Why xterm.js:

- mature browser terminal emulator
- supports raw byte streams and common terminal behavior
- avoids implementing terminal emulation from scratch

### Zod schemas

Frontend payloads are validated with Zod before they are trusted.

Why Zod:

- runtime validation catches malformed server responses
- TypeScript types are inferred from schemas, reducing drift
- keeps API and WebSocket assumptions explicit

## Security Design

Security requirements that must be consulted during implementation live in [security.md](security.md).

Architecture-level security summary:

- local process execution is intentionally funneled through validated command paths and strict argument handling
- remote shell entrypoints validate SSH working directories before interpolating them into shell commands
- host-key handling intentionally preserves compatibility with OpenSSH hashed `known_hosts` entries
- shipped code should structurally avoid `gosec` findings rather than suppress them
- panemux does not terminate TLS; non-loopback exposure is expected to sit behind operator-managed infrastructure (reverse proxy, tunnel, VPN), and the `server.auth_token` config field (implemented, but not yet enforced by any route — see this document's `internal/board` section above) is only meaningful once that transport is encrypted — see [agent-board.md](agent-board.md#security-model)

## Tradeoffs and Intentional Limits

- One WebSocket per pane is simple and isolates failures, but increases connection count with many panes.
- Open CORS and permissive WebSocket origin checks reduce friction for local use, but are not suitable as-is for an untrusted deployment.
- All workspace panes are started at backend startup, including panes in inactive workspaces. This keeps tab switching fast and preserves terminal state, at the cost of using resources for hidden workspaces.
- Dynamic session creation exists, but current UI behavior mainly creates new local panes; this is not yet a full remote session orchestration product.
- The planned `internal/board` cross-host relay (see [agent-board.md](agent-board.md)) makes panemux a persistent relay for agent-to-agent messages between hosts it cannot make talk to each other directly, closer to a TURN server than a STUN server: panemux stays in the data path for the life of the exchange rather than helping two hosts connect directly and stepping aside, and it sees each relayed message as plaintext in process memory between the two encrypted SSH hops.
- The same planned design's command center spawns a `claude -p` subprocess per query rather than keeping one warm — simpler process lifecycle and no persistent extra process, at the cost of response latency that includes subprocess startup on every query (see [agent-board.md's Process lifecycle](agent-board.md#process-lifecycle)).
