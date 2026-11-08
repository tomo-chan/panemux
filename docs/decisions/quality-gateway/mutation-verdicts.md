---
type: Decision
title: "D9 \u2014 Mutation findings require complete, actionable verdicts"
description: "Rationale for D9 \u2014 Mutation findings require complete, actionable verdicts."
---

# D9 — Mutation findings require complete, actionable verdicts

Mutation became blocking after three warning stages established reviewable typed waivers,
complete verdict reporting and an actionable denominator. At d42e406, 37 of 108 survivors (34%)
were constants/unreachable error paths; killing those encourages tautologies. Type-scoped waivers
avoid hiding other mutations on the same line. A 317-line branch asked only five mutation questions,
so reporting the denominator matters as much as survivors.
Real reports from PRs #234/#231 exposed SKIPPED as gremlins' approximate diff disagreeing with git,
not a test verdict; it is reported without failing individually. TIMED OUT, RUNNABLE and unknown
statuses are undecided findings. KILLED, NOT COVERED and NOT VIABLE are explicitly classified,
never dropped in a catch-all. An entirely undecided/uncovered/skipped run fails because it measured
nothing, except when every scoped mutant has an explicit typed, reasoned waiver. Partial waivers
do not qualify. NOT COVERED stays distinct from an individual mutation finding.
No changed implementation or no scoped mutant is legitimate no-measurement, explicitly named.
Missing bases, shallow history, tool failures and truncated reports fail as inability to run.
Pinned workers/timeouts prevent contention hiding survivors: defaults timed out 465/1059 runnable
mutants, hiding survivors despite nearly unchanged efficacy percentages.
See [mutation](../../development/mutation.md) and [dated findings](../../quality-gateway/mutants.md).
