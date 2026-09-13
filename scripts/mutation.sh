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
# THIS IS A WARNING, NOT A GATE — stage 3 of item 6's four, and the exit code
# says so: a surviving mutant prints and exits 0. That is not timidity, it is
# what the measurement showed. Of those 108 survivors, 37 (34%) are ones nobody
# should "fix": buffer sizes (`64*1024`), timeout constants (`30*time.Second`),
# and error branches unreachable without fault injection. A test that killed the
# buffer-size mutants would pin a constant and assert nothing — a tautology, the
# exact thing G4 exists to catch. Failing on those would make this gate wrong
# more often than right in its first weeks, and principle 4 in
# docs/quality-gateway.md is that a gate which cries wolf gets routed around,
# taking the gates that do work with it. Item 6's stage 4 is to make it fail,
# once there is data saying the noise is manageable. That is a separate,
# deliberate change; there is no environment variable here to flip early.
#
# "COULD NOT RUN" IS STILL A FAILURE, and that half is not softened. A warning
# that could not run must not look like a warning that found nothing, which is
# the rule scripts/efficacy.sh and scripts/coverage_blocks.sh both state.
#
# THE SAME RULE, ONE MUTANT AT A TIME. gremlins reports six statuses. This
# script acts on one of them — LIVED, a survivor — and enumerates three more it
# deliberately says nothing about: KILLED (the good case), NOT COVERED (G4(d)
# owns that defect and reports it better), NOT VIABLE (the mutant did not
# compile, so no test could have noticed it behaving differently). Everything
# else — SKIPPED, TIMED OUT, and any status a later gremlins invents — is
# reported as UNDECIDED, carrying the status that produced it, and counted in
# the headline. It used to be the other way round, with a catch-all arm
# silently dropping every status this script did not name, which let a run
# whose mutants all timed out print "no surviving mutants".
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
#                                or the grounds of is one nobody reviewed.
#   MUTATION_EXEMPT=1            the whole branch, from the CI label.
#
# Exit codes: 0 = ran (whether or not survivors were found); 1 = could not run.

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
		LIVED) ;;
		KILLED | "NOT COVERED" | "NOT VIABLE")
			# The three states this gate deliberately says nothing about, each
			# for its own reason. KILLED is the good case. NOT COVERED belongs
			# to G4(d), which fails on it with a clearer message; reporting it
			# here too would have two gates arguing about one defect. NOT
			# VIABLE means the mutant did not compile, so no test could ever
			# have noticed it behaving differently — there is no hole in the
			# suite and nothing for anyone to do.
			continue
			;;
		*)
			# SKIPPED (gremlins' own notion of the diff was narrower than this
			# gate's), TIMED OUT (the suite never reached a verdict), and
			# anything unrecognised. The status travels with the record so the
			# list below can say which of those it was.
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$status" >> "$tmp/undecided"
			continue
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

		case $verdict in
		exempt)
			printf '%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" >> "$tmp/exempt"
			;;
		wildcard)
			# Labelled, because [*] is a claim about mutants nobody looked at.
			# That is what the untyped marker used to do silently.
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "line-wide [*]" >> "$tmp/exempt"
			;;
		*)
			note=""
			case $verdict in
			bare) bare_marker=1 ;;
			untyped) untyped_marker=1 ;;
			malformed) malformed_marker=1 ;;
			mismatch)
				# The one that used to be invisible. Naming what IS on the line
				# turns "why is this still reported" into a one-line answer.
				note="//mutation:exempt on this line names $claimed"
				;;
			esac
			printf '%s%s%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" "$tab" "$note" >> "$tmp/findings"
			;;
		esac
	done < "$tmp/file_mutations"
done

kept_count=$(wc -l < "$tmp/findings" | tr -d ' ')
exempt_count=$(wc -l < "$tmp/exempt" | tr -d ' ')
undecided_count=$(wc -l < "$tmp/undecided" | tr -d ' ')

exempt_note=""
[ "$exempt_count" -gt 0 ] && exempt_note=" ($exempt_count exempt)"

# In the HEADLINE, not only in the section below it. "no surviving mutants on
# lines this branch changed" is a true sentence about a run that decided nothing
# and a false impression, and the headline is the line a reviewer reads — the
# same rule the header states about a warning that could not run.
undecided_note=""
[ "$undecided_count" -gt 0 ] && undecided_note=", $undecided_count undecided"

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
	echo "  SKIPPED means gremlins' own notion of the diff was narrower than this"
	echo "  gate's. TIMED OUT means the suite never finished under the mutant —"
	echo "  usually worker contention, and #180's measurement found timeouts"
	echo "  hiding real survivors. Anything else is a status this gate does not"
	echo "  recognise, which is itself worth looking at."
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
	echo "mutation: exempt — MUTATION_EXEMPT=1 waived $kept_count finding(s)$scope_note$exempt_note$undecided_note."
	exempt_list
	undecided_list
	exit 0
fi

if [ "$kept_count" -eq 0 ]; then
	echo "mutation: no surviving mutants$scope_note$exempt_note$undecided_note."
	exempt_list
	undecided_list
	marker_notes
	exit 0
fi

echo "mutation: $kept_count surviving mutant(s)$scope_note$exempt_note$undecided_note."
echo
echo "  A surviving mutant is a change to your code that every test still"
echo "  passes through. Either an assertion is missing, or the mutant is one"
echo "  of the kinds worth waiving — a tuning constant, or a branch that needs"
echo "  fault injection to reach."
echo
awk -F"$tab" '{ if ($4 == "") printf "    %s:%s  %s\n", $1, $2, $3; else printf "    %s:%s  %s\n        %s\n", $1, $2, $3, $4 }' "$tmp/findings"
exempt_list
undecided_list

marker_notes

echo
echo "  This is a warning: it does not fail the build. Add the assertion, or"
echo "  waive the one mutant with '//mutation:exempt[<TYPE>] <reason>' on the"
echo "  line or directly above it — <TYPE> is the third column above."
exit 0
