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

Test at the smallest unit that exercises the logic, not at the outermost entry point.

Before implementing or declaring a feature covered, enumerate the behavioral factors and cover their meaningful combinations in tests. Do not stop at one happy path plus one error path.

For every new feature or behavior change, explicitly consider:

- Input shape variants: legacy and new schemas, omitted optional fields, defaulted fields, invalid enum values, empty lists, duplicate IDs, unknown references, malformed request bodies.
- State variants: active vs inactive items, empty vs populated state, existing vs missing resources, persisted config vs memory-only config, and visible vs dismissed transient UI errors when user actions can fail after optimistic updates.
- Operation variants: read, create, update, delete, switch, save, reload, restart, and no-op cases where applicable.
- Boundary variants: minimum/maximum values, zero/negative values, size sums, single item vs multiple items, nested structures, and paths with `~/` when paths are involved.
- Compatibility and migration: old config/API shape, new config/API shape, precedence when both exist, migration-on-save behavior, and post-reload behavior after migration.
- Persistence and side effects: in-memory updates, disk writes, unchanged unrelated siblings/items, session manager changes, restart behavior, and API response status/body.
- Frontend runtime validation: Zod acceptance/rejection, API fallback behavior, active UI state, invalid selection no-ops, and all visible mode/position variants.

Use table-driven tests when factors form a matrix. For large matrices, test all high-risk cross-products and pairwise combinations for the rest, but document the factors through test names and fixtures so omissions are intentional and reviewable.

Known anti-patterns:

| Anti-pattern | Problem | Correct approach |
|---|---|---|
| Testing a function that makes real network calls to verify config-resolution logic | Network errors hide config errors; any error is accepted as expected | Extract pure config-resolution into a separate function such as `resolveSSHConfig` and test it directly, asserting exact return values |
| `if err != nil { assert.NotContains(t, err.Error(), "not found") }` | Accepts any error except `not found` and masks setup errors | Assert the specific happy path with `require.NoError(t, err)` and verify returned values. For expected-failure paths, assert the exact error string |
| Test data that never exercises the real-world variant | Bugs in real user inputs stay invisible | Add test cases that cover exact user formats such as `~/`-prefixed paths and omitted optional fields |
| Testing only the error case of a new validation rule, not the passing case | The positive path is never confirmed to work | For every new validation rule, write both a failure test and a success test |

**Testability rule:** if production code calls `os.UserHomeDir()`, `DefaultPath()`, or another global singleton directly, add an injectable override so tests can substitute a controlled value without touching the real filesystem or home directory.

Example: `Config.sshConfigPath` uses `sshconfig.DefaultPath()` only when the override is empty.

### Schema-first

- For Go structure changes, update validation rules and tests in `internal/config/validate.go` first.
- For frontend type changes, update Zod schemas in `frontend/src/schemas/index.ts` first. Do not edit `frontend/src/types/index.ts` manually.
- API responses and WebSocket messages are runtime-validated against the schemas. When schemas change, update the corresponding tests too.

### Coverage

- `make coverage-go` enforces at least 80% combined coverage across `internal/config`, `internal/api`, `internal/ws`, `internal/server`, `internal/board`, `internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, and the root package.
- `make coverage-frontend` enforces at least 80% coverage across `frontend/src/hooks/`, `frontend/src/schemas/`, and `frontend/src/utils/`.
- **The threshold stays at 80%; what gets strengthened is the scope.** Raising it works, but the cheapest way to satisfy a higher number is to generate tautological tests, which lowers both protection against regressions and resistance to refactoring. See decision D1 in [docs/quality-gateway.md](docs/quality-gateway.md).
- The gated package set is checked against `go list ./...` by `TestCoverageScopeCoversEveryPackage`, so a package added to the repository fails the suite until it is either gated or explicitly excluded with a reason. Do not widen the exclusion list to make that failure go away.
- `make coverage-go` builds the frontend first: the root package is gated and `main.go` embeds `frontend/dist`.
- What is excluded, and why, is written in the `Makefile` next to `COVERAGE_PKGS` — `internal/session`'s real PTY / SSH / tmux transports, and the process-lifetime entry points (`main`, `runServer`, `bootstrapWatcher.Run`). Do not add an exclusion without a reason recorded there.

### Per-block coverage (`make coverage-blocks`)

- The 80% threshold above is a *statement* percentage: it cannot see an entire `if err != nil { ... }` body that no test enters, because the happy path around it carries the function past 80%. Issue [#164](https://github.com/tomo-chan/panemux/issues/164) found 28 such branches by hand.
- `make coverage-blocks` re-reads `make coverage-go`'s profile and lists every block the suite never entered; `make coverage-go` prints the count as a one-line summary.
- As a gate it is **scoped to the diff** and pull-request-only — `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks` fails only on a block covering a line your branch changed. Deliberately not in `make check`: it needs the base branch. Why diff-scoped rather than repository-wide: decision D8 in [docs/quality-gateway.md](docs/quality-gateway.md).
- A block that cannot be reached from a test is marked `//coverage:exempt <reason>` on its opening line or the line above. **The reason is required.** The `coverage-blocks-exempt` label exempts the whole branch and is the blunter tool; prefer the marker, which sits in the diff a reviewer reads.
- A changed file in a package `COVERAGE_PKGS` excludes is reported as **not measured**, not as covered.

### Mutation (`make mutation`)

- Asks the question the other three G4 checks cannot: **would the tests notice if your changed code behaved differently?** A block can execute on every run and still have nothing asserted about it.
- `MUTATION_BASE=origin/main make mutation` runs [gremlins](https://github.com/go-gremlins/gremlins) scoped to the diff and names every mutant on a line your branch changed that survives the whole suite.
- **It warns; it does not fail.** A survivor prints and exits 0. Roughly a third of this repository's survivors are ones nobody should act on — buffer sizes and timeout constants whose killing test would be a tautology itself — so failing on them would make the check wrong more often than right. Decision D9 in [docs/quality-gateway.md](docs/quality-gateway.md) records the measurement behind that. It *does* exit 1 when it could not run.
- Needs gremlins, which `make install-deps` does not install: `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`. `make test-mutation` — the checker's own tests, inside `make check` — never needs it, because it drives the script through `--report` against fixture reports.
- A survivor worth keeping is marked `//mutation:exempt <reason>` on the mutated line or the line above. **The reason is required.** The `mutation-exempt` label exempts the whole branch and is the blunter tool.
- Do not pass gremlins' own defaults: `scripts/mutation.sh` pins `--timeout-coefficient` and `--workers` because the defaults report 44% of runnable mutants as timed out on this repository, and those timeouts hide survivors.

### Red-check (`make efficacy`)

- A test you change must **fail** when your implementation diff is reverted. That is the machine-checkable half of the TDD rule above: the order lines were written in cannot be recovered after the fact, but the result can.
- Run it yourself with `make efficacy` (compares against `origin/main`; override with `EFFICACY_BASE`). CI runs it on every pull request.
- It is deliberately **not** part of `make check` — it needs the base branch and a second test run, which is a per-pull-request cost, not a per-turn one.
- A branch that changes no implementation, or changes implementation but no test, is skipped rather than failed.
- If a change genuinely should not go red without its implementation — a pure refactor, a test-only rename, a test pinning behavior this branch never touched — mark that test `//efficacy:exempt <reason>` in the comment directly above it. **The reason is required**, and the marker is the one to reach for first: it is per-test and it sits in the diff a reviewer reads. The `efficacy-exempt` label is the blunter tool and takes the whole branch out of scope.
- Neither is for getting past a test that turned out not to assert anything. A branch where most changed tests survive is worth a second look before it is exempted: on [#190](https://github.com/tomo-chan/panemux/issues/190) 15 of 18 survived and every one was legitimate, because the whole branch existed to pin already-correct behavior — that is the rare shape, not the usual one.
- See decision D4 in [docs/quality-gateway.md](docs/quality-gateway.md).

### Quality gate

- `make check` must pass before `make build`.
- `make check` must pass before reporting implementation complete.
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-go`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-efficacy`, `make test-scenarios-check`, `make test-coverage-blocks`, `make test-mutation`
- Ledger command: `make check-scenarios`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (a warning, not a gate — see above)
- `make test-hooks` uses `jq` where it parses `settings.json` or a hook payload. `jq` is **optional**: without it those checks report themselves as skipped rather than passing or failing, so `make check` — and therefore `git push` — still works. Install it to actually run them.
- Coverage commands: `make coverage-go`, `make coverage-frontend`, `make coverage-blocks`
- Measurement (not a gate): `make bench` for terminal throughput, replay-buffer cost and relay polling; `make test-e2e` also records accessibility violations. Neither asserts a threshold — see [docs/quality-gateway.md](docs/quality-gateway.md)'s "First measurements".
- Lint commands: `make lint-go`, `make lint-frontend`, `make lint`
- Go lint includes `gofmt`, `go vet`, and `golangci-lint run ./...` using `.golangci.yml`.
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

- Product overview: [docs/overview.md](docs/overview.md)
- Architecture and security design: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md)
- UI intent: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Test quality characteristics and the gate design: [docs/quality-gateway.md](docs/quality-gateway.md)
