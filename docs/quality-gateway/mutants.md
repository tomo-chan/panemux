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

**One property of the marker itself, which the `Kind` column above made easy to misread — now
fixed.** `scripts/mutation.sh` used to decide an exemption by file and line, recording the mutant
`$type` for the report and never comparing it, so a reason written about the boundary mutant waived
*every* mutant gremlins produced on that line, `CONDITIONALS_NEGATION` included. The reason a reader
saw and the set it actually covered were not the same set.

Nothing was hidden by it, and that is measured rather than assumed. Running gremlins over
`internal/board` and `internal/session` names every mutant on each of the eleven marked lines: all
eleven carry a `CONDITIONALS_NEGATION` as well, three also carry `ARITHMETIC_BASE`, three
`INVERT_NEGATIVES` — **27 mutants across 11 lines, of which exactly the 11 `CONDITIONALS_BOUNDARY`
ones survive**. Every waiver covered only mutants that were dying anyway. The `endConn` line is the
useful data point rather than a counterexample: its negation mutant (`<= 0`) was killed by the
existing suite the whole time, and what survived under the waiver was exactly the boundary mutant the
reason was written for.

So the gap was structural rather than live, and it mattered most for stage 4, when a survivor becomes
a failure: a line-scoped waiver applied to a real finding of a different kind would then be the
difference between a red gate and a green one, with nothing in the diff to show for it. **The marker
now names the type it waives** — `//mutation:exempt[CONDITIONALS_BOUNDARY] <reason>`, with a
comma-separated list for several and `[*]` for a deliberate, labelled line-wide waiver. An untyped
marker exempts nothing, on the same grounds as a reasonless one; the eleven above were rewritten in
the change that introduced it, each to the type the measurement above says it was always about. When
a marker is present and a mutant of another type survives, the finding names the type the marker
does claim, so the mismatch is on the screen instead of being silently absorbed.

This was the first real evidence for how much of G4(c)'s noise is irreducible rather than fixable,
which is what item 6's fourth stage — whether to make `make mutation` fail — needed before it could
be decided, and [decision D9](decisions.md) records the decision it fed: **the "boundary value" row
is not 45 missing tests.** Four of them cannot be killed by anyone, one only by a fabricated input,
and that count is a floor rather than a census — it is what surfaced while working the sites #190
named, not an audit of all 45. A gate failing on that group would demand tautologies in the very
class the measurement called most actionable. The `endConn` correction is the counterweight: a
reviewer who accepts an exemption's *reason* without asking which kind it claims will wave through
real defects, so stage 4 needs the two words kept apart in the marker itself, not just in this
table.

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
[security.md](../security/command-execution.md#launching-the-operators-browser---open)) and the same thing
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
