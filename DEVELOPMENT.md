# Development Guide

This document is the developer workflow reference for building, testing, changing, and shipping this repository.

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

**Testability rule:** if production code would reach a global singleton directly, add an injectable override instead. Three seams exist and are the only supported way in:

- `internal/homedir` — `homedir.Dir()`, never `os.UserHomeDir()` (enforced by `.golangci.yml`'s `forbidigo`), and never `t.Setenv("HOME", ...)`.
- `internal/cachedir` — `cachedir.Dir()`, never `os.UserCacheDir()` (likewise enforced).
- `internal/fileops` — every file panemux persists is written through `fileops.AtomicWrite` (or `CreateTemp`/`OpenFile`/`Chmod`). Nothing enforces this one.

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
- `make check` must pass before reporting implementation complete.
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-go`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-efficacy`, `make test-scenarios-check`, `make test-coverage-blocks`, `make test-mutation`, `make test-model-check`
- Ledger command: `make check-scenarios`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (all three fail the build — `make mutation` warned until #180's item 6 reached stage 4; see above)
- Model-checking commands (outside `make check`, they need a JDK and `tla2tools.jar`): `make model-check`, `make model-check-write`
- `make test-model-check` uses `python3` to run the transition exporter it tests. `python3` is **optional** for the same reason `jq` is below: without it those checks report themselves as skipped, so `make check` still works.
- `make test-hooks` uses `jq` where it parses `settings.json` or a hook payload. `jq` is **optional**: without it those checks report themselves as skipped rather than passing or failing, so `make check` — and therefore `git push` — still works. Install it to actually run them.
- Coverage commands: `make coverage-go`, `make coverage-frontend`, `make coverage-blocks`
- Measurement (not a gate): `make bench` for terminal throughput, replay-buffer cost and relay polling. It asserts no threshold — see [docs/quality-gateway.md](docs/quality-gateway.md)'s "First measurements".
- Accessibility ceiling: `make test-e2e` scans the dashboard and the pane settings dialog with axe-core and **fails when a violation count rises** above the value frozen in `CEILINGS` in `frontend/e2e/a11y-ceiling.ts`. A count may fall; a rule not listed has a ceiling of zero. After fixing a violation, lower the ceiling in that map and in the "Accessibility" table in [docs/quality-gateway.md](docs/quality-gateway.md) in the same change — the run prints the exact replacement line.
- Lint commands: `make lint-go`, `make lint-frontend`, `make lint`
- Go lint includes `gofmt`, `go vet`, and `golangci-lint run ./...` using `.golangci.yml`.
- `.golangci.yml`'s `forbidigo` rule is where a repository convention is enforced rather than remembered: it fails the build on any `os.UserHomeDir()` call outside `internal/homedir`. The testability rule above had been advice for long enough to accumulate 16 violations across nine packages before anyone counted them.
- `lint-go-deps` refreshes the pinned `golangci-lint` binary when the local version does not match `GOLANGCI_LINT_VERSION`, so local lint matches CI.
- Run `make lint-go` or `make lint` after every Go code change before committing.
- [docs/quality-gateway.md](docs/quality-gateway.md) explains what these gates are responsible for and which further gates are designed but not yet built. Read it before changing the gate set itself.

### Push protection

- `make install-deps` configures the repo-local Git hooks path to `.githooks`.
- The tracked `pre-push` hook runs `make check` and blocks `git push` when it fails.
- The hook clears Git's repository-scoped hook environment variables before running `make check` so nested test Git repositories behave the same way they do outside hook execution.
- Do not bypass the hook for ordinary development. Fix the failing checks instead.

### Documentation updates

- When a behavior, operational assumption, browser requirement, rendering constraint, or user-visible rule becomes confirmed, update the relevant files in `docs/` in the same change.
- Do not leave documentation follow-up as a separate later task once the behavior is settled.
- When a change adds or alters a user-facing use case, add or update its row in [docs/scenarios.md](docs/scenarios.md) in the same change, including the column naming where it is verified. `manual` is an acceptable answer there; an absent row is not.
- Two checks enforce this rather than leaving it to memory:
  - `make check-scenarios` (part of `make check`) resolves every path and Go test name an `auto` row names, and fails when one no longer exists. A row that names a renamed or deleted test reads as coverage and is worth nothing.
  - CI fails a pull request that changes `frontend/src`, `internal/api` or `internal/config` without touching `docs/scenarios.md`. Apply the `scenarios-exempt` label to a change that genuinely alters no use case.

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

Enduring product and design documents:

- Product overview: [docs/overview.md](docs/overview.md)
- Architecture and security design: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md)
- UI intent: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Test quality characteristics and the gate design: [docs/quality-gateway.md](docs/quality-gateway.md)
