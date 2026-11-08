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

See [Documentation screenshots](docs/development/screenshots.md) for CI scope, placeholder
capture, staging recovery, and cleanup; [Browser fixture isolation](docs/development/browser-fixtures.md)
covers E2E isolation.

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

Run `make coverage-go` and `make coverage-frontend` (80%), and the PR-only
`COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`. Follow [Coverage gates](docs/development/coverage.md)
for scope, justified exclusions, and required exemption reasons.

### Mutation (`make mutation`)

Run `MUTATION_BASE=origin/main make mutation` for PRs. Follow [Mutation testing](docs/development/mutation.md)
for gremlins setup, failing outcomes, and typed, justified exemptions.

### Model checking (`make model-check`, issue #168)

Never hand-edit transition JSON: update .tla/.cfg, run `make model-check-write`, and commit the diff.
[Model checking](docs/development/model-checking.md) separates always-on conformance from optional TLC.

### Red-check (`make efficacy`)

Changed tests must fail without their implementation diff. Run the PR-only `make efficacy`;
follow [Red-check](docs/development/red-check.md) for justified exemptions and scope.

### Quality gate

- `make check` must pass before `make build`.
- Before reporting implementation complete, `make check` must pass when run by hand, or the pull request's CI must pass. The pre-push hook does not run the full suite unconditionally (see [Push protection](#push-protection)).
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-verify`, `make test-go`, `make test-go-fixture-agent`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-pre-push`, `make test-tmpdir-guard`, `make test-install-hooks`, `make test-golangci-lint-cache`, `make test-require-pty`, `make test-efficacy`, `make test-scenarios-check`, `make test-docs-links`, `make test-screenshots-check`, `make test-coverage-blocks`, `make test-mutation`, `make test-model-check`
- Ledger command: `make check-scenarios`
- Documentation commands: `make check-docs-links`, `make test-docs-links`, `make check-docs-okf`, `make test-docs-okf`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (all three fail the build)
- Optional test tools: `python3` for model-check exporter and agmsg automation tests, `jq` for
  hook tests. Missing tools skip those checks locally; missing Python fails agmsg automation in CI.
- `make bench` measures without thresholds; see [measurements](docs/quality-gateway/measurements.md).
- Follow the [accessibility ceiling procedure](docs/quality-gateway/measurements.md#accessibility)
  after fixes; counts must not rise.
- `make lint-go`, `make lint-frontend`, and `make lint` run lint checks. Go lint includes
  gofmt, vet, and the pinned golangci-lint; its cache is checkout-local `.cache/golangci-lint/`
  (inherited cache settings are overridden; user caches are left untouched).
- Tagged fixture-agent verification follows [the build contract](docs/quality-gateway.md#the-fixture-agents-tagged-build).
- Run `make lint-go` or `make lint` after every Go code change before committing.
- [docs/quality-gateway.md](docs/quality-gateway.md) explains what these gates are responsible for and which further gates are designed but not yet built. Read it before changing the gate set itself.

### Push protection

- `make install-deps` configures the repo-local Git hooks path to `.githooks`.
- The tracked `pre-push` hook runs only the checks the pushed change touches (`scripts/pre_push_check.sh`), and blocks `git push` when one fails. The whole suite is CI's job; run `make check` yourself to run all of it locally.
- See [Pre-push check selection](docs/development/push-protection.md) for diff bases, selected checks, and full-suite fallback.
- Do not bypass the hook for ordinary development. Fix the failing checks instead.

### Documentation updates

- Inspect the [AGENTS.md document map](AGENTS.md#document-map) for every code change and update
  all affected specifications, guides, operations, and developer rules in the same change.
  Do not report completion with stale related docs or defer settled behavior to a follow-up.
  If no documented contract changes, inspect without making a synthetic edit.
- Record confirmed behavior, operational assumptions, browser/rendering requirements, and
  user-visible rules in docs/. Keep current-state docs free of chronology; put consequential
  history, rejected alternatives, incidents, and dated decisions in [DECISIONLOG.md](docs/DECISIONLOG.md).
- Add or update [scenario rows](docs/scenarios.md) for changed use cases, naming verification;
  manual verification is valid, an absent row is not. Run `make check-scenarios`.
  CI requires scenario edits for frontend/src, internal/api, and internal/config changes;
  use `scenarios-exempt` only when no use case changes.
- Follow [the OKF writing rules](docs/index.md#writing-rules): docs/ is the bundle; concepts have
  type/title/description and directory indexes have described entries. Root workflow files stay
  outside it. Keep one authority per contract and one responsibility per concept, with relative
  links and routes from old sections. Update actual references, filename labels, comments, and
  expected paths when moving content; leave unrelated fixtures alone.
- Run `make check-docs-links`, `make check-docs-okf`, and their test targets; these remain in
  `make check`. Keep topic entrances distinct from directory listings, and dated measurements
  beside their topic with commit/date, as specified by the writing rules.

### Security-sensitive implementation

- When code changes affect command execution, shell argument handling, SSH path handling, host key handling, or `gosec` posture, read [docs/security.md](docs/security.md) before implementation and follow it during the change.
- Prefer structural fixes over `//nolint:gosec` in shipped code.

### Temporary-file discipline

Every script must use an explicit `mktemp` template under TMPDIR and stop on allocation failure;
handle `x=$(mktemp ...)` failure on the same line. Bare macOS `mktemp -d` ignores TMPDIR.
`make test-tmpdir-guard` checks the first call, and the second where multiple call sites exist
(including a fixture helper's first call), leave the caller repository untouched; it also rejects
missing templates and missing assignment failure handling.

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
