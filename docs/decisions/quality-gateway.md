---
type: Decision
title: "Quality gateway decisions"
description: "Design rationale for quality gateway decisions D1–D14, checkout-local lint caches, and worktree-aware agent hooks."
---

# Quality gateway decisions

Current gates and contracts live in [Quality gates](../engineering/quality-gates.md) and
[DEVELOPMENT.md](../../DEVELOPMENT.md).

## D1: Do not raise the coverage threshold above 80%

Recorded 2026-08-28. Raising coverage invites tautological tests that meet a number while reducing
regression protection and refactoring resistance. Keep 80% as a lower bound and strengthen measured
scope. See [Coverage gates](../engineering/quality-gates.md#coverage-gates).

## D2: Mutation testing is scoped to the diff and to pull requests

Recorded 2026-08-28. Whole-repository mutation gates start red on existing survivors and false
positives. Diff and PR scope asks authors about changes they can act on. Faster tooling (a
whole-module run took ~14 minutes, not hours) does not eliminate the existing-survivor problem.
See [Mutation testing](../engineering/quality-gates.md#mutation-testing-make-mutation).

## D3: Consolidate gates rather than adding them

Recorded 2026-08-28. Independent route-test inventories let new routes be forgotten in several
places. One route authority plus an exhaustive integration check removes the forgetting path instead
of adding another gate.

## D4: Verify "the test came first" after the fact (red-check)

Recorded 2026-08-28. TDD chronology cannot be reconstructed reliably, so changed tests are verified
by passing at `HEAD` and failing when the implementation diff for their stack is reverted. Per-case
verdicts (via Go test names and frontend JSON reports) stop one honest test from vouching for
tautological siblings. Benchmarks are excluded (a branch with only benchmarks warns rather than
proving anything); unbuildable `HEAD`, runs where every selected test is skipped or unrunnable,
missing prerequisites, and unavailable worktrees fail; implementation-only refactors warn; per-test
and PR-label exemptions remain explicit and visible. See [Red-check](../engineering/quality-gates.md#red-check-make-efficacy).

## D5: The author does not grade its own work

Recorded 2026-08-28. Authors retain discarded approaches in context; fresh reviewers read the diff
without that bias. Review stays non-blocking because adversarial reviewers can invent gaps and
encourage unnecessary abstraction or impossible-case tests.

## D6: The Stop hook does not run everything

Recorded 2026-08-28. Running all of `make check` on every Stop sacrifices fast feedback and
encourages bypass. Keep turn-local formatting and targeted tests in the Stop hook; full-suite
verification belongs to CI or an explicit manual run (see [D14](#d14-selective-pre-push-checks-and-full-ci)).

## D7: The ledger is cross-checked, not just required

Recorded 2026-08-30. A required scenario row can still go stale when its test moves or disappears.
`make check-scenarios` resolves claimed paths and Go test names to catch stale coverage, with
regression tests for path-like false positives. See [Scenario coverage](../scenarios.md).

## D8: Per-block coverage gates the diff, not a baseline

Recorded 2026-08-30. Repository-wide zero-block gates start red on existing uncovered blocks and
flake on concurrent blocks. Diff-scoped per-block coverage starts green without maintaining a
baseline ceiling; excluded packages report not measured rather than covered, and inline exemptions
require reasons. See [Coverage gates](../engineering/quality-gates.md#coverage-gates).

## D9: Mutation findings require complete, actionable verdicts

Recorded 2026-09-15. Mutation became blocking after three warning stages established typed,
reasoned waivers (so tuning constants and unreachable error paths do not force tautologies),
explicit classification of every status (`KILLED`, `NOT COVERED`, `NOT VIABLE`, `SKIPPED`,
`TIMED OUT`, `RUNNABLE`), an explicit measured denominator, and pinned workers/timeouts to prevent
contention from hiding survivors. When scoped mutants exist and every one is undecided, uncovered,
or skipped, the run fails unless every scoped mutant carries a complete typed, reasoned waiver;
having no changed implementation or zero scoped mutants is a legitimate no-measurement outcome and
is reported explicitly without failing.

The initial whole-module measurement (`d42e406`) found 108 surviving mutants in code reported as
covered by per-block coverage (45 boundary values, 26 other logic, 25 error branches, 12 tuning
constants), proving execution coverage and mutation testing are complementary. Clearing boundary
survivors (issue #190) established the equivalent-versus-unreachable check (*"if the mutant were
reachable, would anything be wrong?"*) after an `endConn` guard (`active > 0`) initially marked
equivalent turned out to protect a real defect, and motivated type-scoped waivers because marked
lines routinely carry additional mutant types. See
[Mutation testing](../engineering/quality-gates.md#mutation-testing-make-mutation).

## D10: Contract fixtures are rewritten on every run, not diffed against

Recorded 2026-09-03. Golden comparison in Go stops before stale frontend schemas see a changed
response. Go rewrites captured fixtures before frontend Zod validation, and parsed output must
equal the capture so renamed optional keys are not silently stripped. Re-capture tests verify
absence of machine paths and deterministic ordering.
See [API contract fixtures](../engineering/testing.md#api-contract-fixtures-testdataapi-contract).

## D11: The accessibility ceiling is per rule, counts nodes, and is lowered by hand

Recorded 2026-09-03. Total violation counts let one fix hide a new violation kind. Per-rule node
ceilings run against an isolated server/fixture so other E2E tests cannot perturb counts, and lower
counts pass with manual lowering instructions rather than failing on improvements.
See [Accessibility ceilings](../engineering/quality-gates.md#accessibility-ceilings).

## D12: A model checker's output is checked in; the checker itself is not in `make check`

Recorded 2026-09-21. Checking in TLC's full transition table lets hermetic Go tests replay
implementation conformance without requiring a JDK/jar in `make check`, while avoiding a
handwritten reference model that could drift. A separate job checks spec/export drift, and the
per-key bound comes from configuration (not observed states) so truncated exports are rejected.
See [State-machine model checking](../engineering/testing.md#state-machine-model-checking).

## D13: Capability probes skip locally and fail in CI

Recorded 2026-10-08, issue #315. The macOS Claude sandbox can deny PTYs, sockets/tmux, `ps`, and
`dscl`, while broad socket or git-config permissions were rejected as unsafe. Tests probe
capabilities once and skip with actual errors locally, while CI fails on the same missing
capability. Screenshot capture and temporary-file allocation fail closed everywhere.
See [Claude Code sandbox](../engineering/runtime-and-sandbox.md#claude-code-sandbox) and
[Temporary-file discipline](../../DEVELOPMENT.md#temporary-file-discipline).

## D14: Selective pre-push checks and full CI

Recorded 2026-10-09, issue #335. Running the full suite on every push penalized small changes. The
pre-push hook checks only the pushed range (falling back to `make check` whenever scope cannot be
narrowed safely), while CI and manual `make check` own the full suite, race detection, and
importers. See [Pre-push check selection](../engineering/quality-gates.md#pre-push-check-selection).

## Benchmark measurements inform allocation tests rather than timing gates (2026-09, issue #193)

Repeated `make bench` runs on identical binaries varied by up to ~2.9× on shared containers, so
wall-clock timing thresholds would create a noisy gate. Replacing the slice replay buffer with a
ring (`internal/session/replay_buffer.go`) eliminated ~598 KB/op of steady-state copy allocations;
regression protection relies on deterministic zero-allocation unit tests
(`TestManagedSession_Publish_SteadyStateDoesNotCopyTheReplayWindow`,
`TestReplayBuffer_SteadyStateAppendDoesNotAllocate`) rather than timing baselines. See
[Performance benchmarks](../engineering/quality-gates.md#performance-benchmarks).

## golangci-lint caches inside each checkout (2026-10-09, issue #325)

`make lint-go` derives `GOLANGCI_LINT_CACHE` from the checkout's own `.cache/golangci-lint/` and
overrides any inherited environment variable so concurrent worktrees cannot share stale findings or
rely on unpredictable sandbox fallbacks. Failure to create the cache directory errors out.

## Agent hooks check the checkout that holds the change (2026-10-11, issue #350)

Hooks may run from `main` while edits belong to a worktree. Git top-level/prefix resolution selects
the edited checkout (requiring the same common `.git` directory; unrelated repositories get
formatting only), and streaming `gofmt` avoids treating a worktree `.git` file as a directory.
Branch-owned hook delegation was rejected so checks cannot be altered by the branch under review.
