---
type: Reference
title: "Quality gateway: surviving mutants, the first measurement"
description: "Recorded surviving mutants, boundary findings, and lessons from mutation measurements."
---

# Quality gateway: surviving mutants, the first measurement

> Part of the [quality gateway](../quality-gateway.md). That document defines the gates this one explains.

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

The same missing boundary assertion recurred at three sites across three packages and times;
the existing execution gates could not detect it.

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

The original marker waived every mutant on a line, even though its reason named only one type.
Current type-scoped marker rules are authoritative in
[Mutation testing](../development/mutation.md).

Nothing was hidden by it, and that is measured rather than assumed. Running gremlins over
`internal/board` and `internal/session` names every mutant on each of the eleven marked lines: all
eleven carry a `CONDITIONALS_NEGATION` as well, three also carry `ARITHMETIC_BASE`, three
`INVERT_NEGATIVES` — **27 mutants across 11 lines, of which exactly the 11 `CONDITIONALS_BOUNDARY`
ones survive**. Every waiver covered only mutants that were dying anyway. The `endConn` line is the
useful data point rather than a counterexample: its negation mutant (`<= 0`) was killed by the
existing suite the whole time, and what survived under the waiver was exactly the boundary mutant the
reason was written for.

The measurement found a structural gap, not a hidden live survivor. Type-scoped waivers prevent
a reason for one mutant from silently waiving a different finding when mutation is a failing gate.
See [D9](../decisions/quality-gateway/mutation-verdicts.md) and the
[marker contract](../development/mutation.md).

The boundary row is not 45 missing tests: four cannot be killed by any input and one requires
fabricated input. This is a floor from the #190 sites, not a census of all 45. The `endConn`
correction shows why reviewers must distinguish equivalent from unreachable in the marker
itself. These findings informed [D9](../decisions/quality-gateway/mutation-verdicts.md).

The first live Go red-check ran here after #185, #186, #187, and #189 correctly skipped Go-free
diffs. **15 of 18 changed tests survived the revert** because they pinned already-correct
behavior; each used a per-test `//efficacy:exempt` reason, not the branch-wide `efficacy-exempt`
label (`scripts/efficacy.sh`). The three `resolveSweepInterval` tests went red. Mutation and
red-check therefore disagree correctly on this branch: a high survivor rate is expected for
already-correct behavior, but warrants scrutiny on a branch changing behavior. See
[Red-check](../development/red-check.md) and [D4](../decisions/quality-gateway/red-check.md).

**One implementation change came out of it**, and it is the shape the repository already prefers.
`New` in `internal/portforward/registry.go` decided three things at once about `Options.SweepInterval`
— disabled, defaulted, or as given — and the boundary between the first two sits exactly at zero,
where nothing observable distinguishes them: a reaper that ticks once a minute cannot be waited on in
a test. Splitting the decision into the pure `resolveSweepInterval` made all three cases assertable,
the same move `browserOpenArgv` is split out of `openChrome` for (see
[security/browser-open.md](../security/browser-open.md)) and the same thing
DEVELOPMENT.md's testability rule asks for. Where a boundary is unverifiable in place, extracting the
decision is the fix; adding a test that asserts the code's own constant back to itself is not.

### Relationship to issue #164

[#164](https://github.com/tomo-chan/panemux/issues/164) proposed failing blocks whose summed
execution count is zero. Block coverage cheaply checks execution; mutation more expensively
checks assertions. The design introduces block coverage before mutation for surviving tautologies.

`scripts/coverage_blocks.sh` / `make coverage-blocks` is diff-scoped (D8) and reuses
`make coverage-go`'s profile. Its tests pin summation per unique block: shared `-coverpkg` emits
each block once per test binary. This is block coverage, not C1. See
[Coverage gates](../development/coverage.md) for the current contract.

Per-block execution and mutation efficacy remain complementary: 108 mutants survived in code
reported as covered by the block gate.
