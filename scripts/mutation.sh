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
#   //mutation:exempt <reason>   on the mutated line, or the line directly
#                                above it. A reason is required — a bare marker
#                                exempts nothing.
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
		sed -n '2,80p' "$0" | sed 's/^#\{1,2\} \{0,1\}//'
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
: > "$tmp/unanalysed"
bare_marker=0

for f in $changed; do
	touched_lines "$f" | sort -un > "$tmp/touched"
	[ -s "$tmp/touched" ] || continue

	# Both spellings of the path. gremlins reports repository-relative paths;
	# `<module>/<path>` is what coverage.out uses, and accepting it costs one
	# comparison. Guessing wrong would leave every finding unmatched, which is
	# this check reporting green.
	# Tab-separated on the way out as well as in. gremlins' statuses include
	# "NOT COVERED", which has a space in it: with awk's default OFS the reader
	# below would split it into status="NOT" and fold "COVERED" into the type.
	# Today that lands in the same branch either way, so nothing visibly breaks
	# — which is precisely why it would have survived until a status this gate
	# does act on gained a space.
	MOD="$module" FILE="$f" awk -F"$tab" -v OFS="$tab" '
		BEGIN { mod = ENVIRON["MOD"]; file = ENVIRON["FILE"]; alt = (mod == "") ? "" : mod "/" file }
		$1 == file || (alt != "" && $1 == alt) { print $2, $3, $4 }
	' "$tmp/mutations" > "$tmp/file_mutations"
	[ -s "$tmp/file_mutations" ] || continue

	while IFS="$tab" read -r line status type; do
		[ -n "$line" ] || continue
		grep -qx -- "$line" "$tmp/touched" || continue

		case $status in
		LIVED) ;;
		SKIPPED)
			# gremlins decided this mutant was outside its own notion of the
			# diff, but it is on a line this branch changed. Nothing ran it,
			# so nothing can be claimed about it.
			printf '%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" >> "$tmp/unanalysed"
			continue
			;;
		*)
			# KILLED is the good case. NOT COVERED belongs to G4(d), which
			# fails on it with a clearer message; reporting it here too would
			# have two gates arguing about one defect.
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

		case $window in
		*//mutation:exempt*)
			# A reason is required. An exemption nobody has to justify is how
			# an exemption stops being reviewable — the rule //coverage:exempt
			# states and this one inherits.
			if printf '%s\n' "$window" | grep -Eq '//mutation:exempt[[:space:]]+[^[:space:]]'; then
				printf '%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" >> "$tmp/exempt"
			else
				bare_marker=1
				printf '%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" >> "$tmp/findings"
			fi
			;;
		*)
			printf '%s%s%s%s%s\n' "$f" "$tab" "$line" "$tab" "$type" >> "$tmp/findings"
			;;
		esac
	done < "$tmp/file_mutations"
done

kept_count=$(wc -l < "$tmp/findings" | tr -d ' ')
exempt_count=$(wc -l < "$tmp/exempt" | tr -d ' ')
unanalysed_count=$(wc -l < "$tmp/unanalysed" | tr -d ' ')

exempt_note=""
[ "$exempt_count" -gt 0 ] && exempt_note=" ($exempt_count exempt)"

# Listed, not counted. A count says an exemption happened; only the list says
# WHICH, and an exemption a reviewer cannot see is one nobody reviewed. #188
# changed its own gate from counting to listing for this reason.
exempt_list() {
	[ -s "$tmp/exempt" ] || return 0
	echo
	echo "  Exempt by //mutation:exempt:"
	awk -F"$tab" '{ printf "    %s:%s  %s\n", $1, $2, $3 }' "$tmp/exempt"
}

unanalysed_list() {
	[ -s "$tmp/unanalysed" ] || return 0
	echo
	echo "  Not analysed — gremlins skipped these mutants although they sit on"
	echo "  lines this branch changed, so nothing is known about them:"
	awk -F"$tab" '{ printf "    %s:%s  %s\n", $1, $2, $3 }' "$tmp/unanalysed"
}

if [ "${MUTATION_EXEMPT:-0}" = "1" ]; then
	echo "mutation: exempt — MUTATION_EXEMPT=1 waived $kept_count finding(s)$exempt_note."
	exempt_list
	unanalysed_list
	exit 0
fi

if [ "$kept_count" -eq 0 ]; then
	echo "mutation: no surviving mutants on lines this branch changed$exempt_note."
	exempt_list
	unanalysed_list
	exit 0
fi

echo "mutation: $kept_count surviving mutant(s) on lines this branch changed$exempt_note."
echo
echo "  A surviving mutant is a change to your code that every test still"
echo "  passes through. Either an assertion is missing, or the mutant is one"
echo "  of the kinds worth waiving — a tuning constant, or a branch that needs"
echo "  fault injection to reach."
echo
awk -F"$tab" '{ printf "    %s:%s  %s\n", $1, $2, $3 }' "$tmp/findings"
exempt_list
unanalysed_list

if [ "$bare_marker" -eq 1 ]; then
	echo
	echo "  Note: a //mutation:exempt with no reason after it exempts nothing."
fi

echo
echo "  This is a warning: it does not fail the build. Add the assertion, or"
echo "  waive it with '//mutation:exempt <reason>' on the line or above it."
exit 0
