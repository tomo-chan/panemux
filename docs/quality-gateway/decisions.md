# Quality gateway: design decisions

> Part of the [quality gateway](../quality-gateway.md). That document defines the gates this one explains.

Several of these are counter-intuitive, so the reasoning is recorded rather than assumed. They are
recorded, here and in the table below, in the order they were taken — which is why D7 comes before
D6.

| | Decision |
|---|---|
| D1 | Do not raise the coverage threshold above 80% |
| D2 | Mutation testing is scoped to the diff and to pull requests |
| D3 | Consolidate gates rather than adding them |
| D4 | Verify "the test came first" after the fact (red-check) |
| D5 | The author does not grade its own work |
| D7 | The ledger is cross-checked, not just required |
| D6 | The Stop hook does not run everything |
| D8 | Per-block coverage gates the diff, not a baseline |
| D9 | The mutation check fails on a finding, after warning for three stages |
| D10 | The contract fixtures are rewritten on every run, not diffed against |
| D11 | The accessibility ceiling is per rule, counts nodes, and is lowered by hand |
| D12 | A model checker's output is checked in; the model checker itself is not in `make check` |

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

  The frontend half had the same mistake in its own dialect, and it survived until PR #240 hit it.
  When the revert deletes an implementation file the branch *added*, the test file importing it
  cannot be collected at all — vitest still writes a JSON report, but it holds no case results, and
  "this case is not in the report" was being read as "it passed". That inverted the gate for every
  new module's tests, which is most of what a feature branch writes: 36 of that branch's tests,
  including every test of the component its review comment was about, came back as survivors while
  the evidence in the log was an import error. A case that passed at HEAD and is absent after the
  revert is now red, because only implementation files move between the two runs, so nothing else
  can explain its absence.
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

**D9 — The mutation check fails on a finding. It warned first, for three stages, and the interesting
part of this decision is what had to be true before the exit code could change.**

**Why it started as a warning.** Of the 108 surviving mutants found at `d42e406`, **37 (34%) are ones
nobody should act on**: buffer sizes (`64*1024` in three files), timeout constants
(`30 * time.Second`), and error branches unreachable without fault injection. A test written to kill
the buffer-size mutants would pin a constant and assert nothing — a tautology, which is the exact
defect G4 exists to find. A check that failed on those would have been wrong more often than right in
its first weeks, and principle 4 says what happens to a gate that cries wolf. So stages 1 to 3 printed
findings and exited 0, with no environment variable to flip it early: an unused switch is an
invitation to enable a gate without the data that should decide it.

**What changed is not patience.** Three things the warning stages measured or repaired, each of which
had to land before failing was defensible:

1. **The 34% have somewhere to go that a reviewer can see.** `//mutation:exempt` now waives a mutant
   *type* rather than a whole line (below, and #236). Before that, waiving the buffer-size mutant
   silently waived every other mutant on the same line, so the escape hatch a failing gate depends on
   was wider than anyone writing one intended.
2. **A mutant that reached no verdict is no longer dropped** (below, and #235). As a warning that was
   misleading. As a gate it would have been the one remaining way to get a green tick out of a run
   that decided nothing.
3. **The size of a red run is known** (below, and #237): roughly **5 mutants per 317 changed lines**.
   A finding is a short list against a small denominator, not a wall — which is the difference between
   a gate people fix and a gate people route around.

**The gating change was then checked against real reports, not only fixtures.** `make mutation` was
run with real gremlins output over the two most substantial recent Go pull requests, and both pass:
**#234** (six files, 317 changed lines) reports `no surviving mutants among 5`, and **#231** (twelve
files) reports `no surviving mutants among 33, 2 skipped by gremlins`. That second run is the one that
found the `SKIPPED` defect below — under the first draft of this change it was red.

**The question stage 3 left open — whether an undecided mutant should fail — is answered yes, with one
carve-out that a real report forced and reasoning had not.** A mutant that reached no verdict is not
evidence that the change is protected; it is this gate failing to run, one mutant at a time. Treating
it as a pass would reinstate exactly the fail-open #235 closed, with a green tick attached. This
repository already lives by that rule one level up, in `scripts/efficacy.sh`'s own words: *"Could not
check" is a failure, never a skip.* The cost is a build that can go red because a runner was slow, and
two things bound it: the pinned settings that took `internal/api` from 114 timeouts to 0, and the rule
that **a marker waives an undecided mutant of the type it names exactly as it waives a survivor** —
whether a verdict was reached is a fact about the runner, while whether the mutant is worth killing is
the claim the author made, and only the second is theirs to make.

**`SKIPPED` is the carve-out, and the way it was found is the point.** The first draft of this change
treated `SKIPPED`, `TIMED OUT` and `RUNNABLE` alike: all three mean "no verdict", so all three failed.
Run against **#231**'s real gremlins report rather than a fixture, that gate went red — not on a
survivor, but on two `SKIPPED` mutants at `internal/commandcenter/history_store.go:58`, a line that
branch demonstrably added. Reading gremlins' source rather than inferring from the name settles what
the status means (`internal/engine/engine.go`):

```go
if mu.codeData.Cov.IsCovered(pos)   { status = mutator.Runnable }
if !mu.codeData.Diff.IsChanged(pos) { status = mutator.Skipped }
```

`SKIPPED` is set from gremlins' own diff and from nothing else — never from a test result. And that
diff is an approximation: `internal/diff/diff.go` builds each changed range as
`EndLine = startLine + LinesAdded - 1`, which assumes a hunk's added lines run contiguously from the
fragment's start. They do not when a hunk mixes context, deletions and additions, which is why git's
`@@ -50 +51,11 @@` covers line 58 and gremlins' window does not.

So `SKIPPED` is **two diff implementations disagreeing**, not a fact about anyone's tests, and no edit
to the line can change it. Failing on it is principle 4's exact shape: a red build for a condition the
person reading it cannot act on. It is reported and does not fail.

**One skipped mutant is a disagreement; a run in which nothing reached a verdict measured nothing**,
so the gate fails when no scoped mutant reached `LIVED` or `KILLED` at all. The condition is total
rather than a proportion deliberately — any threshold below "nothing was decided" would be a number
nobody could defend, and principle 4 applies to arbitrary thresholds as much as to noisy findings.

**That check was written keyed on `SKIPPED` first, and review caught the two holes that left.** Its
own comment claimed `decided_count == 0` was the whole test while the code also required
`skipped_count > 0`, and the gap between the two was reachable from both sides:

- A run whose every scoped mutant was `NOT COVERED` printed `ok — no surviving mutants among 1`. The
  reflex answer is that G4(d) fails on those instead, and it usually does — but `make coverage-blocks`
  reports a changed file in a package `COVERAGE_PKGS` excludes as *not measured* rather than failing,
  so there are diffs about which no gate would have said anything at all.
- A run whose every scoped mutant was `SKIPPED` **and waived** printed the same clean sentence,
  because the exempt arm runs before the skipped one and `skipped_count` never rose.

The condition is now simply "nothing reached a verdict", whatever the cause, and the failure names the
composition — how many were skipped, undecided, uncovered or non-viable — because the action differs
by cause. Note what this does *not* change: a `NOT COVERED` mutant is still never reported here as a
finding. **The claim being corrected is about the run, not about the mutant**, and those are different
sentences.

**The one exception is an explicit waiver of every scoped mutant.** A `//mutation:exempt` is a typed,
reasoned, reviewable claim that a mutant need not be killed, and that claim does not depend on whether
the mutant ran. Failing anyway would make the marker powerless in precisely the run where the author
has said the most about what they expect. Partial waivers do not qualify: they speak for part of the
run.

The general lesson is the one this repository keeps relearning: **"no verdict" is not one category.**
`TIMED OUT` is this gate trying to get an answer and failing, which the author can re-run or waive.
`SKIPPED` is another component declining to be asked. Only the first is "could not check".

**What still exits 0, because a gate is also defined by what it declines to fail on:** a branch that
changes no Go implementation, and a branch with no mutant on any changed line. The second is the one
worth stating, since it is the fail-open shape this very decision is otherwise about. A diff of type
declarations or struct fields has nothing to mutate, and so does a diff whose changed lines carry no
operator tokens even inside a file full of them — `internal/commandcenter/runner.go` in the #234
measurement below is exactly that, 37 mutants in the file and none on its 27 changed lines. Failing
there would fire on most documentation-adjacent branches for a condition the author cannot act on.
What the run does instead is say plainly that it measured nothing, and name how many mutants the
touched files held.

What was never softened is "could not run": a missing base ref, a shallow clone, a gremlins that
exited non-zero or a truncated report all exit 1, because a check that decided nothing must never look
like one that found nothing.

**The same rule holds one mutant at a time, and the first version of the script did not apply it
there.** gremlins defines seven statuses — `internal/mutator/mutator.go` lists
NotCovered, Runnable, Skipped, Lived, Killed, NotViable and TimedOut — and `scripts/mutation.sh`
matched `LIVED`, routed `SKIPPED` to an "unanalysed" list, and let a catch-all arm drop everything
else — so a mutant whose suite never finished (`TIMED OUT`) and a mutant that did not compile
(`NOT VIABLE`) both left no trace, and so would any status a later gremlins invents. `TIMED OUT` is
the one that matters: it is not a near-miss, it is the status the pinned settings exist *because* of,
and the same measurement that pinned them found timeouts hiding 51 survivors. A run reporting "no
surviving mutants" while every mutant on the diff timed out was possible, and said nothing about the
tests.

**The same conflation had one more level, and the numbers say why it matters.** `scripts/mutation.sh`
printed "no surviving mutants on lines this branch changed" whether it had analysed fifty mutants or
none — byte-identical output for "asked and got a clean answer" and "asked nothing". Measured on
**#234**, the last substantial Go pull request before this was written: six non-test Go files, 35
hunks, 317 changed lines. gremlins produced **128 mutants in those files and exactly 5 on a changed
line**, all killed. Two of the six files (`internal/cachedir`, `internal/homedir`, 104 changed lines
between them) produced **no mutants at all** — they are new packages of type declarations and thin
wrappers, and gremlins mutates operator tokens in covered code. A third, `internal/commandcenter/runner.go`,
holds 37 mutants and had **none** on its 27 changed lines.

So the headline now carries the denominator (`no surviving mutants among 5 on lines this branch
changed`), and a run with nothing on a changed line says **"nothing was measured"** in its own words,
naming how many mutants the touched files held so that "the scope discarded everything" and "there
was nothing to discard" stay distinguishable. **This was a stage-4 input as much as a readability
fix: a gate that would rarely fire is not thereby a safe gate — it may be a gate that is usually
saying nothing, and 5 questions per 317-line branch is the order of magnitude stage 4 had to decide
against.** It is also the number that made failing defensible, since it bounds what a red run costs
the author: a short list, not a wall.

The fix is which arm carries the catch-all. `KILLED`, `NOT COVERED` and `NOT VIABLE` are now
enumerated as the statuses the gate deliberately says nothing about — each for a stated reason, and
`NOT VIABLE` belongs there rather than among the unknowns, since a mutant that does not compile is
not a hole in anyone's tests — and **everything else is reported as undecided, carrying the status
string**, in the headline as well as in the list below it. "0 survivors" and "0 survivors, 12 of 12
mutants undecided" are different results, and only the second one is honest about a run that decided
nothing. That handed stage 4 a question it did not have before — whether an undecided mutant on a
changed line should fail, or whether a gate that measured nothing should merely say so louder — and
the answer, recorded in D9 above, is that it fails: a green tick on a run that reached no verdict is
the same fail-open this paragraph closed, with the exit code now agreeing with the prose instead of
contradicting it.

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
  leave a clean checkout dirty. Response *order* is the same hazard by another route — `GET
  /api/sessions` ranges over a map, so its rows swapped places about one run in eight until the
  capture sorted them — and rewriting rather than diffing is what makes it invisible, so a repetition
  check guards it. `TestAPIContractFixtures_ContainNoMachinePaths` re-checks the path
  half by re-capturing and normalizing, not by reading the committed files — for that failure the
  committed file is the suspect, not the reference, and reading disk also made the check silently
  depend on an earlier test in the same file having run first.
- **Which optional fields a capture leaves empty is derived, not declared.** Accepting a capture
  proves nothing about a field the capture does not contain, and the first version of this tracked
  that with a prose note per fixture plus a hand-written list of the fixtures carrying one — two
  copies of the same set, so the check could catch a *declared* gap going undeclared and nothing
  else. It reported full coverage while `OpenUrlResponseSchema.port` and `LayoutNodeSchema.pane` were
  never populated by any capture. Optionality is stated in the schemas, so `contract.test.ts` walks
  each schema against its capture and names every optional field absent everywhere in the corpus,
  against a declared list carrying a reason per entry. A field that starts being covered has to be
  deleted from that list, so it cannot rot into excuses for coverage that already exists.

**D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand.**
Three choices, and the cheap version of each fails to catch a real regression.

*Per rule ID, not a total.* A single number per page state lets one violation be fixed and a
different one introduced in the same change without the total moving — the "fixed one, added one"
case, which is the most likely way this UI regresses while looking maintained. Per rule, a violation
kind that is not in the map has a ceiling of zero, so a **new** rule fails the run the day it
appears.

*Node counts, not just presence.* `color-contrast` is one violation at 2 nodes on the dashboard and
one violation at 10 with the settings dialog open. Counting rules alone would treat those as the
same fact, so a dialog's worth of unreadable text could appear on the dashboard and register as no
change.

The cost of the tighter form is brittleness, and it turned out to be real — which is worth stating
plainly, because the first draft of this decision claimed the counts were simply stable and that
claim was too strong. Against a **fixed rendered page** they are: they reproduced identically to the
ones #187 recorded, on different hardware, in a different container, on a later run, including
`region` at 7 nodes and `color-contrast` at 10 — the counts a layout- or font-sensitive rule would
be expected to move. What a node count is not stable against is a *different page*, and it is a
property of whatever happens to be rendered. Sharing the default E2E server, the same two counts
read **10 and 11** once `pane-move.spec.ts`'s splits had run first: the gate then failed on the
order of the suite rather than on anything about accessibility, which is principle 4's false
positive exactly.

The fix is the one `core-multiplexer.yml` already uses from the other side — that spec was
separated because it is a mutator, and this one because it is the most sensitive to mutators.
`a11y.spec.ts` gets its own panemux process and fixture (`frontend/e2e/a11y.yml`, port 4178), so the
page it audits is pinned rather than inherited. **The general rule this is an instance of: a gate
that counts what is on a page needs to own the page.**

*The comparator is a separate module, and unit tested.* `checkAgainstCeiling` and `CEILINGS` live in
`frontend/e2e/a11y-ceiling.ts` rather than inside the spec, because of a property this gate shares
with every ratchet: **on a healthy repository it only ever takes its uninteresting branch.** Observed
equals ceiling, both returned arrays come back empty, and none of new-rule, risen-count,
lowerable-count or vanished-rule executes on any green run. An inversion in one of them —
`actual >= allowed` written as `>`, or a dropped `?? 0` making an unlisted rule compare against
`undefined` — would ship green and only surface as a *missed* regression later. The perturbation that
proved the gate bites was a one-time manual run; `a11y-ceiling.test.ts` is what keeps it proven, as a
vitest table over `(ceiling, observed)` pairs that needs no browser. DEVELOPMENT.md's
test-granularity rule asks for exactly this: the smallest unit that exercises the logic, not the
outermost entry point.

That splits `frontend/e2e/` between two runners, so the split is by suffix and both configs state it:
`*.spec.ts` is Playwright's, `*.test.ts` is vitest's. Playwright's default `testMatch` claims both,
and letting it load a vitest file is not a subtle failure — `@vitest/expect` throws `Cannot redefine
property: Symbol($$jest-matchers-object)` and takes the **entire** E2E suite down with it, not just
that file.

*A ceiling that may fall, and lowering it is a manual edit.* Failing the run when a count comes in
**under** its ceiling would be the self-enforcing ratchet, and it is the wrong trade here: it makes
improving accessibility break the build, which trains people to stop improving it, and it converts
every one of the brittleness risks above into a two-sided flake instead of a one-sided one. So the
scan prints and attaches the exact replacement entry — `set CEILINGS['pane-settings']['color-contrast']
to 9 (was 10)` — and passes. The nudge is loud precisely because the enforcement is not: a ceiling
left above reality re-admits the regression it was set to catch.

This is the same problem #188 solved differently. Per-block coverage faced 275 uncovered blocks out
of 1801 and gave up the repository-wide gate for a diff-scoped one. A11y has no diff scope available
— axe reports on a rendered page, not on changed lines — so the ceiling is what stands in for it.
Both are the same refusal to ship a gate that starts red (principle 4).

**D12 — A model checker's output is checked in; the model checker itself is not in `make check`.**
Issue #168 pilots TLA+ over `internal/board`'s `ownSendLedger`. The obvious arrangement — run TLC as
part of the gate — fails principle 5 outright: TLC needs a JDK and `tla2tools.jar`, which
`make install-deps` does not install, so `make check` would stop being hermetic. The arrangement that
does work splits it the way the agmsg contract is already split, and the split is doing more work here
than the hermeticity argument alone suggests.

*Model checking proves the model, not the code.* This is the trap the whole design is built around. A
spec and an implementation can each be internally consistent and disagree with each other, and no
amount of TLC says so. A repository that ran TLC in CI and stopped there would have a green formal
method and no more protection than before. So what is checked in is TLC's **output** — the full state
graph, exported as a flat transition table — and what runs on every commit is the real Go
implementation being replayed against it. Tier 2 (`make model-check`, a path-filtered CI job) exists
to keep that table honest about the spec, which is a drift problem, not a verification one.

*The reference model is the table and nothing else.* The tempting alternative is a small hand-written
Go state machine to compare against. That is a third artifact to keep in sync, and it drifts from the
spec in precisely the way the implementation it is checking might — the failure it is supposed to
detect. Indexing the exported table for lookup has no such failure mode.

*Coverage of the model is asserted, not assumed.* The drivers require every non-Expire transition in
the table to have been taken by the real ledger. Without that, a table permissive enough to accept
anything passes exactly as quietly as a correct one — the same tautology problem D1 records for
coverage, one level up.

*It is bounded, and the bound is written down.* `MaxEntries` caps how many occurrences one key may
hold (4 as committed). A defect appearing only above that is outside what this says anything about.
That is the ordinary limitation of bounded model checking and it is stated in
[agent-board.md](../agent-board/model-checking.md#state-machine-model-checking) rather than left for a reader to infer
from the `.cfg`.

*Every property was perturbed before being trusted.* Each of `Conservation`, `ConsumeNeverForges` and
`RecordIsImmediatelyMatchable` was verified by breaking the spec until TLC caught the specific design
bug that property exists for, and each Tier 1 check by breaking the implementation until it failed.
A property that holds vacuously reads exactly like one that holds, which is the same reason #194's
ceilings and #191's fixtures were both confirmed by perturbation.

*The exporter is gated too, and it had to be.* `scripts/tla_transitions.py` writes the table, so it
is the one artifact in this split whose failure mode is a hermetic gate that quietly checks less
while staying green. Its first revision defined the bound as the largest state it happened to
observe, which made its own completeness check unfalsifiable — the expectation was derived from the
data it was validating, so a TLC run that explored to 2 against a `.cfg` asking for 4 exported
`"maxHeld": 2`, and Tier 1, which reads that number back out, shrank its drivers to match. Review
caught it; no gate would have. `make test-model-check` now drives the exporter against committed dot
fixtures in `scripts/testdata/model-check/` — real TLC output at a tiny bound, plus one copy per way
of breaking it — and asserts every rejection arm, so the bound now comes from the `.cfg` and a
truncated dump is refused. The general rule: **a checker that generates the thing other checks are
measured against needs its own tests before those checks mean anything.** Every other gate script in
`scripts/` already had a `*_test.sh`; this one was the exception, and the exception is where the bug
was.
