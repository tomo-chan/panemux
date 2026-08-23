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
| Functional suitability | Panes, workspaces, session types and config read/write behave as specified | Unit tests (thick); E2E covers 6 core tests | Uneven |
| Reliability | Recovery from WS disconnect, replay, session exit, SSH reconnect | `internal/ws` unit tests plus the Alloy model in `docs/models/replay_state.als` and `model-check.yml` | Strong |
| Security | Command injection, bearer token, DNS rebinding, subprocess containment | [security.md](security.md) plus regression tests verified against real binaries, `gosec` | Very strong |
| Compatibility | The agmsg script contract, config schema back-compat | Tier 1 fixtures, Tier 2 tests against a real agmsg install, daily canary | Very strong |
| Maintainability | Refactoring does not break tests; behavior changes do | `golangci-lint` (20+ linters), 80% coverage. **Nothing measures the tests themselves** | Unprotected |
| Interaction capability | Keyboard operation, focus restoration, legibility, notifications | Three focus-restoration E2E tests. No accessibility checks | Thin |
| Performance efficiency | Terminal output throughput, relay polling cost, many-pane rendering | None | Unprotected |
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
| Maintainability | The tests are themselves readable and durable | Test volume, duplication | Was 4 router copies and 3 `*_routes_test.go` files; the copies are gone and the per-route files are consolidated |

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
| **P3 — Injection points are typed** | Do not call globals, environment variables or `os.UserHomeDir()` directly; make them a struct field or constructor argument. | Already stated in [DEVELOPMENT.md](../DEVELOPMENT.md#test-granularity)'s testability rule (`Config.sshConfigPath`). Not applied to `main.go` (0%) or `board.go` (47.8%). |
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
| [Claude Code best practices](https://code.claude.com/docs/en/best-practices) | Give the agent a check it can run, or "looks done" is the only signal available. Enforcement escalates: in-prompt → `/goal` → **Stop hook (deterministic gate)** → verification subagent. TDD is the strongest pattern. Do not let the author grade its own work. | This repository has no `.claude/` directory: zero hooks, skills or subagents. Every agent-side guard is an advisory document. |
| Reported failure modes of AI-written tests | When the same model writes the code and the test, a bug becomes the expected value — a tautological test. Related shapes: asserting a mock's own return value, exact-string matching, order dependence, hard-coded internal helper output. | These break Khorikov's first *and* second pillars simultaneously, and nothing in this repository detects them. |
| [Spec-driven development](https://github.com/github/spec-kit) | A *constitution* of non-negotiable principles is written once and referenced by every later phase (specify → plan → tasks → implement). | panemux already has this shape: `AGENTS.md` → `DEVELOPMENT.md` / `docs/*`, with [scenarios.md](scenarios.md) as the acceptance ledger. What is missing is enforcement. |
| [gremlins](https://github.com/go-gremlins/gremlins) and Go mutation testing | Coverage is unreliable as a measure of test quality. gremlins targets small-to-medium modules; a run over a large one can take hours. Diff mode, scoped to new and changed code, is the practical form. | Whole-repository MSI is not a viable target. Scope it to the diff and to pull requests. |

### Where panemux stands

| | Present | Missing |
|---|---|---|
| **Constitution** | `AGENTS.md` indexing `DEVELOPMENT.md` and seven `docs/` files. TDD, test granularity, schema-first and path sanitization are all written down. | — |
| **Spec** | [scenarios.md](scenarios.md): a use-case ledger with `auto` / `auto (opt-in)` / `manual`, which states that a silently absent row is not a legitimate answer. | No rows at all for the core multiplexer (panes, workspaces, terminal). PR #177 added a user-visible feature and did not add rows either. |
| **Verification** | One command, `make check`. Enforced by `.githooks/pre-push`, re-run in CI. Alloy model checking. Tier 2 agmsg contract plus a daily canary. | Nothing measures the *efficacy* of the tests. Coverage only reports that a line executed. |
| **Agent guard** | — | No `.claude/`. The TDD rule ("write tests first, confirm they fail") is stated but **cannot be verified after the fact**. |

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
| **G0** | Spec | Functional suitability (rework) | The change is tied to a row in [scenarios.md](scenarios.md). A user-visible change adds or updates a row in the same commit. | CI: fail when the diff touches `frontend/src`, `internal/api` or `internal/config` and `scenarios.md` is unchanged; a label grants explicit exemption | Absent |
| **G1** | Edit | Maintainability | `gofmt -s`, `tsc --noEmit`, and `go vet` on the touched packages only | Claude Code `PostToolUse` hook in `.claude/settings.json` | Absent |
| **G2** | Unit | Functional suitability, fast feedback | `make test-go`, `make test-frontend` — unchanged | Existing (`make check`, pre-push, CI) | Present |
| **G3** | Contract | **Resistance to refactoring**, compatibility | (a) HTTP/WS integration through the real `server.New()` router; (b) exhaustiveness check on the route table (every registered route against an expected set); (c) Zod schema round-trips; (d) the agmsg contract | New `make test-contract`, folded into `make check`; (b) as an always-on Go test | Partial — (b) and (d) present, (a) and (c) absent |
| **G4** | Efficacy | **Protection against regressions** | (a) coverage — scope tracks the implementation, threshold stays at 80%; (b) **red-check**: a changed test must fail when the implementation diff is reverted; (c) mutation score over changed lines only | (a) existing `make check`; (b) and (c) a pull-request CI job, `make efficacy` | Partial — (a) and (b) present (`make efficacy`, a pull-request-only CI job); (c) absent |
| **G5** | Scenario | Functional suitability, interaction capability | Playwright E2E, plus a check that every test named by an `auto` row in `scenarios.md` actually exists | CI, extending `make test-e2e` | Partial (E2E only; no ledger cross-check) |
| **G6** | Adversarial | All characteristics (design judgement) | A fresh-context review of the diff alone. The session that wrote the code does not grade it. Findings limited to correctness and stated requirements. | A review subagent in `.claude/agents/` plus human review. **Does not block** | Absent |

### The enforcement ladder

This is the most reusable rule in the document. **Promote every discipline you want kept one rung
up.** A rule sitting on the bottom two rungs must not be described as "enforced".

| Rung | Mechanism | Example here |
|---|---|---|
| L0 | Written in a document (advisory) | DEVELOPMENT.md's TDD rule is here today. Both humans and agents read it; nobody can confirm it was followed. |
| L1 | A Makefile target | Runnable by hand, produces pass/fail. The first rung an agent can drive itself. |
| L2 | An agent hook | Claude Code `PostToolUse` / `Stop` hooks. Unlike `AGENTS.md`, deterministic and impossible to forget. |
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

**D3 — Consolidate gates rather than adding them.**
As #178 showed, a structure where every new feature adds another `*_routes_test.go` is a tax, not a
gate; there are already three. A single source of truth for the route table plus one exhaustiveness
check means adding a route forces the test to be updated — it removes the possibility of forgetting
rather than detecting it afterwards.

**D4 — Verify "the test came first" after the fact (red-check).**
The TDD rule is stuck at L0 and cannot be checked directly. The *result* can be: **a changed test
must fail when the implementation diff is reverted.** This is the strongest possible mutation (remove
the implementation entirely), and it should cost far less than general mutation testing — it runs
the changed tests twice rather than once per generated mutant, though neither cost has been measured
here — while catching both tautological tests and tests written after the fact. That combination is
why it is ordered ahead of mutation testing in the rollout; it is not a claim that nothing else
would catch more.

Implemented as `scripts/efficacy.sh` / `make efficacy`. Three details of it are decisions in their
own right, recorded because each one was a way the gate could have been useless:

- **Scope is the changed test *functions*, not the changed packages.** The script maps the diff's
  touched line numbers onto the function ranges in the file, so editing an assertion inside an
  existing test brings that test into scope, and appending a new test does *not* drag the untouched
  one above it in. That second half matters more than it looks: the gate is satisfied when the whole
  `-run` set goes red, so an over-wide set lets an unrelated failure mask a tautological test. The
  first implementation had exactly that bug, found by a test written for it.
- **Skipping is as important as failing.** A branch that changes no implementation (docs, tests
  only) has nothing to revert, and a branch that changes implementation but no test has no result to
  check — the latter warns rather than blocking, because making it a failure would fire on every
  pure refactor. Principle 4 again.
- **The escape hatch is a pull-request label, not a config file.** `efficacy-exempt` is visible in
  the same place the reviewer sees the diff. A change whose tests genuinely should not go red — a
  pure refactor, a test-only rename — is a real category, and hiding the exemption in a tracked file
  would make it invisible after the fact.

**D5 — The author does not grade its own work.**
A session carries the context of the approaches it tried and discarded. The Claude Code guidance
recommends a review in a fresh context that sees only the diff. G6 nonetheless **does not block**: a
reviewer asked to find gaps will report some even when the work is sound, and blocking on that
invites over-engineering — extra abstraction, defensive code, tests for cases that cannot occur.

**D6 — The Stop hook does not run everything.**
Claude Code's `Stop` hook can deterministically block a turn from ending, but putting all of
`make check` there would run E2E on every turn. Put G1 and G2 (seconds) there and leave G3 onward to
pre-push and CI. A gate that sacrifices fast feedback gets bypassed.

### Rollout order

| Order | Work | Gate | #178 phase | Effect |
|---|---|---|---|---|
| 1 | Real-router integration harness; single source of truth for the route table plus an exhaustiveness check | G3 | Phase 1 | **Landed.** The table has one definition and the exhaustiveness check pins it; the full HTTP/WS integration half is still open. |
| 2 | Widen coverage scope (threshold unchanged) | G4(a) | Phases 2 and 5 | Makes ~2,600 previously ungated lines visible. |
| 3 | `.claude/settings.json` with G1/G2 hooks; a review subagent | G1, G6 | — | Promotes L0 discipline to L2. Closes the agent's own verification loop. |
| 4 | red-check (`make efficacy`) in pull-request CI | G4(b) | — | **Landed.** `scripts/efficacy.sh` reverts the branch's implementation diff in a scratch worktree and requires the tests the branch changed to go red against it. Exempted by the `efficacy-exempt` label. |
| 5 | Core-feature section in `scenarios.md`, ledger cross-check, core E2E | G0, G5 | Phases 4 and 6 | Makes the acceptance ledger real. |
| 6 | Diff-scoped mutation testing (warn first, gate once stable) | G4(c) | merges with #164 | Measures protection against regressions directly. |
| 7 | Performance and accessibility observation (measure only, do not gate) | — | — | First visibility into the two unprotected characteristics. |

### Relationship to issue #164

[#164](https://github.com/tomo-chan/panemux/issues/164) proposes parsing the coverage profile per
block and failing on blocks whose summed execution count is zero. That shares G4(c)'s goal with a
different mechanism. Per-block coverage is cheap and exhaustive but still only reports *whether a
block executed*; mutation reports *whether the tests would notice it changing*, at a much higher
cost. The sensible order is therefore to land per-block coverage first and add mutation only for the
tautologies that survive it — which is why order 6 sits last.

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
