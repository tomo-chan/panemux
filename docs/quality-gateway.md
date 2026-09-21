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
| Interaction capability | Keyboard operation, focus restoration, legibility, notifications | Three focus-restoration E2E tests, plus an axe-core scan of the dashboard and of a modal dialog (`frontend/e2e/a11y.spec.ts`) holding each to a per-rule **ceiling** that may fall and must not rise | Thin, measured and now held |
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
| **P2 — Humble object** | Keep the side-effecting boundary (PTY, SSH, `exec`, filesystem, network) as thin as possible; put decisions in pure functions. | Works: `validRemotePath`, `classifySSHWaitError`, `frontend/src/utils/layoutTree.ts`, and `internal/session`'s post-connect lifecycle seams, which run over an in-process SSH server or injected tmux command. |
| **P3 — Injection points are typed** | Do not call globals, environment variables or `os.UserHomeDir()` directly; make them a struct field, a constructor argument, or — where the caller is a package-level function with no object to hang it on — a named seam with an override, as `internal/homedir` is. | Already stated in [docs/development/test-granularity.md](development/test-granularity.md#test-granularity)'s testability rule (`Config.sshConfigPath`). `os.UserHomeDir` is the measured case of the principle being *stated and not followed*: [#212](https://github.com/tomo-chan/panemux/issues/212) counted 16 direct calls across nine packages against one seam, with 12 test files reaching for `t.Setenv("HOME", ...)` instead. It is now one seam (`internal/homedir`) that `.golangci.yml`'s `forbidigo` rule keeps single — the difference between the principle and a check, and the reason the count could reach 16 unnoticed. Applied to `main.go` as of rollout item 2: `parseOptions` takes its arguments, `loadConfig` takes a `configLoader`, `startSessionsFromConfig` takes the session factory, and `openChrome`'s per-OS decision is the pure `browserOpenArgv`. The root package went 0% to 75.1% overall, `board.go` 47.8% to 76.9% and `main.go` 0% to 38.8% — what is left in `main.go` is `main()` and `runServer()` themselves, which install signal handlers and run for the life of the process. |
| **P4 — Name the observable behavior** | What a test drives is a published contract with domain vocabulary — HTTP responses, WS frames, persisted files, rendered output — not call ordering or mock setup. | `frontend/src/hooks/useTerminalLinks.test.ts` is the model: it drives the real xterm terminal and the real addon, and is named after the behavior rather than a source file. |
| **P5 — Deliberate implementation pinning is documented** | A test that pins implementation shape in violation of P4 is allowed only when a document states why. | `TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection` pins exact argv, and [security/command-center.md](security/command-center.md#command-center-subprocess-execution) explains why. That is the correct use of the exception. |

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
| **G0** | Spec | Functional suitability (rework) | (a) The change is tied to a row in [scenarios.md](scenarios.md). A user-visible change adds or updates a row in the same commit. (b) The documentation itself holds together: every relative link resolves, every `#fragment` matches a real heading, and no label names a file other than the one it opens. | (a) CI: fail when the diff touches `frontend/src`, `internal/api` or `internal/config` and `scenarios.md` is unchanged; a label grants explicit exemption. (b) `make check-docs-links`, in `make check` and in CI | Present — (a) `.github/workflows/scenarios.yml`, exempted by the `scenarios-exempt` label; (b) `scripts/docs_links_check.sh`, added after #248 split five documents and shipped two broken anchors and 41 misdirecting labels under a description that said every link had been checked by hand |
| **G1** | Edit | Maintainability | `gofmt -s`, `tsc --noEmit`, and `go vet` on the touched packages only | Claude Code `PostToolUse` hook in `.claude/settings.json` | Present — `.claude/hooks/post-edit-check.sh` |
| **G2** | Unit | Functional suitability, fast feedback | `make test-go`, `make test-frontend` — unchanged | Existing (`make check`, pre-push, CI) | Present |
| **G3** | Contract | **Resistance to refactoring**, compatibility | (a) HTTP/WS integration through the real `server.New()` router; (b) exhaustiveness check on the route table (every registered route against an expected set); (c) Zod schema round-trips; (d) the agmsg contract | Always-on Go and vitest tests, in `make check` | **Present.** (a) `internal/server/api_integration_test.go` drives every `/api` route and fails when one has no case; `ws_integration_test.go` drives both `/ws` routes over a real handshake. (b) and (d) unchanged. (c) `contract_fixture_test.go` captures real responses into `testdata/api-contract/`, which `frontend/src/schemas/contract.test.ts` parses with the schema that owns each one. (e) added by #168: `internal/board/ledger_conformance_test.go` replays the real `ownSendLedger`'s own transitions against a transition table TLC exported from `spec/agentboard/OwnSendLedger.tla` — a contract between the code and a formal model rather than between two processes, but the same shape and the same tier split (see D12) |
| **G4** | Efficacy | **Protection against regressions** | (a) coverage — scope tracks the implementation, threshold stays at 80%; (b) **red-check**: a changed test must fail when the implementation diff is reverted; (c) mutation score over changed lines only; (d) **per-block coverage**: no block covering a changed line may be one the suite never entered | (a) existing `make check`; (b), (c) and (d) pull-request CI jobs, `make efficacy`, `make mutation` and `make coverage-blocks` | **Present** — (a) present and now scoped to every decision-holding package; (b) present (`make efficacy`, a pull-request-only CI job); (d) present (`make coverage-blocks`, likewise); (c) present **as a gate** (`make mutation`, likewise): a mutant on a changed line that survives, or that reaches no verdict, exits 1 — see D9 |
| **G5** | Scenario | Functional suitability, interaction capability | Playwright E2E, plus a check that every test named by an `auto` row in `scenarios.md` actually exists, plus an axe-core ceiling per page state | CI, extending `make test-e2e` | Present — E2E plus `make check-scenarios`, which resolves every `auto` row, plus `a11y.spec.ts`, which fails when a violation count rises above the frozen current value (D11), its comparator unit-tested in `a11y-ceiling.test.ts` |
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

### Rollout order

| Order | Work | Gate | #178 phase | Effect |
|---|---|---|---|---|
| 1 | Real-router integration harness; single source of truth for the route table plus an exhaustiveness check | G3 | Phase 1 | **Landed.** The table has one definition, the exhaustiveness check pins it, and every `/api` route is now driven through `server.New()` — itself exhaustive across both command-center states, so a new route has no test until someone writes one, and a case that stops asserting a success path has to say so. Both `/ws` routes followed in #191, over real handshakes on a real listener, including that `/ws/board-command` is *absent* rather than rejecting when the command center is off. |
| 2 | Widen coverage scope (threshold unchanged) | G4(a) | Phases 2 and 5 | **Landed.** `internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, the root package and `frontend/src/utils/**` are now gated. Go reports 86% over the wider set (it was 88% over the narrower one — the drop is the point), frontend 95%; the threshold is unchanged at 80%. |
| 3 | `.claude/settings.json` with G1/G2 hooks; a review subagent | G1, G6 | — | **Landed.** A `PostToolUse` hook checks the edited file, a `Stop` hook checks what the turn changed, and `.claude/agents/diff-reviewer.md` reviews a diff in a fresh context. `make test-hooks` tests the hooks themselves. |
| 4 | red-check (`make efficacy`) in pull-request CI | G4(b) | — | **Landed.** `scripts/efficacy.sh` reverts the branch's implementation diff in a scratch worktree and requires each test the branch changed — Go function or vitest case — to pass at HEAD and then go red against the revert, one at a time. Exempted by the `efficacy-exempt` label. |
| 5 | Core-feature section in `scenarios.md`, ledger cross-check, core E2E | G0, G5 | Phases 4 and 6 | **Landed.** Sections H (core multiplexer) and I (opening URLs from a pane, #177's missing rows) added; `make check-scenarios` resolves every `auto` row; `frontend/e2e/core-multiplexer.spec.ts` covers split, resize, layout restore and workspace CRUD. |
| 6 | Diff-scoped mutation testing (warn first, gate once stable) | G4(c) | merges with #164 | **Done.** `scripts/mutation.sh` runs gremlins scoped to the diff and names every mutant on a changed line that survives every test, plus every mutant that reached no verdict at all. It warned for three stages and now **exits 1 on either** — see D9 for what had to be measured first. |
| 7 | Performance and accessibility observation (measure only, do not gate) | — | — | **Landed.** `make bench` measures terminal throughput, replay-buffer cost and the relay's polling cost; `a11y.spec.ts` records axe violations. The performance half still only reports — its spreads are too wide for a threshold (see [measurements.md](quality-gateway/measurements.md)). The accessibility half now asserts: #194 froze the recorded counts as a ceiling, which is the step this row deferred until data existed. |
| 8 | Per-block coverage on changed lines (#164, not a #180 item) | G4(d) | — | **Landed.** `scripts/coverage_blocks.sh` fails when a block covering a changed line never executed. It unblocked row 6's measurement, which is what row 6 was waiting on. |
| 9 | Zod schema round-trips against real Go output (#191, closing G3(c)) | G3 | Phase 1 | **Landed.** `internal/server/contract_fixture_test.go` captures every response the dashboard parses, plus both WebSocket frame streams, into `testdata/api-contract/`; `frontend/src/schemas/contract.test.ts` parses each with the schema that owns it and requires the parsed value to equal the captured one, so a field Zod *strips* fails too. Decision D10 records why the fixtures are rewritten rather than diffed. |
| 10 | TLA+ model checking of `internal/board`'s state machines, piloted on `ownSendLedger` (#168) | G3 | — | **Landed for the pilot.** `spec/agentboard/OwnSendLedger.tla` plus a checked-in transition table TLC exports; `internal/board/ledger_conformance_test.go` replays the real ledger — including through `Relay.Broadcast`/`Poll` — against it inside `make check`, and `make model-check` keeps the table honest about the spec from a path-filtered CI job. `Relay.processRow` is the next candidate now that the scaffolding is settled; `dynamicBoardExecutor` is search-and-retry rather than a state machine and is left out. See D12. |

## Document map

This document carries what the gates are for and how they are arranged — the principles, the gate
table, and the enforcement ladder that the rest of the repository cites by number. The long-form
material behind them lives in [`docs/quality-gateway/`](quality-gateway/):

| Sections | Document |
|---|---|
| Design decisions D1–D12 | [decisions.md](quality-gateway/decisions.md) |
| Surviving mutants: the first measurement | [mutants.md](quality-gateway/mutants.md) |
| First measurements; Accessibility | [measurements.md](quality-gateway/measurements.md) |

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
