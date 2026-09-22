# Quality Gateway

This guide defines the current quality model and the gates that enforce it. Use
[DEVELOPMENT.md](../DEVELOPMENT.md) for commands and workflow, [Scenario coverage](scenarios.md) for
the user-facing acceptance ledger, and [quality-gateway/decisions.md](quality-gateway/decisions.md)
for the rollout history and rationale behind decisions D1–D12.

## What the tests protect

Panemux treats product quality and test quality as separate axes. Passing tests must cover the
observable product contract, and the tests themselves must be capable of detecting a behavioral
regression without preventing behavior-preserving refactors.

| Product characteristic | Primary protection |
|---|---|
| Functional suitability | Scenario ledger, Go/frontend tests, and browser E2E |
| Reliability | Session/WebSocket lifecycle tests, replay tests, and state-machine conformance |
| Security | Per-sink security requirements, regression tests, `gosec`, and lint |
| Compatibility | Config migration tests, Go-to-Zod contract fixtures, and the two-tier agmsg contract |
| Maintainability | Lint, typed seams, single-source wiring, coverage scope, red-check, and mutation testing |
| Interaction capability | Component tests, keyboard/focus E2E, and axe-core ceilings |
| Performance efficiency | Benchmarks reported by `make bench`; no performance threshold is enforced |
| Flexibility | Config compatibility and platform-aware session tests; some environment coverage remains manual |
| Safety | Tests around PTY writes, bootstrap retries, persistence, and remote side effects |

Test quality follows four properties: protection against regressions, resistance to refactoring,
fast feedback, and test maintainability. Coverage is only one input; it is not treated as proof that
the assertions can detect a defect.

## Implementation rules that make testing possible

| Principle | Current rule |
|---|---|
| **P1 — Single source of truth** | Production wiring is defined once. Tests drive that wiring instead of reconstructing it. |
| **P2 — Humble side-effect boundary** | PTY, SSH, process, filesystem, and network boundaries stay thin; decisions move into deterministic functions. |
| **P3 — Typed injection points** | Global lookups and effects use constructor fields or named seams such as `internal/homedir`, `internal/cachedir`, and `internal/fileops`. |
| **P4 — Observable behavior** | Tests name and assert HTTP responses, WebSocket frames, persisted files, rendered UI, and other published behavior rather than helper call order. |
| **P5 — Explicit implementation pinning** | A test may pin an implementation shape only when a security or compatibility document explains why that exact shape is part of the contract. |

## Gates

The order is intentional: cheaper checks should reject a defect before expensive checks run.

| # | Gate | Checks | Enforcement |
|---|---|---|---|
| **G0** | Spec | User-visible changes update `scenarios.md`; documentation links and fragments resolve | Scenario CI plus `make check-docs-links` |
| **G1** | Edit | `gofmt -s`, targeted `go vet`, or `tsc --noEmit` for an edited file | `.claude` post-edit hook |
| **G2** | Unit | Go and frontend unit/integration suites | `make check`, pre-push, and CI |
| **G3** | Contract | Real-router HTTP/WS tests, exhaustive route expectations, Go-produced Zod fixtures, agmsg contract, and model-to-code transition replay | Always-on tests in `make check`; real agmsg and TLC in dedicated CI/opt-in jobs |
| **G4** | Efficacy | Coverage scope and threshold, changed-test red-check, diff-scoped mutation, and changed-block coverage | `make check` plus pull-request CI jobs |
| **G5** | Scenario | Playwright workflows, scenario-reference validation, and accessibility ceilings | `make test-e2e`, `make check-scenarios`, and CI |
| **G6** | Adversarial review | Fresh-context review of the diff for correctness and stated requirements | Review agent plus human review; advisory, not a merge gate |

### Gate details

- Coverage thresholds stay at 80%; newly added decision-holding packages join the measured scope
  instead of raising the percentage target.
- `make efficacy` requires a changed test to pass on the branch and fail when the associated
  implementation diff is reverted.
- `make mutation` fails on a surviving or undecided mutant on a changed line unless the exact
  mutant type has a justified exemption.
- `make coverage-blocks` fails when a block covering a changed line was never entered.
- Go-produced API/WebSocket fixtures are parsed by the owning Zod schemas and must round-trip
  without fields being silently stripped.
- Accessibility is a per-rule ceiling: counts may fall and must not rise. Performance benchmarks
  are measurements only.
- Tier 1 model checking replays checked-in transitions against Go in `make check`; Tier 2 runs TLC
  and verifies that the checked-in transition table matches the TLA+ spec.

## Enforcement ladder

A rule is described as enforced only when a deterministic mechanism above documentation makes it
fail. The levels are:

| Level | Mechanism | Role |
|---|---|---|
| L0 | Documentation | States intent; advisory |
| L1 | Make target | Gives a local pass/fail result |
| L2 | Agent hook | Runs during editing or before an agent stops |
| L3 | Git hook | Blocks a local push |
| L4 | CI job | Runs in the shared pull-request environment |
| L5 | Branch protection | Blocks merge until required jobs pass |

`make check` remains hermetic. Checks that need a real SSH/tmux/agmsg environment, network access,
or a JDK use explicit opt-in commands or dedicated CI jobs.

## Deep dives

| Topic | Document |
|---|---|
| Why the gates have this shape; decisions D1–D12 | [Design decisions](quality-gateway/decisions.md) |
| First mutation findings and their classification | [Mutation findings](quality-gateway/mutants.md) |
| Versioned benchmark and accessibility evidence | [Measurements](quality-gateway/measurements.md) |
| Commands and exemptions for each mechanism | [Development deep dives](development/) |
| Chronological cross-topic milestones | [Decision log](DECISIONLOG.md#quality-gateway) |

## Sources

- [ISO/IEC 25010:2023 product quality model](https://www.iso.org/standard/78176.html)
- Vladimir Khorikov, *Unit Testing: Principles, Practices, and Patterns*
- [DORA 2025 State of AI-assisted Software Development](https://cloud.google.com/blog/products/ai-machine-learning/announcing-the-2025-dora-report)
- [Claude Code best practices](https://code.claude.com/docs/en/best-practices)
- [gremlins mutation testing for Go](https://github.com/go-gremlins/gremlins)
