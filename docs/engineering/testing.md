---
type: Specification
title: "Testing and formal verification"
description: "Test granularity, injectable filesystem seams, API contract fixtures, browser fixture isolation, state-machine model checking, and Agent Board verification contracts."
---

# Testing and formal verification

Daily test commands live in [DEVELOPMENT.md](../../DEVELOPMENT.md) and gate definitions live in
[Quality gates](quality-gates.md).

## Test granularity and testability seams

Test at the smallest unit that exercises the logic rather than at the outermost entry point. Before
declaring a feature covered, enumerate its behavioral factors and test meaningful combinations
(using table-driven matrices for cross-products and pairwise coverage for large spaces):

- **Input shapes**: legacy vs. new schemas, omitted/defaulted fields, invalid enums, empty lists,
  duplicate IDs, unknown references, malformed payloads.
- **State and operation variants**: active/inactive, empty/populated, existing/missing resources,
  persisted vs. memory-only config, transient UI errors, CRUD/reload/restart/no-op paths.
- **Boundaries and compatibility**: min/max/zero/negative values, size sums, nested structures,
  `~/` paths, old/new precedence, migration-on-save and post-reload behavior.
- **Persistence, side effects, and frontend validation**: atomic disk writes, untouched siblings,
  API status/body, Zod acceptance/rejection, fallback behavior, and visible mode/position variants.

| Anti-pattern | Correct approach |
|---|---|
| Making real network calls to test config resolution | Extract pure functions (e.g., `resolveSSHConfig`) and assert exact return values |
| Negative error checks (`NotContains(..., "not found")`) | Assert `require.NoError` on happy paths and exact error strings on expected failures |
| Toy test data omitting real formats | Cover exact user formats such as `~/` paths and omitted optional fields |
| Testing only rejection for a validation rule | Always test both valid acceptance and invalid rejection |

### Injectable filesystem seams

Production code must never call `os.UserHomeDir()` or `os.UserCacheDir()` directly outside their
seam packages (enforced by `.golangci.yml`'s `forbidigo` rules; do not use `t.Setenv("HOME", ...)`).
All three seams use unsynchronized package variables and **must only be substituted from
non-parallel tests** ([filesystem seams decision](../decisions/architecture.md#filesystem-globals-use-named-seams-2026-09-102026-09-13-prs-224-231-234)):

- **`internal/homedir`**: `homedir.Dir()`, overridden with `homedir.SetForTest` / `SetFailingForTest`.
- **`internal/cachedir`**: `cachedir.Dir()`, overridden with `cachedir.SetForTest` / `SetFailingForTest`
  (kept separate because `os.UserCacheDir` has distinct macOS/Unix semantics).
- **`internal/fileops`**: `fileops.AtomicWrite` for persisted files (`config.yaml`, auth token,
  relay cursor, command-center session ID, task records/summaries) and `CreateTemp`/`OpenFile`/`Chmod`
  where atomic rename is not used (MCP config, append-only history). Override with
  `fileops.SetOpsForTest(t, (&fileops.Spy{...}).Ops())` to inject post-creation `Write`/`Close`/`Chmod`
  failures and assert cleanup via `spy.Files()`. Resolve symlinks at callers (`resolveWriteTarget`)
  before `AtomicWrite` when an operator may symlink a config file.

## API contract fixtures (`testdata/api-contract/`)

- Zod schemas are validated against JSON emitted by Go (`internal/server/contract_fixture_test.go`
  against the real `server.New()` router), rewritten on every Go test run and parsed by
  `frontend/src/schemas/contract.test.ts` ([D10](../decisions/quality-gateway.md#d10-contract-fixtures-are-rewritten-on-every-run-not-diffed-against)).
- Always run `make test-go` (or `make check`) after changing a Go response struct before trusting
  frontend tests: flow is one-way (`Go → fixture → TypeScript`).
- `tasks-fixture-agent.json` is captured by `make test-go-fixture-agent` (part of `make test-go`);
  untagged checks know its filename, and frontend optional-field coverage relies on `tasks.json`.
- Never hand-edit `testdata/api-contract/`; commit regenerated diffs with the struct change. Both
  sides enforce exhaustiveness when schemas are added. See
  [testdata/api-contract/README.md](../../testdata/api-contract/README.md) for normalized fields.

## Browser fixture isolation

- The task-dashboard E2E fixture clears `TMUX`, exports a short unique `TMUX_TMPDIR`, starts tmux
  with `-f /dev/null`, and stops its private server (and launched/resumed sessions) on exit.
- Playwright sends `SIGTERM` to task-dashboard, Agent Board, and command-center fixtures (5-second
  grace for the latter two); owning shells reap children before removing per-run fake `HOME` dirs.
- `frontend/e2e/stand-ins.sh` prepends CLI stand-ins to `PATH` before panemux starts and fails if
  any command resolves elsewhere (see [the fixture agent's tagged build](quality-gates.md#the-fixture-agents-tagged-build)).
- The shared E2E/screenshot agmsg fixture supports both positional seed rows and `--body -` stdin
  sends (`TestE2EAgmsgFixtureBroadcastReachesRelayCache`, `TestE2EAgmsgFixturePositionalSeedingStillWorks`).
- `make test-screenshots-check` verifies staging, tmux isolation, stand-in precedence, and process
  filters (skips `ps` checks in the Claude sandbox, fails in CI). Test sockets use `tmux_short_dir`
  (`TMPDIR` if it fits, else `/tmp`). Temporary files follow
  [temporary-file discipline](../../DEVELOPMENT.md#temporary-file-discipline). See
  [Documentation screenshots](screenshots.md) for capture staging.

## State-machine model checking

Model checking covers small, security-sensitive state machines where example tests can miss
interleavings. The pilot model is `internal/board`'s `ownSendLedger` (`spec/agentboard/OwnSendLedger.tla`;
[pilot decision](../decisions/agent-board.md#own-send-ledger-and-model-checking-pilot-2026-082026-09-prs-162-243-issue-168)):

| Property | Requirement |
|---|---|
| Conservation | Each recorded occurrence is held, consumed, forgotten, or expired exactly once. |
| Consume never forges | A successful consume requires a live matching occurrence (protects `_system`). |
| Record is immediately matchable | Recording an occurrence makes one subsequent consume succeed, including duplicates (multiset). |

Verification uses two tiers ([D12](../decisions/quality-gateway.md#d12-a-model-checkers-output-is-checked-in-the-checker-itself-is-not-in-make-check)):

- **Tier 1 (hermetic, in `make check`)**: `internal/board/ledger_conformance_test.go` traces the real
  `ownSendLedger` (directly and via `Relay.Broadcast`/`Poll`) against the committed transition table
  `internal/board/testdata/ownsendledger-transitions.json`, requiring every modeled non-expiry
  transition to be exercised. Do not write a second Go reference model.
- **Tier 2 (`make model-check`, PR CI)**: Runs TLC over `spec/agentboard/OwnSendLedger.tla`, checks
  invariants/properties/deadlock freedom, and verifies zero diff against the committed table.
  The committed bound (`MaxEntries` = 4 per key) comes from `.cfg`, verified offline by
  `make test-model-check` (`scripts/tla_transitions.py` against `scripts/testdata/model-check/`;
  `python3` optional locally).

Never hand-edit `internal/board/testdata/*-transitions.json`. After changing `.tla`/`.cfg`:

```sh
curl -fsSL -o /tmp/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check        # check spec and diff table
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check-write  # regenerate table
```

## Agent Board testing plan

Tests must protect these observable contracts rather than internal structure:

- **Message and relay semantics**: Exact `board_status` discriminator; latest status wins per
  host-qualified name while pane API keys retain matching host reports; opaque host-local agmsg IDs;
  replay on cursor loss, no retry after cursor advance, bounded-loss overflow; pane-free unqualified
  senders accepted while qualified senders, wrong-host pane senders, and unmatched `_system` senders
  are rejected; qualified forwarding strips recipient host and qualifies every sender (delayed
  copies rejected after pane removal, movement, or restart); SSH key alphabet matrix (`._-+`,
  Unicode, rejecting whitespace/`@`/control chars regardless of Board enablement); multiset ledger
  expiry/rollback; pre-delivery broadcast recipient validation and partial-delivery reporting;
  NUL-body rejection before host execution; E2E fixture stdin/positional body parity.
- **Execution and security**: Shell-safe remote args on reads/writes; arbitrary body round-trip;
  `--force` writes compensated by sender validation; non-loopback requires explicit token;
  token-gated board REST/WS vs. legacy terminal boundary; loopback-only `/api/session-token`
  rejecting forwarding headers (with `0.0.0.0` host exception).
- **Bootstrap and dependencies**: No Board path ever calls `Session.Write`; home-path expansion
  before commands; missing install/failed presence retries without PTY writes; legacy mode validated,
  warned once, ignored, and omitted on save; presence debounce (2 detections, stop after success,
  reset on session/host change, silent managed pauses); retired bootstrap state left untouched.
- **Command center and frontend**: Panemux-minted session ID, one query at a time, single terminal
  frame, timeout stop, isolated settings and three-tool MCP scope only, ordered history with
  warnings; runtime Zod validation, independent dashboard/command-center flags, configured/active/stale/removed
  pane states, overlay streaming and focus restoration.

Real external scripts are verified separately by the
[agmsg compatibility contract](../agent-board/agmsg-contract.md#agmsg-compatibility-contract).
