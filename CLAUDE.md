# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

**Full build** (frontend must be built before backend, because Go embeds `frontend/dist/`):
```sh
cd frontend && npm run build   # outputs to frontend/dist/
go build -o bin/panemux .
```

Or via Makefile:
```sh
make install-deps   # npm install + go mod download (first time only)
make install-deps-ci  # npm ci + go mod download (CI / reproducible installs)
make build          # build-frontend then build-backend
make release-snapshot  # local release archives via GoReleaser
```

**Run**:
```sh
./bin/panemux                          # default config (single local shell)
./bin/panemux --config config.yaml    # load layout from YAML
./bin/panemux --port 9090 --open      # override port, auto-open Chrome
```

**Frontend dev server** (hot-reload, proxies `/api` and `/ws` to `:8080`):
```sh
make dev-frontend   # Vite dev server on :5173
make dev-backend    # backend must be running separately
```

**Checks**:
```sh
make lint
make test
make check
```

**Distribution**:
```sh
./install.sh --repo owner/panemux
make release-snapshot
```

## Architecture

### Data flow

```
Browser (xterm.js) ──binary frames──▶ WebSocket /ws/{sessionID} ──▶ PTY/SSH stdin
                   ◀─binary frames── WebSocket                   ◀── PTY/SSH stdout
                   ──JSON frames───▶ {"type":"resize","cols":…}
                   ◀─JSON frames─── {"type":"status","state":"connected|exited"}
```

Each terminal pane opens its own WebSocket connection (`/ws/{sessionID}`). There is no multiplexing — one socket per session.

### Backend (`internal/`)

- **`config/`** — Loads YAML config into `Config` struct. `LayoutNode`/`LayoutChild`/`PaneConfig` have both `yaml:` and `json:` tags so they can be served directly from the REST API. `LayoutChild.Size` is `float64` to preserve fractional percentages after drag-resize.
- **`session/`** — `Session` interface (`Read`/`Write`/`Resize`/`Close`). Four implementations: `LocalSession` (creack/pty), `SSHSession` (x/crypto/ssh), `TmuxLocalSession` (tmux attach via pty), `TmuxSSHSession` (SSH → tmux attach). `Manager` is a thread-safe map of active sessions.
- **`ws/handler.go`** — Upgrades HTTP to WebSocket, then bidirectionally bridges the session. Binary frames = raw terminal I/O (no encoding). Text frames = JSON control messages (resize, status).
- **`api/handler.go`** — REST handlers for layout and session management.
- **`server/server.go`** — chi router wiring API, WebSocket, and embedded frontend static files. The embedded `frontend/dist` is served with SPA fallback to `index.html`.

Sessions are started at server startup from the YAML config (`main.go: startSessionsFromConfig`). There is no dynamic session creation from the UI yet — the REST `POST /api/sessions` endpoint is reserved for future use.

### Frontend (`frontend/src/`)

- **`useWebSocket.ts`** — Manages a single WebSocket with auto-reconnect. Callbacks (`onMessage`, `onOpen`, `onClose`) are stored in refs so the `connect` function is stable and does not trigger reconnects on re-renders.
- **`useTerminal.ts`** — Initialises an xterm.js `Terminal` instance once per container element. `send` is accessed via `sendRef` inside `onData`/`onBinary` so the handlers never capture a stale closure.
- **`SplitContainer.tsx`** — Recursively renders `LayoutNode`/`LayoutChild` trees. `LayoutRenderer` handles drag-resize by computing `deltaPercent` from the container's pixel size and updating sibling `size` values (which are percentages summing to 100).
- **`TerminalPane.tsx`** — Mounts the container div, passes it to `useTerminal`, attaches a `ResizeObserver` to call `handleResize` whenever the pane changes size.

### Config format

`config.example.yaml` is the authoritative reference. Key points:
- `layout` is a recursive tree of `direction`/`children` nodes and leaf `pane` nodes.
- `size` values in siblings should sum to 100 (treated as percentages).
- SSH panes reference a key in `ssh_connections`; tmux panes set `tmux_session` to the session name.

---

## Development Rules

### TDD
- Always **write tests first**, confirm they fail, then implement.
- Go: all tests must pass (`go test ./... -v -race`) before moving on.
- Frontend: all tests must pass (`cd frontend && npm test`) before moving on.

### Schema-first
- **Go**: when changing data structures, update the validation rules and tests in `internal/config/validate.go` first.
- **Frontend**: when changing types, update the Zod schemas in `frontend/src/schemas/index.ts` first. TypeScript types are derived via `z.infer<>` — do not edit `types/index.ts` manually.
- API responses and WebSocket messages are validated at runtime against the schemas. When schemas change, update the corresponding tests too.

### Coverage (≥ 80 %)
- **Go**: `make coverage-go` checks that the combined coverage of `internal/config`, `internal/api`, `internal/ws`, and `internal/server` is ≥ 80 %. Session implementations that require live SSH / tmux are integration-tested separately and excluded from this gate.
- **Frontend**: `cd frontend && npm run coverage` enforces ≥ 80 % across `src/hooks/` and `src/schemas/`. UI components require a real browser renderer and are covered by integration / E2E tests.

### Quality gate
- `make check` (lint + test + coverage) must pass before `make build`.
- Test commands: `go test ./... -v -race` / `cd frontend && npm test`
- Coverage commands: `make coverage-go` / `cd frontend && npm run coverage`
- Lint commands: `go vet ./...` / `cd frontend && npx tsc --noEmit`

### Documentation updates
- When a behavior, operational assumption, browser requirement, rendering constraint, or user-visible rule becomes confirmed, update the relevant files in `docs/` in the same change.
- Do not leave documentation follow-up as a separate “later” task once the behavior is considered settled.

### GitHub Actions pinning
- Pin GitHub Actions to full commit SHAs, not floating tags like `@v4` or `@v5`.
- When updating workflows, resolve the tag you intend to use to its exact commit SHA first, then commit the SHA-pinned reference.

### Ignore generated resources
- When adding new generated artifacts, caches, release outputs, or other non-source resources, update `.gitignore` in the same change.
- Do not leave new build or release byproducts such as `dist/` as recurring untracked files.

### Pull request test plan
- After creating a PR, run every item in the test plan locally and verify it passes.
- Update the PR description with all checkboxes checked (`- [x]`) before considering the task complete.
- Do not leave test plan items unchecked — the description must reflect actual verified results.
