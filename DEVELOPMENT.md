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

**The home directory has one seam, and it is `internal/homedir`.** Call `homedir.Dir()`; substitute it
in a test with `homedir.SetForTest(t, dir)` or `homedir.SetFailingForTest(t, err)`. Do **not** call
`os.UserHomeDir()` — `.golangci.yml`'s `forbidigo` rule fails the build on it outside that package —
and do not reach for `t.Setenv("HOME", ...)`, which mutates the process environment every goroutine
and subprocess in the binary shares, and does nothing on Windows, where `os.UserHomeDir` reads
`USERPROFILE`. Note what the seam does **not** fix: `homedir.dirFn` is one unsynchronized package
variable, so substituting it is no safer under `t.Parallel` than `$HOME` was — `t.Setenv` at least
panics there, while the seam races silently. Substitute it only from non-parallel tests. It is one
package rather than one variable per caller because
the callers do not line up with the tests: `internal/api`'s handler tests drive tilde expansion that
happens inside `internal/config`, `internal/server`'s integration tests drive config, api and session
at once, and the root package's tests drive `internal/commandcenter`'s default paths — none of which
a package-private variable can reach. See issue [#212](https://github.com/tomo-chan/panemux/issues/212).

**The cache directory has its own seam, and it is `internal/cachedir`.** Same shape — `cachedir.Dir()`,
`cachedir.SetForTest(t, dir)`, `cachedir.SetFailingForTest(t, err)`, and a `forbidigo` rule on
`os.UserCacheDir()` outside that package — and a *separate* package rather than a second function in
`internal/homedir`, because it is a different global with different platform behavior:
`os.UserCacheDir` never consults `os.UserHomeDir`, reading `$XDG_CACHE_HOME` (falling back to
`$HOME/.cache`) on non-darwin Unix but `$HOME/Library/Caches` on darwin, where `XDG_CACHE_HOME` is
ignored outright. Substituting the home directory therefore cannot influence it, and neither
environment variable alone covers it — `internal/server`'s integration helpers had to set **both**,
and CI, being Linux-only, would have stayed green had one been dropped while a Mac developer's real
`~/Library/Caches` collected files from a test run. Keeping the packages apart also keeps each
`forbidigo` exclusion to one function, so a stray `os.UserHomeDir` inside the cache seam is still
caught. See issue [#226](https://github.com/tomo-chan/panemux/issues/226).

**Neither seam is safe under `t.Parallel`, and that is a decision rather than an oversight.** Each is
one unsynchronized package variable, so two parallel tests substituting it race and the loser
silently reads the other's directory — where `t.Setenv` would have panicked. A mutex would remove the
race without making it correct (restore stays last-writer-wins, so one test's `Cleanup` can still
restore over another's live substitution), which buys the appearance of a guarantee; the only real
fix is per-test injection with no global at all, which means changing exported signatures across six
packages for a suite that today has **zero** `t.Parallel` calls and runs in seconds. So: substitute
the seams only from non-parallel tests, and if a concrete need for `t.Parallel` ever appears, take the
per-test-injection route for the packages that need it rather than adding a lock. See issue
[#227](https://github.com/tomo-chan/panemux/issues/227).

**Persisted files are written through one seam, and it is `internal/fileops`.** `fileops.AtomicWrite`
is the temp-file-plus-rename write every file panemux persists goes through — `config.yaml`, the
auth token file, the relay cursor, bootstrap state, and the command-center session id;
`fileops.CreateTemp` / `OpenFile` / `Chmod` are the individual operations for the two callers that
need the steps rather than the whole write (the MCP config file, which is never renamed, and the
history file, which is appended to). Substitute them in a test with
`fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: err}).Ops())` — a `Spy` performs every real
operation and fails only the steps it is given, so the filesystem still ends up in the state it
would have been in (an injected write failure is a *short* write, as a real ENOSPC is), and
`spy.Files()` names the files it handed out so a test can assert they were cleaned up.

Moving a write onto it changes one thing worth checking: `AtomicWrite` finishes with a rename, and
rename replaces what is at the path rather than following it, so a symlinked target is swapped for a
regular file where `os.WriteFile` would have written through it. If the path is one an operator may
have hand-linked, resolve it first — `internal/config`'s `resolveWriteTarget` is the example, and its
doc comment says why that resolution is at the caller rather than in the seam.

Unlike the home-directory seam, nothing enforces this one — there is no `forbidigo` rule that can
tell a legitimate `os.WriteFile` from one that should have been an `AtomicWrite`. It is true as
written today because the writes were moved onto it, not because the build would fail otherwise, so
a new persisted file is the reader's job to route correctly.

Reach for it when a failure arm sits *after* the file was created by the function under test: a path
whose shape can be broken (a regular file where a directory goes, a directory where a file goes,
`/dev/full`) is still the better fixture and needs no seam, but that only works while the path came
from the caller. Once `os.CreateTemp` has succeeded, the file exists, is writable and is owned by the
process, and CI runs as root, so the `Write`/`Close`/`Chmod` arms — the disk filling partway through
a write, which is the failure the rename discipline exists to survive — have no fixture at all. Same
`t.Parallel` caveat as `homedir`: `fileops.ops` is one unsynchronized package variable. See issue
[#222](https://github.com/tomo-chan/panemux/issues/222).

### Schema-first

- For Go structure changes, update validation rules and tests in `internal/config/validate.go` first.
- For frontend type changes, update Zod schemas in `frontend/src/schemas/index.ts` first. Do not edit `frontend/src/types/index.ts` manually.
- API responses and WebSocket messages are runtime-validated against the schemas. When schemas change, update the corresponding tests too.

### Contract fixtures (`testdata/api-contract/`)

- The Zod schemas are validated against **JSON that Go actually emitted**, not against hand-written
  TypeScript objects. `internal/server/contract_fixture_test.go` captures each response from the real
  `server.New()` router and rewrites `testdata/api-contract/` on every Go test run;
  `frontend/src/schemas/contract.test.ts` parses each file with the schema that owns it.
- **Run `make test-go` (or `make check`) after changing a Go response struct, before trusting a green
  frontend run.** The direction is one-way — Go → fixture → TypeScript — so the frontend suite only
  sees a renamed field once the Go suite has regenerated the fixture. `make test`, `make check` and
  CI all run them in that order.
- Commit the fixture diff alongside the struct change; it is the contract change, in the diff a
  reviewer reads.
- Never hand-edit a file under `testdata/api-contract/` — the next Go test run overwrites it. Adding
  a response schema means adding a capture there and an entry in `contract.test.ts`; both sides have
  an exhaustiveness check that fails until you do.
- See [testdata/api-contract/README.md](testdata/api-contract/README.md) for the values normalized out
  of a capture, and decision D10 in [docs/quality-gateway.md](docs/quality-gateway.md) for why the
  fixtures are rewritten rather than diffed.

### Coverage

- `make coverage-go` enforces at least 80% combined coverage across `internal/config`, `internal/api`, `internal/ws`, `internal/server`, `internal/board`, `internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, `internal/fileops`, `internal/homedir`, `internal/cachedir`, and the root package. The `Makefile`'s `COVERAGE_PKGS` is the authority; this list follows it.
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
- **A mutant with no verdict is reported as undecided, not as passing — and it fails the build.** gremlins defines **seven** statuses, and the gate sorts them into four groups. `LIVED` is a survivor and fails. `KILLED` is the good case, and it is also the counter that says the run decided anything at all. `NOT COVERED` and `NOT VIABLE` are deliberately silent (one `make coverage-blocks` reports better, and a mutant that did not compile could not have been noticed by any test). `SKIPPED` is reported but does **not** fail — see the next bullet. **Everything else — `TIMED OUT`, `RUNNABLE`, and any status a later gremlins invents — is listed with the status that produced it, counted in the headline, and exits 1.** `TIMED OUT` is why this matters rather than a nicety: it is the status the pinned settings below exist because of, and the measurement that pinned them found timeouts hiding 51 survivors. Read "0 survivors, 12 of 12 mutants undecided" as a run that measured nothing, not as a clean branch.
- **`SKIPPED` is the one status that is not the author's problem.** gremlins sets it from its own diff and nothing else (`internal/engine/engine.go`: `if !Diff.IsChanged(pos) { status = Skipped }`), and that diff is an approximation — `internal/diff/diff.go` builds each changed range as `EndLine = startLine + LinesAdded - 1`, which assumes a hunk's added lines run contiguously from the fragment's start. They do not when a hunk mixes context, deletions and additions. On [#231](https://github.com/tomo-chan/panemux/pull/231)'s real report, git's `@@ -50 +51,11 @@` covers line 58 and gremlins' window does not, so two mutants on a line that branch added came back `SKIPPED`. No edit to that line changes it, so the gate reports the disagreement and stays green.
- **A run in which nothing reached a verdict fails, whatever the reason.** If not one scoped mutant came back `LIVED` or `KILLED` — all skipped, all timed out, all uncovered, all non-viable, or any mix — the run measured nothing and must not print the sentence a clean branch gets. The failure names the composition, because the action differs: re-run a timeout, add a test for an uncovered line, use the label for a non-viable mutant. **The exception is waiving every scoped mutant yourself** with `//mutation:exempt[<TYPE>]`: that is an explicit, reviewable claim about each one, and it does not depend on whether the mutant ran. A partial waiver does not clear it.
- **The headline carries the denominator, because a verdict without one is unreadable.** "no surviving mutants among 5 on lines this branch changed" and "…among 50…" are different evidence for the same sentence. When a diff puts *no* mutant on a changed line the run says **"nothing was measured"** rather than borrowing the clean-branch wording, and names how many mutants the touched files did hold — a diff of type declarations, struct fields or plain returns genuinely has nothing to mutate, and that is worth knowing rather than reading as a pass.
- **It fails the build.** A surviving or undecided mutant on a changed line exits 1. It warned for three stages first, and decision D9 in [docs/quality-gateway.md](docs/quality-gateway.md) records both the measurement that kept it a warning — roughly a third of this repository's survivors are ones nobody should act on, and a test that killed them would be a tautology — and the three things that had to land before failing was defensible: a type-scoped waiver (#236), a status that can no longer be silently dropped (#235), and a known denominator of about 5 mutants per 317 changed lines (#237). It still exits 1 when it could not run.
- **If it goes red, your options in order are: add the assertion, waive the one mutant with `//mutation:exempt[<TYPE>] <reason>`, or — last — apply the `mutation-exempt` label.** Prefer the marker: it is per-mutant and it sits in the diff a reviewer reads. A `TIMED OUT` mutant is usually worker contention, so re-running is a legitimate first move there and nowhere else.
- Needs gremlins, which `make install-deps` does not install: `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`. `make test-mutation` — the checker's own tests, inside `make check` — never needs it, because it drives the script through `--report` against fixture reports.
- `make mutation` builds the frontend first, for the same reason `make coverage-go` does: gremlins gathers coverage over the whole module before mutating anything, so the root package has to compile and `main.go` embeds `frontend/dist`.
- A survivor worth keeping is marked `//mutation:exempt[<TYPE>] <reason>` on the mutated line or the line above. `<TYPE>` is gremlins' mutant type — the third column of `make mutation`'s own output, so it is copied from the finding you are waiving. **Both halves are required**: a marker with no reason and a marker with no type each exempt nothing, and the run says which it saw. The `mutation-exempt` label exempts the whole branch and is the blunter tool.
- **The waiver covers the type it names, and only that type.** `//mutation:exempt[CONDITIONALS_BOUNDARY, ARITHMETIC_BASE] <reason>` names two; `//mutation:exempt[*] <reason>` waives every mutant on the line and is reported as line-wide, because it is a claim about mutants nobody has looked at. When a line carries a marker and a mutant of another type survives, the finding says which type the marker names — that difference used to be invisible. Measured on this repository's own eleven markers, every marked line carries one to three further types.
- **`[CONDITIONALS_BOUNDARY]` costs 24 characters, and `lll` allows 120.** Seven of this repository's eleven markers went over when the types were added, and the fix was to shorten the reasons rather than widen the limit — the type tag now carries the *which mutant* half, so a reason no longer has to restate it ("equivalent — `validProcessIDArg` rejects `"0"` with the same error" says everything the longer form did). Write the *why*; the tag is the *what*.
- Say which kind of survivor it is, because the two are not interchangeable. **Equivalent** means the mutant computes the same thing on every possible input — nothing can distinguish it. **Unreachable** means it *is* killable, but only by input the code's own callers cannot produce. Before writing "equivalent", ask: *if the mutant were reachable, would anything be wrong?* If the answer is yes, it is unreachable, not equivalent — and it may be a real defect behind a guard nobody tests. [#190](https://github.com/tomo-chan/panemux/issues/190) filed one such mutant as equivalent and it was neither: the guard it sat behind was load-bearing, and it got a test instead of an exemption.
- Do not pass gremlins' own defaults: `scripts/mutation.sh` pins `--timeout-coefficient` and `--workers` because the defaults report 44% of runnable mutants as timed out on this repository, and those timeouts hide survivors.

### Model checking (`make model-check`, issue #168)

- `internal/board`'s `ownSendLedger` — the record of Send calls panemux itself issued, which the
  relay consults before believing a row that claims `From == SystemID` — has a TLA+ spec at
  `spec/agentboard/OwnSendLedger.tla`. It is the pilot subsystem for [#168](https://github.com/tomo-chan/panemux/issues/168), picked because it is small,
  self-contained, security-critical, and the place PR #167's second review round found a real
  multiset-vs-set bug.
- **Model checking alone proves the model, never the code.** The split that closes that gap is two
  tiers, mirroring [docs/agent-board.md](docs/agent-board.md)'s agmsg compatibility contract:
  - **Tier 1 — hermetic, inside `make check`.** `internal/board/ledger_conformance_test.go` attaches
    a tracer to the real `ownSendLedger`, drives it (directly, and through `Relay.Broadcast`/`Poll`),
    and looks every transition it makes up in the table TLC exported to
    `internal/board/testdata/ownsendledger-transitions.json`. No JDK, no jar, no network.
  - **Tier 2 — `make model-check`, a pull-request CI job.** Runs TLC over the spec, regenerates the
    table and diffs it against the committed copy. Outside `make check` because it needs a JDK and
    `tla2tools.jar`, which `make install-deps` does not install.
- The Tier 1 reference model is **the exported table and nothing else**. Do not hand-write a second
  Go state machine beside it: it would be a third artifact to keep in sync, and it would drift from
  the spec exactly the way the implementation it is checking might.
- Never hand-edit `internal/board/testdata/*-transitions.json`. Change the `.tla`/`.cfg`, run
  `make model-check-write`, and commit the table diff alongside — that diff is the behavioral change
  a reviewer reads.
- The check is **bounded**: `MaxEntries` in the `.cfg` caps how many occurrences one key may hold, so
  Tier 1 says nothing about a ledger holding more. Raise the bound in the `.cfg` and regenerate if a
  driver needs to go further.
- The Tier 1 drivers assert not only that the implementation never contradicted the model, but that
  it **took every transition the model has**. Without that, a table permissive enough to accept
  anything would pass as quietly as a correct one.

```sh
curl -fsSL -o /tmp/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check        # check the spec, diff the table
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check-write  # regenerate the table
```

### Red-check (`make efficacy`)

- A test you change must **fail** when your implementation diff is reverted. That is the machine-checkable half of the TDD rule above: the order lines were written in cannot be recovered after the fact, but the result can.
- Run it yourself with `make efficacy` (compares against `origin/main`; override with `EFFICACY_BASE`). CI runs it on every pull request.
- It is deliberately **not** part of `make check` — it needs the base branch and a second test run, which is a per-pull-request cost, not a per-turn one.
- A branch that changes no implementation, or changes implementation but no test, is skipped rather than failed.
- If a change genuinely should not go red without its implementation — a pure refactor, a test-only rename, a test pinning behavior this branch never touched — mark that test `//efficacy:exempt <reason>` in the comment directly above it. **The reason is required** — a bare marker waives nothing and the run names the tests carrying one — and the marker is the one to reach for first: it is per-test and it sits in the diff a reviewer reads. The `efficacy-exempt` label is the blunter tool and takes the whole branch out of scope.
- Neither is for getting past a test that turned out not to assert anything. A branch where most changed tests survive is worth a second look before it is exempted: on [#190](https://github.com/tomo-chan/panemux/issues/190) 15 of 18 survived and every one was legitimate, because the whole branch existed to pin already-correct behavior — that is the rare shape, not the usual one.
- See decision D4 in [docs/quality-gateway.md](docs/quality-gateway.md).

### Quality gate

- `make check` must pass before `make build`.
- `make check` must pass before reporting implementation complete.
- There are no exceptions for frontend-only, docs-adjacent, or "small" code changes.
- Test commands: `make test-go`, `make test-frontend`, `make test-e2e`, `make test`, `make test-hooks`, `make test-efficacy`, `make test-scenarios-check`, `make test-coverage-blocks`, `make test-mutation`
- Ledger command: `make check-scenarios`
- Pull-request-only gates: `make efficacy`, `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks`, and `MUTATION_BASE=origin/main make mutation` (all three fail the build — `make mutation` warned until #180's item 6 reached stage 4; see above)
- Model-checking commands (outside `make check`, they need a JDK and `tla2tools.jar`): `make model-check`, `make model-check-write`
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

- Product overview: [docs/overview.md](docs/overview.md)
- Architecture and security design: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md)
- UI intent: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Test quality characteristics and the gate design: [docs/quality-gateway.md](docs/quality-gateway.md)
