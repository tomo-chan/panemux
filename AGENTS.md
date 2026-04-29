# AGENTS.md

This file provides guidance to Codex, Claude Code, and other coding agents when working with code in this repository.

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

**Format**:
```sh
make fmt        # apply go fmt -s to all Go files
```

**Checks**:
```sh
make lint       # includes go fmt check; fails if any Go file is unformatted
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
- Go: all tests must pass (`make test-go`) before moving on.
- Frontend: all tests must pass (`make test-frontend`) before moving on.

### Test granularity

**Test at the smallest unit that exercises the logic, not at the outermost entry point.**

**Test-case design rule:** before implementing or declaring a feature covered, enumerate the feature's behavioral factors and cover their meaningful combinations in tests. Do not stop at one happy path plus one error path.

For every new feature or behavior change, explicitly consider:

- **Input shape variants:** legacy and new schemas, omitted optional fields, defaulted fields, invalid enum values, empty lists, duplicate IDs, unknown references, malformed request bodies.
- **State variants:** active vs inactive items, empty vs populated state, existing vs missing resources, edit mode on vs off, persisted config vs memory-only config.
- **Operation variants:** read, create, update, delete, switch, save, reload, restart, and no-op cases where applicable.
- **Boundary variants:** minimum/maximum values, zero/negative values, size sums, single item vs multiple items, nested structures, and paths with `~/` when paths are involved.
- **Compatibility and migration:** old config/API shape, new config/API shape, precedence when both exist, migration-on-save behavior, and post-reload behavior after migration.
- **Persistence and side effects:** in-memory updates, disk writes, unchanged unrelated siblings/items, session manager changes, restart behavior, and API response status/body.
- **Frontend runtime validation:** Zod acceptance/rejection, API fallback behavior, active UI state, invalid selection no-ops, and all visible mode/position variants.

Use table-driven tests when factors form a matrix. For large matrices, test all high-risk cross-products and pairwise combinations for the rest, but document the factors through test names and fixtures so omissions are intentional and reviewable.

The following anti-patterns caused bugs to slip through:

| Anti-pattern | Problem | Correct approach |
|---|---|---|
| Testing a function that makes real network calls to verify config-resolution logic | Network errors hide config errors; any error is accepted as "expected" | Extract pure config-resolution into a separate function (e.g., `resolveSSHConfig`) and test it directly, asserting exact return values |
| `if err != nil { assert.NotContains(t, err.Error(), "not found") }` | Accepts *any* error except "not found" — masks setup errors like "no auth methods" or "reading key file ~/..." | Assert the specific happy path: `require.NoError(t, err)` and check returned values. For expected-failure paths, assert the exact error string |
| Test data that never exercises the real-world variant | e.g., test SSH config with absolute `IdentityFile` paths, never `~/` — the tilde-expansion bug is invisible | Add test cases that cover the exact formats users produce: `~/`-prefixed paths, omitted optional fields |
| Testing only the error case of a new validation rule, not the passing case | The positive path (new feature works correctly) is never confirmed to pass | For every new validation rule, write *both* a failure test *and* a success test |

**Testability rule:** if production code calls `os.UserHomeDir()`, `DefaultPath()`, or any other global singleton directly, add an injectable override (unexported struct field, function parameter, or interface) so tests can substitute a controlled value without touching the real filesystem or home directory.

Example: `Config.sshConfigPath` (empty = use `sshconfig.DefaultPath()`, non-empty = use in tests).

### Schema-first
- **Go**: when changing data structures, update the validation rules and tests in `internal/config/validate.go` first.
- **Frontend**: when changing types, update the Zod schemas in `frontend/src/schemas/index.ts` first. TypeScript types are derived via `z.infer<>` — do not edit `types/index.ts` manually.
- API responses and WebSocket messages are validated at runtime against the schemas. When schemas change, update the corresponding tests too.

### Coverage (≥ 80 %)
- **Go**: `make coverage-go` checks that the combined coverage of `internal/config`, `internal/api`, `internal/ws`, and `internal/server` is ≥ 80 %. Session implementations that require live SSH / tmux are integration-tested separately and excluded from this gate.
- **Frontend**: `cd frontend && npm run coverage` enforces ≥ 80 % across `src/hooks/` and `src/schemas/`. UI components require a real browser renderer and are covered by integration / E2E tests.

### Quality gate
- `make check` (lint + test + coverage) must pass before `make build`.
- Test commands: `make test-go` / `make test-frontend` / `make test`
- Coverage commands: `make coverage-go` / `make coverage-frontend`
- Lint commands: `make lint-go` / `make lint-frontend` / `make lint`
- **Go lint includes `gofmt`, `go vet`, and `golangci-lint run ./...`** (v2, config in `.golangci.yml`). The lint binary auto-installs via `make install-deps` / `make install-deps-ci`.
- **Run `make lint-go` (or `make lint`) after every Go code change, before committing.** CI gates PRs on the same command.

### Documentation updates
- When a behavior, operational assumption, browser requirement, rendering constraint, or user-visible rule becomes confirmed, update the relevant files in `docs/` in the same change.
- Do not leave documentation follow-up as a separate "later" task once the behavior is considered settled.

### GitHub Actions pinning
- Pin GitHub Actions to full commit SHAs, not floating tags like `@v4` or `@v5`.
- When updating workflows, resolve the tag you intend to use to its exact commit SHA first, then commit the SHA-pinned reference.

### Ignore generated resources
- When adding new generated artifacts, caches, release outputs, or other non-source resources, update `.gitignore` in the same change.
- Do not leave new build or release byproducts such as `dist/` as recurring untracked files.

### CodeQL compliance (`go/command-injection`)

When writing code that passes user-supplied values to `exec.Command`:

- **The first argument (command) must never be user input.** Use a hardcoded literal or a value read from a trusted system source (`/etc/shells`, `/etc/passwd`, etc.).
- **Avoid `os.Getenv` for values that flow to exec.** Environment variables are CodeQL taint sources. Use hardcoded defaults instead.
- **Returning `m[1]` from `FindStringSubmatch(userInput)` does not break taint.** The submatch is still derived from user input in CodeQL's data-flow model.
- **Returning a key `s` from `for s := range trustedMap` breaks taint**, provided no other code path in the same function returns the user-supplied value directly (including fallbacks).
- **Regex `MatchString` as a guard is the CodeQL-recommended sanitization pattern** for subsequent arguments; for the command itself, a system-registry lookup (returning the registry's own value) is required.

See `docs/architecture.md` → *Security Design* for the full rationale and the `/etc/shells` pattern used in this codebase.

**Remote path arguments (SSH working directory):** user-supplied paths that flow into `sess.Start()` shell commands must be validated with `validRemotePath` (defined in `internal/session/ssh.go`) before use. This regex guard is the CodeQL-recommended sanitization pattern for shell arguments, and it rejects shell metacharacters (`;|&$\`'"<>(){}[]!\`) and control characters while allowing valid Unix path characters including spaces and Unicode.

### Release workflow
- **Never manually close a release-please PR.** Doing so leaves the `release-please--branches--main` internal tracking branch in a stale state. On the next push to `main`, release-please re-creates the release PR from that stale state, producing incorrect release notes that include all historical commits.
- If you need to override the version release-please proposes (e.g., cut a patch instead of a minor):
  - Add the `release-as: x.y.z` label to the existing release PR **before** merging it, or
  - Include `Release-As: x.y.z` in the footer of a commit message on `main`.
- If the internal branch does become stale (e.g., after an incident), reset it:
  ```sh
  gh pr close <release-pr-number>   # close the incorrect PR first
  git push origin origin/main:refs/heads/release-please--branches--main --force
  ```

### Branch workflow
- **Never commit directly to `main`.** All changes — including documentation and `CLAUDE.md` — must go through a feature branch and PR.
- Create a worktree and branch before making any edits:
  ```sh
  git worktree add ../<repo>-<feature> -b feature/<name>
  ```
- Do all editing, testing, and committing inside the worktree.
- Push and open a PR, then merge with `--squash --delete-branch`.
- Remove the worktree after merging:
  ```sh
  git worktree remove ../<repo>-<feature>
  ```

### Pull request title
- PR titles must follow Conventional Commits format: `<type>: <description>`.
- Allowed types are defined in `.github/workflows/pr-title.yml`: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `build`, and `ci`.
- Use `.github/labeler.yml` as the guide for choosing the type when the change maps cleanly to one label: `docs:` for Markdown/docs-only changes, `ci:` for `.github`/workflow/build configuration changes, and the best matching product type for frontend/backend code changes.

### Pull request test plan
- After creating a PR, run every item in the test plan locally and verify it passes.
- Update the PR description with all checkboxes checked (`- [x]`) before considering the task complete.
- Do not leave test plan items unchecked — the description must reflect actual verified results.
