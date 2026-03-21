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

This package loads and validates YAML configuration, expands `~/` paths, exposes flattened pane traversal, and persists layout updates.

Why it exists as a separate package:

- keeps config rules out of handlers
- gives one source of truth for layout validation
- makes config behavior easy to test without network/session dependencies

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

### `internal/api`

REST endpoints expose layout, display settings, and session lifecycle operations.

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

Fetches `/api/layout` and `/api/display`, applies runtime validation, updates local state, and persists layout changes.

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

## Tradeoffs and Intentional Limits

- One WebSocket per pane is simple and isolates failures, but increases connection count with many panes.
- Open CORS and permissive WebSocket origin checks reduce friction for local use, but are not suitable as-is for an untrusted deployment.
- Dynamic session creation exists, but current UI behavior mainly creates new local panes; this is not yet a full remote session orchestration product.
