#!/bin/sh
#
# Diff-scoped mutation testing: gate G4(c) in docs/quality-gateway.md, roadmap
# item 6 of issue #180.
#
# The other three G4 gates each answer a different question, and this one
# answers the last of them:
#
#   (a) make coverage-go      — what percentage of statements ran?
#   (b) make efficacy         — does a test this branch CHANGED fail when this
#                               branch's implementation is reverted?
#   (d) make coverage-blocks  — did every block on a changed line run at all?
#   (c) THIS                  — would the tests NOTICE if changed code behaved
#                               differently?
#
# (d) and (c) are the pair that look alike and are not. A block can execute on
# every test run and still have nothing asserted about it. #180's measurement
# ran gremlins over the whole module at d42e406 and found 108 such mutants —
# every one of them in code the per-block gate reports as covered, because
# gremlins only ever mutates covered code.
#
# The worked example, because it is the clearest statement of what this gate is
# for: `port > 65535` appears in internal/config/validate.go, internal/api/
# handler.go and internal/session/loopback.go. Changing it to `>= 65535` makes
# all three reject port 65535, which is a legal port. Every test still passes.
# The cause is the same in all three places — the tests use 65536, one past the
# boundary, and never 65535, the boundary itself. `make coverage-blocks` lists
# none of those lines, correctly: the blocks execute.
#
# THIS IS A GATE: A SURVIVING MUTANT ON A CHANGED LINE EXITS 1. That is stage 4
# of item 6's four, and it was deliberately not the starting position. Stages 1
# to 3 shipped this as a warning because the measurement said so: of the 108
# survivors module-wide, 37 (34%) are ones nobody should "fix" — buffer sizes
# (`64*1024`), timeout constants (`30*time.Second`), and error branches
# unreachable without fault injection. A test that killed the buffer-size
# mutants would pin a constant and assert nothing: a tautology, the exact thing
# G4 exists to catch. Failing on those from day one would have made this gate
# wrong more often than right, and principle 4 in docs/quality-gateway.md is
# that a gate which cries wolf gets routed around, taking the gates that do work
# with it.
#
# WHAT CHANGED BETWEEN THEN AND NOW is not patience, it is three measurements
# and two repairs:
#
#   - The 34% have somewhere to go. //mutation:exempt takes a TYPE and a reason
#     (#236), so waiving the buffer-size mutant no longer waives its neighbours
#     on the same line. Measured over this repository's eleven markers: 27
#     mutants on 11 lines, and exactly the 11 the markers are about survive.
#   - A mutant that reached no verdict is no longer dropped (#235). Before that,
#     a run whose mutants all timed out printed "no surviving mutants" — which
#     as a warning was merely misleading and as a gate would have been the way
#     to get a green tick out of a run that decided nothing.
#   - The denominator is known (#237). A typical branch presents this gate with
#     about 5 mutants per 317 changed lines, so a red gate here is a small,
#     readable list rather than a wall.
#
# "COULD NOT RUN" IS STILL A FAILURE, and gating extends that rule one level
# down rather than softening it: an UNDECIDED mutant — one that reached no
# verdict — exits 1 as well. A check that could not check must not look like one
# that found nothing, which is what scripts/efficacy.sh means by "'Could not
# check' is a failure, never a skip". The escape hatches below cover it: a
# marker waives an undecided mutant of the type it names, exactly as it waives a
# survivor, because a mutant the author has said need not be killed should not
# fail a build over how long the runner took.
#
# WHAT STILL EXITS 0, and why each is a considered answer rather than a gap:
#
#   - No Go implementation changed. There is no question to ask.
#   - No mutant on any changed line ("nothing was measured"). A diff of type
#     declarations or struct fields has nothing to mutate; so does a diff whose
#     changed lines carry no operator tokens, even in a file full of them. This
#     is common and benign, and failing on it would fire on most documentation-
#     adjacent branches. The run says plainly that it measured nothing, and
#     prints how many mutants the touched files held (#237) so "the scope threw
#     everything away" stays distinguishable from "there was nothing to throw".
#   - A SKIPPED mutant. See the SKIPPED arm below: it is gremlins' diff
#     disagreeing with this gate's, which is not a fact about the tests and not
#     something the author can edit.
#   - A NOT COVERED or NOT VIABLE mutant, which belong to G4(d) and to nobody.
#
# WHAT DOES NOT EXIT 0, even though each of its parts would on its own: a run in
# which NOTHING reached a verdict. All skipped, all timed out, all uncovered,
# all non-viable, or any mix of them — "no surviving mutants" would then rest on
# nothing measured. That claim is about the RUN rather than about any mutant,
# which is why it can fail while every individual status in it stays silent.
# The exception is an explicit //mutation:exempt on EVERY scoped mutant: that is
# a reviewable claim about each one, and it does not depend on whether the
# mutant ran.
#
# THE SAME RULE, ONE MUTANT AT A TIME. gremlins defines SEVEN statuses
# (`internal/mutator/mutator.go`: NotCovered, Runnable, Skipped, Lived, Killed,
# NotViable, TimedOut), and this script sorts them into four groups:
#
#   LIVED                      a survivor. Fails, unless waived.
#   KILLED                     the good case, and the counter that says this run
#                              decided something at all.
#   NOT COVERED, NOT VIABLE    deliberately silent. G4(d) owns NOT COVERED and
#                              reports it better; a NOT VIABLE mutant did not
#                              compile, so no test could have noticed it
#                              behaving differently.
#   SKIPPED                    reported, does NOT fail on its own. See the arm
#                              itself for why: gremlins sets it from its own
#                              diff, whose changed-line arithmetic is an
#                              approximation, so it is two diff implementations
#                              disagreeing rather than anything about the tests.
#   everything else            UNDECIDED — TIMED OUT, RUNNABLE, and any status a
#                              later gremlins invents. Fails, unless waived.
#
# The catch-all sits on the UNDECIDED arm, and used to sit on the silent one:
# every status this script did not name was dropped, which let a run whose
# mutants all timed out print "no surviving mutants". The enumeration that has
# to be exhaustive is the silencing one.
#
# SKIPPED is the only group whose treatment was decided by a real report rather
# than by reasoning. The first draft of stage 4 failed on it along with TIMED
# OUT; run against #231's actual gremlins output it went red over two mutants on
# a line that branch demonstrably added, for a reason no edit to that line could
# change. A whole run of nothing but SKIPPED is still a failure — that one is
# "nothing was measured", not "gremlins and git disagree about one hunk".
#
# Usage:
#   make mutation                                # report against origin/main
#   MUTATION_BASE=origin/develop make mutation
#   sh scripts/mutation.sh --base origin/main --report gremlins.json
#   MUTATION_EXEMPT=1 make mutation              # branch-wide, from the CI label
#
# `--report <file>` reads an existing gremlins `--output` report instead of
# running gremlins. scripts/mutation_test.sh drives every case through it, so
# the suite is hermetic and needs no gremlins install — the same split
# scripts/coverage_blocks.sh has with `--profile`.
#
# SCOPED TO THE DIFF, for the reason decision D2 gave and one the measurement
# added. D2 scoped it because gremlins' own documentation warns of hours on a
# large module; measured on this repository the whole module takes 14m25s, so
# that reason is weaker than it looked. The reason that survives is the false
# positive rate: 108 survivors repo-wide means a gate over all of them starts
# red on day one. `gremlins --diff` also makes the run take seconds rather than
# minutes, which is a welcome side effect rather than the argument.
#
# PINNED GREMLINS SETTINGS, and this is not tuning. Run with defaults on this
# repository, gremlins reports 465 of 1059 runnable mutants (44%) as TIMED OUT.
# They are not infinite loops — they are worker contention on a shared runner.
# Re-run with these settings, internal/api's 114 timeouts become 0 and reveal 7
# survivors the default run had hidden; module-wide the survivor count goes from
# 57 to 108. A gate built on the default configuration would report on a little
# over half of what it claims to measure.
#
# Escape hatches, narrow before broad, matching //coverage:exempt:
#   //mutation:exempt[<TYPE>] <reason>
#                                on the mutated line, or the line directly above
#                                it. <TYPE> is gremlins' mutant type, the third
#                                column of this script's own output, and the
#                                waiver covers THAT TYPE ONLY. A comma-separated
#                                list names several; [*] waives every mutant on
#                                the line, which is a claim about mutants nobody
#                                has looked at and is labelled as such in the
#                                report. A reason is required, and an untyped
#                                marker exempts nothing — both for the same
#                                reason, that a waiver nobody stated the scope
#                                or the grounds of is one nobody reviewed. The
#                                marker covers an undecided mutant of that type
#                                too, not only a survivor.
#   MUTATION_EXEMPT=1            the whole branch, from the CI label.
#
# Exit codes: 0 = the gate passed, or had nothing to check; 1 = a mutant on a
# changed line survived or reached no verdict, or the gate could not run.

set -u

base=${MUTATION_BASE:-}
report=""
gremlins_bin=${GREMLINS:-gremlins}
# Both pinned above; overridable so a future measurement can move them without
# editing the script, but never defaulted to gremlins' own values.
timeout_coefficient=${MUTATION_TIMEOUT_COEFFICIENT:-10}
workers=${MUTATION_WORKERS:-2}

while [ $# -gt 0 ]; do
	case $1 in
	--base)
		[ $# -ge 2 ] || {
			echo "mutation: --base needs a ref"
			exit 1
		}
		base=$2
		shift 2
		;;
	--report)
		[ $# -ge 2 ] || {
			echo "mutation: --report needs a file"
			exit 1
		}
		report=$2
		shift 2
		;;
	-h | --help)
		# The header, however long it is. A hand-counted line range goes
		# stale the moment the header grows, and it fails in the quiet
		# direction: it TRUNCATES, dropping whole paragraphs with nothing
		# in the output to say so. Simulated against this header, the old
		# `2,80p` stops mid-sentence in "PINNED GREMLINS SETTINGS".
		awk 'NR > 1 && /^#/ { print; next } NR > 1 { exit }' "$0" |
			sed 's/^#\{1,2\} \{0,1\}//'
		exit 0
		;;
	*)
		echo "mutation: unknown argument '$1'"
		exit 1
		;;
	esac
done

repo_root=$(git rev-parse --show-toplevel 2> /dev/null || pwd)

if [ -z "$base" ]; then
	echo "mutation: ERROR — no base ref."
	echo "  This check is scoped to the diff, so it cannot run without one:"
	echo "    MUTATION_BASE=origin/main make mutation"
	exit 1
fi

if ! git rev-parse --verify --quiet "$base" > /dev/null 2>&1; then
	echo "mutation: ERROR — base ref '$base' does not exist."
	echo "  Without it there is no diff to scope this to, so it cannot run."
	echo "  In CI this usually means the checkout lost 'fetch-depth: 0'."
	exit 1
fi

# Checked, not assumed — the fail-open #188 found in the same family. A failing
# `git merge-base` prints nothing, the empty ref makes the diff below error out
# and name no files, and the run lands in "no Go implementation changed" and
# exits 0 having decided nothing. `rev-parse --verify` above does not cover it:
# in a clone whose history does not reach the merge base the ref resolves fine
# and only this command fails.
merge_base=$(git merge-base "$base" HEAD 2> /dev/null) || merge_base=""
if [ -z "$merge_base" ]; then
	echo "mutation: ERROR — no merge base between '$base' and HEAD."
	echo "  Without it there is no diff to scope this to, so it cannot run."
	echo "  In CI this usually means the checkout lost 'fetch-depth: 0'."
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

tab=$(printf '\t')

# The changed Go implementation files. _test.go is excluded on purpose: mutating
# a test asks whether the tests test the tests, and what judges a changed test
# is the red-check (G4(b)), which reverts the implementation under it. This gate
# judges changed implementation.
changed=""
for f in $(git -C "$repo_root" diff --name-only "$merge_base" HEAD); do
	case $f in
	*_test.go) ;;
	*.go) changed="$changed $f" ;;
	esac
done

if [ -z "$changed" ]; then
	echo "mutation: no Go implementation changed against $base — nothing to check."
	exit 0
fi

# ── The gremlins run ──────────────────────────────────────────────────────────

if [ -z "$report" ]; then
	if ! command -v "$gremlins_bin" > /dev/null 2>&1; then
		echo "mutation: ERROR — '$gremlins_bin' not found on PATH."
		echo "  Install it, or pass a report from an earlier run:"
		echo "    go install github.com/go-gremlins/gremlins/cmd/gremlins@latest"
		echo "    sh scripts/mutation.sh --base $base --report gremlins.json"
		exit 1
	fi
	report="$tmp/gremlins.json"
	# --diff takes the merge base rather than the base ref itself, so a base
	# branch that has moved ahead does not drag its own commits into this
	# branch's scope.
	if ! (cd "$repo_root" && "$gremlins_bin" unleash \
		--diff "$merge_base" \
		--timeout-coefficient "$timeout_coefficient" \
		--workers "$workers" \
		--output "$report" \
		. > "$tmp/gremlins.log" 2>&1); then
		echo "mutation: ERROR — gremlins exited non-zero."
		sed 's/^/  /' "$tmp/gremlins.log" | tail -20
		exit 1
	fi
fi

case $report in
/*) report_path=$report ;;
*) report_path=$PWD/$report ;;
esac

if [ ! -f "$report_path" ]; then
	echo "mutation: ERROR — no gremlins report at '$report'."
	echo "  A report is how this check knows what ran; without one it would"
	echo "  report no survivors having analysed nothing."
	exit 1
fi

# ── Reading the report ────────────────────────────────────────────────────────
#
# The report is JSON, and this is a POSIX shell script, so the parse is a
# deliberately small one: it walks the file recording the current file_name and
# emitting one line per mutation. It does NOT tolerate a shape it does not
# recognise — an empty or truncated report is what a killed gremlins run leaves
# behind, and "no files key" must never read as "no survivors".
module=""
if [ -f "$repo_root/go.mod" ]; then
	module=$(awk '$1 == "module" { print $2; exit }' "$repo_root/go.mod")
fi

# The shape check. `files` is the one key every gremlins report has, including
# one that analysed nothing (`"files":[]`), so its absence means the file is not
# a gremlins report at all — truncated, empty, or something else entirely.
if ! grep -q '"files"' "$report_path"; then
	echo "mutation: ERROR — '$report' is not a gremlins report."
	echo "  It has no \"files\" key, which is what a truncated or empty report"
	echo "  looks like. Reading it as 'no survivors' would state a result"
	echo "  nothing measured."
	exit 1
fi

# One mutation per line, as file<TAB>line<TAB>status<TAB>type. Braces become
# record separators so each mutation object is read whole — pairing a status
# with the type and line from its OWN record rather than reading ahead into the
# next one.
awk -v OFS="$tab" '
	{
		s = $0
		gsub(/[{}]/, "\n", s)
		n = split(s, recs, "\n")
		for (i = 1; i <= n; i++) {
			r = recs[i]
			if (index(r, "file_name") > 0) {
				if (match(r, /"file_name"[ \t]*:[ \t]*"[^"]*"/)) {
					seg = substr(r, RSTART, RLENGTH)
					if (match(seg, /:[ \t]*"[^"]*"$/)) {
						cur = substr(seg, RSTART + 1, RLENGTH - 1)
						gsub(/^[ \t]*"|"$/, "", cur)
					}
				}
			}
			if (index(r, "\"status\"") == 0) continue
			ty = ""; st = ""; ln = ""
			if (match(r, /"type"[ \t]*:[ \t]*"[^"]*"/)) {
				seg = substr(r, RSTART, RLENGTH); sub(/^"type"[ \t]*:[ \t]*"/, "", seg); sub(/"$/, "", seg); ty = seg
			}
			if (match(r, /"status"[ \t]*:[ \t]*"[^"]*"/)) {
				seg = substr(r, RSTART, RLENGTH); sub(/^"status"[ \t]*:[ \t]*"/, "", seg); sub(/"$/, "", seg); st = seg
			}
			if (match(r, /"line"[ \t]*:[ \t]*[0-9]+/)) {
				seg = substr(r, RSTART, RLENGTH); sub(/^"line"[ \t]*:[ \t]*/, "", seg); ln = seg
			}
			if (cur != "" && ln != "" && st != "") print cur, ln, st, ty
		}
	}
' "$report_path" > "$tmp/mutations"

# ── Scoping to the diff ───────────────────────────────────────────────────────

# touched_lines <file> — the line numbers this branch added or modified, against
# the file as it stands now. -U0 keeps the hunk headers exact so no untouched
# neighbour is swept in.
#
# `-C "$repo_root"` is load-bearing. `git diff --name-only` prints
# repository-relative paths wherever it runs, but a pathspec after `--` resolves
# against the caller's cwd — so from a subdirectory every pathspec would miss,
# every touched-line set would come back empty, and this would report nothing
# having analysed nothing. #188 found exactly that bug in the per-block gate.
touched_lines() {
	git -C "$repo_root" diff -U0 "$merge_base" HEAD -- "$1" |
		awk '
			/^@@/ {
				plus = $3
				sub(/^\+/, "", plus)
				n = split(plus, parts, ",")
				start = parts[1] + 0
				count = (n > 1) ? parts[2] + 0 : 1
				for (i = 0; i < count; i++) print start + i
			}
		'
}

: > "$tmp/findings"
: > "$tmp/exempt"
: > "$tmp/undecided"
: > "$tmp/skipped"
# Mutants that reached a real verdict about the tests — LIVED or KILLED, and
# nothing else. It is what separates "this run found no survivors" from "this
# run never asked": if gremlins skipped everything it was handed, the first
# sentence is true and worthless, and only this counter can tell them apart.
decided_count=0
# The two statuses this gate stays silent about, counted only so the
# "decided nothing" check can name what a run was actually made of.
notcovered_count=0
notviable_count=0
# Every mutant that survived the diff filter, whatever its status. It is the
# denominator the counts below are reported against: "2 undecided" says nothing
# about whether the run was mostly useless or almost complete.
scoped_count=0
# What the line filter discarded. On #234 — six non-test Go files, 35 hunks —
# gremlins produced 128 mutants in the touched files and exactly 5 sat on a
# changed line. Without this number a scoped_count of 0 is unreadable: it could
# mean the files held nothing to mutate, or that the scope threw everything
# away, and those call for different reactions.
file_mutant_count=0
bare_marker=0
untyped_marker=0
malformed_marker=0

for f in $changed; do
	# Both spellings of the path. gremlins reports repository-relative paths;
	# `<module>/<path>` is what coverage.out uses, and accepting it costs one
	# comparison. Guessing wrong would leave every finding unmatched, which is
	# this check reporting green.
	# Tab-separated on the way out as well as in. gremlins' statuses include
	# "NOT COVERED", which has a space in it: under whitespace separation the
	# reader below splits it into status="NOT" and folds "COVERED" into the
	# type.
	#
	# THE STAKE ROSE WITH THE CATCH-ALL. This note used to say the hazard was
	# invisible — every status the script did not name fell into the same
	# `continue`, so a split "NOT" behaved exactly like "NOT COVERED". That is
	# no longer true. The default arm now REPORTS, so a split "NOT" matches
	# nothing, lands in the undecided list, and every NOT COVERED and NOT
	# VIABLE mutant is announced as an unknown — loudly, immediately and
	# wrongly. Confirmed by perturbation: dropping the reader's `IFS="$tab"`
	# alone fails four cases. The separator went from load-bearing and silent
	# to load-bearing and loud.
	MOD="$module" FILE="$f" awk -F"$tab" -v OFS="$tab" '
		BEGIN { mod = ENVIRON["MOD"]; file = ENVIRON["FILE"]; alt = (mod == "") ? "" : mod "/" file }
		$1 == file || (alt != "" && $1 == alt) { print $2, $3, $4 }
	' "$tmp/mutations" > "$tmp/file_mutations"
	file_mutant_count=$((file_mutant_count + $(wc -l < "$tmp/file_mutations" | tr -d ' ')))
	[ -s "$tmp/file_mutations" ] || continue

	# AFTER the file-level count, not before it. `touched_lines` reports the
	# lines a diff ADDS, so a file this branch only deleted from has an empty
	# set — and with the skip ahead of the counter, such a file contributed
	# nothing to "what the touched files held". A zero-scope run then claimed
	# there were no mutants anywhere in those files about a file that still
	# holds them, which is the one sentence this counter exists to get right.
	touched_lines "$f" | sort -un > "$tmp/touched"
	[ -s "$tmp/touched" ] || continue

	while IFS="$tab" read -r line status type; do
		[ -n "$line" ] || continue
		grep -qx -- "$line" "$tmp/touched" || continue

		scoped_count=$((scoped_count + 1))

		# THE ENUMERATION THAT HAS TO BE EXHAUSTIVE IS THE SILENCING ONE, and
		# the catch-all used to be on the other arm. A status this script did
		# not recognise fell through to `continue` and vanished — TIMED OUT and
		# NOT VIABLE did exactly that, and so would any status a later gremlins
		# invents. The default is now "nothing is known about this", which is
		# the only honest thing to say about a verdict string nobody matched.
		case $status in
		LIVED)
			decided_count=$((decided_count + 1))
			kind=survivor
			;;
		KILLED)
			decided_count=$((decided_count + 1))
			continue
			;;
		SKIPPED)
			# NOT A FAILURE, AND THIS IS THE ONE PLACE THIS GATE DEFERS TO A
			# TOOL'S OPINION OVER ITS OWN. gremlins sets SKIPPED from its own
			# diff and nothing else (internal/engine/engine.go):
			#
			#     if Cov.IsCovered(pos)    { status = Runnable }
			#     if !Diff.IsChanged(pos)  { status = Skipped }
			#
			# So it means "gremlins' diff says this line did not change", which
			# is a claim about two diff implementations disagreeing, not about
			# anybody's tests. The disagreement is real and reproducible rather
			# than theoretical: internal/diff/diff.go builds each changed range
			# as `EndLine = startLine + LinesAdded - 1`, which assumes a hunk's
			# added lines run contiguously from the fragment's start. They do
			# not when the hunk mixes context, deletions and additions. Measured
			# on #231, git's `@@ -50 +51,11 @@` covers line 58 and gremlins'
			# window does not, so two mutants on a line that branch demonstrably
			# added came back SKIPPED.
			#
			# There is no edit an author can make to that line to change it.
			# Failing here would be principle 4's exact shape: a red build for a
			# condition the person reading it cannot act on. It is still
			# reported, because two diff notions disagreeing is worth seeing —
			# and the "decided nothing" check below covers the case where the
			# disagreement is total.
			kind=skipped
			;;
		"NOT COVERED" | "NOT VIABLE")
			# Two of the states this gate deliberately says nothing about, each
			# for its own reason. NOT COVERED belongs to G4(d), which fails on
			# it with a clearer message; reporting it here too would have two
			# gates arguing about one defect. NOT VIABLE means the mutant did
			# not compile, so no test could ever have noticed it behaving
			# differently — there is no hole in the suite and nothing for anyone
			# to do. KILLED, the good case, is counted above rather than here,
			# because the count of mutants that reached a real verdict is what
			# the "decided nothing" check below is built on.
			#
			# Counted, though, so that check can say what a run was made of.
			# Neither is reported as a finding — that is still G4(d)'s job for
			# NOT COVERED and nobody's for NOT VIABLE — but a run consisting of
			# nothing else decided nothing, and has to be able to say which.
			case $status in
			"NOT COVERED") notcovered_count=$((notcovered_count + 1)) ;;
			*) notviable_count=$((notviable_count + 1)) ;;
			esac
			continue
			;;
		*)
			# TIMED OUT (the suite never reached a verdict), RUNNABLE
			# (identified and covered, but never run — what `--dry-run` leaves
			# behind), and anything unrecognised. The status travels with the
			# record so the list below can say which of those it was.
			#
			# These fail the build, and SKIPPED above does not, which is the
			# distinction this gate got wrong on its first attempt at stage 4
			# and #231's real report caught. The difference is whether anybody
			# can act on it: a TIMED OUT mutant is this gate trying to get an
			# answer and not getting one — re-run it, or waive it — while a
			# SKIPPED mutant is a second diff implementation declining to be
			# asked. "Could not check" is a failure; "was not asked, by a
			# component whose mind the author cannot change" is not.
			#
			# It no longer skips the marker lookup below, and at stage 4 that is
			# the difference between a usable gate and a flaky one: an undecided
			# mutant now fails the build, so a line whose author has already
			# written "this mutant need not be killed" would fail on a slow
			# runner for a mutant nobody is asking anyone to kill. Whether a
			# verdict was reached is a fact about the runner; whether the mutant
			# is worth killing is the claim the marker makes, and only the second
			# one is the author's.
			kind=undecided
			;;
		esac

		marker=$(sed -n "${line}p" "$repo_root/$f" 2> /dev/null)
		above=""
		if [ "$line" -gt 1 ]; then
			above=$(sed -n "$((line - 1))p" "$repo_root/$f" 2> /dev/null)
		fi
		# The window is the mutated line, plus the line above ONLY when that
		# line is a comment. #188's own marker had this bug: reading the two
		# lines as one string let a marker on one construct's opening line
		# waive the construct on the next, with no reason ever written for it.
		window=$marker
		case $above in
		*//mutation:exempt*)
			case $above in
			*[!\ ]*)
				trimmed=$(printf '%s' "$above" | sed 's/^[[:space:]]*//')
				case $trimmed in
				//*) window="$marker
$above" ;;
				esac
				;;
			esac
			;;
		esac

		# THE MARKER WAIVES A MUTANT TYPE, NOT A LINE. It used to match on
		# file and line alone and never look at $type, so a reason written
		# about the boundary mutant waived every other mutant gremlins produced
		# on that line — #180's judgement note 3, and measured: all eleven
		# markers in this repository sit on a line carrying one to three
		# further types. None of them was hiding a survivor, so the gap was
		# structural rather than live; at stage 4, where a survivor becomes a
		# failure, it is the difference between a red gate and a green one with
		# nothing in the diff to show for it.
		#
		# Prints one verdict word, and for a mismatch the types the line's
		# markers DO name, so the finding can say what is there.
		verdict=$(printf '%s\n' "$window" | MUTANT_TYPE="$type" awk -v OFS="$tab" '
			BEGIN { want = ENVIRON["MUTANT_TYPE"]; tag = "//mutation:exempt"; taglen = length(tag) }
			{
				rest = $0
				while ((i = index(rest, tag)) > 0) {
					rest = substr(rest, i + taglen)
					spec = ""
					if (substr(rest, 1, 1) == "[") {
						j = index(rest, "]")
						if (j == 0) { malformed = 1; continue }
						spec = substr(rest, 2, j - 2)
						rest = substr(rest, j + 1)
					}
					# A second marker on the same line is not this one\047s reason.
					reason = rest
					k = index(reason, tag)
					if (k > 0) reason = substr(reason, 1, k - 1)
					sub(/^[ \t]+/, "", reason)
					sub(/[ \t]+$/, "", reason)
					if (reason == "") { bare = 1; continue }
					if (spec == "") { untyped = 1; continue }
					n = split(spec, types, ",")
					for (t = 1; t <= n; t++) {
						one = types[t]
						gsub(/[ \t]/, "", one)
						# An empty entry (a trailing comma) must match
						# nothing, and a mutant whose report carried no type
						# must be matchable by nothing but [*]. Without both
						# guards the two empties meet and the marker waives a
						# mutant nobody wrote a word about.
						if (one == "") continue
						if (one == "*") wildcard = 1
						else if (want != "" && one == want) matched = 1
						else claimed = claimed (claimed == "" ? "" : ", ") one
					}
				}
			}
			END {
				if (matched) print "exempt"
				else if (wildcard) print "wildcard"
				else if (bare) print "bare"
				else if (untyped) print "untyped"
				else if (malformed) print "malformed"
				else if (claimed != "") print "mismatch", claimed
				else print "none"
			}
		')
		claimed=""
		case $verdict in
		mismatch*)
			claimed=${verdict#*"$tab"}
			verdict=mismatch
			;;
		esac

		# A marker that waives nothing is worth saying so about whichever arm the
		# mutant lands in, so these are set before the routing rather than inside
		# the survivor branch of it.
		case $verdict in
		bare) bare_marker=1 ;;
		untyped) untyped_marker=1 ;;
		malformed) malformed_marker=1 ;;
		esac

		case $verdict in
		exempt | wildcard)
			# [*] is labelled, because it is a claim about mutants nobody looked
			# at — what the untyped marker used to do silently. An undecided
			# mutant carries its status into the label for the opposite reason:
			# waived is not hidden, and "waived a survivor" and "waived something
			# that never reached a verdict" are different claims to have reviewed.
			label=""
			[ "$verdict" = wildcard ] && label="line-wide [*]"
			if [ "$kind" != survivor ]; then
				label="${label:+$label, }$status"
			fi
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$label" >> "$tmp/exempt"
			continue
			;;
		esac

		case $kind in
		skipped)
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$status" >> "$tmp/skipped"
			continue
			;;
		undecided)
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$status" >> "$tmp/undecided"
			continue
			;;
		esac

		note=""
		case $verdict in
		mismatch)
			# The one that used to be invisible. Naming what IS on the line
			# turns "why is this still reported" into a one-line answer.
			note="//mutation:exempt on this line names $claimed"
			;;
		esac
		printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$note" >> "$tmp/findings"
	done < "$tmp/file_mutations"
done

kept_count=$(wc -l < "$tmp/findings" | tr -d ' ')
exempt_count=$(wc -l < "$tmp/exempt" | tr -d ' ')
undecided_count=$(wc -l < "$tmp/undecided" | tr -d ' ')
skipped_count=$(wc -l < "$tmp/skipped" | tr -d ' ')

exempt_note=""
[ "$exempt_count" -gt 0 ] && exempt_note=" ($exempt_count exempt)"

# In the HEADLINE, not only in the section below it. "no surviving mutants on
# lines this branch changed" is a true sentence about a run that decided nothing
# and a false impression, and the headline is the line a reviewer reads — the
# same rule the header states about a warning that could not run.
undecided_note=""
[ "$undecided_count" -gt 0 ] && undecided_note=", $undecided_count undecided"

# Reported in the headline like the rest, and separately from them, because it
# is the one count here that does not by itself fail the build. Folding it into
# "undecided" would put a number a reader must act on and a number they cannot
# act on under one word.
skipped_note=""
[ "$skipped_count" -gt 0 ] && skipped_note=", $skipped_count skipped by gremlins"

# THE DENOMINATOR IS PART OF THE RESULT, not context for it. "no surviving
# mutants" rests on five mutants or on fifty, and the sentence is identical
# either way — which is the same conflation one level up from the one above.
# Stage 4 cannot be decided without it: a gate that would rarely fire is not
# thereby a safe gate, it is a gate that is often saying nothing.
scope_note=" among $scoped_count on lines this branch changed"

# Listed, not counted. A count says an exemption happened; only the list says
# WHICH, and an exemption a reviewer cannot see is one nobody reviewed. #188
# changed its own gate from counting to listing for this reason.
exempt_list() {
	[ -s "$tmp/exempt" ] || return 0
	echo
	echo "  Exempt by //mutation:exempt:"
	awk -F"$tab" '{ if ($4 == "") printf "    %s:%s  %s\n", $1, $2, $3; else printf "    %s:%s  %s  — %s\n", $1, $2, $3, $4 }' "$tmp/exempt"
}

undecided_list() {
	[ -s "$tmp/undecided" ] || return 0
	echo
	echo "  Undecided — these mutants sit on lines this branch changed and never"
	echo "  reached a verdict, so nothing is known about them either way:"
	awk -F"$tab" '{ printf "    %s:%s  %s  (%s)\n", $1, $2, $3, $4 }' "$tmp/undecided"
	echo
	# Only the statuses that can actually land in this list. SKIPPED used to be
	# described here and now has its own section and its own consequence, so
	# explaining it here put an answer about a status that cannot appear above
	# the rows, and contradicted the section below about whether it fails.
	echo "  TIMED OUT means the suite never finished under the mutant — usually"
	echo "  worker contention, and #180's measurement found timeouts hiding real"
	echo "  survivors. RUNNABLE means the mutant was identified and covered but"
	echo "  never run, which is what --dry-run leaves behind. Anything else is a"
	echo "  status this gate does not recognise, which is itself worth looking at."
}

skipped_list() {
	[ -s "$tmp/skipped" ] || return 0
	echo
	echo "  Skipped by gremlins — on lines this branch changed, but gremlins' own"
	echo "  diff disagrees and did not run them. Not a failure on its own:"
	awk -F"$tab" '{ printf "    %s:%s  %s  (%s)\n", $1, $2, $3, $4 }' "$tmp/skipped"
	echo
	echo "  gremlins sets SKIPPED purely from its own diff, never from a test"
	echo "  result, and its changed-line window is an approximation: it takes"
	echo "  each hunk's added lines to run contiguously from the fragment's"
	echo "  start, which is wrong whenever a hunk mixes context, deletions and"
	echo "  additions. There is nothing to fix on the line, so this gate reports"
	echo "  the disagreement and does not fail on it. What does fail is a run in"
	echo "  which NOTHING reached a verdict, whatever the mix of reasons."
}

# A marker that waives nothing is worth a sentence saying so. Each of these
# is silent unless one was actually seen, so a clean run stays clean.
marker_notes() {
	if [ "$bare_marker" -eq 1 ]; then
		echo
		echo "  Note: a //mutation:exempt with no reason after it exempts nothing."
	fi
	if [ "$untyped_marker" -eq 1 ]; then
		echo
		echo "  Note: a //mutation:exempt with no [<TYPE>] exempts nothing. The"
		echo "  marker waives one mutant type, not a whole line — write"
		echo "  //mutation:exempt[CONDITIONALS_BOUNDARY] <reason>, taking the type"
		echo "  from the third column above, or [*] to waive the line knowingly."
	fi
	if [ "$malformed_marker" -eq 1 ]; then
		echo
		echo "  Note: a //mutation:exempt with an unclosed [ exempts nothing."
	fi
}

# Nothing reached a verdict because there was nothing to reach one about. Not a
# pass and not a failure: this gate asked no question, and has to say that
# rather than borrow the wording of a branch whose mutants were all killed.
if [ "$scoped_count" -eq 0 ]; then
	echo "mutation: no mutants on the lines this branch changed — nothing was measured."
	echo
	if [ "$file_mutant_count" -gt 0 ]; then
		echo "  gremlins produced $file_mutant_count mutant(s) in the files this branch touched, and"
		echo "  none of them sits on a line the diff changed. That is what the line"
		echo "  scope is for (decision D2) and what it costs: this run says nothing"
		echo "  about whether the change is protected."
	else
		echo "  gremlins produced no mutants at all in the files this branch touched."
		echo "  It mutates operator tokens in covered code, so a diff of type"
		echo "  declarations, struct fields, plain returns or literals has nothing"
		echo "  for it to change."
	fi
	marker_notes
	exit 0
fi

if [ "${MUTATION_EXEMPT:-0}" = "1" ]; then
	# EVERYTHING THE LABEL WAIVED, not only the survivors. Once an undecided
	# mutant fails the build, a run rescued solely by this label could report
	# "waived 0 finding(s)" — a label claiming to have waived nothing, in the
	# run that would have been red without it. The undecided count belongs in
	# the same sentence for the same reason the headline carries it elsewhere.
	echo "mutation: exempt — MUTATION_EXEMPT=1 waived $kept_count finding(s) and $undecided_count undecided mutant(s)$scope_note$exempt_note$skipped_note."
	if [ "$decided_count" -eq 0 ] && [ "$exempt_count" -lt "$scoped_count" ]; then
		# The third thing it waives, and the one with no count of its own:
		# without the label this run would have failed for having decided
		# nothing at all, which is not the same as having found something.
		echo
		echo "  It also waived the failure for deciding nothing: no mutant on a"
		echo "  changed line reached a verdict in this run."
	fi
	exempt_list
	undecided_list
	skipped_list
	exit 0
fi

# NOT ONE MUTANT REACHED A VERDICT, so "no surviving mutants" would rest on
# nothing measured — the one sentence this script's header forbids. Each
# individual cause is one this gate deliberately does not fail on: a SKIPPED
# mutant is gremlins' diff disagreeing with this gate's, a NOT COVERED one is
# G4(d)'s defect to report, a NOT VIABLE one is nobody's. A run made of nothing
# but those is a different claim from any of them, and it is about the RUN
# rather than about a mutant: this gate asked, and learned nothing.
#
# The first version of this keyed on `skipped_count > 0` while its own comment
# claimed `decided_count` was the whole test, and review caught the two holes
# that opened. A run of nothing but NOT COVERED printed "ok — no surviving
# mutants among 1"; so did a run of nothing but SKIPPED once each one was
# waived. `make coverage-blocks` does not close the first: it reports a changed
# file in a package COVERAGE_PKGS excludes as "not measured" rather than
# failing, so there are diffs about which no gate would have said anything.
#
# The exception is an explicit waiver of EVERY scoped mutant. A marker is a
# typed, reasoned, reviewable claim that a mutant need not be killed, and it
# does not depend on whether that mutant ran. Failing anyway would make the
# marker powerless in the one run where the author has said the most. Partial
# waivers do not count: they speak for part of the run.
if [ "$decided_count" -eq 0 ] && [ "$exempt_count" -lt "$scoped_count" ]; then
	echo "mutation: FAIL — nothing was decided$scope_note$undecided_note$skipped_note$exempt_note."
	exempt_list
	undecided_list
	skipped_list
	marker_notes
	echo
	echo "  Not one mutant on a changed line reached a verdict, so this run has"
	echo "  no evidence either way — and it must not print the sentence a clean"
	echo "  branch gets. What it was made of:"
	[ "$skipped_count" -gt 0 ] && echo "    $skipped_count skipped — gremlins' diff disagrees with this gate's (see above)."
	[ "$undecided_count" -gt 0 ] && echo "    $undecided_count undecided — the suite reached no verdict. Re-run, or waive."
	[ "$notcovered_count" -gt 0 ] && echo "    $notcovered_count not covered — no test executes the line. 'make coverage-blocks' names these."
	[ "$notviable_count" -gt 0 ] && echo "    $notviable_count not viable — the mutant did not compile. Nothing to fix; use the label."
	exit 1
fi

if [ "$kept_count" -eq 0 ] && [ "$undecided_count" -eq 0 ]; then
	# "ok" rather than a bare sentence, matching scripts/efficacy.sh. Once one
	# headline says FAIL, "no FAIL in the output" becomes how people read a
	# result — and that is how a truncated or crashed run gets read as a pass.
	# The passing line has to be as scannable as the failing one.
	echo "mutation: ok — no surviving mutants$scope_note$skipped_note$exempt_note."
	exempt_list
	skipped_list
	marker_notes
	exit 0
fi

if [ "$kept_count" -eq 0 ]; then
	# Nothing survived, and that is not enough. The sentence is deliberately the
	# same one stage 3 printed — the counts were already there (#235, #237) —
	# and only the verdict in front of it and the exit code are new, because the
	# facts were never the problem: exiting 0 on them was.
	echo "mutation: FAIL — no surviving mutants$scope_note$undecided_note$skipped_note$exempt_note."
	exempt_list
	undecided_list
	skipped_list
	marker_notes
	echo
	echo "  Nothing survived, but nothing was decided about $undecided_count of them either,"
	echo "  so this run cannot say the change is protected. A mutant that reached"
	echo "  no verdict is the gate failing to run, one mutant at a time, and"
	echo "  'could not check' is a failure rather than a skip here for the same"
	echo "  reason it is in scripts/efficacy.sh."
	echo
	echo "  A TIMED OUT mutant is usually worker contention: re-run it. If the"
	echo "  mutant is one you would have waived anyway, waive it — a"
	echo "  '//mutation:exempt[<TYPE>] <reason>' covers an undecided mutant of"
	echo "  that type exactly as it covers a survivor."
	exit 1
fi

echo "mutation: FAIL — $kept_count surviving mutant(s)$scope_note$exempt_note$undecided_note$skipped_note."
echo
echo "  A surviving mutant is a change to your code that every test still"
echo "  passes through. Either an assertion is missing, or the mutant is one"
echo "  of the kinds worth waiving — a tuning constant, or a branch that needs"
echo "  fault injection to reach."
echo
awk -F"$tab" '{ if ($4 == "") printf "    %s:%s  %s\n", $1, $2, $3; else printf "    %s:%s  %s\n        %s\n", $1, $2, $3, $4 }' "$tmp/findings"
exempt_list
undecided_list
skipped_list

marker_notes

echo
echo "  This fails the build. Add the assertion, or waive the one mutant with"
echo "  '//mutation:exempt[<TYPE>] <reason>' on the line or directly above it —"
echo "  <TYPE> is the third column above, and the reason is required. Say which"
echo "  kind it is: EQUIVALENT means no input can tell the mutant from the"
echo "  original; UNREACHABLE means it is killable, but only by input this"
echo "  code's own callers cannot produce. They are not interchangeable, and"
echo "  #190 filed a live defect as 'equivalent' when it was neither."
echo
echo "  The mutation-exempt label waives the whole branch and is the blunter"
echo "  tool: it leaves nothing in the diff a reviewer can read."
exit 1
