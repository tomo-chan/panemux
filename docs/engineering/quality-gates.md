---
type: Guide
title: "Quality gates"
description: "Quality model, testability principles, verification gates, coverage, mutation, red-check, pre-push selection, and accessibility ceilings."
---

# Quality gates

This guide defines panemux's quality model, verification gates, and enforcement rules. Use
[DEVELOPMENT.md](../../DEVELOPMENT.md) for daily commands, [Scenario coverage](../scenarios.md) for
the user-facing acceptance ledger, and [Quality gateway decisions](../decisions/quality-gateway.md)
for the rationale behind decisions D1–D14.

## Quality model and principles

Panemux treats product quality and test quality as separate axes. Passing tests must protect the
observable product contract while resisting refactoring churn. Coverage alone is never treated as
proof that assertions detect defects.

| Product characteristic | Primary protection |
|---|---|
| Functional suitability | Scenario ledger, Go/frontend tests, and browser E2E |
| Reliability | Session/WebSocket lifecycle tests, replay tests, and state-machine conformance |
| Security | Per-sink security requirements, regression tests, `gosec`, and lint |
| Compatibility | Config migration tests, Go-to-Zod contract fixtures, and the two-tier agmsg contract |
| Maintainability | Lint, typed seams, single-source wiring, coverage scope, red-check, and mutation testing |
| Interaction capability | Component tests, keyboard/focus E2E, and axe-core ceilings |
| Performance efficiency | `make bench` observations (no timing gate; prefer allocation/shape unit tests) |
| Flexibility | Config compatibility and platform-aware session tests; some environment coverage remains manual |
| Safety | Tests around PTY writes, bootstrap retries, persistence, and remote side effects |

| Principle | Current rule |
|---|---|
| **P1 — Single source of truth** | Production wiring is defined once. Tests drive that wiring instead of reconstructing it. |
| **P2 — Humble side-effect boundary** | PTY, SSH, process, filesystem, and network boundaries stay thin; decisions move into deterministic functions. |
| **P3 — Typed injection points** | Global lookups and effects use constructor fields or named seams (`internal/homedir`, `internal/cachedir`, `internal/fileops`). |
| **P4 — Observable behavior** | Tests assert HTTP responses, WebSocket frames, persisted files, rendered UI, and other published behavior rather than helper call order. |
| **P5 — Explicit implementation pinning** | A test may pin an implementation shape only when a security or compatibility document explains why that exact shape is part of the contract. |

## Gates and enforcement ladder

Cheaper checks reject defects before expensive checks run:

| # | Gate | Checks | Enforcement |
|---|---|---|---|
| **G0** | Spec | User-visible changes update `scenarios.md`; screenshot-visible UI changes retake images; links/anchors resolve; OKF metadata/indexes are valid | Scenario & screenshots CI, `make check-docs-links`, `make check-docs-okf` |
| **G1** | Edit | `gofmt -s`, targeted `go vet`, or `tsc --noEmit` for an edited file | `.claude` post-edit hook |
| **G2** | Unit | Go and frontend unit/integration suites | `make check` and CI; pre-push runs changed packages/modules only (D14) |
| **G3** | Contract | Real-router HTTP/WS tests, exhaustive route expectations, Go-produced Zod fixtures, agmsg contract, and model-to-code transition replay | Always-on tests in `make check`; real agmsg and TLC in dedicated CI/opt-in jobs |
| **G4** | Efficacy | Coverage scope and 80% threshold, changed-test red-check, diff-scoped mutation, and changed-block coverage | `make check` plus pull-request CI jobs |
| **G5** | Scenario | Playwright workflows, scenario-reference validation, and accessibility ceilings | `make test-e2e`, `make check-scenarios`, and CI |
| **G6** | Adversarial review | Fresh-context review of the diff for correctness and stated requirements | Review agent plus human review; advisory, not a merge gate |

Enforcement levels: **L0** Documentation (advisory) → **L1** Make target → **L2** Agent hook →
**L3** Git hook (`pre-push`) → **L4** CI job → **L5** Branch protection. `make check` remains
hermetic; checks needing real SSH/tmux/agmsg, network access, or a JDK use opt-in targets or CI.
Capability probes (`internal/testcap`) skip locally when a sandbox denies PTY/tmux/`ps`/`dscl` and
fail when `CI` is set ([D13](../decisions/quality-gateway.md#d13-capability-probes-skip-locally-and-fail-in-ci)).

- **Toolchain consistency**: Gates use exact versions from `go.mod` and `.node-version`
  ([Runtime and sandbox](runtime-and-sandbox.md)); `make test-go-toolchain` and
  `make test-node-toolchain` run in `make check` and CI.
- **Local verification wrappers**: `make verify-*` preserves original gate exit codes, stores
  checkout-local logs, and requires a valid saved PR base for diff gates (`make test-verify`;
  [Codex verification](codex-verification.md)).
- **agmsg proposal automation**: `make test-agmsg-automation` runs offline Python tests in
  `make check` and CI; live App publishing stays an operational check
  ([agmsg maintenance](agmsg-maintenance.md#the-agmsg-compatibility-contract-job)).

### The fixture agent's tagged build

A third agent (`panemux-fixture-agent`) is registered only with `-tags panemux_fixture_agent`,
never in shipped binaries (issue #377). `make test-go-fixture-agent` (in `make test-go` and CI)
runs its tests, registry-wide adapter contract tests (`*_MatchTheRegistry`, workdir, summary,
collection prefilter), HTTP route handling, and captures `tasks-fixture-agent.json`. It fails if
any test named in `FIXTURE_AGENT_REQUIRED` does not run or a package matches none.

`make lint-go` vets both builds and golangci-lint reads tagged files; `pre-push` vets and tests
touched tagged packages. Coverage, red-check, mutation (which names ignored tagged files), and the
Claude Code `PostToolUse` hook operate on the untagged build. Task-dashboard E2E runs the tagged
build with a fixture `HOME` and stand-in `ps`/`lsof`/`claude`/`codex` commands that expose only
fixture descendants and verify a detached simulated developer session is never listed
(`make test-screenshots-check`). See [Adding an agent](../backend/agent-registry.md#adding-an-agent).

## Coverage gates

- `make coverage-go` enforces ≥80% combined statement coverage across `Makefile`'s `COVERAGE_PKGS`
  (building the frontend first because `main.go` embeds `frontend/dist`), with a 120-second timeout
  per Go package. `make coverage-frontend` enforces ≥80% across `frontend/src/hooks/`, `schemas/`,
  and `utils/`.
- The threshold stays at 80% ([D1](../decisions/quality-gateway.md#d1-do-not-raise-the-coverage-threshold-above-80));
  `TestCoverageScopeCoversEveryPackage` fails whenever a new Go package is neither gated nor
  explicitly excluded with a recorded reason beside `COVERAGE_PKGS` in `Makefile`.
- `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks` (PR-only, [D8](../decisions/quality-gateway.md#d8-per-block-coverage-gates-the-diff-not-a-baseline))
  re-reads `make coverage-go`'s profile and fails on any unexecuted block covering a changed line.
  Unreachable blocks require `//coverage:exempt <reason>` on the opening line or line above (prefer
  the inline marker over the `coverage-blocks-exempt` label). Excluded packages report **not
  measured**, never covered.

## Red-check (`make efficacy`)

- Changed tests must pass at `HEAD` and fail when the branch's implementation diff for their stack
  is reverted ([D4](../decisions/quality-gateway.md#d4-verify-the-test-came-first-after-the-fact-red-check)).
- Run locally with `make efficacy` (defaults to `origin/main`; override with `EFFICACY_BASE`). Not
  in `make check` because it requires a base branch and second run.
- Branches changing no implementation, or changing implementation with no tests (including
  benchmark-only changes), warn/skip rather than failing.
- Legitimate non-red tests (pure refactors, renames, tests pinning unchanged behavior) use
  `//efficacy:exempt <reason>` directly above the test (**reason required**; prefer per-test
  markers over the `efficacy-exempt` PR label).

## Mutation testing (`make mutation`)

- `MUTATION_BASE=origin/main make mutation` runs pinned `gremlins` (`v0.6.0`, installed separately
  via `go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0`)
  scoped to changed Go lines ([D2](../decisions/quality-gateway.md#d2-mutation-testing-is-scoped-to-the-diff-and-to-pull-requests),
  [D9](../decisions/quality-gateway.md#d9-mutation-findings-require-complete-actionable-verdicts)),
  building `frontend/dist` first and pinning `--timeout-coefficient` and `--workers` to avoid
  contention hiding survivors. `make test-mutation` (in `make check`) tests the script offline with
  fixture reports.
- **Verdict handling**: `LIVED` fails; `KILLED` counts decided coverage; `NOT COVERED` and
  `NOT VIABLE` are silent individually; `SKIPPED` (gremlins diff-window approximation) is reported
  without failing individually; `TIMED OUT`, `RUNNABLE`, and any unknown status are **undecided**
  and fail. A run with scoped mutants where zero reached `LIVED`/`KILLED` fails unless every scoped
  mutant has a complete typed waiver. A diff with zero scoped mutants reports `"nothing was
  measured"` without failing. Tagged `panemux_fixture_agent` files ignored by the default build are
  dropped from scope and named.
- **Resolving failures** (in order): add the missing assertion, waive with
  `//mutation:exempt[<TYPE>] <reason>` on the mutated line or line above (kept within 120 chars;
  both `<TYPE>` and `<reason>` are required; multiple types comma-separated; `[*]` is flagged as
  line-wide), or last apply the `mutation-exempt` PR label. Re-running is valid only for `TIMED OUT`.
- **Equivalent vs. unreachable**: Ask *"if the mutant were reachable, would anything be wrong?"*
  `equivalent` survives that test (identical on all possible inputs); `unreachable` does not
  (differs on inputs callers cannot produce, and warrants checking whether the guard is load-bearing).

## Pre-push check selection

`.githooks/pre-push` compares the pushed branch to its remote counterpart (or merge-base with
`origin/main` on first push; deletions select nothing) and checks changed packages/modules
([D14](../decisions/quality-gateway.md#d14-selective-pre-push-checks-and-full-ci)):

| Change | Checks |
|---|---|
| Go (including package `testdata`) | `gofmt -s`, `vet`, pinned `golangci-lint` (checkout cache), package tests (no `-race`) |
| Package with fixture-agent files | Also tagged `vet` and `make test-go-fixture-agent` over `FIXTURE_AGENT_PKGS` |
| `frontend/src` TypeScript | `tsc --noEmit` and `vitest related` |
| `testdata/api-contract` | Frontend contract test |
| Shell (`*.sh`) | Matching `scripts/<name>_test.sh` and `TMPDIR` guard |
| `.claude/` | Hook tests (`make test-hooks`) |
| `frontend/screenshots/` or `frontend/e2e/*.sh` | Screenshot fixture tests (`make test-screenshots-check`) |
| `docs/` Markdown or `scripts/docsokf/` | OKF checker and tests (checker Go edits also run vet/lint/tests) |
| Any Markdown | Link check (`make check-docs-links`); `docs/scenarios.md` also runs `make check-scenarios` |

Shared dependencies/config (`go.mod`, `go.sum`, `Makefile`, `.golangci.yml`, `.node-version`,
frontend package/build config, runtime selectors, `.githooks/`) or an unknown diff range fall back
to full `make check`. The hook clears repository-scoped Git env vars before nested fixture repos;
`make test-pre-push` verifies selection in CI.

## Accessibility ceilings

`frontend/e2e/a11y.spec.ts` enforces per-rule node ceilings via `frontend/e2e/a11y-ceiling.ts`
against its own isolated fixture (`frontend/e2e/a11y.yml`, port 4178), excluding `.xterm`
([D11](../decisions/quality-gateway.md#d11-the-accessibility-ceiling-is-per-rule-counts-nodes-and-is-lowered-by-hand)).
The task dashboard and its host connection menu are also scanned with every rule held at ceiling 0.

| Page state | Rule | Impact | Ceiling (nodes) |
|---|---|---|---|
| Dashboard | `aria-required-children` | critical | 1 |
| Dashboard | `color-contrast` | serious | 2 |
| Dashboard | `region` | moderate | 7 |
| Pane settings dialog open | `aria-required-children` | critical | 1 |
| Pane settings dialog open | `select-name` | critical | 1 |
| Pane settings dialog open | `color-contrast` | serious | 10 |
| Pane settings dialog open | `region` | moderate | 7 |

Listed rows are existing violations, not approvals: counts may fall and must not rise; unlisted
rules have ceiling 0. To lower a ceiling after a fix:

1. Run `make test-e2e` and read the `a11y[<page state>] ceiling can be lowered` output (also saved
   in `axe-<page state>.txt`).
2. Update `CEILINGS` in `frontend/e2e/a11y-ceiling.ts` and the table above in the same commit.

## Performance benchmarks

`make bench` reports indicative timings without a regression gate (container timings vary up to
~2.9× across identical runs). When a performance issue reflects algorithmic work or allocations
(such as the ring replay buffer), protect it with a deterministic allocation/shape unit test rather
than a wall-clock threshold.

## Sources

- [ISO/IEC 25010:2023 product quality model](https://www.iso.org/standard/78176.html)
- Vladimir Khorikov, *Unit Testing: Principles, Practices, and Patterns*
- [DORA 2025 State of AI-assisted Software Development](https://cloud.google.com/blog/products/ai-machine-learning/announcing-the-2025-dora-report)
- [Claude Code best practices](https://code.claude.com/docs/en/best-practices)
- [gremlins mutation testing for Go](https://github.com/go-gremlins/gremlins)
