# Development Guide

Daily setup, commands, and mandatory development rules. Details live in the
[development index](docs/development/index.md).

## Runtime version contract

Use the exact Go and Node versions pinned in `go.mod` and `.node-version`. Run ordinary
`make` commands without overriding `GOTOOLCHAIN`, `PATH`, or `NODE_OPTIONS` for runtime selection.
Install a Go launcher supporting toolchain selection (Go 1.21+); use `make install-deps`
or `scripts/npm.sh` for dependency setup. See the [runtime contract](docs/development/runtime.md)
for SDK bootstrap prerequisites, supported platforms, offline use, and failure recovery.

## Build And Run

```sh
make install-deps      # first-time setup: npm install + go mod download
make install-deps-ci   # CI / reproducible dependency install
make build             # full build; frontend/dist is embedded into the Go binary
make release-snapshot  # local release archives via GoReleaser
```

```sh
./bin/panemux
./bin/panemux --config config.yaml
./bin/panemux --port 9090 --open
```

```sh
make dev-frontend   # Vite dev server on :5173
make dev-backend    # run backend separately while the frontend proxies /api and /ws
```

Markdown diagrams use a separate bundled classic script. `make build-frontend` builds both the
SPA and that runtime into `frontend/dist`; rerun it after changing the preview entry or its
libraries, including during Vite development. Vite proxies `/preview/` and `/preview-assets/`
to the backend. The backend must be rebuilt/restarted to embed the updated runtime.

### Documentation screenshots

`make screenshots` regenerates `docs/images/*.png`. Any change to what these images show must
retake them in the same change: inspect every rewritten image and commit it. This includes
UI components, layout, styles, text, and the capture itself; the rule also covers hooks,
schemas, or utilities that change the screen.

CI checks `frontend/src/components/`, `frontend/src/App.tsx`, `frontend/src/styles/`,
`frontend/index.html`, and `frontend/screenshots/` (excluding `*.test.ts`, `*.test.tsx`,
`*_test.sh`; moved files count under their old paths). It requires a direct `docs/images/*.png`
output change; non-image files do not count. Use `screenshots-exempt` only when a change
alters nothing shown. See [Documentation screenshots](docs/development/screenshots.md) for
placeholder capture, staging recovery, prerequisites, and cleanup, and
[Browser fixture isolation](docs/development/browser-fixtures.md) for E2E isolation.

### Format and checks

```sh
make fmt
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

See [Claude Code sandbox](docs/development/claude-sandbox.md) for shared local-network
permissions, platform and managed-policy differences, skipped capability tests, and required
verification after settings changes. CI verifies capabilities unavailable inside the sandbox.

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

Test at the smallest unit that exercises the logic. Enumerate behavioral factors and cover
their meaningful combinations; use tables for matrices. Add injectable overrides for global
side effects. Use `internal/homedir`, `internal/cachedir`, and `internal/fileops` for their
respective boundaries, only from non-parallel tests. Follow the full
[test granularity and testability rules](docs/development/test-granularity.md).

### Schema-first

- For Go structure changes, update validation rules and tests in `internal/config/validate.go` first.
- For frontend type changes, update Zod schemas in `frontend/src/schemas/index.ts` first. Do not edit `frontend/src/types/index.ts` manually.
- API responses and WebSocket messages are runtime-validated against the schemas. When schemas change, update the corresponding tests too.

### Contract fixtures (`testdata/api-contract/`)

Run `make test-go` (or `make check`) after changing a Go response struct before trusting
frontend results: fixtures flow Go → JSON → TypeScript. Commit fixture changes with the struct;
never hand-edit `testdata/api-contract/`. See [Contract fixtures](docs/development/contract-fixtures.md).

### Coverage

`make coverage-go` and `make coverage-frontend` enforce 80%; strengthen scope rather than
raising the threshold. New Go packages must be gated or excluded with a reason in the Makefile.
`COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks` gates unentered changed blocks;
unreachable blocks require `//coverage:exempt <reason>`. See [Coverage gates](docs/development/coverage.md).

### Mutation (`make mutation`)

`MUTATION_BASE=origin/main make mutation` fails on surviving or undecided mutants on changed
lines, or inability to run. A waiver needs `//mutation:exempt[<TYPE>] <reason>` on the line or
above it, using the finding's type; distinguish equivalent from unreachable survivors. Prefer
that marker to the branch-wide `mutation-exempt` label. Install gremlins separately:
`go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`.
See [Mutation testing](docs/development/mutation.md) for status handling and pinned flags.

### Model checking (`make model-check`, issue #168)

Transition conformance runs in `make check`; TLC itself runs separately with `make model-check`
and needs a JDK and `tla2tools.jar`. Never hand-edit `internal/board/testdata/*-transitions.json`:
change `.tla`/`.cfg`, run `make model-check-write`, and commit the table diff. See
[Model checking](docs/development/model-checking.md).

### Red-check (`make efficacy`)

A test you change must **fail** when your implementation diff is reverted — the machine-checkable half of the TDD rule above. CI runs it on every pull request; run it yourself with `make efficacy`. It is deliberately not part of `make check`.

A test that genuinely should not go red without its implementation is marked `//efficacy:exempt <reason>` in the comment directly above it. The reason is required; the `efficacy-exempt` label takes the whole branch out of scope.

[docs/development/red-check.md](docs/development/red-check.md) has what is skipped, and what neither marker is for.

### Quality gate

- `make check` must pass before `make build`.
- Before reporting implementation complete, `make check` must pass when run by hand, or the pull request's CI must pass. The pre-push hook does not run the full suite unconditionally (see [Push protection](#push-protection)).
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-verify`, `make test-go`, `make test-go-fixture-agent`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-pre-push`, `make test-tmpdir-guard`, `make test-install-hooks`, `make test-golangci-lint-cache`, `make test-require-pty`, `make test-efficacy`, `make test-scenarios-check`, `make test-docs-links`, `make test-screenshots-check`, `make test-coverage-blocks`, `make test-mutation`, `make test-model-check`
- Ledger command: `make check-scenarios`
- Documentation commands: `make check-docs-links`, `make test-docs-links`, `make check-docs-okf`, `make test-docs-okf`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (all three fail the build)
- `make test-model-check` uses `python3` to run the transition exporter it tests. `python3` is **optional** for the same reason `jq` is below: without it those checks report themselves as skipped, so `make check` still works.
- `make test-agmsg-automation` uses Python 3 for offline release/SHA, capture-update and PR-delivery tests. It skips locally without Python and fails if Python is missing in CI. Real upstream installation stays in the separate contract workflow.
- `make test-hooks` uses `jq` where it parses `settings.json` or a hook payload. `jq` is **optional**: without it those checks report themselves as skipped rather than passing or failing, so `make check` still works. Install it to actually run them.
- Measurement (not a gate): `make bench` for terminal throughput, replay-buffer cost and relay polling. It asserts no threshold — see [docs/quality-gateway/measurements.md](docs/quality-gateway/measurements.md).
- Accessibility ceiling: `make test-e2e` scans the dashboard and the pane settings dialog with axe-core and **fails when a violation count rises** above the value frozen in `CEILINGS` in `frontend/e2e/a11y-ceiling.ts`. A count may fall; a rule not listed has a ceiling of zero. After fixing a violation, lower the ceiling in that map and in the "Accessibility" table in [docs/quality-gateway/measurements.md](docs/quality-gateway/measurements.md) in the same change — the run prints the exact replacement line.
- Lint commands: `make lint-go`, `make lint-frontend`, `make lint`
- Go lint includes `gofmt`, `go vet`, and `golangci-lint run ./...` using `.golangci.yml`.
- The fixture agent's files build only with the `panemux_fixture_agent` tag (issue #377). `make test-go`
  also runs `make test-go-fixture-agent`, which tests that build; `make lint-go` vets both builds, and
  `.golangci.yml`'s `build-tags` makes golangci-lint read the tagged files. The coverage, red-check and
  mutation gates and the Claude Code `PostToolUse` hook see the untagged build only
  ([the fixture agent's tagged build](docs/quality-gateway.md#the-fixture-agents-tagged-build)).
- `.golangci.yml`'s `forbidigo` rule is where a repository convention is enforced rather than remembered: it fails the build on any `os.UserHomeDir()` call outside `internal/homedir`.
- `make lint-go` hands golangci-lint `.cache/golangci-lint/` inside the checkout it runs from as its cache (`scripts/golangci_lint_cache.sh`, Git-ignored). The main checkout and every worktree each keep their own, whatever the working directory, and an inherited `GOLANGCI_LINT_CACHE` is overridden — it may have been exported for another checkout. Existing caches in the user cache directory are left in place, unused; remove them yourself if you want the space back.
- `lint-go-deps` refreshes the pinned `golangci-lint` binary when the local version does not match `GOLANGCI_LINT_VERSION`, so local lint matches CI.
- Run `make lint-go` or `make lint` after every Go code change before committing.
- [docs/quality-gateway.md](docs/quality-gateway.md) explains what these gates are responsible for and which further gates are designed but not yet built. Read it before changing the gate set itself.

### Push protection

- `make install-deps` configures the repo-local Git hooks path to `.githooks`.
- The tracked `pre-push` hook runs only the checks the pushed change touches (`scripts/pre_push_check.sh`), and blocks `git push` when one fails. The whole suite is CI's job; run `make check` yourself to run all of it locally.
- See [Pre-push check selection](docs/development/push-protection.md) for diff bases, selected checks, and full-suite fallback.
- Do not bypass the hook for ordinary development. Fix the failing checks instead.

### Documentation updates

- Use [docs/index.md](docs/index.md) as the information-architecture guide: product overview,
  concise current-state topic guides, and focused deep dives serve different reader needs.
- Every code change must update all related documentation in the same change. Inspect the
  [AGENTS.md document map](AGENTS.md#document-map), including current-state specifications,
  guides, deep dives, operational guidance, and developer rules. Do not report completion while
  a related document describes old behavior. If no documented contract changes, inspect without
  making a synthetic edit.
- Update confirmed behavior, operational assumptions, browser requirements, rendering constraints,
  and user-visible rules in `docs/` immediately; do not defer settled documentation to a later task.
- Keep specifications and topic guides current-state only. Move chronology, rejected alternatives,
  rollout phases, incidents, and reasons for replacing an approach to
  [docs/DECISIONLOG.md](docs/DECISIONLOG.md); update both places in the same change when a decision
  changes the current contract.
- When a change adds or alters a user-facing use case, add or update its row in [docs/scenarios.md](docs/scenarios.md) in the same change, including the column naming where it is verified. `manual` is an acceptable answer there; an absent row is not.
- Run `make check-scenarios` to validate paths and Go test names in `auto` rows.
  CI requires scenario edits for changes to `frontend/src`, `internal/api`, or `internal/config`;
  use `scenarios-exempt` only if no use case changes.
- Run `make check-docs-links` for relative paths, heading anchors, and filename labels;
  update targets and filename labels together when moving content.
- Follow [the OKF writing rules](docs/index.md#writing-rules). Treat `docs/` as one OKF v0.2
  bundle, with `type`, `title`, and `description` frontmatter on concepts and a described
  `index.md` listing in each directory. Only the bundle-root index carries `okf_version: "0.2"`.
  Root workflow documents stay outside the bundle. `make check-docs-okf` and `make test-docs-okf`
  run through `make check` without replacing or weakening the existing link checks.
- Split by one responsibility per concept and keep each contract authoritative in one concept;
  link from other documents. The directory hierarchy is a tree and document links are graph
  edges. Keep `docs/<name>.md` as the topic entrance and `docs/<name>/index.md` as its directory
  listing. Preserve routes from old section names, and update references and expected paths
  that actually name moved documents, including comments and tests; leave unrelated fixtures alone.
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

Use the [development index](docs/development/index.md) for runtime selection, sandbox setup,
screenshots, browser fixtures, pre-push selection, Codex verification, and focused testing contracts.
The [documentation index](docs/index.md) routes to product, architecture, behavior, security,
UI, maintenance, scenarios, quality-gateway, and decision documents.
