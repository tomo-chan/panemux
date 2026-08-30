# Quality Gateway

This document defines what panemux's tests are meant to protect, what implementation shape makes
that possible, and the layered set of gates that enforces it.

**Status: design document.** The measurements in it are real — every number was produced by running
the repository's own test and coverage commands at the commit named below. The gateway itself is a
proposal: gates marked *present* exist today, gates marked *absent* or *partial* do not. Check a
gate's status row before treating it as shipped behavior.

Measured at `b93051f`.

## Why this document exists

panemux does not have a shortage of tests. At the commit above:

| Layer | Size | Gated coverage |
|---|---|---|
| Go unit | 53 files, 841 `func Test` | ≈88% across `config`/`api`/`ws`/`server`/`board` — 87.8–88.0% depending on the run (threshold 80%) |
| Frontend unit | 31 files, 704 tests | hooks 95.6%, schemas 100% (threshold 80%) |
| E2E (Playwright) | 8 specs, 23 tests | not gated |

And yet [issue #178](https://github.com/tomo-chan/panemux/issues/178) established that the route
table in `internal/server/server.go` could be renamed, reordered, or wrapped in new middleware and
161 tests in `internal/api` would stay green, because those tests built their own copy of the router
rather than going through `server.New()`. The `/api/board/*` routes were registered in that copy
*without* `bearerAuthMiddleware`, so 25 board handler tests asserted against a shape that did not
exist in production. That specific defect is closed — see gate G3 below — but it is the reason this
document exists, and the reasoning it produced applies to every gate here, not just that one.

That is not a gap in test quantity. It is a gap in two things this repository has never written
down:

1. **Which quality characteristics the tests are responsible for**, and
2. **How the quality of the tests themselves is measured.**

The rest of this document supplies both, and then designs the gates that hold them.

## What tests must protect

There are two distinct axes here, and conflating them is what allows 88% coverage to feel
sufficient while the defect above sits in the open.

### Product quality

[ISO/IEC 25010:2023](https://www.iso.org/standard/78176.html) defines nine product quality
characteristics (the 2023 revision renamed Usability to Interaction Capability and Portability to
Flexibility, and added Safety). Mapped onto panemux, the depth of protection is extremely uneven:

| Characteristic | What it means here | Current protection | State |
|---|---|---|---|
| Functional suitability | Panes, workspaces, session types and config read/write behave as specified | Unit tests (thick); E2E now covers splitting, resizing, layout restore and workspace CRUD as well (`core-multiplexer.spec.ts`), and every scenario row is cross-checked against a real test | Improving |
| Reliability | Recovery from WS disconnect, replay, session exit, SSH reconnect | `internal/ws` unit tests plus the Alloy model in `docs/models/replay_state.als` and `model-check.yml` | Strong |
| Security | Command injection, bearer token, DNS rebinding, subprocess containment | [security.md](security.md) plus regression tests verified against real binaries, `gosec` | Very strong |
| Compatibility | The agmsg script contract, config schema back-compat | Tier 1 fixtures, Tier 2 tests against a real agmsg install, daily canary | Very strong |
| Maintainability | Refactoring does not break tests; behavior changes do | `golangci-lint` (20+ linters), 80% coverage. **Nothing measures the tests themselves** | Unprotected |
| Interaction capability | Keyboard operation, focus restoration, legibility, notifications | Three focus-restoration E2E tests, plus an axe-core scan of the dashboard and of a modal dialog (`frontend/e2e/a11y.spec.ts`) that **records** violations without gating on them | Thin, now measured |
| Performance efficiency | Terminal output throughput, relay polling cost, many-pane rendering | Benchmarks over the replay buffer and the board cache (`make bench`). No threshold — measurement only | Unprotected, now measured |
| Flexibility | Old config shapes, migration, environment differences (OS, shell, tmux) | `internal/config` unit tests (thick); environment differences are manual | Partial |
| Safety | A PTY write does not destroy the user's work; no stray remote side effects | Bootstrap short-write and retry tests (limited) | Limited |

Security and Compatibility are ahead of what a project this size usually carries — the discipline in
`security.md` of separating verified claims from unverified ones, and the Tier 2 agmsg contract with
its daily canary, are both unusual. **Maintainability is the one characteristic with no structural
protection at all**, and it is the one that AI-assisted development amplifies hardest.

### Test quality

Vladimir Khorikov's four pillars evaluate the tests rather than the product. The important part is
the shape of the trade-off: the first three cannot be maximised together, and resistance to
refactoring is the one that is not negotiable.

| Pillar | Meaning | What can measure it | panemux today |
|---|---|---|---|
| Protection against regressions | Probability a test fails when a bug is introduced | Coverage is only a *lower bound* proxy. The real measure is mutation score | Coverage 88%; mutation never measured |
| Resistance to refactoring | Behavior-preserving changes do not fail tests (few false positives) | No direct metric exists. Only structure can guarantee it | The duplicated router that prompted this document is gone; nothing measures the property itself |
| Fast feedback | Wall-clock time | Seconds | Go ≈3s for the gated packages, ≈8s for the whole suite under `-race`; frontend ≈13s. Good |
| Maintainability | The tests are themselves readable and durable | Test volume, duplication | Was 4 router copies and 3 `*_routes_test.go` files; the copies are gone, and the per-route files are now consolidated into two exhaustive tables (route set, and integration behavior) that a new route must be added to |

### Why coverage alone misleads

Coverage measures a lower bound on the **first** pillar. It says nothing at all about the **second**.
The 88% figure was never evidence about the defect #178 found, because it does not measure that axis.

Worse, the two are anti-correlated under optimisation pressure. A test that reaches deep into
implementation detail executes more lines than one that goes through a public contract, so **gating
on coverage alone applies pressure in the direction that lowers resistance to refactoring.**

This is the central claim the gateway is built on: raise the *scope* of coverage, never the
threshold, and measure protection against regressions directly instead.

**Stated plainly, because this repository separates verified claims from unverified ones: the
anti-correlation above is reasoning, not a measurement.** Nothing here has measured how coverage
pressure actually changes the tests this project writes. The mechanism is plausible and matches the
reported failure modes of AI-written tests cited below, but it has not been demonstrated on this
codebase. What *is* measured is the defect that prompted the document: a route table that could be
renamed with 161 tests staying green. Treat D1 as a decision taken under that reasoning, and
revisable if evidence contradicts it.

## Implementation practices that make tests possible

Test quality is decided by the shape of the implementation before it is decided by how the test is
written. Every unprotected area in panemux reduces to "there is no injection point".

| Principle | Rule | Evidence in this repository |
|---|---|---|
| **P1 — Single source of truth** | Do not encode the same knowledge twice. Never reconstruct production wiring inside a test. | The route table used to exist in both `internal/server/server.go` and `internal/api/handler_test.go`, and had drifted: `/api/board/*` was authenticated in one and flat and unauthenticated in the other. It now lives once, in `internal/api`'s `Handler.Mount`. |
| **P2 — Humble object** | Keep the side-effecting boundary (PTY, SSH, `exec`, filesystem, network) as thin as possible; put decisions in pure functions. | Works: `validRemotePath`, `classifySSHWaitError`, `frontend/src/utils/layoutTree.ts`. Fails: `internal/session/ssh.go`'s lifecycle methods mix transport with decisions and sit at 0%. |
| **P3 — Injection points are typed** | Do not call globals, environment variables or `os.UserHomeDir()` directly; make them a struct field or constructor argument. | Already stated in [DEVELOPMENT.md](../DEVELOPMENT.md#test-granularity)'s testability rule (`Config.sshConfigPath`). Applied to `main.go` as of rollout item 2: `parseOptions` takes its arguments, `loadConfig` takes a `configLoader`, `startSessionsFromConfig` takes the session factory, and `openChrome`'s per-OS decision is the pure `browserOpenArgv`. The root package went 0% to 75.1% overall, `board.go` 47.8% to 76.9% and `main.go` 0% to 38.8% — what is left in `main.go` is `main()` and `runServer()` themselves, which install signal handlers and run for the life of the process. |
| **P4 — Name the observable behavior** | What a test drives is a published contract with domain vocabulary — HTTP responses, WS frames, persisted files, rendered output — not call ordering or mock setup. | `frontend/src/hooks/useTerminalLinks.test.ts` is the model: it drives the real xterm terminal and the real addon, and is named after the behavior rather than a source file. |
| **P5 — Deliberate implementation pinning is documented** | A test that pins implementation shape in violation of P4 is allowed only when a document states why. | `TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection` pins exact argv, and [security.md](security.md#command-center-subprocess-execution) explains why. That is the correct use of the exception. |

P1 and P4 together are what actually buys resistance to refactoring: **the test has to travel the
same wiring the product does.** Deleting the router copies and going through `server.New()` is not a
"more tests" change, it makes a class of false negative structurally impossible. That is the
rationale for gate G3.

## Working with AI coding agents

### External findings

| Source | Finding | Consequence here |
|---|---|---|
| [DORA 2025](https://cloud.google.com/blog/products/ai-machine-learning/announcing-the-2025-dora-report) | AI adoption now correlates positively with throughput but **still correlates negatively with delivery stability**. AI is an amplifier; strong automated testing, mature version control and fast feedback loops are prerequisites. | The safety net has to be invested in first. panemux's net is *fast*, but nothing measures whether it is *effective*. |
| [Claude Code best practices](https://code.claude.com/docs/en/best-practices) | Give the agent a check it can run, or "looks done" is the only signal available. Enforcement escalates: in-prompt → `/goal` → **Stop hook (deterministic gate)** → verification subagent. TDD is the strongest pattern. Do not let the author grade its own work. | This repository had no `.claude/` directory at the commit above: zero hooks, skills or subagents, every agent-side guard an advisory document. Rollout item 3 closed that — see gates G1 and G6. |
| Reported failure modes of AI-written tests | When the same model writes the code and the test, a bug becomes the expected value — a tautological test. Related shapes: asserting a mock's own return value, exact-string matching, order dependence, hard-coded internal helper output. | These break Khorikov's first *and* second pillars simultaneously, and nothing in this repository detects them. |
| [Spec-driven development](https://github.com/github/spec-kit) | A *constitution* of non-negotiable principles is written once and referenced by every later phase (specify → plan → tasks → implement). | panemux already has this shape: `AGENTS.md` → `DEVELOPMENT.md` / `docs/*`, with [scenarios.md](scenarios.md) as the acceptance ledger. What is missing is enforcement. |
| [gremlins](https://github.com/go-gremlins/gremlins) and Go mutation testing | Coverage is unreliable as a measure of test quality. gremlins targets small-to-medium modules; a run over a large one can take hours. Diff mode, scoped to new and changed code, is the practical form. | Whole-repository MSI is not a viable target. Scope it to the diff and to pull requests. |

### Where panemux stands

| | Present | Missing |
|---|---|---|
| **Constitution** | `AGENTS.md` indexing `DEVELOPMENT.md` and seven `docs/` files. TDD, test granularity, schema-first and path sanitization are all written down. | — |
| **Spec** | [scenarios.md](scenarios.md): a use-case ledger with `auto` / `auto (opt-in)` / `manual`, which states that a silently absent row is not a legitimate answer — now with sections H and I for the core multiplexer and for #177, a cross-check that every `auto` row resolves, and a CI gate on user-visible changes. | The trigger covers `frontend/src`, `internal/api` and `internal/config` only; a user-visible change confined to `internal/session` still slips past it. |
| **Verification** | One command, `make check`. Enforced by `.githooks/pre-push`, re-run in CI. Alloy model checking. Tier 2 agmsg contract plus a daily canary. | Nothing measures the *efficacy* of the tests. Coverage only reports that a line executed. |
| **Agent guard** | `.claude/settings.json`'s `PostToolUse` and `Stop` hooks (G1, G2) and `.claude/agents/diff-reviewer.md` (G6), added by rollout item 3. | The TDD rule ("write tests first, confirm they fail") is still stated but **cannot be verified after the fact** — that is what red-check (item 4) is for. |

### The specific risk

The gates that exist today are: coverage ≥ 80%, lint, and the tests passing. All three can be
satisfied by generating tautological tests. **For an agent, the cheapest way to satisfy the current
gates coincides with the way that most degrades quality.** Closing that is the first purpose of the
gateway.

## The gateway

### Design principles

1. **Deterministic.** An exit code, not a "should" in a document. `AGENTS.md` is advisory; a hook is
   deterministic.
2. **Cheap checks first.** Each gate only receives what passed the one before it. Feedback speed is
   Khorikov's third pillar directly.
3. **Runnable by the agent itself.** One command, readable output, an unambiguous pass or fail. A
   check a human has to interpret is not a gate.
4. **No false positives.** A gate that loses trust gets bypassed. Unstable checks belong in warnings,
   not gates.
5. **`make check` stays hermetic.** Anything needing real SSH, real tmux, real agmsg or the network
   goes in the opt-in path. `make test-agmsg-contract` is the existing precedent.

### The gates

Ordering is meaningful: the point is to stop a defect at the cheapest gate that can see it.

| # | Gate | Protects | Checks | Enforced by | Status |
|---|---|---|---|---|---|
| **G0** | Spec | Functional suitability (rework) | The change is tied to a row in [scenarios.md](scenarios.md). A user-visible change adds or updates a row in the same commit. | CI: fail when the diff touches `frontend/src`, `internal/api` or `internal/config` and `scenarios.md` is unchanged; a label grants explicit exemption | Present — `.github/workflows/scenarios.yml`, exempted by the `scenarios-exempt` label |
| **G1** | Edit | Maintainability | `gofmt -s`, `tsc --noEmit`, and `go vet` on the touched packages only | Claude Code `PostToolUse` hook in `.claude/settings.json` | Present — `.claude/hooks/post-edit-check.sh` |
| **G2** | Unit | Functional suitability, fast feedback | `make test-go`, `make test-frontend` — unchanged | Existing (`make check`, pre-push, CI) | Present |
| **G3** | Contract | **Resistance to refactoring**, compatibility | (a) HTTP/WS integration through the real `server.New()` router; (b) exhaustiveness check on the route table (every registered route against an expected set); (c) Zod schema round-trips; (d) the agmsg contract | Always-on Go and vitest tests, in `make check` | **Present.** (a) `internal/server/api_integration_test.go` drives every `/api` route and fails when one has no case; `ws_integration_test.go` drives both `/ws` routes over a real handshake. (b) and (d) unchanged. (c) `contract_fixture_test.go` captures real responses into `testdata/api-contract/`, which `frontend/src/schemas/contract.test.ts` parses with the schema that owns each one |
| **G4** | Efficacy | **Protection against regressions** | (a) coverage — scope tracks the implementation, threshold stays at 80%; (b) **red-check**: a changed test must fail when the implementation diff is reverted; (c) mutation score over changed lines only; (d) **per-block coverage**: no block covering a changed line may be one the suite never entered | (a) existing `make check`; (b), (c) and (d) pull-request CI jobs, `make efficacy`, `make mutation` and `make coverage-blocks` | Partial — (a) present and now scoped to every decision-holding package; (b) present (`make efficacy`, a pull-request-only CI job); (d) present (`make coverage-blocks`, likewise); (c) present as a **warning** (`make mutation`), not yet as a gate |
| **G5** | Scenario | Functional suitability, interaction capability | Playwright E2E, plus a check that every test named by an `auto` row in `scenarios.md` actually exists | CI, extending `make test-e2e` | Present — E2E plus `make check-scenarios`, which resolves every `auto` row |
| **G6** | Adversarial | All characteristics (design judgement) | A fresh-context review of the diff alone. The session that wrote the code does not grade it. Findings limited to correctness and stated requirements. | A review subagent in `.claude/agents/` plus human review. **Does not block** | Present — `.claude/agents/diff-reviewer.md`; still does not block |

### The enforcement ladder

This is the most reusable rule in the document. **Promote every discipline you want kept one rung
up.** A rule sitting on the bottom two rungs must not be described as "enforced".

| Rung | Mechanism | Example here |
|---|---|---|
| L0 | Written in a document (advisory) | DEVELOPMENT.md's TDD rule is here today. Both humans and agents read it; nobody can confirm it was followed. |
| L1 | A Makefile target | Runnable by hand, produces pass/fail. The first rung an agent can drive itself. |
| L2 | An agent hook | Claude Code `PostToolUse` / `Stop` hooks, in `.claude/`. Unlike `AGENTS.md`, deterministic and impossible to forget. gofmt, `go vet` and the touched packages' tests sit here as of rollout item 3. |
| L3 | A git hook | `.githooks/pre-push`. The last local line of defence. |
| L4 | A CI job | Unavoidable. Slow checks and checks needing an environment can only live here. The red-check (`efficacy.yml`) and the agmsg contract are both here. |
| L5 | Branch protection | Blocks the merge. The agmsg contract job already depends on this rung. |

### Design decisions

Several of these are counter-intuitive, so the reasoning is recorded rather than assumed.

**D1 — Do not raise the coverage threshold above 80%.**
Raising it works: the threshold gets met. But the cheapest way to meet it is to generate tautological
tests, which lowers Khorikov's first and second pillars at once. Coverage is only meaningful as a
lower bound, so it stays used as one. The correct strengthening is **scope, not threshold** — add
`internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, the root package and
`frontend/src/utils/**`, which together are roughly 2,600 lines currently outside any gate.

**D2 — Mutation testing is scoped to the diff and to pull requests.**
gremlins' own documentation states it suits small-to-medium modules and that a run on a large one can
take hours. Chasing a whole-repository MSI would make the gate unaffordable and therefore ignored.
Scoring only changed lines, outside `make check`, keeps the gate starting from green.

*Amended once measured.* The cost premise was weaker than it looked: the whole module takes **14m25s**
here, not hours, and `gremlins --diff` brings a typical branch to seconds. The conclusion stands on
the other half — 108 surviving mutants repo-wide means a whole-repository gate starts red — so the
governing reason is now the false-positive rate, not the clock. Recorded rather than quietly
rewritten, because the two reasons fail differently: a cost argument would be reopened by a faster
tool, and this one would not.

**D3 — Consolidate gates rather than adding them.**
As #178 showed, a structure where every new feature adds another `*_routes_test.go` is a tax, not a
gate; there are already three. A single source of truth for the route table plus one exhaustiveness
check means adding a route forces the test to be updated — it removes the possibility of forgetting
rather than detecting it afterwards.

**D4 — Verify "the test came first" after the fact (red-check).**
The TDD rule is stuck at L0 and cannot be checked directly. The *result* can be: **a changed test
must fail when the implementation diff is reverted.** This is the strongest possible mutation (remove
the implementation entirely), and it costs far less than general mutation testing — it runs each
changed test twice rather than once per generated mutant — while catching both tautological tests
and tests written after the fact. Measured once, on the branch for rollout item 2: 34 changed tests,
both phases, about 30 seconds with a warm Go build cache. Mutation testing's cost is still
unmeasured here, so "far less" is a claim about the shape of the work, not a measured ratio. That combination is
why it is ordered ahead of mutation testing in the rollout; it is not a claim that nothing else
would catch more.

Implemented as `scripts/efficacy.sh` / `make efficacy`. Five details of it are decisions in their
own right, recorded because each one was a way the gate could have reported green on exactly what it
was built to catch — and four of them were, in the first implementation, until a review reproduced
them:

- **The verdict is per test, and it takes two runs.** Every changed test is checked on its own: it
  must PASS at HEAD, then FAIL with the implementation reverted. Judging the whole changed set with
  one `go test` invocation is a different, much weaker rule, because that command exits nonzero if
  *any* selected test fails — so one honest test covers for every tautology beside it, and almost
  every branch here changes more than one test. The HEAD phase is the other half: without it, a test
  that was never going to run, a package that was never going to build, or a test that was already
  failing all report as "red" for reasons that have nothing to do with the revert. `go test` also
  exits 0 when `-run` selects nothing, so "the command did not go red" and "the tests passed" have
  to be told apart by looking for the test's own `--- PASS`/`--- FAIL` line rather than by exit
  status. Benchmarks are excluded at collection for the same reason: `-run` never selects them, and
  a benchmark asserts nothing that could go red.
- **Scope is the changed test *functions* and *cases*, not the changed files.** The script maps the
  diff's touched line numbers onto the function ranges in the file, so editing an assertion inside
  an existing test brings that test into scope, and appending a new test does *not* drag the
  untouched one above it in. The frontend half does the same thing with `it(...)`/`test(...)`
  blocks. Scope and per-test verdict are separate decisions that were once conflated: narrowing the
  set does not stop one member masking another, and this gate needs both.

  There is deliberately **no whole-file fallback**. A file the static extraction cannot map — an
  `it.each([...])` has a template for a title, not a literal, and there are five such blocks in this
  repository — is narrowed *after* the run instead, from the per-case source locations vitest's
  reporter emits under `--includeTaskLocation`. Running the whole file and reading the aggregate
  would be the last place a sibling's red could still vouch for the changed case. "Over-wide" is not
  the safe direction here: a wider run is likelier to go red for someone else's reason, and going
  red is what this gate reads as success.

  The block *boundaries* come from the source, not from that report: vitest reports a case at the
  line of its call, which for a multi-line `it.each([` sits several lines below where the block
  starts, so ranges derived from the report put a block's own opening lines inside the case above
  it. The same rule the static mapper spells out — the lines between two cases belong to the one
  below — has to hold in both paths, and the two share a scanner so they cannot disagree.

  One consequence is worth stating because it flips a verdict: a branch whose only frontend test
  change is describe-level setup — a `beforeEach` gaining a mock the new implementation needs, with
  no case body touched — has no case to judge, so it lands in "could not check" and fails, where it
  previously ran the file whole and passed.

  On the frontend the verdict comes from vitest's **JSON reporter**, read per case, not from
  `-t` plus the summary line. That was a second, subtler instance of the same masking: `-t` matches
  as an *unanchored* regex over the full `describe`-joined name, so `-t 'renders'` also selects
  `renders empty state`, and the `Tests …` summary is an aggregate — a tautology was reported red on
  the strength of its sibling. Anchoring does not fix it (`renders$` still matches `always
  renders`), and this repository has five such name pairs today. Running the whole file once and
  looking up each case's own status fixes it and is cheaper than one filtered run per case.
- **Skipping is as important as failing.** A branch that changes no implementation (docs, tests
  only) has nothing to revert, and a branch that changes implementation but no test has no result to
  check — the latter warns rather than blocking, because making it a failure would fire on every
  pure refactor. Each stack is judged only against its own revert, too: a branch that changes Go
  code and also touches a frontend test has nothing reverted under that frontend test, and failing
  it there would be a verdict on a mutation that never happened. Principle 4 again.

  **One case in that family is deliberately left open, and it is a real false positive**: a Go test
  in a package whose *implementation* this branch never touched — an assertion tightened next to the
  change, a flake fixed alongside a feature — is still judged against the revert, and cannot go red.
  Conditioning per package is not the fix: `internal/server`'s tests legitimately cover
  `internal/api`'s implementation, so narrowing by package would blind the gate to exactly the case
  it exists for. The answer is a **per-test marker**, `//efficacy:exempt` in the doc comment above
  the test, because the PR-wide `efficacy-exempt` label is too blunt for it — applying the label to
  get past one unrelated test also exempts a genuine tautology elsewhere in the same branch. The
  marker sits in the diff a reviewer reads, one line above the test it excuses.
- **"Could not check" is a failure, never a skip.** A missing base ref, a scratch worktree that
  could not be created, a missing `frontend/node_modules`, or every changed test turning out to be
  skipped or unrunnable — each of these once printed one line and exited 0. The last is the one that
  looks most like a skip and is not: `internal/board`'s agmsg contract tests carry six `t.Skip`s
  that fire on every runner without a real agmsg install, so a branch touching only those tests
  would have gone green having red-checked nothing. A required check that goes green having checked nothing is the single failure mode a
  required check exists to rule out, and it is invisible: the job is green, so nobody looks. These
  now exit 1 with a message saying what to fix. The related trap is the scratch worktree itself:
  `main.go` embeds `frontend/dist`, which is gitignored, so the root package cannot build there at
  all and every root-package test "went red" with zero files reverted. The script writes a
  placeholder into the worktree; the HEAD phase above is the backstop that would have caught it.
- **The escape hatch is a pull-request label, not a config file.** `efficacy-exempt` is visible in
  the same place the reviewer sees the diff. A change whose tests genuinely should not go red — a
  pure refactor, a test-only rename — is a real category, and hiding the exemption in a tracked file
  would make it invisible after the fact.

**D5 — The author does not grade its own work.**
A session carries the context of the approaches it tried and discarded. The Claude Code guidance
recommends a review in a fresh context that sees only the diff. G6 nonetheless **does not block**: a
reviewer asked to find gaps will report some even when the work is sound, and blocking on that
invites over-engineering — extra abstraction, defensive code, tests for cases that cannot occur.

**D7 — The ledger is cross-checked, not just required.**
Requiring a row is the obvious half of G0, and it is the weaker one. A ledger's most likely failure
is not an absent row but a **stale** one: a row naming a test that has since been renamed, moved or
deleted still reads as coverage, and nobody grepping for a test name expects to find it in a markdown
table. `make check-scenarios` resolves every path and Go test name an `auto` row claims. It found a
stale row the day it was written — C7 named `..._TransportError_DistinctFromNo` while the test is
`TestBootstrapWatcher_RemotePresenceCheckTransportError_DistinctFromNo`.

Its own risk is false positives, because it reads prose: `Cmd/Ctrl+Shift+B` looks like a path,
`/ws/board-command` looks like a path, `bin/panemux` looks like a path and is a build artifact that
does not exist on a clean checkout. All three were false positives in the first version and all three
are now regression-tested, because principle 4 applies hardest to a gate that reads English.

**D6 — The Stop hook does not run everything.**
Claude Code's `Stop` hook can deterministically block a turn from ending, but putting all of
`make check` there would run E2E on every turn. Put G1 and G2 (seconds) there and leave G3 onward to
pre-push and CI. A gate that sacrifices fast feedback gets bypassed.

**D8 — Per-block coverage gates the diff, not a baseline.**
Two measurements decide the shape. **275 to 278 blocks of 1801 have never executed** at `d0e88ee`
(70 of them in `internal/api/handler.go`), so #164's own proposal — fail on any zero-count block in
the gated packages — starts red, which is principle 4's failure mode. And **the zero-block set is
not deterministic**: six runs of the identical `make coverage-go` gave 275 or 278, differing by three
goroutine-timing-dependent blocks in `internal/ws/board_command.go`. That rules out the other obvious
shape, a checked-in ceiling that may fall but not rise, since the same noise fails it in both
directions.

Scoped to the diff it starts green, needs no second exclusion list to drift beside `COVERAGE_PKGS`,
and asks the author a question they can answer: you wrote this line, does anything execute it?
`//coverage:exempt <reason>` covers the residue, with the reason required for the same reason
`COVERAGE_PKGS`' exclusions carry one. A changed file in a package `COVERAGE_PKGS` excludes is
reported as *not measured* rather than as covered — failing there would start the gate red again for
`internal/session`. The ~275 remain a backlog, listed by `make coverage-blocks`; a gate is the wrong
instrument for a backlog.

**D9 — The mutation check warns; it does not fail.**
Stage 3 of #180's item 6, and the measurement is the argument. Of the 108 surviving mutants found at
`d42e406`, **37 (34%) are ones nobody should act on**: buffer sizes (`64*1024` in three files),
timeout constants (`30 * time.Second`), and error branches unreachable without fault injection. A
test written to kill the buffer-size mutants would pin a constant and assert nothing — a tautology,
which is the exact defect G4 exists to find. A check that failed on those would be wrong more often
than right in its first weeks, and principle 4 says what happens to a gate that cries wolf.

So `make mutation` prints its findings and exits 0. What it does **not** soften is "could not run":
a missing base ref, a shallow clone, a gremlins that exited non-zero or a truncated report all exit 1,
because a check that decided nothing must never look like one that found nothing. Making the findings
themselves fail is stage 4, and deliberately a separate change with its own evidence — there is no
environment variable to flip it early, since an unused switch is an invitation to enable it without
the data that should decide it.

**Its settings are pinned, and that is not tuning.** With gremlins' defaults on this repository, 465
of 1059 runnable mutants (44%) come back `TIMED OUT`. They are not infinite loops — they are worker
contention — and they *hide survivors*: `internal/api` alone reports 0 survivors with the defaults and
7 without them, and the module-wide count goes from 57 to 108. Note what does *not* move: efficacy
reads 90.40% before and 89.74% after. A percentage that barely shifts while the absolute count doubles
is how this would have gone unnoticed, and it is why `--timeout-coefficient` and `--workers` are set
in `scripts/mutation.sh` rather than left to the tool.

**D10 — The contract fixtures are rewritten on every run, not diffed against.**
G3(c)'s obvious shape is a golden test: capture the response, compare it to the committed file, fail
when they differ. That shape defeats the gate. The failure it exists to catch is a Go struct changing
while the Zod schema does not, and a golden test stops at the **Go** suite — the frontend, which is
the side holding the stale schema, never sees the new shape at all. So
`internal/server/contract_fixture_test.go` writes `testdata/api-contract/` unconditionally, and
because the Go suite runs before the frontend suite in `make test`, `make check` and `ci.yml`, the
rename reaches `frontend/src/schemas/contract.test.ts` and *that* is what goes red. Both directions
were confirmed by perturbation before this was called done: renaming a **required** field
(`is_status`) fails the parse, and renaming an **optional** one (`last_tool`) fails the
equality check below.

Three consequences worth stating rather than discovering:

- **A fixture is only as current as the last `make test-go`.** After changing a response struct, a
  green frontend run alone proves nothing. The cost is real; the alternative costs the gate.
- **Parsing is not enough, so the parsed value must equal the captured one.** Zod *strips* keys a
  schema does not declare, so an optional field renamed in Go passes `safeParse` cleanly: the old
  key is absent, the new one is silently dropped. `expect(schema.parse(captured)).toEqual(captured)`
  is what makes that visible, and it is the check that caught `last_tool` above.
- **Only values are normalized, never keys or types.** Timestamps, the capture's temp `HOME`, the
  random `BoardCache` epoch and the detected shell would otherwise rewrite the files on every run and
  leave a clean checkout dirty. `TestAPIContractFixtures_ContainNoMachinePaths` re-checks the path
  half against the written files, so a normalization rule that quietly stops matching fails the suite
  instead of committing someone's home directory.

### Rollout order

| Order | Work | Gate | #178 phase | Effect |
|---|---|---|---|---|
| 1 | Real-router integration harness; single source of truth for the route table plus an exhaustiveness check | G3 | Phase 1 | **Landed.** The table has one definition, the exhaustiveness check pins it, and every `/api` route is now driven through `server.New()` — itself exhaustive across both command-center states, so a new route has no test until someone writes one, and a case that stops asserting a success path has to say so. Both `/ws` routes followed in #191, over real handshakes on a real listener, including that `/ws/board-command` is *absent* rather than rejecting when the command center is off. |
| 2 | Widen coverage scope (threshold unchanged) | G4(a) | Phases 2 and 5 | **Landed.** `internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, the root package and `frontend/src/utils/**` are now gated. Go reports 86% over the wider set (it was 88% over the narrower one — the drop is the point), frontend 95%; the threshold is unchanged at 80%. |
| 3 | `.claude/settings.json` with G1/G2 hooks; a review subagent | G1, G6 | — | **Landed.** A `PostToolUse` hook checks the edited file, a `Stop` hook checks what the turn changed, and `.claude/agents/diff-reviewer.md` reviews a diff in a fresh context. `make test-hooks` tests the hooks themselves. |
| 4 | red-check (`make efficacy`) in pull-request CI | G4(b) | — | **Landed.** `scripts/efficacy.sh` reverts the branch's implementation diff in a scratch worktree and requires each test the branch changed — Go function or vitest case — to pass at HEAD and then go red against the revert, one at a time. Exempted by the `efficacy-exempt` label. |
| 5 | Core-feature section in `scenarios.md`, ledger cross-check, core E2E | G0, G5 | Phases 4 and 6 | **Landed.** Sections H (core multiplexer) and I (opening URLs from a pane, #177's missing rows) added; `make check-scenarios` resolves every `auto` row; `frontend/e2e/core-multiplexer.spec.ts` covers split, resize, layout restore and workspace CRUD. |
| 6 | Diff-scoped mutation testing (warn first, gate once stable) | G4(c) | merges with #164 | **Warning landed.** `scripts/mutation.sh` runs gremlins scoped to the diff and names every mutant on a changed line that survives every test. Exits 0 on a finding — see D9. Making it fail is the remaining step. |
| 7 | Performance and accessibility observation (measure only, do not gate) | — | — | **Landed.** `make bench` measures terminal throughput, replay-buffer cost and the relay's polling cost; `a11y.spec.ts` records axe violations. Both report; neither asserts. |
| 8 | Per-block coverage on changed lines (#164, not a #180 item) | G4(d) | — | **Landed.** `scripts/coverage_blocks.sh` fails when a block covering a changed line never executed. It unblocked row 6's measurement, which is what row 6 was waiting on. |
| 9 | Zod schema round-trips against real Go output (#191, closing G3(c)) | G3 | Phase 1 | **Landed.** `internal/server/contract_fixture_test.go` captures every response the dashboard parses, plus both WebSocket frame streams, into `testdata/api-contract/`; `frontend/src/schemas/contract.test.ts` parses each with the schema that owns it and requires the parsed value to equal the captured one, so a field Zod *strips* fails too. Decision D10 records why the fixtures are rewritten rather than diffed. |

## Surviving mutants: the first measurement

Roadmap item 6's second stage asks whether tautologies survive per-block coverage. They do. The run
below is at `d42e406`, whole module, `gremlins v0.6.0 unleash --timeout-coefficient 10 --workers 2`,
14m25s.

| | Count |
|---|---|
| Killed | 945 |
| **Lived** | **108** |
| Not covered | 172 |
| Timed out | 6 |
| Test efficacy | 89.74% |

Every survivor is in code the per-block gate reports as covered, by construction: gremlins only
mutates covered code. The two gates are not redundant.

| Survivor | Count | What it is |
|---|---|---|
| Boundary value | 45 | Already required by DEVELOPMENT.md's "Test granularity" |
| Other logic | 26 | Case by case |
| Error branch | 25 | Needs fault injection to reach |
| Tuning constant | 12 | Killing it would pin a constant and assert nothing |

**The worked example, because it is the clearest statement of what G4(c) is for.** `port > 65535`
appears in `internal/config/validate.go`, `internal/api/handler.go` and `internal/session/loopback.go`.
Changing it to `>= 65535` makes all three reject port 65535 — a legal port — and every test still
passes, in all three packages. The cause is identical in each: the tests use `65536`, one past the
boundary, and never `65535`, the boundary itself. `make coverage-blocks` lists none of those lines,
and is right not to: the blocks execute. What is missing is not execution but an assertion.

That the same blind spot appears three times, in three packages, written at three different times, is
the part worth keeping. It is not an oversight in one test; it is a habit the existing gates cannot
see.

### Clearing the boundary-value class (issue #190)

[#190](https://github.com/tomo-chan/panemux/issues/190) took the 45 boundary survivors above. What it
found is worth recording, because it changes how the row should be read.

**The habit was real and it is now pinned.** Every site the issue named is exercised at the boundary
itself and at both neighbors: the three `port > 65535` checks, `child.Size <= 0` and the `±0.1` sum
tolerance in `internal/config`, `forwardablePort`'s `1024`/`65535` ends, the registry's TTL default,
the board cache's history bound, the replay buffer's 256 KiB bound, the three `timeout <= 0`
fallbacks, and `remoteShellPID`'s `pid <= 0`. Each was confirmed by reverting the comparison by hand
and watching the suite go red — a passing new test proves nothing on its own here.

**Five of the 45 are survivors no reasonable test kills — and the distinction between the two ways
that happens is the part worth keeping**, because they argue for stage 4 differently. An *equivalent*
mutant computes the same thing on every possible input; nothing can distinguish it. An *unreachable*
one is killable, but only by input the code's own callers cannot produce.

| Site | Mutant | Kind | Why |
|---|---|---|---|
| `internal/board/cache.go` | `overflow > 0` → `>= 0` | equivalent | at `overflow == 0`, `history[0:]` is the same slice |
| `internal/session/manager.go` | `len > limit` → `>=` | equivalent | at exactly the limit, `history[len-limit:]` is the same bytes |
| `internal/session/local.go` (`processIDArg`) | `pid <= 0` → `< 0` | equivalent | `validProcessIDArg` (`^[1-9][0-9]*$`) rejects `"0"` with the identical error |
| `internal/session/local.go` (`newestMatchingDescendantPID`) | `PID > matched` → `>=` | equivalent | on an equal PID the guard reassigns the value already there, and the function returns nothing else for it to change |
| `internal/session/local.go` (`newestKnownAgentTypeDescendantPID`) | `PID > pid` → `>=` | unreachable | it *does* differ on two matching processes sharing a PID — in the reported `agmsgType`, not the PID — but only a fabricated snapshot has that, and the assertion would pin traversal order, an accident of the stack |

Each carries a `//mutation:exempt` naming which of the two it is. **The line between them is not
rhetorical, and this repository got it wrong first: a sixth mutant was filed here as "equivalent" and
was not.** `endConn`'s `active > 0` in `internal/portforward/registry.go` was exempted on the grounds
that `endConn` is only ever called paired with `beginConn` — which is an argument about
*reachability*, and a correct one, but says nothing about equivalence. `>= 0` drives `active` to −1
on an unpaired call, `reapExpired`'s own `f.active == 0` then never matches again, and the forward
outlives its TTL for the life of the process. That is a real defect behind a real guard, and it now
has a test (`TestRegistryEndConnNeverDrivesTheCounterNegative`) rather than an exemption. The check
that catches this class: *if the mutant were reachable, would anything be wrong?* — "equivalent"
survives that question, "unreachable" does not.

**One property of the marker itself, which the `Kind` column above makes easy to misread.**
`scripts/mutation.sh` decides an exemption by file and line — it records the mutant `$type` for the
report and never compares it — so a reason written about the boundary mutant waives *every* mutant
gremlins produces on that line, `CONDITIONALS_NEGATION` included. The reason a reader sees and the
set it actually covers are not the same set.

Measured on the five sites above, nothing is currently hidden by that: each one's negation mutant is
killed by the suite independently, so the waiver covers only mutants that were dying anyway. The
`endConn` line is the useful data point rather than a counterexample — its negation mutant (`<= 0`)
was killed by the existing suite the whole time; what survived under the waiver was exactly the
boundary mutant the reason was written for. So the gap is structural, not yet load-bearing. It
matters most for stage 4, when a survivor becomes a failure: a line-scoped waiver applied to a real
finding of a different kind would then be the difference between a red gate and a green one, with
nothing in the diff to show for it.

This is the first real evidence for how much of G4(c)'s noise is irreducible rather than fixable,
which is what item 6's fourth stage — whether to make `make mutation` fail — needs before it can be
decided: **the "boundary value" row is not 45 missing tests.** Four of them cannot be killed by
anyone, one only by a fabricated input, and that count is a floor rather than a census — it is what
surfaced while working the sites #190 named, not an audit of all 45. A gate failing on that group
would demand tautologies in the very class the measurement called most actionable. The `endConn`
correction is the counterweight: a reviewer who accepts an exemption's *reason* without asking which
kind it claims will wave through real defects, so stage 4 needs the two words kept apart in the
marker itself, not just in this table.

**The red-check's first live verdict on a Go diff came from here too, and it is worth reading
carefully.** G4(b) had never judged a real pull request — #185, #186, #187 and #189 all changed no Go
implementation, so it correctly skipped every time. On this branch it ran, and **15 of 18 changed
tests survived the revert**. Nothing was wrong with them. A branch that exists to pin behavior that
is *already correct* cannot make those tests go red by reverting an implementation it never changed;
that is arithmetic, not a weak assertion. Each carries a per-test `//efficacy:exempt` with that
reason — the narrow marker `scripts/efficacy.sh` documents, not the branch-wide `efficacy-exempt`
label — and the three tests around `resolveSweepInterval` went red exactly as they should.

The lesson for stage 4 is not "the gate is too strict". It is that **G4(b) and G4(c) disagree about
this branch by construction**: mutation says these tests are the fix, the red-check says they are
out of its scope, and both are right. A rule of thumb falls out of it — a *high* survivor rate on a
branch whose diff is nearly all tests is expected; the same rate on a branch that changed real
behavior is the signal D4 was built for.

**One implementation change came out of it**, and it is the shape the repository already prefers.
`New` in `internal/portforward/registry.go` decided three things at once about `Options.SweepInterval`
— disabled, defaulted, or as given — and the boundary between the first two sits exactly at zero,
where nothing observable distinguishes them: a reaper that ticks once a minute cannot be waited on in
a test. Splitting the decision into the pure `resolveSweepInterval` made all three cases assertable,
the same move `browserOpenArgv` is split out of `openChrome` for (see
[security.md](security.md#launching-the-operators-browser---open)) and the same thing
DEVELOPMENT.md's testability rule asks for. Where a boundary is unverifiable in place, extracting the
decision is the fix; adding a test that asserts the code's own constant back to itself is not.

### Relationship to issue #164

[#164](https://github.com/tomo-chan/panemux/issues/164) proposed parsing the coverage profile per
block and failing on blocks whose summed execution count is zero. That shares G4(c)'s goal with a
different mechanism. Per-block coverage is cheap and exhaustive but still only reports *whether a
block executed*; mutation reports *whether the tests would notice it changing*, at a much higher
cost. The sensible order is therefore to land per-block coverage first and add mutation only for the
tautologies that survive it — which is why order 6 sits last.

**Landed** as `scripts/coverage_blocks.sh` / `make coverage-blocks` (row 8), with two changes from
#164's sketch, both taken from measurement: the gate is scoped to the diff (D8), and it re-reads the
profile `make coverage-go` already writes rather than running a second suite. #164's correctness
point stands and is what the script's tests pin — counts must be summed per unique block, because a
shared `-coverpkg` list emits each block once per test binary. Neither alternative #164 surveyed has
moved: [golang/go#70306](https://github.com/golang/go/issues/70306) is still an undecided proposal
and gobco still instruments one package at a time. This remains block coverage, not C1.

It closed item 6's first stage, not item 6. That item's completion condition — regression protection
measured by whether the tests would *notice a change* — is untouched by a gate that reports whether a
block *ran*. The step it unblocked was the measurement, which has now been taken: 108 mutants survive
in code this gate reports as covered (see "Surviving mutants" above). The two are complementary, and
the measurement is what sized G4(c)'s first shape.

## First measurements

Roadmap item 7 asked for observation without gating, so the point of it is the numbers.

**Read them as indicative, not as a baseline to diff against.** They come from `make bench
BENCH_ARGS='-count 5'` on one 4-core Intel Xeon @ 2.80GHz shared container, and the median is quoted
with the observed min–max beside it because that spread is the story: the `publish` rows move by up
to **2.9× between runs of the same binary on the same machine**. Anyone rerunning these and getting
a different number has not found a regression. Choosing a threshold from a single run of these
figures would produce a gate that fires on container noise, which is principle 4's failure mode
exactly — a distribution measured on dedicated hardware is what that step actually needs.

### Terminal output throughput

`managedSession.publish` is the path every byte a pane produces travels. Measured per 4KB chunk with
no subscribers, five runs:

| Replay buffer | ns/op (median) | range | B/op |
|---|---|---|---|
| Empty (cold) | 1,746 | 1,686 – 2,251 | 4,096 |
| Full (steady state) | 343,034 | 268,696 – 355,473 | ~598,000 |

**The gap — two orders of magnitude — is the trim.** `publish` retains a fixed 256KB replay window by
reallocating and copying it on every chunk, so a pane that has been open for more than a few seconds
pays a full window copy per 4KB of output, forever. A ring buffer would make it constant. That is a
real finding and it is deliberately **not** acted on here: item 7 is measurement, and a change to the
buffer belongs with the reliability tests that cover replay, not with the benchmark that spotted it.

The multiple itself is not worth quoting to two significant figures — at these spreads the same two
benchmarks support anything from ~120× to ~200×, and an earlier revision of this section said "~33×",
computed from a cold figure that the benchmark's own `b.StopTimer` calls had inflated threefold. The
finding survives that correction comfortably; the precision never existed.

Subscriber fan-out is comparatively cheap — 16 subscribers add roughly 35% to a 4KB chunk — so
many-pane cost is dominated by the per-pane buffer, not by the fan-out.

`Subscribe` (what a workspace switch pays per remounted pane) copies the whole buffer: ~1µs empty,
~141µs at the full 256KB. Both figures include the matching unsubscribe, for the reason
`BenchmarkSessionSubscribe`'s own comment records.

### Relay polling cost

Medians of five runs; these are far steadier than the `publish` rows above (≤1.1× spread).

| Operation | ns/op (median) | range |
|---|---|---|
| `AppendMessage`, at the 2000-row limit | 288 | 276 – 303 |
| `MessagesSince`, caught-up cursor | 9,171 | 9,102 – 9,258 |
| `MessagesSince`, cold start | 602,421 | 567,966 – 625,917 |
| `StatusSnapshot`, 64 panes | 9,873 | 9,575 – 10,654 |

`MessagesSince` scans the whole history even when the caller is caught up and gets nothing back,
which is the shape to watch if the history limit is ever raised.

There is no empty-history `AppendMessage` row, and an earlier revision's claim of a ~270 vs ~730
contrast between empty and full was noise read as signal. A benchmark cannot hold the cache empty:
`b.N` is millions, so the first 2000 iterations fill it and the rest measure the full case. Nor is
there anything to find — the trim is a pointer bump, and append's amortized regrow is paid alike
either way. `BenchmarkBoardCacheAppendMessage`'s comment records both halves.

### Accessibility

Excluding `.xterm` (xterm.js owns that canvas and its markup):

| Page state | critical | serious | moderate |
|---|---|---|---|
| Dashboard | 1 (`aria-required-children`) | 1 (`color-contrast`, 2 nodes) | 1 (`region`, 7 nodes) |
| Pane settings dialog open | 2 (`+ select-name`) | 1 (`color-contrast`, 10 nodes) | 1 (`region`, 7 nodes) |

The scan asserts nothing. Turning axe on over an existing UI produces a backlog, and **a gate that
starts red is routed around on day one** — taking the gates that do work with it (principle 4). The
intended next step is to freeze these counts as a ceiling that may fall but not rise, which is a
threshold chosen from data rather than guessed at.

## Related documents

- Developer workflow and the TDD rules this sits on top of: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- Use-case scenario coverage map: [scenarios.md](scenarios.md)
- CI and release maintenance: [maintenance.md](maintenance.md)
- Security requirements for implementation: [security.md](security.md)
- Architecture and security rationale: [architecture.md](architecture.md)

## Sources

- [ISO/IEC 25010:2023 — Product quality model](https://www.iso.org/standard/78176.html); nine-characteristic summary at [Sonar](https://www.sonarsource.com/resources/library/iso-iec-25010-explained/) and [arc42](https://quality.arc42.org/standards/iso-25010)
- Vladimir Khorikov, *Unit Testing: Principles, Practices, and Patterns* — [the four pillars, author's infographic](https://khorikov.org/files/infographic.pdf); summary at [Samman Coaching](https://www.sammancoaching.org/learning_hours/test_design/four_pillars_khorikov.html)
- [DORA 2025 — State of AI-assisted Software Development](https://cloud.google.com/blog/products/ai-machine-learning/announcing-the-2025-dora-report) ([report PDF](https://services.google.com/fh/files/misc/2025_state_of_ai_assisted_software_development.pdf))
- [Best practices for Claude Code](https://code.claude.com/docs/en/best-practices); [building verification loops with skills](https://claude.com/blog/building-verification-loops-in-claude-code-with-skills)
- Failure modes of AI-written tests: [tests that pass without asserting](https://getautonoma.com/blog/ai-generated-tests-pass-but-dont-assert), [the tautological anti-pattern](https://getautonoma.com/blog/useless-unit-tests-tautological-anti-pattern), [high coverage is not test quality](https://techdebt.guru/ai-testing-gaps/)
- [GitHub Spec Kit — spec-driven development](https://github.com/github/spec-kit)
- [gremlins — mutation testing for Go](https://github.com/go-gremlins/gremlins); [go-mutesting](https://github.com/jonbaldie/go-mutesting) for MSI gates and diff mode
- [Characterization / golden master testing](https://en.wikipedia.org/wiki/Characterization_test)
