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

### `internal/api`

REST endpoints expose workspaces, layout compatibility, display settings, session lifecycle operations, and editor integrations.

Workspace-related endpoints:

- `GET /api/workspaces` returns the workspace list, active workspace ID, tab position, and each workspace layout.
- `POST /api/workspaces` adds a single-local-pane workspace while edit mode is enabled and makes it active.
- `PUT /api/workspaces/active` switches the active workspace and persists the selection.
- `PUT /api/workspaces/{id}/layout` updates a specific workspace layout.
- `GET/PUT /api/layout` remain as compatibility endpoints for the active workspace layout.

`POST /api/sessions/{id}/open-vscode` launches VSCode pointed at the session's live working directory. For local sessions it runs `code <cwd>`; for SSH sessions it runs `code --remote ssh-remote+<connection> <cwd>`. The binary is located via `exec.LookPath("code")` with a macOS app-bundle fallback.

Why REST here:

- layout and display data are request/response resources, not streams
- easier to test and inspect than pushing everything through WebSocket
- clear separation between configuration mutations and terminal byte transport

### `internal/ws`

The WebSocket handler bridges one browser pane to one session.

Protocol split:

- binary frames: raw terminal input/output
- text frames: JSON control messages such as `resize` and `status`

Why this split:

- avoids encoding terminal traffic into JSON
- keeps control messages explicit and versionable
- matches the low-latency needs of terminal streaming

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

Fetches `/api/workspaces` and `/api/display`, applies runtime validation, tracks the active workspace layout, and persists layout changes back to the active workspace. The tab bar is hidden when only one workspace exists during normal use; edit mode exposes the workspace add control.

Why this hook:

- keeps server synchronization in one place
- isolates debounce/persistence logic from view components
- makes split/close behavior easier to reason about and test

### `useWebSocket`

Owns a single socket connection, reconnect behavior, and validated text-frame handling.

Why this hook:

- prevents reconnection logic from leaking into terminal rendering code
- stores callbacks in refs to avoid reconnects on rerender
- keeps transport behavior reusable and testable

### `useTerminal`

Owns xterm.js setup, fit behavior, byte forwarding, and resize reporting.

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

### Session command execution

All session types that execute a local process use `exec.Command` with user-configurable values (shell path, tmux session name). These values must be sanitized before reaching the exec sink. The rules are:

**Shell path (`local`, `ssh` sessions)**

`validateShell` in `internal/session/local.go` applies three layers:

1. **Absolute-path check** — rejects relative paths outright.
2. **Regex character allowlist** — `^(/[a-zA-Z0-9._\-/]+)$` rejects shell metacharacters (spaces, semicolons, quotes, etc.).
3. **`/etc/shells` allowlist** — iterates the system shell registry and returns the **key from the map** (`s`), not the caller-supplied value.

The third point is critical for CodeQL's `go/command-injection` analysis. CodeQL tracks data flow from taint sources (environment variables, HTTP request bodies) to exec sinks. A sanitization function only breaks the taint chain if its **return value has no data-flow path back to user input**. Returning `m[1]` from `regexp.FindStringSubmatch(shell)` is insufficient because the submatch is still derived from `shell`. Returning the `/etc/shells` map key `s` works because CodeQL does not propagate taint through equality comparisons in a range loop — `s` originates from file I/O, not user input.

For the same reason, `os.Getenv("SHELL")` is not used as a default shell. Environment variables are taint sources in CodeQL's model; if the env-var value were to flow through `exec.Command` even after validation, the alert would remain. The default is always the hardcoded literal `"/bin/sh"`.

**Tmux session name (`tmux`, `ssh_tmux` sessions)**

`validTmuxSessionName` in `internal/session/tmux_ssh.go` uses a strict regex (`^[a-zA-Z0-9_.-]+$`) validated at construction time, and arguments are passed as discrete `exec.Command` args (not via `sh -c`), so no shell interpolation occurs.

**Remote path arguments (SSH working directory)**

When an SSH or SSH+tmux pane has `cwd` set, the path is passed as part of a remote shell command (`cd <cwd> && exec $SHELL`). User-supplied paths that flow into `sess.Start()` must be validated with `validRemotePath` (defined in `internal/session/ssh.go`) before use.

`validRemotePath` is a regex guard (`^(/[^;|&$\`'"<>()\[\]{}!\\\x00-\x1f\x7f]*)+$`) that accepts only absolute Unix paths and rejects shell metacharacters and control characters. This is the CodeQL-recommended sanitization pattern for shell arguments.

After validation, the path is wrapped with `shellQuotePath`, which single-quotes the value and escapes any interior single quotes. This ensures paths containing spaces or unusual (but allowed) characters are safe to embed in a shell string.

**General rule**

When adding new session types or new `exec.Command` calls: the value passed as the command (first argument) must come from a hardcoded literal or from a trusted system source (file, registry) with no data-flow path to user input. Arguments after the command may be user-supplied if they cannot be interpreted as commands by the target binary.

## Tradeoffs and Intentional Limits

- One WebSocket per pane is simple and isolates failures, but increases connection count with many panes.
- Open CORS and permissive WebSocket origin checks reduce friction for local use, but are not suitable as-is for an untrusted deployment.
- All workspace panes are started at backend startup, including panes in inactive workspaces. This keeps tab switching fast and preserves terminal state, at the cost of using resources for hidden workspaces.
- Dynamic session creation exists, but current UI behavior mainly creates new local panes; this is not yet a full remote session orchestration product.
