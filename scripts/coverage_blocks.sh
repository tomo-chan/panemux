#!/bin/sh
#
# Per-block coverage: issue #164, gate G4(d) in docs/quality-gateway.md.
#
# `make coverage-go` gates on a statement percentage over a set of packages.
# That number cannot see an entire `if err != nil { ... }` body that no test
# ever enters, because the happy path around it carries the function well past
# the threshold on its own. Issue #164 found 28 such branches by hand in
# `internal/board` and `internal/server/board.go`, none of which the 80% gate
# had anything to say about. This script formalises the technique that found
# them:
#
#   Parse coverage.out, sum the execution count per unique block, and report
#   every block whose summed count is zero.
#
# SUMMING IS THE WHOLE CORRECTNESS ARGUMENT. `make coverage-go` runs one
# `go test` over nine package patterns with a shared `-coverpkg` list, so each
# package's test binary is linked against every gated package and emits its own
# entry for the same block. `internal/api/handler.go:1114.16,1123.3` appears
# nine times, with nine different counts. Reading the raw lines one at a time
# reports a block as unexecuted whenever ANY package's tests did not reach it,
# which on this repository is most of them.
#
# Go has no native branch coverage to defer to: golang/go#70306 is still an
# undecided proposal, and gobco — the visible third-party option — instruments
# one package at a time with no documented `-coverpkg` or `-race` story. This
# is not C1/MC/DC either. It does not ask whether every condition in a compound
# `if` was taken both ways; it asks whether the block ever ran at all, which is
# the class of gap #164 actually found.
#
# Usage:
#   make coverage-blocks                       # report: list every unexecuted block
#   sh scripts/coverage_blocks.sh --summary    # one line, printed by `make coverage-go`
#   COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks   # the gate
#   sh scripts/coverage_blocks.sh --base origin/main --profile coverage.out
#
# THE GATE IS SCOPED TO THE DIFF, and that is a decision taken from the
# measurement rather than from taste. At the commit this was written on, the
# repository has ~275 unexecuted blocks out of 1801 (~375 statements of 2772);
# the figure moves by three between runs of the identical command, which is a
# finding in its own right — see decision D8.
# A gate that failed on all of them would start red, and docs/quality-gateway.md
# principle 4 says what happens to a gate that starts red: it gets routed
# around, taking the gates that do work with it. So the gate only speaks about
# blocks covering a line THIS BRANCH changed. It starts green, it never argues
# about code nobody touched, and it needs no second exclusion list to drift out
# of date. Decision D2 scopes mutation testing to the diff for the same reason.
#
# Escape hatches, both narrow before they are broad:
#   //coverage:exempt <reason>   on the block's opening line, or the line
#                               directly above it. A reason is required — a
#                               bare marker exempts nothing.
#   COVERAGE_BLOCKS_EXEMPT=1     the whole branch, from the CI label.
#
# Exit codes: 0 = passed, or had nothing to check; 1 = a block on a changed
# line never executed, or the check could not run.

set -u

profile=coverage.out
base=${COVERAGE_BLOCKS_BASE:-}
summary_only=0

while [ $# -gt 0 ]; do
	case $1 in
	--profile)
		[ $# -ge 2 ] || {
			echo "coverage-blocks: --profile needs a file"
			exit 1
		}
		profile=$2
		shift 2
		;;
	--base)
		[ $# -ge 2 ] || {
			echo "coverage-blocks: --base needs a ref"
			exit 1
		}
		base=$2
		shift 2
		;;
	--summary)
		summary_only=1
		shift
		;;
	-h | --help)
		sed -n '2,60p' "$0" | sed 's/^#\{1,2\} \{0,1\}//'
		exit 0
		;;
	*)
		echo "coverage-blocks: unknown argument '$1'"
		exit 1
		;;
	esac
done

repo_root=$(git rev-parse --show-toplevel 2> /dev/null || pwd)

# "Could not check" is a failure, never a skip — the same rule scripts/efficacy.sh
# records. A missing profile is the likely shape of that here: someone wired
# this into CI without `make coverage-go` ahead of it, and a green job that
# measured nothing is exactly what a required check exists to rule out.
if [ ! -f "$profile" ]; then
	echo "coverage-blocks: ERROR — no coverage profile at '$profile'."
	echo "  Run 'make coverage-go' first, or pass --profile <file>."
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

tab=$(printf '\t')

# The module path, so profile keys (`panemux/internal/api/handler.go`) become
# repository-relative paths (`internal/api/handler.go`) that a git diff and a
# `sed -n` can both use.
module=""
if [ -f "$repo_root/go.mod" ]; then
	module=$(awk '$1 == "module" { print $2; exit }' "$repo_root/go.mod")
fi

# Sum per unique block, then emit the zero-count ones as
# file<TAB>startLine<TAB>endLine<TAB>statements, plus a TOTALS line.
#
# A profile line is `path:startLine.startCol,endLine.endCol numStmts count`.
# The path is matched by anchoring the position/extent suffix at the end rather
# than by splitting on ":", which would break on any path containing one.
awk -v module="$module" -v OFS="$tab" '
	NR == 1 && /^mode:/ { next }
	NF != 3 { next }
	{
		key = $1
		if (!(key in stmts)) { order[++n] = key }
		stmts[key] = $2 + 0
		count[key] += $3 + 0
	}
	END {
		prefix = module "/"
		plen = length(prefix)
		zero = 0; zerostmts = 0; total = 0; totalstmts = 0
		for (i = 1; i <= n; i++) {
			key = order[i]
			total++
			totalstmts += stmts[key]
			if (count[key] != 0) continue
			zero++
			zerostmts += stmts[key]

			pos = match(key, /:[0-9]+\.[0-9]+,[0-9]+\.[0-9]+$/)
			if (pos == 0) continue
			file = substr(key, 1, pos - 1)
			span = substr(key, pos + 1)
			if (module != "" && substr(file, 1, plen) == prefix) {
				file = substr(file, plen + 1)
			}
			split(span, ends, ",")
			split(ends[1], a, ".")
			split(ends[2], b, ".")
			print file, a[1] + 0, b[1] + 0, stmts[key]
		}
		print "TOTALS", zero, zerostmts, total, totalstmts > "/dev/stderr"
	}
' "$profile" 2> "$tmp/totals" | sort -t"$tab" -k1,1 -k2,2n > "$tmp/zero"

read -r _ zero_blocks zero_stmts total_blocks total_stmts < "$tmp/totals"

# A profile that describes no blocks at all is not a clean bill of health, it
# is a measurement that did not happen — a truncated file, a `go test` that
# built nothing, a profile from a different command. Same rule as above.
if [ "${total_blocks:-0}" -eq 0 ]; then
	echo "coverage-blocks: ERROR — '$profile' contains no coverage blocks."
	echo "  Either the profile is truncated or the test run measured nothing."
	exit 1
fi

headline="coverage-blocks: $zero_blocks of $total_blocks blocks never executed ($zero_stmts of $total_stmts statements)"

# ── Report modes ──────────────────────────────────────────────────────────────

if [ -z "$base" ]; then
	if [ "$summary_only" -eq 1 ]; then
		echo "$headline"
		exit 0
	fi

	echo "$headline"
	if [ "$zero_blocks" -gt 0 ]; then
		echo
		awk -F"$tab" '
			$1 != last { printf "  %s\n", $1; last = $1 }
			{ printf "    %s:%s-%s  %s stmt(s)\n", $1, $2, $3, $4 }
		' "$tmp/zero"
		echo
		echo "  by file:"
		awk -F"$tab" '{ blocks[$1]++; stmts[$1] += $4 }
			END { for (f in blocks) printf "  %5d blocks %5d stmts  %s\n", blocks[f], stmts[f], f }' \
			"$tmp/zero" | sort -k1,1rn -k3,3rn
	fi
	echo
	echo "  This is a report, not a gate. The gate runs against a base ref:"
	echo "    COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks"
	exit 0
fi

# ── Gate mode ─────────────────────────────────────────────────────────────────

if ! git rev-parse --verify --quiet "$base" > /dev/null 2>&1; then
	echo "coverage-blocks: ERROR — base ref '$base' does not exist."
	echo "  Without it there is no diff to scope the gate to, so it cannot run."
	echo "  In CI this usually means the checkout lost 'fetch-depth: 0'."
	exit 1
fi

merge_base=$(git merge-base "$base" HEAD)

# Implementation files only. A branch that changes a test file has not created
# an obligation for some block elsewhere to be covered, and test files never
# appear in a coverage profile in the first place.
changed=""
for f in $(git diff --name-only "$merge_base" HEAD); do
	case "$f" in
	*_test.go) ;;
	*.go) changed="$changed $f" ;;
	esac
done

if [ -z "$changed" ]; then
	echo "coverage-blocks: no Go implementation changed against $base — nothing to check."
	exit 0
fi

# touched_lines <file> — the line numbers this branch added or modified, against
# the file as it stands now. -U0 keeps the hunk headers exact so no untouched
# neighbour is swept in. Same helper, same reasoning, as scripts/efficacy.sh.
touched_lines() {
	git diff -U0 "$merge_base" HEAD -- "$1" |
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
bare_marker=0

for f in $changed; do
	awk -F"$tab" -v want="$f" '$1 == want' "$tmp/zero" > "$tmp/blocks" || true
	[ -s "$tmp/blocks" ] || continue

	touched_lines "$f" > "$tmp/touched"
	[ -s "$tmp/touched" ] || continue

	# A block is in scope when ANY line this branch touched falls inside it —
	# not only when its opening line did. Editing the body of an error path
	# that no test enters is the same finding as adding one.
	awk -F"$tab" '
		NR == FNR { touched[$1 + 0] = 1; next }
		{
			for (l = $2 + 0; l <= $3 + 0; l++) {
				if (l in touched) { print; next }
			}
		}
	' "$tmp/touched" "$tmp/blocks" >> "$tmp/findings"
done

if [ ! -s "$tmp/findings" ]; then
	echo "coverage-blocks: nothing to report — every block on a line this branch changed was executed."
	exit 0
fi

# The narrow escape hatch. A reason is required: the Makefile's own coverage
# exclusion list carries the same rule ("do not add an exclusion without a
# reason recorded there"), and an exemption nobody has to justify is how an
# allowlist turns into a place gaps go to be forgotten.
: > "$tmp/kept"
while IFS="$tab" read -r file start end stmts; do
	src="$repo_root/$file"
	marker=""
	if [ -f "$src" ]; then
		above=$((start - 1))
		[ "$above" -lt 1 ] && above=$start
		marker=$(sed -n "${above},${start}p" "$src" 2> /dev/null)
	fi
	case "$marker" in
	*//coverage:exempt*)
		if printf '%s\n' "$marker" | grep -Eq '//coverage:exempt[[:space:]]+[^[:space:]]'; then
			printf '%s%s%s%s%s%s%s\n' "$file" "$tab" "$start" "$tab" "$end" "$tab" "$stmts" >> "$tmp/exempt"
			continue
		fi
		bare_marker=1
		;;
	esac
	printf '%s%s%s%s%s%s%s\n' "$file" "$tab" "$start" "$tab" "$end" "$tab" "$stmts" >> "$tmp/kept"
done < "$tmp/findings"

exempt_count=$(wc -l < "$tmp/exempt" | tr -d ' ')
kept_count=$(wc -l < "$tmp/kept" | tr -d ' ')

exempt_note=""
[ "$exempt_count" -gt 0 ] && exempt_note=" ($exempt_count exempt)"

# The branch-wide hatch, applied last so its output still says what it waved
# through. Matches EFFICACY_EXEMPT / the efficacy-exempt label: visible on the
# pull request, not buried in a tracked config file.
if [ "${COVERAGE_BLOCKS_EXEMPT:-0}" = "1" ]; then
	echo "coverage-blocks: exempt — COVERAGE_BLOCKS_EXEMPT=1 waived $kept_count finding(s)$exempt_note."
	echo "  Say why in the pull request description."
	exit 0
fi

if [ "$kept_count" -eq 0 ]; then
	echo "coverage-blocks: nothing to report — every block on a line this branch changed was executed$exempt_note."
	exit 0
fi

echo "coverage-blocks: $kept_count block(s) this branch changed never executed$exempt_note."
echo
awk -F"$tab" '{ printf "  %s:%s-%s  %s stmt(s)\n", $1, $2, $3, $4 }' "$tmp/kept"
echo
echo "  Each of these is a block the test suite never entered, on a line this"
echo "  branch changed. Cover it, or mark it with a reason:"
echo
echo "    //coverage:exempt <why this block cannot be covered>"
echo
echo "  on the block's opening line or the line directly above it."
if [ "$bare_marker" -eq 1 ]; then
	echo
	echo "  NOTE: a //coverage:exempt marker with no reason after it was found and"
	echo "  ignored. The marker needs a reason to count."
fi
exit 1
