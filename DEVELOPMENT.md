# Development Guide

This document is the developer workflow reference for building, testing, changing, and shipping this repository.

## Runtime version contract

`go.mod`'s Go directive and the root `.node-version` are the exact Go and Node versions
used locally and in CI. Node is 24 LTS. Run ordinary `make` commands without setting
`GOTOOLCHAIN`, `PATH`, or a web-storage `NODE_OPTIONS` workaround: the common shell selects
both SDKs before Makefile initialization, recipes, and child processes. It overrides inherited
`GOTOOLCHAIN`, selects the SDK's `go`/`gofmt` and Node's bundled `npm`/`npx`, and preserves
user `NODE_OPTIONS`. A newer installed runtime does not change the selected version.

Install a Go launcher supporting toolchain selection (Go 1.21 or later). Go uses its normal
module cache. Node reuses a matching SDK on PATH or in nvm; otherwise the first normal command,
including `make install-deps`, fetches the exact official SDK into the ignored checkout-local
`.cache/runtimes/` directory. This bootstrap needs `curl`, `tar`, and `sha256sum` or `shasum`.
It supports macOS/Linux x64 and arm64; Windows uses WSL2. Acquisition uses HTTPS and official
SHA256 checksums, a bounded install lock, and atomic publication after startup/version checks.
Offline commands reuse an installed SDK. Invalid pins, unavailable SDKs, corrupted cache or
archives, and unsupported systems fail before the command proceeds. Inspect a stale lock or
bad cache before removing it; the selector never overwrites a bad published SDK.

Direct hooks, efficacy/mutation scripts, browser fixture launchers, and frontend npm scripts
select the same contract from their checkout. For ad hoc commands use
`scripts/runtime-shell.sh -c 'node --version; go version'`. From `frontend`, use
`../scripts/npm.sh install` or `../scripts/npm.sh ci` for the selected npm CLI; bare
`npm install/ci` rejects a mismatched Node via the root lifecycle guard. Bare npm may perform
its own dependency preparation before that guard, so use the wrapper or `make install-deps`
for dependency setup. `npm run` scripts select the contract for their actual tooling commands.
Interactive bare `node` and `go` still follow the caller's shell.
Browser fixture launchers validate SDKs before changing fixture state. The Stop hook
handles `stop_hook_active` retries before SDK selection so a failed SDK cannot cause
repeated blocks; the initial invocation still validates both SDKs.

`make test-go-toolchain` and `make test-node-toolchain` protect selection and bootstrap
failure behavior. Update the single-source pins to change versions; a Go `toolchain` directive
alone does not force a newer local launcher to downgrade. Diff-gate comparison refs have a
separate contract. Node release and bundled npm metadata come from the
[official release index](https://nodejs.org/dist/index.json); support dates come from the
[official release schedule](https://github.com/nodejs/Release/blob/main/schedule.json).

## Build And Run

**Full build**:

```sh
make install-deps      # first-time setup: npm install + go mod download
make install-deps-ci   # CI / reproducible dependency install
make build             # full build; frontend/dist is embedded into the Go binary
make release-snapshot  # local release archives via GoReleaser
```

**Run**:

```sh
./bin/panemux
./bin/panemux --config config.yaml
./bin/panemux --port 9090 --open
```

**Frontend dev server**:

```sh
make dev-frontend   # Vite dev server on :5173
make dev-backend    # run backend separately while the frontend proxies /api and /ws
```

**Documentation screenshots**:

```sh
make screenshots   # regenerates docs/images/*.png
```

The images in `README.md` and `docs/` are captured by Playwright from a real panemux run
(`frontend/screenshots/`) with placeholder content only: a fake `HOME` with the `XDG_*` and git
configuration variables cleared (so neither tmux nor git reads the developer's own configuration), a
throwaway git repository at `/tmp/sample-project`, a private tmux socket, a stub agmsg store, and a
fixed task list served to the task dashboard in place of the real collection, which would list the
developer's own agent sessions. It needs `tmux` for the tmux pane; set `PLAYWRIGHT_CHROMIUM_EXECUTABLE`
as for `make test-e2e` when the installed Chromium is not the one Playwright expects. It is not part of
`make check`.

- The run stages everything under its root, `$TMPDIR/panemux-screenshots`, which it empties when it
  carries the `.panemux-screenshots` marker the run leaves in it, or when every entry in it is a
  name the run creates there (`home`, `tmux`, `panemux`, `showcase.yml`, `sample-project`,
  `panemux-screenshots-agmsg`). `/tmp/sample-project` and `/tmp/panemux-screenshots-agmsg` are
  symbolic links into that root, so no marker sits at those fixed paths to be lost.
  - Why markers get lost: macOS deletes old temporary files on its own. `com.apple.tmp_cleaner`
    deletes files under `/tmp` whose access, modification and change times are all over three days
    old, then empty directories over three days old, daily at 00:00; `com.apple.bsd.dirhelper`
    cleans `$TMPDIR` (`/var/folders/...`) of files over three days old. The marker is never read
    after it is written, so it can go while files read since stay.
  - A link the run made — its target is `<...>/panemux-screenshots/<the link's own name>` — is
    replaced, and what it pointed to is left alone.
  - A directory an earlier version of the run left there is removed: one carrying the marker, or one
    holding nothing but empty directories (what remains when its files, marker included, are
    deleted and the directories kept; it is removed with `rmdir`, which removes nothing else).
  - Anything else at those paths — a directory with any file or link in it, a file, a link to
    anywhere else — stops the run untouched: move it away and run again.
  - The links and the root are left in place after the run; the next run replaces them.
  - A lock (`/tmp/panemux-screenshots.lock`) refuses a second run while one is in progress. A lock
    whose process is gone, or whose `pid` file is missing, is taken over.
  - The panes' prompt shows `/tmp/sample-project`, not where the link leads: the run `cd`s through
    the link, panemux and the panes' bash inherit that `PWD`, and bash shows `PWD` while it names
    the directory bash is in.
- The tmux server the run starts is stopped by the capture's teardown
  (`frontend/screenshots/global-teardown.ts`), so nothing the run started outlives it.
- On macOS the panes' `/bin/bash` would announce that the default shell is now zsh; the run sets
  `BASH_SILENCE_DEPRECATION_WARNING=1` so the images read the same on every OS. The private tmux socket
  lives under `$TMPDIR/panemux-screenshots/tmux`, and a socket path is limited to 104 bytes on macOS
  (108 on Linux), counting the terminating NUL. tmux binds the socket under the directory's resolved
  path, and macOS's `/tmp` and `/var/folders` are links into `/private`, so the length counts there:
  macOS's default per-user `$TMPDIR` (about 57 bytes resolved) fits, but one much longer does not.
  The run checks this before staging anything (`tmux_socket_path_check` in
  `frontend/e2e/tmux-env.sh`, which the task-dashboard E2E fixture runs too) and stops, saying so —
  set a shorter `TMPDIR`, such as `TMPDIR=/tmp`, for it.
- `make screenshots` needs a pseudo-terminal and tmux, which the Claude Code sandbox denies: run it
  outside the sandbox. Its fixed paths (`/tmp/sample-project`, the agmsg store and the lock) stay
  where they are for the same reason, and `/tmp/sample-project` is the path the images show.
- Screenshot tmux helpers clear inherited `TMUX` for both startup and teardown: `TMUX` takes
  precedence over `TMUX_TMPDIR`, so changing the socket directory alone does not isolate a run
  started inside tmux. Their tests also clear it before any fixture commands, and verify with a
  separate caller server that startup, stop and teardown never touch that server.
- The task-dashboard E2E fixture clears `TMUX` and exports a short, unique `TMUX_TMPDIR` to
  panemux and its children. It starts tmux with `-f /dev/null` and stops its private server on exit,
  including sessions started or resumed through the dashboard. Playwright sends `SIGTERM` to this
  fixture so its cleanup trap runs. It never uses the caller's server.
- `make test-screenshots-check` tests both fixtures' tmux isolation and these staging helpers
  (`frontend/screenshots/screenshots-env.sh`). Its tmux sockets sit deeper than either fixture's, so
  it makes their directory with `tmux_short_dir`: under `$TMPDIR` when the socket fits there (inside
  the Claude Code sandbox, where `/tmp` is not writable), otherwise under `/tmp` (outside the sandbox
  on macOS, whose per-user `$TMPDIR` leaves no room). Where neither fits it fails, naming the path,
  rather than with tmux's `File name too long` — in CI as everywhere else.

- Any change that alters what those images show must retake them in the same change: run
  `make screenshots`, look at every image it rewrote, and commit them. This covers changes to the
  UI's components, layout, styles and text, and to the capture itself (`frontend/screenshots/`).
- CI enforces it (`.github/workflows/screenshots.yml`): a pull request that changes
  `frontend/src/components/`, `frontend/src/App.tsx`, `frontend/src/styles/`, `frontend/index.html`
  or `frontend/screenshots/` (test files aside; a file moved out of them counts under its old path)
  without touching `docs/images/` fails. Apply the
  `screenshots-exempt` label to a change that genuinely alters nothing the images show.
- The rule does not stop at those paths: a change under `frontend/src/hooks/`, `schemas/` or `utils/`
  that changes what is on screen needs new images too, though CI does not ask for them.

**Format**:

```sh
make fmt
```

**Checks**:

```sh
make lint
make test
make test-e2e
make check
```

### Codex verification

Use the fixed `make verify-*` entry points for checkout-local logs/build caches
and saved PR bases. See [Codex verification](docs/development/codex-verification.md)
for first-time user approval setup, supported scope, and remaining sandbox limits.

### Claude Code sandbox

The shared `.claude/settings.json` sets `sandbox.network.allowLocalBinding: true` for **macOS**
so HTTP/WebSocket tests and Playwright web servers can bind ports inside the Claude Code sandbox.
Keep this in the shared settings rather than requiring each contributor to configure
`.claude/settings.local.json`.

On macOS this also lets sandboxed commands connect to **any localhost port**, including services
started outside the sandbox. An unauthenticated local service, such as a debugger, can act for
the command outside the sandbox; a command listening on a non-loopback address can accept
connections from other machines. Every macOS contributor using these shared settings receives
this broader local network access. The setting does not edit domain allowlists or filesystem
permissions; dependency downloads, Git configuration, and shared tool caches still follow the
applicable sandbox permissions.

On **Linux and WSL2**, including Linux cloud containers, a sandboxed command already has its own
private localhost: it can bind ports and reach servers it starts itself, but cannot directly
reach host services there. `allowLocalBinding` has no effect and is not needed on those platforms.

When the sandbox is **admin-required**, Claude Code ignores this permission-loosening key in
repository settings, including `.claude/settings.json` and `.claude/settings.local.json`.
Configure it through user, CLI, or managed settings as permitted by the organization's policy;
managed settings may still prevent it from taking effect. See the official
[Claude Code sandboxing documentation](https://code.claude.com/docs/en/sandboxing#a-command-fails-to-reach-a-server-on-localhost).

The shared settings grant nothing else. `sandbox.allowPty`, `sandbox.network.allowUnixSockets` and
`sandbox.filesystem.allowGitConfig` are deliberately left out — why is decision D13 in
[docs/quality-gateway/decisions.md](docs/quality-gateway/decisions.md) — so on macOS the sandbox still
denies pseudo-terminals, Unix sockets (tmux cannot start a server), `ps`, `dscl`, and writes to
`.git/config` and to the user cache directory. The make targets work within that:

| Command | Inside the macOS sandbox |
|---|---|
| `make install-deps` | Passes once the hooks are installed: `install-hooks` writes `.git/config` only when `core.hooksPath` does not already lead to an identical, executable `pre-push` (git silently skips one without the executable bit). In a fresh clone run `make install-hooks` once outside the sandbox. `npm install` may warn `EPERM` on `.idea/` files inside packages; those warnings are harmless. |
| `make check` | Passes. A Go test that needs a pty, tmux, `ps` or `dscl` calls `internal/testcap`'s `RequirePTY`/`RequireTmux`/`RequirePS`/`RequireDscl`, and the screenshot fixtures' tmux checks probe the same way: each reports itself **skipped** where the probe fails. `golangci-lint` caches in the checkout's own `.cache/golangci-lint/`, never in the user cache directory (`scripts/golangci_lint_cache.sh`). |
| `make test-e2e` | Reports itself skipped: every pane needs a pty (`scripts/require_pty.sh`). |
| `make screenshots` | Fails, saying to run it outside the sandbox. It writes tracked images, so it is never skipped. |
| `git push -u` | Pushes the branch, then cannot record its upstream in `.git/config`. Name the remote and branch on every push instead: `git push origin <branch>`. |

With `CI` set, every one of those probes that fails **fails** instead of skipping, so CI is where the
skipped tests are verified. Write a new test that needs one of these capabilities the same way: call
the `testcap` helper first, never let it fail on the sandbox's error.

Every script makes its temporary files under `$TMPDIR` with an explicit `mktemp` template, and stops
when it cannot: macOS's bare `mktemp -d` ignores `$TMPDIR`, and a test script that carried on with an
empty work directory once committed its fixtures in the caller's own worktree.
`scripts/tmpdir_guard_test.sh` (`make test-tmpdir-guard`) runs each script with a failing `mktemp` —
its first call, and also its second where it has more than one call site, the first a fixture helper
makes — and asserts the repository it ran from is untouched. It fails on any `mktemp` call without a
template, and on any `x=$(mktemp ...)` assignment that does not handle a failure on the same line.

After changing sandbox settings, verify `make check`, `make test-e2e`, and `make test-hooks`
from Claude Code with sandboxing active. Runs from another agent or outside the sandbox verify
the suites but do not establish that Claude Code sandbox permissions work. Record any remaining
filesystem or cache restrictions separately from local port binding.

## Development Rules

### Path sanitization

- Do not record real local or remote directory paths from a developer machine or environment in tests, fixtures, docs, screenshots, PR bodies, PR comments, review replies, or commit history.
- Replace environment-specific paths with fake placeholders such as `/workspace/user/project`, `/tmp/sample-project`, or `/remote/home/demo`.
- If a real path is used temporarily for investigation, remove it before committing and rewrite the branch history if that path already landed in a published commit on the PR branch.

### TDD

- Always write tests first, confirm they fail, then implement.
- Go changes must pass `make test-go` before moving on.
- Frontend changes must pass `make test-frontend` before moving on.

### Test granularity

Test at the smallest unit that exercises the logic, not at the outermost entry point. Before implementing or declaring a feature covered, enumerate the behavioral factors — input shapes, state, operations, boundaries, compatibility and migration, persistence and side effects, frontend runtime validation — and cover their meaningful combinations. Do not stop at one happy path plus one error path. Use table-driven tests when the factors form a matrix.

**Testability rule:** if production code calls `os.UserHomeDir()`, `DefaultPath()`, or another global singleton directly, add an injectable override so a test can substitute a controlled value. A struct field is the usual form — `Config.sshConfigPath` falls back to `sshconfig.DefaultPath()` only when the override is empty. Where the caller is a package-level function with nothing to hang a field on, there is a named seam package instead, and these three are the whole set:

- `internal/homedir` — call `homedir.Dir()`, substitute with `homedir.SetForTest(t, dir)` or `homedir.SetFailingForTest(t, err)`. Never `os.UserHomeDir()` (`.golangci.yml`'s `forbidigo` fails the build on it outside that package), and never `t.Setenv("HOME", ...)`.
- `internal/cachedir` — `cachedir.Dir()`, `cachedir.SetForTest`, `cachedir.SetFailingForTest`. Never `os.UserCacheDir()` (likewise enforced).
- `internal/fileops` — every file panemux persists is written through `fileops.AtomicWrite` (or `CreateTemp`/`OpenFile`/`Chmod`); substitute with `fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: err}).Ops())`. Nothing enforces this one.

None of the three is safe under `t.Parallel`: each is one unsynchronized package variable, so substitute them only from non-parallel tests.

[docs/development/test-granularity.md](docs/development/test-granularity.md) carries the full factor checklist, the known-anti-pattern table, why each seam is its own package, and the `AtomicWrite`-replaces-symlinks caveat.

### Schema-first

- For Go structure changes, update validation rules and tests in `internal/config/validate.go` first.
- For frontend type changes, update Zod schemas in `frontend/src/schemas/index.ts` first. Do not edit `frontend/src/types/index.ts` manually.
- API responses and WebSocket messages are runtime-validated against the schemas. When schemas change, update the corresponding tests too.

### Contract fixtures (`testdata/api-contract/`)

The Zod schemas are validated against JSON that Go actually emitted, not against hand-written TypeScript objects. The direction is one-way — Go → fixture → TypeScript — so:

- Run `make test-go` (or `make check`) after changing a Go response struct, before trusting a green frontend run.
- Commit the fixture diff alongside the struct change; it is the contract change a reviewer reads.
- Never hand-edit a file under `testdata/api-contract/` — the next Go test run overwrites it.

[docs/development/contract-fixtures.md](docs/development/contract-fixtures.md) has the rest, including what adding a response schema requires on both sides.

### Coverage

`make coverage-go` and `make coverage-frontend` each enforce at least 80% over a fixed package set; the `Makefile`'s `COVERAGE_PKGS` is the authority for the Go side. **The threshold stays at 80%; what gets strengthened is the scope.** A package added to the repository fails `TestCoverageScopeCoversEveryPackage` until it is either gated or excluded with a reason recorded in the `Makefile`.

`make coverage-blocks` re-reads that profile and lists every block the suite never entered. As a gate it is diff-scoped and pull-request-only (`COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`), and a block no test can reach is marked `//coverage:exempt <reason>` — the reason is required.

[docs/development/coverage.md](docs/development/coverage.md) has the gated package list, the exclusions, and why statement coverage alone cannot see an unentered branch.

### Mutation (`make mutation`)

`MUTATION_BASE=origin/main make mutation` asks the question the coverage gates cannot: **would the tests notice if your changed code behaved differently?** It **fails the build** on a surviving or undecided mutant on a line your branch changed, and when it could not run at all.

- Waive one survivor with `//mutation:exempt[<TYPE>] <reason>` on the mutated line or the line above. Both halves are required, and the type is copied from the finding's own third column. The `mutation-exempt` label is the blunter tool.
- Say which kind of survivor it is: **equivalent** (no input can distinguish it) or **unreachable** (killable, but not by input the callers can produce). Getting this wrong hides real defects.
- Needs gremlins, which `make install-deps` does not install: `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.

[docs/development/mutation.md](docs/development/mutation.md) has how each gremlins status is treated, why `SKIPPED` is not the author's problem, and why the runner pins its own flags.

### Model checking (`make model-check`, issue #168)

`internal/board`'s `ownSendLedger` has a TLA+ spec at `spec/agentboard/OwnSendLedger.tla`. Tier 1 (`internal/board/ledger_conformance_test.go`) replays the real implementation against the transition table TLC exported, is hermetic, and runs inside `make check`. Tier 2 (`make model-check`) runs TLC itself and sits outside `make check` because it needs a JDK and `tla2tools.jar`.

Never hand-edit `internal/board/testdata/*-transitions.json`. Change the `.tla`/`.cfg`, run `make model-check-write`, and commit the table diff alongside.

[docs/development/model-checking.md](docs/development/model-checking.md) has the two-tier rationale, the bound the `.cfg` sets, and the commands.

### Red-check (`make efficacy`)

A test you change must **fail** when your implementation diff is reverted — the machine-checkable half of the TDD rule above. CI runs it on every pull request; run it yourself with `make efficacy`. It is deliberately not part of `make check`.

A test that genuinely should not go red without its implementation is marked `//efficacy:exempt <reason>` in the comment directly above it. The reason is required; the `efficacy-exempt` label takes the whole branch out of scope.

[docs/development/red-check.md](docs/development/red-check.md) has what is skipped, and what neither marker is for.

### Quality gate

- `make check` must pass before `make build`.
- Before reporting implementation complete, `make check` must pass when run by hand, or the pull request's CI must pass. The pre-push hook no longer runs it (see [Push protection](#push-protection)).
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-verify`, `make test-go`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-pre-push`, `make test-tmpdir-guard`, `make test-install-hooks`, `make test-golangci-lint-cache`, `make test-require-pty`, `make test-efficacy`, `make test-scenarios-check`, `make test-docs-links`, `make test-screenshots-check`, `make test-coverage-blocks`, `make test-mutation`, `make test-model-check`
- Ledger command: `make check-scenarios`
- Documentation-link command: `make check-docs-links`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (all three fail the build — `make mutation` warned until #180's item 6 reached stage 4; see above)
- Model-checking commands (outside `make check`, they need a JDK and `tla2tools.jar`): `make model-check`, `make model-check-write`
- `make test-model-check` uses `python3` to run the transition exporter it tests. `python3` is **optional** for the same reason `jq` is below: without it those checks report themselves as skipped, so `make check` still works.
- `make test-hooks` uses `jq` where it parses `settings.json` or a hook payload. `jq` is **optional**: without it those checks report themselves as skipped rather than passing or failing, so `make check` still works. Install it to actually run them.
- Coverage commands: `make coverage-go`, `make coverage-frontend`, `make coverage-blocks`
- Measurement (not a gate): `make bench` for terminal throughput, replay-buffer cost and relay polling. It asserts no threshold — see [docs/quality-gateway/measurements.md](docs/quality-gateway/measurements.md).
- Accessibility ceiling: `make test-e2e` scans the dashboard and the pane settings dialog with axe-core and **fails when a violation count rises** above the value frozen in `CEILINGS` in `frontend/e2e/a11y-ceiling.ts`. A count may fall; a rule not listed has a ceiling of zero. After fixing a violation, lower the ceiling in that map and in the "Accessibility" table in [docs/quality-gateway/measurements.md](docs/quality-gateway/measurements.md) in the same change — the run prints the exact replacement line.
- Lint commands: `make lint-go`, `make lint-frontend`, `make lint`
- Go lint includes `gofmt`, `go vet`, and `golangci-lint run ./...` using `.golangci.yml`.
- `.golangci.yml`'s `forbidigo` rule is where a repository convention is enforced rather than remembered: it fails the build on any `os.UserHomeDir()` call outside `internal/homedir`. The testability rule above had been advice for long enough to accumulate 16 violations across nine packages before anyone counted them.
- `make lint-go` hands golangci-lint `.cache/golangci-lint/` inside the checkout it runs from as its cache (`scripts/golangci_lint_cache.sh`, Git-ignored). The main checkout and every worktree each keep their own, whatever the working directory, and an inherited `GOLANGCI_LINT_CACHE` is overridden — it may have been exported for another checkout. Existing caches in the user cache directory are left in place, unused; remove them yourself if you want the space back.
- `lint-go-deps` refreshes the pinned `golangci-lint` binary when the local version does not match `GOLANGCI_LINT_VERSION`, so local lint matches CI.
- Run `make lint-go` or `make lint` after every Go code change before committing.
- [docs/quality-gateway.md](docs/quality-gateway.md) explains what these gates are responsible for and which further gates are designed but not yet built. Read it before changing the gate set itself.

### Push protection

- `make install-deps` configures the repo-local Git hooks path to `.githooks`.
- The tracked `pre-push` hook runs only the checks the pushed change touches (`scripts/pre_push_check.sh`), and blocks `git push` when one fails. The whole suite is CI's job; run `make check` yourself to run all of it locally.
- The change is the difference between each pushed branch and the same branch on the remote. A branch the remote does not have yet is measured from its merge base with `origin/main`, so a first push checks what the branch adds and nothing `main` gained since. Deleting a branch checks nothing.
- What the changed files select:
  - `.go` files: `gofmt -s` on each, then `go vet`, `golangci-lint` (the pinned binary and this checkout's cache, as `make lint-go` uses) and `go test` on the packages that hold them — without `-race`, and not on the packages that import them. A file under a package's `testdata/` selects that package.
  - `frontend/src` TypeScript: `tsc --noEmit` and `vitest related` on the changed modules. A file under `testdata/api-contract/` selects the frontend contract test.
  - Shell scripts: `scripts/<name>.sh` selects `scripts/<name>_test.sh`, and any `.sh` selects the `$TMPDIR` guard. `.claude/` selects the hook tests; `frontend/screenshots/` and `frontend/e2e/*.sh` select the screenshot fixtures' tests.
  - Markdown selects `make check-docs-links`; `docs/scenarios.md` also selects `make check-scenarios`.
- It falls back to `make check` when the change cannot be narrowed — `go.mod`/`go.sum`, the `Makefile`, `.golangci.yml`, `.node-version`, the frontend's package and build configuration, the runtime selection scripts, `.githooks/` — and when the range cannot be worked out: a remote commit this clone does not have, no `origin/main`, or an unreadable pre-push line.
- `make test-pre-push` tests the selection, and CI runs it.
- The hook clears Git's repository-scoped hook environment variables first so nested test Git repositories behave the same way they do outside hook execution.
- Do not bypass the hook for ordinary development. Fix the failing checks instead.

### Documentation updates

- Use [docs/README.md](docs/README.md) as the information-architecture guide: product overview,
  concise current-state topic guides, and focused deep dives serve different reader needs.
- Every code change must update all related documentation in the same change. Before declaring the
  work complete, inspect the document map in [AGENTS.md](AGENTS.md) and update every current-state
  specification, topic guide, deep dive, operational guide, and developer rule whose contract or
  explanation the code changed. A code change is incomplete while any related document still
  describes the old behavior. If the change affects no documented contract, do not make a synthetic
  documentation edit, but still perform this inspection.
- When a behavior, operational assumption, browser requirement, rendering constraint, or user-visible rule becomes confirmed, update the relevant files in `docs/` in the same change.
- Do not leave documentation follow-up as a separate later task once the behavior is settled.
- Keep specifications and topic guides current-state only. Move chronology, rejected alternatives,
  rollout phases, incidents, and reasons for replacing an approach to
  [docs/DECISIONLOG.md](docs/DECISIONLOG.md); update both places in the same change when a decision
  changes the current contract.
- When a change adds or alters a user-facing use case, add or update its row in [docs/scenarios.md](docs/scenarios.md) in the same change, including the column naming where it is verified. `manual` is an acceptable answer there; an absent row is not.
- Two checks enforce this rather than leaving it to memory:
  - `make check-scenarios` (part of `make check`) resolves every path and Go test name an `auto` row names, and fails when one no longer exists. A row that names a renamed or deleted test reads as coverage and is worth nothing.
  - CI fails a pull request that changes `frontend/src`, `internal/api` or `internal/config` without touching `docs/scenarios.md`. Apply the `scenarios-exempt` label to a change that genuinely alters no use case.
- `make check-docs-links` (also part of `make check`) checks the documentation's own integrity: every relative link resolves, every `#fragment` matches a heading in the file it names, and no label names a file other than the one it opens. That last one is the rule to know when moving a section between files — rewriting the target and leaving a label that still says `security.md` while the link opens `security/auth.md` misdirects a reader as surely as a 404 does, and nothing about the rendered page looks wrong.
- When a document outgrows one file, split it into `docs/<name>/` and leave `docs/<name>.md` as the entry point: orientation, the rules that always apply, and a document map keyed by the section names the file used to carry.
- Put dated evidence such as coverage counts or benchmark results in a topic-specific measurements
  document, with its commit/date, rather than presenting the number as timeless specification.

### Security-sensitive implementation

- When code changes affect command execution, shell argument handling, SSH path handling, host key handling, or `gosec` posture, read [docs/security.md](docs/security.md) before implementation and follow it during the change.
- Prefer structural fixes over `//nolint:gosec` in shipped code.

### Ignore generated resources

- When adding generated artifacts, caches, release outputs, or other non-source resources, update `.gitignore` in the same change.
- Do not leave new build or release byproducts such as `dist/` as recurring untracked files.

## Branch And PR Workflow

### Branch workflow

- Never commit directly to `main`.
- Create a separate worktree and feature branch before implementation:

```sh
git worktree add /tmp/<repo>-<feature> -b feature/<name>
```

- Use `/tmp/<repo>-<feature>` as the default location so the workflow matches the repository's agent instructions and avoids editing in the main working directory by accident.

- Do all editing, testing, and committing inside the feature worktree.
- Push and open a PR before merging.
- Merge with `--squash --delete-branch`.
- Remove the feature worktree after merge:

```sh
git worktree remove /tmp/<repo>-<feature>
```

### Pull request title

- PR titles must follow Conventional Commits format: `<type>: <description>`.
- Allowed types are defined in `.github/workflows/pr-title.yml`: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `build`, and `ci`.
- Use `.github/labeler.yml` as the guide when the change maps cleanly to one label.

### Pull request test plan

- After creating a PR, run every item in the test plan locally and verify it passes.
- Update the PR description with all checkboxes checked before considering the task complete.
- Do not leave test plan items unchecked.

## Related Documents

Long-form testing references split out of this guide live in [`docs/development/`](docs/development/):

- Test granularity and the testability seams: [docs/development/test-granularity.md](docs/development/test-granularity.md)
- Contract fixtures: [docs/development/contract-fixtures.md](docs/development/contract-fixtures.md)
- Coverage gates: [docs/development/coverage.md](docs/development/coverage.md)
- Mutation testing: [docs/development/mutation.md](docs/development/mutation.md)
- Model checking: [docs/development/model-checking.md](docs/development/model-checking.md)
- Red-check: [docs/development/red-check.md](docs/development/red-check.md)
- Codex verification: [docs/development/codex-verification.md](docs/development/codex-verification.md)

Enduring product and design documents:

- Documentation index and reader routes: [docs/README.md](docs/README.md)
- Product overview: [docs/overview.md](docs/overview.md)
- Architecture and security design: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md)
- UI intent: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Test quality characteristics and the gate design: [docs/quality-gateway.md](docs/quality-gateway.md)
- Design history and rejected alternatives: [docs/DECISIONLOG.md](docs/DECISIONLOG.md)
