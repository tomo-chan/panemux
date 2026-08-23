#!/bin/sh
#
# red-check: gate G4(b) in docs/quality-gateway.md.
#
# The rule this enforces is decision D4. DEVELOPMENT.md requires tests to be
# written first and confirmed failing, and that rule is stuck at rung L0 of the
# enforcement ladder because it cannot be checked after the fact — nothing in a
# merged branch records the order its lines were written in. The *result* can be
# checked, though, and it is the part that actually matters:
#
#   A test this branch changed must FAIL when this branch's implementation
#   diff is reverted.
#
# That is the strongest possible mutation — remove the implementation entirely —
# and it catches both shapes this repository's existing gates are blind to: a
# tautological test (one that would pass with the implementation deleted) and a
# test written after the fact to describe code that already worked.
#
# Deliberately NOT part of `make check`: it needs the base branch, a scratch
# worktree, and a second test run, which is a pull-request-shaped cost rather
# than an every-turn one. Same reasoning that keeps mutation testing (D2) and
# the agmsg contract out of the local gate.
#
# Usage:
#   make efficacy                       # against origin/main
#   EFFICACY_BASE=origin/main make efficacy
#   scripts/efficacy.sh --changed-tests # print what it would check, and stop
#
# Exit codes: 0 = the gate passed or had nothing to check, 1 = a changed test
# survived its implementation being reverted.

set -u

base=${EFFICACY_BASE:-origin/main}
repo_root=$(git rev-parse --show-toplevel)
mode=${1:-run}

if ! git rev-parse --verify --quiet "$base" > /dev/null; then
	echo "efficacy: base ref '$base' not found; set EFFICACY_BASE to a ref that exists"
	exit 0
fi

merge_base=$(git merge-base "$base" HEAD)
changed=$(git diff --name-only "$merge_base" HEAD)

go_tests=""
go_impl=""
fe_tests=""
fe_impl=""

for f in $changed; do
	case "$f" in
	*_test.go) go_tests="$go_tests $f" ;;
	*.go) go_impl="$go_impl $f" ;;
	frontend/*.test.ts | frontend/*.test.tsx) fe_tests="$fe_tests $f" ;;
	frontend/src/*.ts | frontend/src/*.tsx) fe_impl="$fe_impl $f" ;;
	esac
done

# changed_test_funcs <file> — the Go test functions this branch added or
# modified in <file>. A function counts as changed when any line the diff
# touched falls inside it, so editing an assertion counts, not only adding a
# whole function. Reverting the implementation under a test that was merely
# renamed proves nothing, which is why the mapping is by line and not by
# "+func Test".
changed_test_funcs() {
	file=$1

	# The lines this branch touched, one number per line. -U0 keeps the hunk
	# headers exact so no untouched neighbour is swept in.
	git diff -U0 "$merge_base" HEAD -- "$file" |
		awk '
			/^@@/ {
				# @@ -a,b +c,d @@ — take c and d.
				plus = $3
				sub(/^\+/, "", plus)
				n = split(plus, parts, ",")
				start = parts[1] + 0
				count = (n > 1) ? parts[2] + 0 : 1
				for (i = 0; i < count; i++) print start + i
			}
		' > "$tmp/touched"

	[ -s "$tmp/touched" ] || return 0

	# Every top-level test function in the file as it stands now, with the
	# line range it spans. Top-level `func` at column 0 is the boundary; Go
	# gofmt guarantees that shape, and gofmt is already gate G1.
	#
	# The blank line and the doc comment BETWEEN two functions belong to the
	# one below, not the one above. Getting that wrong is not cosmetic: an
	# append at the end of a file touches the blank line before the new
	# function, which would otherwise pull the previous, unchanged test into
	# scope — and since the gate is satisfied when the whole -run set goes
	# red, an unrelated test failing would then mask a tautological one.
	awk '
		{ line[NR] = $0 }
		END {
			n = NR
			cnt = 0
			for (i = 1; i <= n; i++) {
				if (line[i] !~ /^func /) continue
				cnt++
				fstart[cnt] = i
				fname[cnt] = ""
				if (line[i] ~ /^func (Test|Benchmark|Fuzz|Example)[A-Za-z0-9_]*\(/) {
					rest = substr(line[i], 6)
					p = index(rest, "(")
					fname[cnt] = substr(rest, 1, p - 1)
				}
			}
			for (k = 1; k <= cnt; k++) {
				if (fname[k] == "") continue
				s = fstart[k]
				j = s - 1
				while (j >= 1 && line[j] ~ /^\/\//) { s = j; j-- }
				e = (k < cnt) ? fstart[k + 1] - 1 : n
				while (e > s && (line[e] ~ /^[ \t]*$/ || line[e] ~ /^\/\//)) e--
				print fname[k], s, e
			}
		}
	' "$file" > "$tmp/funcs"

	awk 'NR == FNR { touched[$1] = 1; next }
		{
			for (i = $2; i <= $3; i++) if (touched[i]) { print $1; break }
		}' "$tmp/touched" "$tmp/funcs"
}

tmp=$(mktemp -d)
worktree=""
cleanup() {
	[ -n "$worktree" ] && git -C "$repo_root" worktree remove --force "$worktree" > /dev/null 2>&1
	rm -rf "$tmp"
}
trap cleanup EXIT

names=""
for f in $go_tests; do
	[ -f "$f" ] || continue
	for n in $(changed_test_funcs "$f"); do
		case " $names " in
		*" $n "*) ;;
		*) names="$names $n" ;;
		esac
	done
done

pkgs=""
for f in $go_tests; do
	[ -f "$f" ] || continue
	d=$(dirname "$f")
	case " $pkgs " in
	*" ./$d "*) ;;
	*) pkgs="$pkgs ./$d" ;;
	esac
done

if [ "$mode" = "--changed-tests" ]; then
	echo "base:            $base ($merge_base)"
	echo "go impl:        $go_impl"
	echo "go tests:       $go_tests"
	echo "go test funcs:  $names"
	echo "go packages:    $pkgs"
	echo "frontend impl:  $fe_impl"
	echo "frontend tests: $fe_tests"
	exit 0
fi

if [ "${EFFICACY_EXEMPT:-}" = "1" ]; then
	echo "efficacy: skipped — EFFICACY_EXEMPT=1."
	echo "  Use this only for a change whose tests are not expected to go red without it:"
	echo "  a pure refactor, or a test-only rename. Say which in the pull request."
	exit 0
fi

if [ -z "$go_impl" ] && [ -z "$fe_impl" ]; then
	echo "efficacy: no implementation changed against $base — nothing to revert. Skipping."
	exit 0
fi

if [ -z "$names" ] && [ -z "$fe_tests" ]; then
	echo "efficacy: WARNING — this branch changes implementation but no test."
	echo "  There is nothing to red-check. DEVELOPMENT.md requires tests first;"
	echo "  this gate can only check the result, and here there is no result to check."
	exit 0
fi

# A scratch worktree at HEAD, with the implementation diff reverted. The real
# checkout is never touched: this runs on a pull request, where a half-reverted
# working tree would be a nasty thing to leave behind on a failure.
worktree=$(mktemp -d)
rm -rf "$worktree"
git worktree add --detach "$worktree" HEAD > /dev/null 2>&1 || {
	echo "efficacy: could not create a scratch worktree; skipping."
	worktree=""
	exit 0
}

revert() {
	for f in $1; do
		if git -C "$worktree" cat-file -e "$merge_base:$f" 2> /dev/null; then
			git -C "$worktree" checkout "$merge_base" -- "$f"
		else
			# Added by this branch: reverting means it never existed.
			rm -f "$worktree/$f"
		fi
	done
}

revert "$go_impl"
revert "$fe_impl"

status=0

if [ -n "$names" ]; then
	pattern=$(printf '%s' "$names" | sed 's/^ //; s/ /|/g')
	echo "efficacy: running the changed Go tests with the implementation reverted"
	echo "  packages: $pkgs"
	echo "  tests:    $pattern"

	# shellcheck disable=SC2086 # word splitting is the intent: one arg per package
	if (cd "$worktree" && go test $pkgs -run "^($pattern)$" -count=1 > "$tmp/go.log" 2>&1); then
		echo
		echo "FAIL: these tests still passed with this branch's implementation reverted."
		echo "      A test that passes without the code it covers protects nothing."
		echo "      Either it asserts something the old implementation already did,"
		echo "      or it asserts nothing at all. See decision D4 in docs/quality-gateway.md."
		echo
		sed 's/^/      /' "$tmp/go.log"
		status=1
	else
		echo "  ok — they went red, as a test written before its implementation must."
	fi
fi

if [ -n "$fe_tests" ] && [ -d "$repo_root/frontend/node_modules" ]; then
	ln -s "$repo_root/frontend/node_modules" "$worktree/frontend/node_modules" 2> /dev/null
	echo "efficacy: running the changed frontend tests with the implementation reverted"
	rel=""
	for f in $fe_tests; do rel="$rel ${f#frontend/}"; done
	echo "  tests: $rel"

	# shellcheck disable=SC2086 # word splitting is the intent: one arg per file
	if (cd "$worktree/frontend" && npx --no-install vitest run $rel > "$tmp/fe.log" 2>&1); then
		echo
		echo "FAIL: these frontend tests still passed with the implementation reverted."
		echo
		sed 's/^/      /' "$tmp/fe.log"
		status=1
	else
		echo "  ok — they went red."
	fi
fi

exit $status
